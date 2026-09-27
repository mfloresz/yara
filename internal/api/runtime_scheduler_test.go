package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"translator-server/internal/ai"
	"translator-server/internal/store"
)

// forceJobQueueSaturation makes enqueueJob reject every admission (the old
// unbuffered-channel trick, expressed through the scheduler's own knob).
func forceJobQueueSaturation(t *testing.T) {
	t.Helper()
	oldCap := jobQueueCapacity
	jobQueueCapacity = 0
	t.Cleanup(func() { jobQueueCapacity = oldCap })
}

func TestJobClassClassification(t *testing.T) {
	for _, tc := range []struct {
		op    string
		class jobClass
		ok    bool
	}{
		{"translate", aiClass, true},
		{"refine", aiClass, true},
		{"generate-glossary", aiClass, true},
		{"download", webClass, true},
		{"check", webClass, true},
		{"bogus", aiClass, false},
		{"", aiClass, false},
	} {
		class, ok := classifyJobOperation(tc.op)
		if ok != tc.ok || (ok && class != tc.class) {
			t.Errorf("classifyJobOperation(%q) = %v, %v; want %v, %v", tc.op, class, ok, tc.class, tc.ok)
		}
	}
}

func TestURLOrigin(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want string
		err  bool
	}{
		{"https://novelfire.net/book/x", "https://novelfire.net", false},
		{"http://Example.COM/a?b=1#frag", "http://example.com", false},
		{"http://example.com:80/a", "http://example.com", false},
		{"https://example.com:443/a", "https://example.com", false},
		{"http://example.com:8080/a", "http://example.com:8080", false},
		{"https://example.com:8443/a", "https://example.com:8443", false},
		{"ftp://example.com/a", "", true},
		{"/relative/path", "", true},
		{"", "", true},
	} {
		got, err := urlOrigin(tc.raw)
		if tc.err {
			if err == nil {
				t.Errorf("urlOrigin(%q) = %q, want error", tc.raw, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("urlOrigin(%q): %v", tc.raw, err)
			continue
		}
		if got != tc.want {
			t.Errorf("urlOrigin(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestWebJobOrigins(t *testing.T) {
	downloadOpts, _ := json.Marshal(map[string]any{
		"url": "https://novelfire.net/book/x",
		"chapters": []store.DownloadChapterInfo{
			{URL: "https://NOVELFIRE.net/chapter-1"},
			{URL: "https://novelfire.net:443/chapter-2"},
			{URL: "https://mirror.example.net/chapter-2"},
		},
	})
	job := &store.Job{Operation: "download", OptionsJSON: string(downloadOpts)}
	origins, err := webJobOrigins(job)
	if err != nil {
		t.Fatalf("webJobOrigins: %v", err)
	}
	if len(origins) != 2 || origins[0] != "https://novelfire.net" || origins[1] != "https://mirror.example.net" {
		t.Errorf("origins = %v, want deduped [novelfire, mirror]", origins)
	}

	checkJob := &store.Job{Operation: "check", OptionsJSON: `{"url":"http://example.com:8080/x"}`}
	origins, err = webJobOrigins(checkJob)
	if err != nil {
		t.Fatalf("webJobOrigins(check): %v", err)
	}
	if len(origins) != 1 || origins[0] != "http://example.com:8080" {
		t.Errorf("origins = %v, want [http://example.com:8080]", origins)
	}

	invalidJob := &store.Job{Operation: "download", OptionsJSON: `{"url":"notaurl"}`}
	if _, err := webJobOrigins(invalidJob); err == nil {
		t.Error("expected error for invalid URL snapshot")
	}
	emptyJob := &store.Job{Operation: "download", OptionsJSON: `{}`}
	if _, err := webJobOrigins(emptyJob); err == nil {
		t.Error("expected error for snapshot without URLs")
	}
}

// gatedProvider blocks every Translate call until release is closed, so tests
// can observe scheduler state deterministically while a job is in flight.
type gatedProvider struct {
	started   chan struct{}
	release   chan struct{}
	closeOnce sync.Once
	calls     atomic.Int32
}

func newGatedProvider() *gatedProvider {
	return &gatedProvider{started: make(chan struct{}, 64), release: make(chan struct{})}
}

func (p *gatedProvider) releaseGate() {
	p.closeOnce.Do(func() { close(p.release) })
}

func (p *gatedProvider) waitStarted(t *testing.T, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-p.started:
		case <-time.After(10 * time.Second):
			t.Fatalf("timed out waiting for provider call %d (calls=%d, started buffered=%d)", i+1, p.calls.Load(), len(p.started))
		}
	}
}

func (p *gatedProvider) run(ctx context.Context) error {
	p.calls.Add(1)
	select {
	case p.started <- struct{}{}:
	default:
	}
	select {
	case <-p.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *gatedProvider) TranslateTitle(ctx context.Context, in ai.TranslateTitleInput) (string, error) {
	return "T", p.run(ctx)
}

func (p *gatedProvider) TranslateText(ctx context.Context, in ai.TranslateTextInput) (string, error) {
	return "TRADUCIDO: " + in.TextToTranslate, p.run(ctx)
}

func (p *gatedProvider) Refine(ctx context.Context, in ai.RefineInput) (ai.RefineOutput, error) {
	return ai.RefineOutput{}, nil
}

func (p *gatedProvider) Check(ctx context.Context, in ai.CheckInput) (ai.CheckOutput, error) {
	return ai.CheckOutput{OK: true}, nil
}

func (p *gatedProvider) GenerateGlossary(ctx context.Context, in ai.GenerateGlossaryInput) (ai.GenerateGlossaryOutput, error) {
	return ai.GenerateGlossaryOutput{}, nil
}

// newSchedulerEnv boots an env with one user, venice + opencode-go provider
// settings, the gated provider injected, and count novels each holding one
// pending chapter. Every gate is force-released on cleanup so StopJobWorker
// in the env cleanup can never hang.
func newSchedulerEnv(t *testing.T, novelCount int) (*apiTestEnv, authPayload, *gatedProvider, []string) {
	t.Helper()
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "alice-scheduler@example.com", "secret123", "Alice")

	if _, err := env.store.UpsertProviderSettingsWithConcurrency(alice.User.ID, "venice", "deepseek-v4-flash", "https://api.venice.ai/api/v1", 120000, 1); err != nil {
		t.Fatalf("upsert venice provider: %v", err)
	}
	if _, err := env.store.ReplaceProviderAPIKey(alice.User.ID, "venice", "key-venice"); err != nil {
		t.Fatalf("set venice key: %v", err)
	}
	if _, err := env.store.UpsertProviderSettingsWithConcurrency(alice.User.ID, "opencode-go", "gpt-x", "https://api.example.ai/v1", 120000, 1); err != nil {
		t.Fatalf("upsert opencode-go provider: %v", err)
	}
	if _, err := env.store.ReplaceProviderAPIKey(alice.User.ID, "opencode-go", "key-opencode"); err != nil {
		t.Fatalf("set opencode-go key: %v", err)
	}
	if _, err := env.store.SaveAppSettings(alice.User.ID, store.AppSettings{
		AI:          store.AISettings{Provider: "venice", Model: "deepseek-v4-flash", BaseURL: "https://api.venice.ai/api/v1", TimeoutMs: 120000, Concurrency: 1},
		Translation: store.DefaultTranslationDefaults,
	}); err != nil {
		t.Fatalf("save app settings: %v", err)
	}

	gate := newGatedProvider()
	env.server.NewAIProvider = func(store.AISettings, string) (ai.Provider, error) {
		return gate, nil
	}
	t.Cleanup(gate.releaseGate)

	novelIDs := make([]string, 0, novelCount)
	for i := 0; i < novelCount; i++ {
		novel := createNovel(t, env.handler, alice.Token, "Novela Scheduler "+string(rune('A'+i)), "en", "es")
		if _, err := env.store.UpsertChapter(alice.User.ID, novel.ID, &store.Chapter{
			ChapterOrder:    1,
			Title:           "Chapter 1",
			OriginalContent: "Original content long enough to translate.",
			Status:          "pending",
		}); err != nil {
			t.Fatalf("upsert chapter: %v", err)
		}
		novelIDs = append(novelIDs, novel.ID)
	}
	return env, alice, gate, novelIDs
}

func createTranslateJob(t *testing.T, env *apiTestEnv, alice authPayload, novelID, provider string) string {
	t.Helper()
	job := &store.Job{NovelID: novelID, Status: "pending", Operation: "translate", Provider: provider}
	if err := env.store.CreateJob(alice.User.ID, job); err != nil {
		t.Fatalf("create job: %v", err)
	}
	return job.ID
}

// schedulerState snapshots the dispatcher counters under the scheduler lock.
func schedulerState(s *Server) (runningAI, runningWeb, pendingAI, pendingWeb int) {
	s.jobMu.Lock()
	defer s.jobMu.Unlock()
	return s.runningAI, s.runningWeb, len(s.pendingAI), len(s.pendingWeb)
}

func waitForJobStatus(t *testing.T, env *apiTestEnv, jobID, want string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		job, err := env.store.GetJob(jobID)
		if err != nil {
			t.Fatalf("get job: %v", err)
		}
		if job.Status == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("job %s did not reach %q in time", jobID, want)
}

func TestSchedulerSameProviderJobsNeverOverlap(t *testing.T) {
	env, alice, gate, novels := newSchedulerEnv(t, 2)
	j1 := createTranslateJob(t, env, alice, novels[0], "")
	j2 := createTranslateJob(t, env, alice, novels[1], "")

	if !env.server.enqueueJob(j1) {
		t.Fatal("enqueue j1 failed")
	}
	gate.waitStarted(t, 1) // j1 holds ai:venice while blocked in the mock

	if !env.server.enqueueJob(j2) {
		t.Fatal("enqueue j2 failed")
	}
	runningAI, _, pendingAI, _ := schedulerState(env.server)
	if runningAI != 1 || pendingAI != 1 {
		t.Fatalf("j2 must wait: runningAI=%d pendingAI=%d, want 1/1", runningAI, pendingAI)
	}
	if got := gate.calls.Load(); got != 1 {
		t.Fatalf("j2 started despite busy provider key: calls=%d", got)
	}

	gate.releaseGate()
	waitForJobStatus(t, env, j1, "done")
	waitForJobStatus(t, env, j2, "done")
}

func TestSchedulerDistinctProvidersRunInParallel(t *testing.T) {
	env, alice, gate, novels := newSchedulerEnv(t, 2)
	j1 := createTranslateJob(t, env, alice, novels[0], "venice")
	j2 := createTranslateJob(t, env, alice, novels[1], "opencode-go")

	if !env.server.enqueueJob(j1) || !env.server.enqueueJob(j2) {
		t.Fatal("enqueue failed")
	}
	gate.waitStarted(t, 2)
	runningAI, _, _, _ := schedulerState(env.server)
	if runningAI != 2 {
		t.Fatalf("expected both jobs running, runningAI=%d", runningAI)
	}
	gate.releaseGate()
	waitForJobStatus(t, env, j1, "done")
	waitForJobStatus(t, env, j2, "done")
}

func TestSchedulerSameNovelSerializesAcrossProviders(t *testing.T) {
	env, alice, gate, novels := newSchedulerEnv(t, 1)
	j1 := createTranslateJob(t, env, alice, novels[0], "venice")
	j2 := createTranslateJob(t, env, alice, novels[0], "opencode-go")

	if !env.server.enqueueJob(j1) {
		t.Fatal("enqueue j1 failed")
	}
	gate.waitStarted(t, 1)
	if !env.server.enqueueJob(j2) {
		t.Fatal("enqueue j2 failed")
	}
	runningAI, _, pendingAI, _ := schedulerState(env.server)
	if runningAI != 1 || pendingAI != 1 {
		t.Fatalf("second job on the same novel must wait: runningAI=%d pendingAI=%d", runningAI, pendingAI)
	}
	gate.releaseGate()
	waitForJobStatus(t, env, j1, "done")
	waitForJobStatus(t, env, j2, "done")
}

func TestSchedulerBlockedJobDoesNotBlockIndependentJob(t *testing.T) {
	env, alice, gate, novels := newSchedulerEnv(t, 3)
	j1 := createTranslateJob(t, env, alice, novels[0], "venice")
	j2 := createTranslateJob(t, env, alice, novels[1], "venice") // blocked by j1
	j3 := createTranslateJob(t, env, alice, novels[2], "opencode-go")

	if !env.server.enqueueJob(j1) {
		t.Fatal("enqueue j1 failed")
	}
	gate.waitStarted(t, 1)
	if !env.server.enqueueJob(j2) || !env.server.enqueueJob(j3) {
		t.Fatal("enqueue failed")
	}
	// One MORE call: j3 must start although j2 is blocked (j1 already holds
	// its call, j2 never starts).
	gate.waitStarted(t, 1)
	runningAI, _, pendingAI, _ := schedulerState(env.server)
	if runningAI != 2 || pendingAI != 1 {
		t.Fatalf("want j1+j3 running and j2 pending: runningAI=%d pendingAI=%d", runningAI, pendingAI)
	}
	gate.releaseGate()
	waitForJobStatus(t, env, j1, "done")
	waitForJobStatus(t, env, j2, "done")
	waitForJobStatus(t, env, j3, "done")
}

func TestSchedulerCapacityLimit(t *testing.T) {
	oldLimit := maxConcurrentAIJobs
	maxConcurrentAIJobs = 2
	t.Cleanup(func() { maxConcurrentAIJobs = oldLimit })

	env, alice, gate, novels := newSchedulerEnv(t, 3)
	// The third job needs a third resolvable provider.
	if _, err := env.store.UpsertProviderSettingsWithConcurrency(alice.User.ID, "google", "gemini-flash", "https://api.google.ai/v1", 120000, 1); err != nil {
		t.Fatalf("upsert google provider: %v", err)
	}
	jobs := []string{
		createTranslateJob(t, env, alice, novels[0], "venice"),
		createTranslateJob(t, env, alice, novels[1], "opencode-go"),
		createTranslateJob(t, env, alice, novels[2], "google"),
	}
	for _, id := range jobs {
		if !env.server.enqueueJob(id) {
			t.Fatalf("enqueue %s failed", id)
		}
	}
	runningAI, _, pendingAI, _ := schedulerState(env.server)
	if runningAI != 2 || pendingAI != 1 {
		t.Fatalf("capacity 2 must hold the third job: runningAI=%d pendingAI=%d", runningAI, pendingAI)
	}
	gate.releaseGate()
	for _, id := range jobs {
		waitForJobStatus(t, env, id, "done")
	}
}

func TestSchedulerCancelPendingJobNeverExecutes(t *testing.T) {
	env, alice, gate, novels := newSchedulerEnv(t, 2)
	j1 := createTranslateJob(t, env, alice, novels[0], "")
	j2 := createTranslateJob(t, env, alice, novels[1], "")

	if !env.server.enqueueJob(j1) {
		t.Fatal("enqueue j1 failed")
	}
	gate.waitStarted(t, 1)
	if !env.server.enqueueJob(j2) {
		t.Fatal("enqueue j2 failed")
	}

	// Cancel j2 while it waits: it must leave the queue without executing.
	if err := env.store.UpdateJobForUser(alice.User.ID, j2, map[string]any{"status": "cancelled"}); err != nil {
		t.Fatalf("cancel j2: %v", err)
	}
	env.server.onJobCancelled(j2)
	_, _, pendingAI, _ := schedulerState(env.server)
	if pendingAI != 0 {
		t.Fatalf("cancelled pending job must leave the queue, pendingAI=%d", pendingAI)
	}

	gate.releaseGate()
	waitForJobStatus(t, env, j1, "done")
	final, err := env.store.GetJob(j2)
	if err != nil {
		t.Fatalf("get j2: %v", err)
	}
	if final.Status != "cancelled" {
		t.Fatalf("j2 status = %q, want cancelled", final.Status)
	}
	// j2 never executed: its chapter has no translation. The provider call
	// count alone cannot discriminate (j1 makes one call per chapter piece).
	chapters, err := env.store.ListChaptersAccessible(alice.User.ID, novels[1])
	if err != nil {
		t.Fatalf("list j2 chapters: %v", err)
	}
	if len(chapters) != 1 || chapters[0].TranslatedContent != "" {
		t.Fatalf("cancelled job produced a translation: %+v", chapters)
	}
}

func TestSchedulerStopJobWorkerDrainsPending(t *testing.T) {
	env, alice, gate, novels := newSchedulerEnv(t, 2)
	j1 := createTranslateJob(t, env, alice, novels[0], "")
	j2 := createTranslateJob(t, env, alice, novels[1], "")
	if !env.server.enqueueJob(j1) || !env.server.enqueueJob(j2) {
		t.Fatal("enqueue failed")
	}
	gate.waitStarted(t, 1)

	stopped := make(chan struct{})
	go func() {
		env.server.StopJobWorker()
		close(stopped)
	}()

	// Drain semantics: j2 (same provider, still pending) must run after j1
	// finishes even though StopJobWorker already stopped admission.
	gate.releaseGate()
	waitForJobStatus(t, env, j1, "done")
	waitForJobStatus(t, env, j2, "done")
	select {
	case <-stopped:
	case <-time.After(15 * time.Second):
		t.Fatal("StopJobWorker did not return after draining")
	}
}

func TestSchedulerQueueFullRejectsJob(t *testing.T) {
	env, alice, _, novels := newSchedulerEnv(t, 1)
	forceJobQueueSaturation(t)
	job := createTranslateJob(t, env, alice, novels[0], "")
	if env.server.enqueueJob(job) {
		t.Fatal("expected enqueue to fail when the queue is saturated")
	}
	stored, err := env.store.GetJob(job)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if stored.Status != "failed" || stored.ErrorMessage != jobQueueFullMessage {
		t.Fatalf("rejected job: status=%q error=%q", stored.Status, stored.ErrorMessage)
	}
}

func TestSchedulerUnknownOperationIsNotQueuedAsTranslate(t *testing.T) {
	// The store rejects unknown operations at creation time; the classifier
	// is the second line of defense for jobs persisted before that guard.
	if _, ok := classifyJobOperation("bogus"); ok {
		t.Fatal("unknown operation must not classify as AI work")
	}
}

func TestSchedulerDownloadWithInvalidURLOptionsFailsAtDispatch(t *testing.T) {
	env, alice, _, novels := newSchedulerEnv(t, 1)
	job := &store.Job{
		NovelID:     novels[0],
		Status:      "pending",
		Operation:   "download",
		OptionsJSON: `{"url":"notaurl"}`,
	}
	if err := env.store.CreateJob(alice.User.ID, job); err != nil {
		t.Fatalf("create job: %v", err)
	}
	// The invalid snapshot is admitted, then failed at dispatch with a
	// visible error instead of running without a key.
	if !env.server.enqueueJob(job.ID) {
		t.Fatal("enqueue should succeed; the failure happens at dispatch")
	}
	waitForJobStatus(t, env, job.ID, "failed")
}

func TestBuildJobRunPlanKeys(t *testing.T) {
	env, alice, _, novels := newSchedulerEnv(t, 1)

	// Content provider + distinct title provider → both AI keys reserved.
	if _, err := env.store.SaveAppSettings(alice.User.ID, store.AppSettings{
		AI:            store.AISettings{Provider: "venice", Model: "deepseek-v4-flash", BaseURL: "https://api.venice.ai/api/v1", TimeoutMs: 120000, Concurrency: 1},
		TitleProvider: "opencode-go",
		Translation:   store.DefaultTranslationDefaults,
	}); err != nil {
		t.Fatalf("save app settings: %v", err)
	}
	job := &store.Job{NovelID: novels[0], OwnerID: alice.User.ID, Status: "pending", Operation: "translate"}
	plan, err := env.server.buildJobRunPlan(job)
	if err != nil {
		t.Fatalf("buildJobRunPlan(translate): %v", err)
	}
	want := []string{"novel:" + novels[0], "ai:venice", "ai:opencode-go"}
	if len(plan.keys) != len(want) {
		t.Fatalf("keys = %v, want %v", plan.keys, want)
	}
	for i, k := range want {
		if plan.keys[i] != k {
			t.Fatalf("keys = %v, want %v", plan.keys, want)
		}
	}
	if plan.novel == nil || plan.cfg == nil {
		t.Fatal("translate plan must carry the pre-resolved novel and config")
	}

	// Same title provider as content provider → single AI key.
	if _, err := env.store.SaveAppSettings(alice.User.ID, store.AppSettings{
		AI:            store.AISettings{Provider: "venice", Model: "deepseek-v4-flash", BaseURL: "https://api.venice.ai/api/v1", TimeoutMs: 120000, Concurrency: 1},
		TitleProvider: "venice",
		Translation:   store.DefaultTranslationDefaults,
	}); err != nil {
		t.Fatalf("save app settings: %v", err)
	}
	plan, err = env.server.buildJobRunPlan(job)
	if err != nil {
		t.Fatalf("buildJobRunPlan(same provider): %v", err)
	}
	if len(plan.keys) != 2 {
		t.Fatalf("keys = %v, want novel + one AI key", plan.keys)
	}

	// Glossary reserves the provider it actually resolves.
	glossaryJob := &store.Job{NovelID: novels[0], OwnerID: alice.User.ID, Status: "pending", Operation: "generate-glossary"}
	plan, err = env.server.buildJobRunPlan(glossaryJob)
	if err != nil {
		t.Fatalf("buildJobRunPlan(glossary): %v", err)
	}
	if len(plan.keys) != 2 || plan.keys[1] != "ai:venice" {
		t.Fatalf("glossary keys = %v, want novel + ai:venice", plan.keys)
	}
	if plan.glossaryAI == nil {
		t.Fatal("glossary plan must carry the pre-resolved AI settings")
	}

	// Check reserves the novel + the site origin.
	checkJob := &store.Job{NovelID: novels[0], OwnerID: alice.User.ID, Status: "pending", Operation: "check", OptionsJSON: `{"url":"https://novelfire.net/book/x"}`}
	plan, err = env.server.buildJobRunPlan(checkJob)
	if err != nil {
		t.Fatalf("buildJobRunPlan(check): %v", err)
	}
	if len(plan.keys) != 2 || plan.keys[1] != "web:https://novelfire.net" {
		t.Fatalf("check keys = %v, want novel + web origin", plan.keys)
	}

	// Unknown operations are rejected, never planned as translation.
	bogus := &store.Job{NovelID: novels[0], Status: "pending", Operation: "bogus"}
	if _, err := env.server.buildJobRunPlan(bogus); err == nil {
		t.Fatal("unknown operation must fail planning")
	}
}

func TestCreateJobOnBusyNovelReturnsConflict(t *testing.T) {
	env, alice, gate, novels := newSchedulerEnv(t, 1)
	j1 := createTranslateJob(t, env, alice, novels[0], "")
	if !env.server.enqueueJob(j1) {
		t.Fatal("enqueue j1 failed")
	}
	gate.waitStarted(t, 1)

	chapters, err := env.store.ListChaptersAccessible(alice.User.ID, novels[0])
	if err != nil {
		t.Fatalf("list chapters: %v", err)
	}
	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/"+novels[0]+"/jobs", alice.Token, map[string]any{
		"chapterIds": []string{chapters[0].ID},
		"operation":  "translate",
		"options":    map[string]any{},
	})
	if resp.Code != http.StatusConflict {
		t.Fatalf("expected 409 for busy novel, got %d: %s", resp.Code, resp.Body.String())
	}

	gate.releaseGate()
	waitForJobStatus(t, env, j1, "done")
}

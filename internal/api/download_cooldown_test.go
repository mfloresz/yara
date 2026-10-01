package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"translator-server/internal/store"
)

// A download job holds its origin keys until finishJob runs, so a queued job
// for the same site cannot start until the running one returns. The trailing
// cooldown therefore only spaces two requests when such a job exists; with an
// empty queue it just delays the finished job.

func TestPendingOriginsOnlyForDownloadJobs(t *testing.T) {
	download := &store.Job{
		Operation:   "download",
		OptionsJSON: `{"url":"https://novelfire.net/book/x","chapters":[{"url":"https://novelfire.net/chapter-1"}]}`,
	}
	origins := pendingOrigins(download)
	if len(origins) != 1 || origins[0] != "https://novelfire.net" {
		t.Errorf("pendingOrigins(download) = %v, want [https://novelfire.net]", origins)
	}

	// A check job waits before its own fetch, so it must not be counted as
	// somebody needing a cooldown.
	if got := pendingOrigins(&store.Job{Operation: "check", OptionsJSON: `{"url":"https://novelfire.net/book/x"}`}); got != nil {
		t.Errorf("pendingOrigins(check) = %v, want nil", got)
	}

	// Derivation failures belong to buildJobRunPlan; here they just mean
	// "nothing to space", never a failed enqueue.
	if got := pendingOrigins(&store.Job{Operation: "download", OptionsJSON: `{"url":"notaurl"}`}); got != nil {
		t.Errorf("pendingOrigins(invalid) = %v, want nil", got)
	}
}

func TestHasPendingWebJobForOrigins(t *testing.T) {
	job := &store.Job{
		Operation:   "download",
		OptionsJSON: `{"url":"https://novelfire.net/book/x","chapters":[{"url":"https://novelfire.net/chapter-1"}]}`,
	}
	queued := pendingJob{
		id:      "job-2",
		origins: []string{"https://novelfire.net"},
	}
	otherSite := pendingJob{
		id:      "job-3",
		origins: []string{"https://mirror.example.net"},
	}

	for _, tc := range []struct {
		name    string
		pending []pendingJob
		want    bool
	}{
		{"empty queue", nil, false},
		{"same site queued", []pendingJob{queued}, true},
		{"only another site queued", []pendingJob{otherSite}, false},
		{"same site among others", []pendingJob{otherSite, queued}, true},
		{"check job queued", []pendingJob{{id: "job-4"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{pendingWeb: tc.pending}
			if got := s.hasPendingWebJobForOrigins(job); got != tc.want {
				t.Errorf("hasPendingWebJobForOrigins = %v, want %v", got, tc.want)
			}
		})
	}
}

// newCooldownTestEnv boots an env whose parser fetcher hits a local mock for
// novelfire.net, with a fixed inter-fetch delay so the cooldown window is
// predictable.
func newCooldownTestEnv(t *testing.T, delay time.Duration) *apiTestEnv {
	t.Helper()

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/chapter-") {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(testNovelfireChapterHTML))
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><body></body></html>`))
	}))
	t.Cleanup(mock.Close)

	env := newAPITestEnv(t)
	useRewritingClient(env, map[string]string{"novelfire.net": mock.URL})
	setParserThrottle(env, int(delay.Milliseconds()), int(delay.Milliseconds()))
	return env
}

// enqueueCooldownDownload creates a pending download job for one chapter and
// hands it to the scheduler.
func enqueueCooldownDownload(t *testing.T, env *apiTestEnv, ownerID, novelID string) string {
	t.Helper()

	options, err := json.Marshal(downloadJobOptions{
		URL: "https://novelfire.net/book/cooldown",
		Chapters: []store.DownloadChapterInfo{
			{URL: "https://novelfire.net/chapter-1", Title: "Chapter 1", Order: 1},
		},
		StartOrder: 1,
	})
	if err != nil {
		t.Fatalf("marshal download options: %v", err)
	}
	job := &store.Job{
		OwnerID:     ownerID,
		NovelID:     novelID,
		Status:      "pending",
		Operation:   "download",
		OptionsJSON: string(options),
	}
	if err := env.store.CreateJob(ownerID, job); err != nil {
		t.Fatalf("create download job: %v", err)
	}
	if !env.server.enqueueJob(job.ID) {
		t.Fatalf("enqueue download job %s", job.ID)
	}
	return job.ID
}

func jobStatus(t *testing.T, env *apiTestEnv, jobID string) string {
	t.Helper()
	job, err := env.store.GetJob(jobID)
	if err != nil {
		t.Fatalf("get job %s: %v", jobID, err)
	}
	return job.Status
}

// jobRunning reports whether the scheduler still holds the job: it keeps the
// origin keys and a worker slot until the worker goroutine returns, so a
// trailing wait is visible here even after the job status is already done.
func jobRunning(s *Server, jobID string) bool {
	s.jobMu.Lock()
	defer s.jobMu.Unlock()
	_, running := s.runningJobs[jobID]
	return running
}

// A single download must finish right after its last chapter: nothing else is
// queued for the site, so the cooldown would only delay the finished job.
func TestDownloadJobSkipsCooldownWithNoQueuedJob(t *testing.T) {
	env := newCooldownTestEnv(t, 30*time.Second)
	alice := registerUser(t, env, "alice-cooldown-solo@example.com", "secret123", "Alice")
	novel := createNovel(t, env.handler, alice.Token, "Solo", "en", "es")

	jobID := enqueueCooldownDownload(t, env, alice.User.ID, novel.ID)

	waitForCondition(t, 15*time.Second, "download job to reach a terminal status", func() bool {
		return jobStatus(t, env, jobID) == "done"
	})
	// The status is written before the cooldown, so only the scheduler slot
	// proves the worker actually returned instead of still waiting it out.
	waitForCondition(t, 10*time.Second, "download job to release its scheduler slot", func() bool {
		return !jobRunning(env.server, jobID)
	})
}

// With a second download queued for the same site, the cooldown must survive:
// it is the only thing spacing the two jobs' requests.
func TestDownloadJobKeepsCooldownWhenSameSiteJobQueued(t *testing.T) {
	const delay = 5 * time.Second

	env := newCooldownTestEnv(t, delay)
	alice := registerUser(t, env, "alice-cooldown-queue@example.com", "secret123", "Alice")
	first := createNovel(t, env.handler, alice.Token, "First", "en", "es")
	second := createNovel(t, env.handler, alice.Token, "Second", "en", "es")

	start := time.Now()
	job1 := enqueueCooldownDownload(t, env, alice.User.ID, first.ID)
	job2 := enqueueCooldownDownload(t, env, alice.User.ID, second.ID)

	waitForCondition(t, 30*time.Second, "both download jobs to finish", func() bool {
		return jobStatus(t, env, job1) == "done" && jobStatus(t, env, job2) == "done"
	})

	// Both chapters are fetched in milliseconds, so the wall time is
	// dominated by the cooldown the first job must keep.
	if elapsed := time.Since(start); elapsed < delay {
		t.Errorf("queued same-site job finished in %v, want at least the %v cooldown", elapsed, delay)
	}
}

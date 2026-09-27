package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"translator-server/internal/store"
)

// Global execution capacity per scheduler class. Together with the exclusive
// resource keys below they bound how much work runs at once even when many
// distinct providers/origins are in play. Per-provider and per-origin keys
// serialize within one resource; a job's own per-chapter concurrency
// (AISettings.Concurrency) is independent and still applies inside the job.
// Values are deliberately conservative — measure before raising them.
// ponytail: package vars (not consts) so tests can shrink them
// deterministically; production never reassigns them.
var (
	maxConcurrentAIJobs  = 4
	maxConcurrentWebJobs = 4
)

// jobQueueCapacity bounds how many jobs may wait per class (the cap of the
// old per-class channels). Saturation keeps the 503 + jobQueueFullMessage
// contract.
var jobQueueCapacity = 128

// errNovelBusy is returned by acquireNovelJobSlot when the novel already has
// a pending or running job: per policy only one job per novel may exist,
// regardless of class.
var errNovelBusy = errors.New("novel has an active job")

const novelBusyMessage = "Ya hay otro trabajo en curso para esta novela. Espera a que termine e inténtalo de nuevo."

type jobClass int

const (
	aiClass jobClass = iota
	webClass
)

func (c jobClass) String() string {
	if c == aiClass {
		return "ai"
	}
	return "web"
}

// classifyJobOperation maps an operation to its scheduler class. AI work
// (translate/refine/generate-glossary) and web work (download/check) have
// separate pending queues and capacities. Unknown operations are rejected
// instead of falling into translation implicitly.
func classifyJobOperation(op string) (jobClass, bool) {
	switch op {
	case "translate", "refine", "generate-glossary":
		return aiClass, true
	case "download", "check":
		return webClass, true
	}
	return aiClass, false
}

// pendingJob is a job waiting in a class queue for dispatch.
type pendingJob struct {
	id         string
	enqueuedAt time.Time
}

// jobRunPlan carries everything resolved once for one execution, so the keys
// reserved at dispatch are exactly the resources the processor uses — even if
// user or novel settings change while the job was waiting. Those changes
// affect the next job, not the one already resolved.
type jobRunPlan struct {
	jobID      string
	class      jobClass
	keys       []string
	novel      *store.Novel       // pre-loaded for AI classes
	cfg        *resolvedJobConfig // translate/refine
	glossaryAI *store.AISettings  // generate-glossary
	startedAt  time.Time
}

// urlOrigin returns the scheme://host[:port] origin of an HTTP(S) URL with
// default ports collapsed, so any two URLs on the same site share one
// scheduler key regardless of path, query or fragment.
func urlOrigin(rawURL string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("unsupported URL scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("URL has no host")
	}
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		port = ""
	}
	if port != "" {
		return u.Scheme + "://" + host + ":" + port, nil
	}
	return u.Scheme + "://" + host, nil
}

// webJobOrigins derives the distinct site origins a web job will touch, from
// the options snapshot persisted at creation time. An invalid snapshot fails
// the job instead of running it without a key: redirects/mirrors are not
// covered by a key computed at scheduling time, but every URL the job was
// planned with is.
func webJobOrigins(job *store.Job) ([]string, error) {
	var raw []string
	switch job.Operation {
	case "download":
		var opts downloadJobOptions
		if err := json.Unmarshal([]byte(job.OptionsJSON), &opts); err != nil {
			return nil, fmt.Errorf("parse download options: %w", err)
		}
		raw = append(raw, opts.URL)
		for _, ch := range opts.Chapters {
			raw = append(raw, ch.URL)
		}
	case "check":
		var opts checkJobOptions
		if err := json.Unmarshal([]byte(job.OptionsJSON), &opts); err != nil {
			return nil, fmt.Errorf("parse check options: %w", err)
		}
		raw = append(raw, opts.URL)
	}
	seen := make(map[string]bool, len(raw))
	origins := make([]string, 0, len(raw))
	for _, u := range raw {
		if strings.TrimSpace(u) == "" {
			continue
		}
		o, err := urlOrigin(u)
		if err != nil {
			return nil, fmt.Errorf("invalid job URL %q: %w", u, err)
		}
		if !seen[o] {
			seen[o] = true
			origins = append(origins, o)
		}
	}
	if len(origins) == 0 {
		return nil, fmt.Errorf("job has no valid source URL")
	}
	return origins, nil
}

// buildJobRunPlan resolves the class and the exclusive resource keys of a
// pending job. It accesses PocketBase (novel lookup, settings resolution) and
// therefore must never run under the scheduler mutex. Every plan reserves the
// novel key: at most one job per novel runs at a time.
func (s *Server) buildJobRunPlan(job *store.Job) (*jobRunPlan, error) {
	class, ok := classifyJobOperation(job.Operation)
	if !ok {
		return nil, fmt.Errorf("unknown job operation %q", job.Operation)
	}
	plan := &jobRunPlan{jobID: job.ID, class: class, keys: []string{"novel:" + job.NovelID}}
	switch class {
	case webClass:
		origins, err := webJobOrigins(job)
		if err != nil {
			return nil, err
		}
		for _, o := range origins {
			plan.keys = append(plan.keys, "web:"+o)
		}
	case aiClass:
		novel, err := s.Store.GetOwnedNovel(job.OwnerID, job.NovelID)
		if err != nil {
			return nil, fmt.Errorf("get novel: %w", err)
		}
		plan.novel = novel
		if job.Operation == "generate-glossary" {
			settings, err := s.resolveGlossaryAISettings(job, novel)
			if err != nil {
				return nil, err
			}
			plan.glossaryAI = &settings
			plan.keys = append(plan.keys, "ai:"+settings.Provider)
			return plan, nil
		}
		cfg, err := s.resolveJobConfig(novel, job)
		if err != nil {
			return nil, fmt.Errorf("resolve job config: %w", err)
		}
		plan.cfg = &cfg
		plan.keys = append(plan.keys, "ai:"+cfg.AI.Provider)
		// Reserve the title provider conservatively: title generation may fall
		// back to the content provider, but when it does run it would hit a
		// provider this job did not reserve.
		if cfg.TitleAI != nil && cfg.TitleAI.Provider != cfg.AI.Provider {
			plan.keys = append(plan.keys, "ai:"+cfg.TitleAI.Provider)
		}
	}
	return plan, nil
}

// startJobWorker re-enqueues the jobs persisted as pending/running before the
// last shutdown. Classification and keys are re-derived from the persisted
// jobs — a recovered job may run with the provider configuration currently in
// effect, same as before.
func (s *Server) startJobWorker() {
	jobs, err := s.Store.ListRunnableJobs()
	if err != nil {
		slog.Error("list runnable jobs", "error", err)
		return
	}
	if len(jobs) >= 500 {
		slog.Warn("runnable jobs recovered at the query limit; older pending jobs may need a manual retry", "recovered", len(jobs))
	}
	for _, job := range jobs {
		s.enqueueJob(job.ID)
	}
}

// StopJobWorker stops admitting new jobs and waits until every pending job
// has been dispatched and every in-flight job finished (same drain contract
// as before: call at most once, and before closing the store — e.g. in tests,
// ahead of the PocketBase unbootstrap). Jobs blocked on a busy resource start
// as their blockers finish; jobs whose plan fails at dispatch are marked
// failed. When dispatchDisabled is set (tests), pending jobs are dropped
// without running, mirroring the old closed-channel behavior.
func (s *Server) StopJobWorker() {
	s.jobMu.Lock()
	s.stopping = true
	s.jobMu.Unlock()
	if s.dispatchDisabled {
		s.jobMu.Lock()
		s.pendingAI = nil
		s.pendingWeb = nil
		s.queuedJobs = map[string]struct{}{}
		s.jobMu.Unlock()
		return
	}
	s.dispatchJobs()
	s.workerWG.Wait()
}

// enqueueJob is the single admission point for the scheduler: dedup by job
// ID, class classification, queue-capacity rejection (503 contract kept) and
// a dispatch pass. A job that waits for a busy resource stays pending and
// never gets a 503 — the 503 is reserved for a queue that is actually full.
func (s *Server) enqueueJob(jobID string) bool {
	if jobID == "" {
		return false
	}
	job, err := s.Store.GetJob(jobID)
	if err != nil {
		slog.Error("enqueue job: get job", "jobId", jobID, "error", err)
		return false
	}
	class, ok := classifyJobOperation(job.Operation)
	if !ok {
		slog.Error("enqueue job: unknown operation", "jobId", jobID, "operation", job.Operation)
		if ue := s.Store.UpdateJob(jobID, map[string]any{
			"status":       "failed",
			"errorMessage": fmt.Sprintf("unknown job operation %q", job.Operation),
		}); ue != nil {
			slog.Error("update job status on unknown operation", "jobId", jobID, "error", ue)
		}
		return false
	}

	s.jobMu.Lock()
	if s.queuedJobs == nil {
		s.queuedJobs = map[string]struct{}{}
	}
	if _, exists := s.queuedJobs[jobID]; exists {
		s.jobMu.Unlock()
		return true
	}
	if s.stopping {
		s.jobMu.Unlock()
		slog.Warn("enqueue job during shutdown rejected", "jobId", jobID)
		return false
	}
	pending := &s.pendingAI
	if class == webClass {
		pending = &s.pendingWeb
	}
	if len(*pending) >= jobQueueCapacity {
		s.jobMu.Unlock()
		if ue := s.Store.UpdateJob(jobID, map[string]any{
			"status":       "failed",
			"errorMessage": jobQueueFullMessage,
		}); ue != nil {
			slog.Error("update job status on queue saturation", "jobId", jobID, "error", ue)
		}
		slog.Warn("job queue full, job rejected",
			"jobId", jobID,
			"class", class.String(),
			"queueLen", len(*pending),
			"queueCap", jobQueueCapacity)
		return false
	}
	s.queuedJobs[jobID] = struct{}{}
	*pending = append(*pending, pendingJob{id: jobID, enqueuedAt: time.Now()})
	s.jobMu.Unlock()

	s.dispatchJobs()
	return true
}

// dispatchJobs scans both pending queues in FIFO order and launches every job
// whose resource keys are free and whose class has capacity left. A blocked
// job never prevents a later, independent job from starting; FIFO order is
// preserved among jobs that share a key. Plan resolution touches PocketBase,
// so no scheduler mutex is held while scanning. Every enqueue/finish/cancel
// wakes a pass; with the same wake pattern recurring, each pending job is
// re-resolved per pass — ponytail: pending counts are small (queue cap 128)
// and resolution is a handful of local SQLite reads, so re-resolution per
// pass is the accepted ceiling; upgrade path is caching plans per pending
// entry with explicit invalidation.
func (s *Server) dispatchJobs() {
	if s.dispatchDisabled {
		return
	}
	s.jobMu.Lock()
	candidates := make([]pendingJob, 0, len(s.pendingAI)+len(s.pendingWeb))
	candidates = append(candidates, s.pendingAI...)
	candidates = append(candidates, s.pendingWeb...)
	s.jobMu.Unlock()

	for _, cand := range candidates {
		job, err := s.Store.GetJob(cand.id)
		if err != nil {
			s.dropPendingJob(cand, "job lookup failed")
			continue
		}
		if job.Status == "cancelled" || job.Status == "done" || job.Status == "failed" {
			s.dropPendingJob(cand, "job already settled")
			continue
		}
		plan, err := s.buildJobRunPlan(job)
		if err != nil {
			s.failPendingJob(cand, err)
			continue
		}
		s.tryLaunch(plan, cand.enqueuedAt)
	}
}

// tryLaunch atomically reserves the plan's keys and class capacity and starts
// the job goroutine. It returns false when the job left the queue meanwhile,
// a key is held by another job, or the class has no capacity left.
func (s *Server) tryLaunch(plan *jobRunPlan, enqueuedAt time.Time) bool {
	s.jobMu.Lock()
	if _, stillQueued := s.queuedJobs[plan.jobID]; !stillQueued {
		s.jobMu.Unlock()
		return false
	}
	running := &s.runningAI
	limit := maxConcurrentAIJobs
	if plan.class == webClass {
		running = &s.runningWeb
		limit = maxConcurrentWebJobs
	}
	// Draining (stopping) ignores the capacity limit to preserve the old
	// queue-drain shutdown semantics; keys still apply.
	if *running >= limit && !s.stopping {
		s.jobMu.Unlock()
		return false
	}
	for _, key := range plan.keys {
		if _, held := s.reservedKeys[key]; held {
			s.jobMu.Unlock()
			slog.Debug("job dispatch blocked, key busy", "jobId", plan.jobID, "key", key)
			return false
		}
	}
	for _, key := range plan.keys {
		s.reservedKeys[key] = plan.jobID
	}
	*running++
	s.runningJobs[plan.jobID] = struct{}{}
	s.removePendingLocked(plan.jobID)
	s.jobMu.Unlock()

	plan.startedAt = time.Now()
	slog.Info("job dispatched",
		"jobId", plan.jobID,
		"class", plan.class.String(),
		"waitedMs", plan.startedAt.Sub(enqueuedAt).Milliseconds())
	s.workerWG.Add(1)
	go func() {
		// finishJob (registered after Done) runs first: it releases the keys
		// and wakes a dispatch pass before Wait() observes this goroutine
		// exit, so StopJobWorker's Wait also covers jobs launched by that pass.
		defer s.workerWG.Done()
		defer s.finishJob(plan)
		if err := s.runJob(plan); err != nil {
			slog.Error("job failed", "jobId", plan.jobID, "error", err)
		}
	}()
	return true
}

// finishJob releases the job's reserved keys and runs another dispatch pass.
func (s *Server) finishJob(plan *jobRunPlan) {
	s.jobMu.Lock()
	for _, key := range plan.keys {
		if s.reservedKeys[key] == plan.jobID {
			delete(s.reservedKeys, key)
		}
	}
	if plan.class == aiClass {
		s.runningAI--
	} else {
		s.runningWeb--
	}
	delete(s.runningJobs, plan.jobID)
	delete(s.queuedJobs, plan.jobID)
	s.jobMu.Unlock()
	if !plan.startedAt.IsZero() {
		slog.Info("job finished",
			"jobId", plan.jobID,
			"class", plan.class.String(),
			"ranMs", time.Since(plan.startedAt).Milliseconds())
	}
	s.dispatchJobs()
}

func (s *Server) dropPendingJob(cand pendingJob, reason string) {
	s.jobMu.Lock()
	s.removePendingLocked(cand.id)
	s.jobMu.Unlock()
	slog.Info("job dropped from dispatch queue", "jobId", cand.id, "reason", reason)
}

func (s *Server) failPendingJob(cand pendingJob, cause error) {
	s.jobMu.Lock()
	s.removePendingLocked(cand.id)
	s.jobMu.Unlock()
	slog.Error("job failed at dispatch", "jobId", cand.id, "error", cause)
	if ue := s.Store.UpdateJob(cand.id, map[string]any{
		"status":       "failed",
		"errorMessage": cause.Error(),
	}); ue != nil {
		slog.Error("update job status on dispatch failure", "jobId", cand.id, "error", ue)
	}
}

// removePendingLocked drops a job from both pending queues and the dedup set.
// Caller must hold jobMu.
func (s *Server) removePendingLocked(jobID string) {
	s.pendingAI = removeFromPending(s.pendingAI, jobID)
	s.pendingWeb = removeFromPending(s.pendingWeb, jobID)
	delete(s.queuedJobs, jobID)
}

func removeFromPending(list []pendingJob, jobID string) []pendingJob {
	for i, cand := range list {
		if cand.id == jobID {
			return append(list[:i], list[i+1:]...)
		}
	}
	return list
}

// onJobCancelled removes a cancelled job that is still waiting in the
// dispatch queue (freeing its slot without executing it) and wakes the
// dispatcher. In-flight jobs are left to the executor, which observes the
// persisted status and the run context.
func (s *Server) onJobCancelled(jobID string) {
	s.jobMu.Lock()
	if _, running := s.runningJobs[jobID]; running {
		s.jobMu.Unlock()
		return
	}
	s.removePendingLocked(jobID)
	s.jobMu.Unlock()
	s.dispatchJobs()
}

// acquireNovelJobSlot enforces "at most one active job per novel" at
// admission time. It serializes the check+create+enqueue sequence per novel
// under the per-novel lock; on success it returns the unlock func, which the
// caller must hold until the job has been created and enqueued. On conflict
// it returns errNovelBusy; on store failure the error from the check.
func (s *Server) acquireNovelJobSlot(novelID string) (func(), error) {
	unlock := s.lockNovel(novelID)
	active, err := s.Store.HasActiveJobsForNovel(novelID)
	if err != nil {
		unlock()
		return nil, err
	}
	if active {
		unlock()
		return nil, errNovelBusy
	}
	return unlock, nil
}

// admitNovelJob is the HTTP flavor of acquireNovelJobSlot: on conflict or
// store failure it returns the shaped ApiError (409/500) for the handler to
// return; on success it returns the unlock func.
func (s *Server) admitNovelJob(e *core.RequestEvent, novelID string) (func(), error) {
	unlock, err := s.acquireNovelJobSlot(novelID)
	if err == nil {
		return unlock, nil
	}
	if errors.Is(err, errNovelBusy) {
		return nil, e.Error(http.StatusConflict, novelBusyMessage, nil)
	}
	return nil, e.InternalServerError("failed to check active jobs", err)
}

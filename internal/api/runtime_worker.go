package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"translator-server/internal/noveldownloader"
	"translator-server/internal/store"
)

// chapterDownloadMaxRetries is how many extra attempts are made when a single
// chapter download fails transiently (e.g. a Cloudflare challenge that the
// browser worker could not solve on the first try). A single transient
// failure must not drop the chapter permanently from the novel.
const chapterDownloadMaxRetries = 3

// chapterDownloadRetryBaseDelay scales the backoff between retries: attempt n
// waits n * baseDelay before retrying.
const chapterDownloadRetryBaseDelay = 5 * time.Second

func (s *Server) startJobWorker() {
	s.downloadQueue = make(chan string, 128)
	s.translateQueue = make(chan string, 128)

	go s.downloadWorkerLoop()
	go s.translateWorkerLoop()

	jobs, err := s.Store.ListRunnableJobs()
	if err != nil {
		slog.Error("list runnable jobs", "error", err)
		return
	}
	for _, job := range jobs {
		s.enqueueJob(job.ID)
	}
}

func (s *Server) enqueueJob(jobID string) {
	if jobID == "" {
		return
	}
	s.queueMu.Lock()
	if s.queuedJobs == nil {
		s.queuedJobs = map[string]struct{}{}
	}
	if _, exists := s.queuedJobs[jobID]; exists {
		s.queueMu.Unlock()
		return
	}
	s.queuedJobs[jobID] = struct{}{}
	s.queueMu.Unlock()

	job, err := s.Store.GetJob(jobID)
	if err != nil {
		slog.Error("enqueue job: get job", "jobId", jobID, "error", err)
		return
	}

	var queue chan string
	switch job.Operation {
	case "download":
		queue = s.downloadQueue
	default:
		queue = s.translateQueue
	}

	select {
	case queue <- jobID:
	default:
		s.queueMu.Lock()
		delete(s.queuedJobs, jobID)
		s.queueMu.Unlock()
		msg := "Server is busy processing other jobs. Please wait a few minutes and try again."
		if ue := s.Store.UpdateJob(jobID, map[string]any{
			"status":       "failed",
			"errorMessage": msg,
		}); ue != nil {
			slog.Error("update job status on queue saturation", "jobId", jobID, "error", ue)
		}
		slog.Warn("job queue full, job rejected",
			"jobId", jobID,
			"queueLen", len(queue),
			"queueCap", cap(queue))
	}
}

func (s *Server) workerLoop(queue chan string) {
	for jobID := range queue {
		s.queueMu.Lock()
		delete(s.queuedJobs, jobID)
		s.queueMu.Unlock()
		if err := s.processJob(jobID); err != nil {
			slog.Error("job failed", "jobId", jobID, "error", err)
		}
	}
}

func (s *Server) downloadWorkerLoop() {
	s.workerLoop(s.downloadQueue)
}

func (s *Server) translateWorkerLoop() {
	s.workerLoop(s.translateQueue)
}

func (s *Server) processJob(jobID string) error {
	job, err := s.Store.GetJob(jobID)
	if err != nil {
		return fmt.Errorf("get job: %w", err)
	}
	if job.Status == "cancelled" || job.Status == "done" || job.Status == "failed" {
		return nil
	}

	runCtx, cancel := context.WithCancel(context.Background())
	s.registerJobCancel(jobID, cancel)
	defer func() {
		cancel()
		s.unregisterJobCancel(jobID)
	}()

	if job.Operation == "download" {
		return s.processDownloadJob(runCtx, job)
	}
	if job.Operation == "check" {
		return s.processCheckJob(runCtx, job)
	}
	if job.Operation == "generate-glossary" {
		return s.processGenerateGlossaryJob(runCtx, job)
	}

	jc, err := s.buildJobContext(runCtx, job)
	if err != nil {
		if ue := s.Store.UpdateJob(jobID, map[string]interface{}{"status": "failed", "errorMessage": err.Error()}); ue != nil {
			slog.Error("update job status on build context failure", "jobId", jobID, "error", ue)
		}
		return fmt.Errorf("load job context: %w", err)
	}
	if len(jc.chapters) == 0 {
		err := fmt.Errorf("job %s has no chapters to process", jobID)
		if ue := s.Store.UpdateJob(jobID, map[string]interface{}{"status": "failed", "errorMessage": err.Error()}); ue != nil {
			slog.Error("update job status on empty chapters", "jobId", jobID, "error", ue)
		}
		return err
	}

	if err := s.Store.UpdateJob(jobID, map[string]interface{}{
		"status":                  "running",
		"operation":               job.Operation,
		"provider":                jc.cfg.AI.Provider,
		"model":                   effectiveModel(jc.cfg.AI),
		"errorMessage":            "",
		"totalChapters":           len(jc.chapters),
		"autoSegmentEnabled":      jc.cfg.Translation.AutoSegment,
		"autoSegmentActive":       false,
		"autoSegmentCount":        0,
		"autoSegmentChapterId":    "",
		"autoSegmentChapterTitle": "",
	}); err != nil {
		return fmt.Errorf("set job running: %w", err)
	}

	var wasCancelled bool
	for idx := range jc.chapters {
		if runCtx.Err() != nil {
			wasCancelled = true
			break
		}
		chapter := jc.chapters[idx]
		jc.resetSegmentProgress()

		var chapterErr error
		switch job.Operation {
		case "refine":
			chapterErr = s.runRefineChapter(jc, idx, &chapter)
		default:
			segmentation := previewChapterSegmentation(jc.cfg, chapter)
			jc.recordSegProgress(0, 0, segmentation.SegmentCount, chapter.ID, chapter.Title, segmentation.Applied)
			jc.flushProgress(s)
			var segErr error
			_, segErr = s.runTranslateChapterDetailed(jc, idx, &chapter)
			chapterErr = segErr
		}

		if runCtx.Err() != nil {
			wasCancelled = true
		}

		jc.recordChapterResult(chapterErr)
		if chapterErr != nil {
			if wasCancelled {
				if err := s.Store.UpdateChapterStatusFast(chapter.ID, "pending", ""); err != nil {
					slog.Warn("reset chapter status on cancel", "chapterId", chapter.ID, "error", err)
				}
			} else {
				if err := s.Store.UpdateChapterStatusFast(chapter.ID, "failed", chapterErr.Error()); err != nil {
					slog.Warn("update chapter status on failure", "chapterId", chapter.ID, "error", err)
				}
			}
		}
		jc.resetSegmentProgress()
		jc.flushProgress(s)
	}

	if jc.statsDirty {
		if err := s.Store.RecalculateNovelStats(jc.novel.ID); err != nil {
			slog.Error("recalculate novel stats at job end", "jobId", jobID, "error", err)
		}
	}

	finalStatus := "done"
	finalError := ""
	if wasCancelled {
		finalStatus = "cancelled"
	} else if jc.failed > 0 {
		finalStatus = "failed"
		finalError = jc.lastError
	}

	return s.Store.UpdateJob(jobID, map[string]interface{}{
		"status":                    finalStatus,
		"completedChapters":         jc.completed,
		"failedChapters":            jc.failed,
		"errorMessage":              finalError,
		"autoSegmentActive":         false,
		"autoSegmentCurrentIndex":   0,
		"autoSegmentCompletedCount": 0,
		"autoSegmentChapterId":      "",
		"autoSegmentChapterTitle":   "",
	})
}

type downloadJobOptions struct {
	URL            string                      `json:"url"`
	Chapters       []store.DownloadChapterInfo `json:"chapters"`
	StartOrder     int                         `json:"startOrder"`
	SourceLanguage string                      `json:"sourceLanguage"`
	TargetLanguage string                      `json:"targetLanguage"`
}

func (s *Server) processDownloadJob(ctx context.Context, job *store.Job) error {
	var opts downloadJobOptions
	if err := json.Unmarshal([]byte(job.OptionsJSON), &opts); err != nil {
		if ue := s.Store.UpdateJob(job.ID, map[string]interface{}{"status": "failed", "errorMessage": fmt.Sprintf("invalid job options: %v", err)}); ue != nil {
			slog.Error("update job status on invalid options", "jobId", job.ID, "error", ue)
		}
		return fmt.Errorf("parse download options: %w", err)
	}
	dl := s.DownloaderFactory(job.OwnerID)
	if len(opts.Chapters) == 0 {
		if ue := s.Store.UpdateJob(job.ID, map[string]interface{}{"status": "done"}); ue != nil {
			slog.Error("update job status on no chapters", "jobId", job.ID, "error", ue)
		}
		if ctx.Err() == nil {
			if err := dl.SleepBetweenChapters(ctx); err != nil {
				return err
			}
		}
		return nil
	}
	if err := s.Store.UpdateJob(job.ID, map[string]interface{}{
		"status":        "running",
		"totalChapters": len(opts.Chapters),
		"errorMessage":  "",
	}); err != nil {
		return fmt.Errorf("set job running: %w", err)
	}

	parser := dl.FindParser(opts.URL)

	if parser == nil && !s.HasBrowserWorkerForUser(job.OwnerID) {
		if ue := s.Store.UpdateJob(job.ID, map[string]interface{}{"status": "failed", "errorMessage": "unsupported URL"}); ue != nil {
			slog.Error("update job status on unsupported URL", "jobId", job.ID, "error", ue)
		}
		return fmt.Errorf("unsupported URL: %s", opts.URL)
	}

	completed := 0
	failed := 0
	var proxyDL *noveldownloader.Downloader
	if parser == nil && s.HasBrowserWorkerForUser(job.OwnerID) {
		proxyDL = s.DownloaderFactoryWithClient(NewProxyHTTPClient(s, job.OwnerID))
	}
	for idx, chInfo := range opts.Chapters {
		if err := ctx.Err(); err != nil {
			break
		}
		if idx > 0 {
			if err := dl.SleepBetweenChapters(ctx); err != nil {
				return err
			}
		}

		ch, downloadErr := s.downloadChapterWithRetry(ctx, dl, proxyDL, parser, chInfo, job.ID, job.OwnerID)

		if downloadErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				break
			}
			failed++
			slog.Error("failed to download chapter", "jobId", job.ID, "chapter", chInfo.Title, "error", downloadErr)
		} else if ch != nil {
			chOrder := chInfo.Order
			if chOrder <= 0 {
				chOrder = opts.StartOrder + idx
			}
			chTitle := ch.Title
			if chTitle == "" {
				chTitle = chInfo.Title
			}
			if chTitle == "" {
				chTitle = fmt.Sprintf("Capítulo %d", chOrder)
			}
			if _, err := s.Store.UpsertChapterWithoutStats(job.OwnerID, job.NovelID, &store.Chapter{
				ChapterOrder:    chOrder,
				Title:           chTitle,
				OriginalContent: ch.Markdown,
				Status:          "pending",
			}); err != nil {
				failed++
				slog.Error("failed to save chapter", "jobId", job.ID, "chapter", chTitle, "error", err)
			} else {
				completed++
			}
		} else {
			failed++
			slog.Warn("empty download result", "jobId", job.ID, "chapter", chInfo.Title)
		}
		if ue := s.Store.UpdateJob(job.ID, map[string]interface{}{
			"completedChapters": completed,
			"failedChapters":    failed,
		}); ue != nil {
			slog.Warn("update job progress", "jobId", job.ID, "error", ue)
		}
	}
	if err := s.Store.RecalculateNovelStats(job.NovelID); err != nil {
		slog.Error("failed to recalculate novel stats after download", "jobId", job.ID, "error", err)
	}
	if ctx.Err() == nil {
		if err := dl.SleepBetweenChapters(ctx); err != nil {
			return err
		}
	}
	if ctx.Err() != nil {
		if ue := s.Store.UpdateJob(job.ID, map[string]interface{}{
			"completedChapters": completed,
			"failedChapters":    failed,
		}); ue != nil {
			slog.Warn("update job final counts on cancel", "jobId", job.ID, "error", ue)
		}
		return nil
	}
	status := "done"
	if failed > 0 {
		status = "failed"
	}
	return s.Store.UpdateJob(job.ID, map[string]interface{}{
		"status":            status,
		"completedChapters": completed,
		"failedChapters":    failed,
	})
}

// downloadChapterWithRetry downloads a single chapter, retrying transient
// failures (Cloudflare challenges, timeouts, empty responses) a few times
// with a small backoff before giving up. A single transient failure should
// not permanently drop a chapter from the novel.
func (s *Server) downloadChapterWithRetry(ctx context.Context, dl, proxyDL *noveldownloader.Downloader, parser noveldownloader.Parser, chInfo store.DownloadChapterInfo, jobID, ownerID string) (*noveldownloader.Chapter, error) {
	chURLs := []noveldownloader.ChapterURL{{URL: chInfo.URL, Title: chInfo.Title}}
	var lastErr error
	for attempt := 0; attempt <= chapterDownloadMaxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt) * chapterDownloadRetryBaseDelay
			slog.Warn("retrying chapter download after transient failure",
				"jobId", jobID, "chapter", chInfo.Title, "attempt", attempt+1,
				"maxAttempts", chapterDownloadMaxRetries+1, "backoff", backoff, "error", lastErr)
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}

		var downloaded []noveldownloader.Chapter
		var err error
		switch {
		case parser != nil:
			// Fallback to proxy is handled automatically by LazyFallbackClient
			downloaded, err = dl.DownloadChapters(ctx, chURLs, 1, 1)
		case s.HasBrowserWorkerForUser(ownerID):
			localProxy := proxyDL
			if localProxy == nil {
				localProxy = s.DownloaderFactoryWithClient(NewProxyHTTPClient(s, ownerID))
			}
			downloaded, err = localProxy.DownloadChapters(ctx, chURLs, 1, 1)
		default:
			return nil, fmt.Errorf("no download method available")
		}

		if err == nil && len(downloaded) > 0 {
			return &downloaded[0], nil
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("empty download result for %s", chInfo.URL)
		}
	}
	return nil, lastErr
}

type checkJobOptions struct {
	URL string `json:"url"`
}

func (s *Server) processCheckJob(ctx context.Context, job *store.Job) error {
	var opts checkJobOptions
	if err := json.Unmarshal([]byte(job.OptionsJSON), &opts); err != nil {
		if ue := s.Store.UpdateJob(job.ID, map[string]interface{}{"status": "failed", "errorMessage": fmt.Sprintf("invalid job options: %v", err)}); ue != nil {
			slog.Error("update job status on invalid options", "jobId", job.ID, "error", ue)
		}
		return fmt.Errorf("parse check options: %w", err)
	}
	if opts.URL == "" {
		if ue := s.Store.UpdateJob(job.ID, map[string]interface{}{"status": "failed", "errorMessage": "no source URL"}); ue != nil {
			slog.Error("update job status on missing URL", "jobId", job.ID, "error", ue)
		}
		return fmt.Errorf("check job %s has no URL", job.ID)
	}

	if err := s.Store.UpdateJob(job.ID, map[string]interface{}{
		"status":       "running",
		"errorMessage": "",
	}); err != nil {
		return fmt.Errorf("set job running: %w", err)
	}

	dl := s.DownloaderFactory(job.OwnerID)
	if err := dl.SleepBetweenChapters(ctx); err != nil {
		return err
	}
	info, err := dl.GetNovelInfo(ctx, opts.URL)
	if err != nil {
		if ue := s.Store.UpdateJob(job.ID, map[string]interface{}{
			"status":       "failed",
			"errorMessage": err.Error(),
		}); ue != nil {
			slog.Error("update job status on fetch failure", "jobId", job.ID, "error", ue)
		}
		return fmt.Errorf("fetch novel info: %w", err)
	}

	existingOrders, err := s.Store.GetExistingChapterOrders(job.OwnerID, job.NovelID)
	if err != nil {
		if ue := s.Store.UpdateJob(job.ID, map[string]interface{}{
			"status":       "failed",
			"errorMessage": err.Error(),
		}); ue != nil {
			slog.Error("update job status on existing orders failure", "jobId", job.ID, "error", ue)
		}
		return fmt.Errorf("get existing orders: %w", err)
	}
	existingTitles, err := s.Store.GetExistingChapterURLs(job.OwnerID, job.NovelID)
	if err != nil {
		if ue := s.Store.UpdateJob(job.ID, map[string]interface{}{
			"status":       "failed",
			"errorMessage": err.Error(),
		}); ue != nil {
			slog.Error("update job status on existing titles failure", "jobId", job.ID, "error", ue)
		}
		return fmt.Errorf("get existing titles: %w", err)
	}

	newAvailable := 0
	for _, ch := range info.Chapters {
		chNum := extractChapterOrder(ch.Title)
		if chNum > 0 && existingOrders[chNum] {
			continue
		}
		if existingTitles[ch.Title] {
			continue
		}
		newAvailable++
	}

	cacheKey := job.OwnerID + ":" + job.NovelID
	s.previewCacheMu.Lock()
	s.previewCache[cacheKey] = previewCacheEntry{
		chapters:  info.Chapters,
		createdAt: time.Now(),
	}
	s.previewCacheMu.Unlock()
	time.AfterFunc(previewCacheTTL, func() {
		s.previewCacheMu.Lock()
		defer s.previewCacheMu.Unlock()
		if entry, exists := s.previewCache[cacheKey]; exists {
			if time.Since(entry.createdAt) >= previewCacheTTL {
				delete(s.previewCache, cacheKey)
			}
		}
	})

	checkedAt := time.Now().Format(time.RFC3339)
	if err := s.Store.UpdateNovelCheckResult(job.NovelID, checkedAt, newAvailable); err != nil {
		slog.Warn("update novel check result", "jobId", job.ID, "novelId", job.NovelID, "error", err)
	}

	return s.Store.UpdateJob(job.ID, map[string]interface{}{
		"status":      "done",
		"newChapters": newAvailable,
	})
}

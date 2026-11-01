package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"translator-server/internal/comicdownloader"
	"translator-server/internal/store"
)

// comicDownloadJobOptions mirrors downloadJobOptions for the comics tree.
type comicDownloadJobOptions struct {
	URL            string                           `json:"url"`
	Chapters       []store.DownloadComicChapterInfo `json:"chapters"`
	StartOrder     int                              `json:"startOrder"`
	SourceLanguage string                           `json:"sourceLanguage"`
	TargetLanguage string                           `json:"targetLanguage"`
	ReDownload     bool                             `json:"reDownload"`
}

// downloadComicChapterUploads fetches every page image of a parsed chapter
// into memory, in reading order, without touching the database. Callers
// persist the result with ReplaceComicChapterPages only after the chapter row
// exists — downloading first guarantees a failed chapter never leaves an
// empty (page-less) row behind.
func (s *Server) downloadComicChapterUploads(ctx context.Context, dl *comicdownloader.Downloader, ch *comicdownloader.Chapter) ([]store.ComicPageUpload, error) {
	if ch == nil {
		return nil, fmt.Errorf("nil chapter")
	}
	if len(ch.Pages) == 0 {
		return nil, fmt.Errorf("chapter %q has no pages", ch.Title)
	}
	uploads := make([]store.ComicPageUpload, 0, len(ch.Pages))
	for i, pageURL := range ch.Pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		blob, mimeType, err := dl.DownloadImage(ctx, pageURL, ch.SourceURL)
		if err != nil {
			return nil, fmt.Errorf("downloading page %d of %q: %w", i+1, ch.Title, err)
		}
		uploads = append(uploads, store.ComicPageUpload{
			Data:      blob,
			MimeType:  mimeType,
			SourceURL: pageURL,
			Order:     i + 1,
		})
	}
	return uploads, nil
}

// processComicDownloadJob downloads the planned chapters of one comic: each
// chapter's pages are fetched through its parser script, downloaded as
// images, and persisted in reading order.
func (s *Server) processComicDownloadJob(ctx context.Context, job *store.Job) error {
	var opts comicDownloadJobOptions
	if err := json.Unmarshal([]byte(job.OptionsJSON), &opts); err != nil {
		if ue := s.Store.UpdateJob(job.ID, map[string]interface{}{"status": "failed", "errorMessage": fmt.Sprintf("invalid job options: %v", err)}); ue != nil {
			slog.Error("update job status on invalid options", "jobId", job.ID, "error", ue)
		}
		return fmt.Errorf("parse download options: %w", err)
	}
	if opts.ReDownload {
		activeJobs, err := s.Store.ListActiveComicJobs(job.ComicID)
		if err != nil {
			return fmt.Errorf("check active comic jobs: %w", err)
		}
		for _, activeJob := range activeJobs {
			if activeJob.ID == job.ID {
				continue
			}
			message := "No se puede re-descargar: ya hay otro trabajo en curso para este cómic. Espera a que termine e inténtalo de nuevo."
			if ue := s.Store.UpdateJob(job.ID, map[string]interface{}{"status": "failed", "errorMessage": message}); ue != nil {
				slog.Error("update redownload job after active job conflict", "jobId", job.ID, "error", ue)
			}
			return fmt.Errorf("redownload blocked by active job %s", activeJob.ID)
		}
	}
	if len(opts.Chapters) == 0 {
		// Nothing was fetched, so there is no request to space out: the job
		// ends here rather than holding the origin keys for a pointless wait.
		if ue := s.Store.UpdateJob(job.ID, map[string]interface{}{"status": "done"}); ue != nil {
			slog.Error("update job status on no chapters", "jobId", job.ID, "error", ue)
		}
		return nil
	}
	if err := s.Store.UpdateJob(job.ID, map[string]interface{}{
		"status":                  "running",
		"totalChapters":           len(opts.Chapters),
		"errorMessage":            "",
		"autoSegmentChapterTitle": "",
	}); err != nil {
		return fmt.Errorf("set job running: %w", err)
	}

	// Site data comes exclusively from the parser engine: resolve the script
	// for the comic's source URL once, so every chapter in the job is fetched
	// through the same (possibly browser-routed) path.
	scriptEntry, scriptErr := s.resolveScriptForExecution(ctx, job.OwnerID, opts.URL)
	if scriptErr != nil {
		if ue := s.Store.UpdateJob(job.ID, map[string]interface{}{"status": "failed", "errorMessage": parserErrorMessage(scriptErr)}); ue != nil {
			slog.Error("update job status on unsupported URL", "jobId", job.ID, "error", ue)
		}
		return scriptErr
	}
	fetcher := newParserFetcher(s, job.OwnerID, scriptEntry.script.RequiresBrowser(), scriptEntry.script.LivewireCatalogPattern())
	cooldown := func(ctx context.Context) error { return fetcher.sleepBetweenFetches(ctx, opts.URL) }
	// Image downloader for the chapter pages: direct HTTP with a lazy
	// browser-worker fallback scoped to the comic owner.
	dl := s.ComicDownloaderFactory(job.OwnerID)

	completed := 0
	failed := 0
	// Seed claimed orders from what is already stored so a repeated site
	// number never hits the (comic, chapter_order) unique index. Re-downloads
	// update by ID and are exempt.
	claimed := make(map[int]bool, len(opts.Chapters))
	if !opts.ReDownload {
		if existing, err := s.Store.GetExistingComicChapterOrders(job.OwnerID, job.ComicID); err != nil {
			slog.Warn("download job without existing-order snapshot", "jobId", job.ID, "error", err)
		} else {
			for o := range existing {
				claimed[o] = true
			}
		}
	}
	for idx, chInfo := range opts.Chapters {
		if err := ctx.Err(); err != nil {
			break
		}
		// Space consecutive chapters from the same site (first chapter is
		// never delayed, matching the throttle's rule).
		if idx > 0 {
			if err := cooldown(ctx); err != nil {
				break
			}
		}
		if ue := s.Store.UpdateJobProgressFast(job.ID, map[string]interface{}{
			"autoSegmentChapterTitle": chInfo.Title,
		}); ue != nil {
			slog.Warn("update job current chapter", "jobId", job.ID, "error", ue)
		}

		page, downloadErr := s.downloadComicChapterPages(ctx, job.OwnerID, opts.URL, chInfo)
		if downloadErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				break
			}
			failed++
			slog.Error("failed to download chapter", "jobId", job.ID, "chapter", chInfo.Title, "error", downloadErr)
			if ue := s.Store.UpdateJobProgressFast(job.ID, map[string]interface{}{
				"completedChapters": completed,
				"failedChapters":    failed,
			}); ue != nil {
				slog.Warn("update job progress", "jobId", job.ID, "error", ue)
			}
			continue
		}
		chOrder := chInfo.Order
		if chOrder <= 0 {
			chOrder = opts.StartOrder + idx
		}
		if !opts.ReDownload {
			if claimed[chOrder] {
				fixed := chOrder + 1
				for claimed[fixed] {
					fixed++
				}
				chOrder = fixed
			}
			claimed[chOrder] = true
		}
		chTitle := page.Title
		if chTitle == "" {
			chTitle = chInfo.Title
		}
		if chTitle == "" {
			chTitle = fmt.Sprintf("Capítulo %d", chOrder)
		}
		if opts.ReDownload {
			// Re-download mode: replace the page images of an existing
			// chapter (matched by id). Title and order are preserved.
			if chInfo.ChapterID == "" {
				failed++
				slog.Error("re-download chapter without id", "jobId", job.ID, "chapter", chTitle)
			} else if uploads, pagesErr := s.downloadComicChapterUploads(ctx, dl, page); pagesErr != nil {
				failed++
				slog.Error("failed to download re-download pages", "jobId", job.ID, "chapter", chTitle, "error", pagesErr)
			} else if err := s.Store.ReplaceComicChapterPages(chInfo.ChapterID, uploads); err != nil {
				failed++
				slog.Error("failed to save re-downloaded pages", "jobId", job.ID, "chapter", chTitle, "error", err)
			} else {
				completed++
			}
		} else {
			// Download the image bytes before creating the chapter row: a
			// failed chapter must leave no empty (page-less) row behind.
			uploads, pagesErr := s.downloadComicChapterUploads(ctx, dl, page)
			if pagesErr != nil {
				failed++
				slog.Error("failed to download chapter pages", "jobId", job.ID, "chapter", chTitle, "error", pagesErr)
			} else if saved, err := s.Store.UpsertComicChapterWithoutStats(job.OwnerID, job.ComicID, &store.ComicChapter{
				ChapterOrder: chOrder,
				Title:        chTitle,
				SourceKey:    chInfo.SourceKey,
				Status:       store.ComicChapterPending,
			}); err != nil {
				failed++
				slog.Error("failed to save chapter", "jobId", job.ID, "chapter", chTitle, "error", err)
			} else if err := s.Store.ReplaceComicChapterPages(saved.ID, uploads); err != nil {
				failed++
				slog.Error("failed to save chapter pages", "jobId", job.ID, "chapter", chTitle, "error", err)
				// Roll back the row created above: without pages it is an
				// empty ghost, not a failed chapter.
				if delErr := s.Store.DeleteComicChapterRecord(saved.ID); delErr != nil {
					slog.Warn("delete orphan chapter after page save failure", "jobId", job.ID, "chapter", saved.ID, "error", delErr)
				} else {
					delete(claimed, chOrder)
				}
			} else {
				completed++
			}
		}
		if ue := s.Store.UpdateJobProgressFast(job.ID, map[string]interface{}{
			"completedChapters": completed,
			"failedChapters":    failed,
		}); ue != nil {
			slog.Warn("update job progress", "jobId", job.ID, "error", ue)
		}
	}
	if err := s.Store.RecalculateComicStats(job.ComicID); err != nil {
		slog.Error("failed to recalculate comic stats after download", "jobId", job.ID, "error", err)
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
	// The chapters the last check flagged as new are now stored, so the
	// "pending updates" flag must go back to zero. Skipped for re-downloads
	// and partial failures, where some flagged chapters never arrived.
	if status == "done" && !opts.ReDownload {
		if err := s.Store.ClearComicPendingNewChapters(job.ComicID); err != nil {
			slog.Warn("clear pending new chapters after download", "jobId", job.ID, "comicId", job.ComicID, "error", err)
		}
	}
	return s.Store.UpdateJob(job.ID, map[string]interface{}{
		"status":            status,
		"completedChapters": completed,
		"failedChapters":    failed,
	})
}

// processComicCheckJob re-fetches the comic's TOC, diffs it against the stored
// chapters and records the check result (mirrors the novel check job).
func (s *Server) processComicCheckJob(ctx context.Context, job *store.Job) error {
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

	snapshot, err := s.fetchSourceSnapshot(ctx, job.OwnerID, opts.URL)
	if err != nil {
		if ue := s.Store.UpdateJob(job.ID, map[string]interface{}{
			"status":       "failed",
			"errorMessage": parserErrorMessage(err),
		}); ue != nil {
			slog.Error("update job status on fetch failure", "jobId", job.ID, "error", ue)
		}
		return fmt.Errorf("fetch comic info: %w", err)
	}

	existingOrders, err := s.Store.GetExistingComicChapterOrders(job.OwnerID, job.ComicID)
	if err != nil {
		return s.failComicCheckJob(job, err, "get existing orders")
	}
	existingTitles, err := s.Store.GetExistingComicChapterURLs(job.OwnerID, job.ComicID)
	if err != nil {
		return s.failComicCheckJob(job, err, "get existing titles")
	}
	existingKeys, err := s.Store.GetExistingComicChapterKeys(job.OwnerID, job.ComicID)
	if err != nil {
		return s.failComicCheckJob(job, err, "get existing chapter keys")
	}

	keyPlan, forceLegacy, err := s.legacySourceKeyPlan(job.OwnerID, job.ComicID, snapshot.Chapters)
	if err != nil {
		return s.failComicCheckJob(job, err, "plan source keys")
	}
	newChapters, _ := diffNovelSnapshot(snapshot, mergeSourceKeys(existingKeys, keyPlan), existingOrders, existingTitles, forceLegacy)
	newAvailable := len(newChapters)

	cacheKey := job.OwnerID + ":" + job.ComicID
	s.previewCacheMu.Lock()
	s.previewCache[cacheKey] = previewCacheEntry{
		chapters:  snapshot.Chapters,
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

	if err := s.Store.UpdateComicCheckResult(job.ComicID, time.Now().Format(time.RFC3339), newAvailable); err != nil {
		slog.Warn("update comic check result", "jobId", job.ID, "comicId", job.ComicID, "error", err)
	}
	return s.Store.UpdateJob(job.ID, map[string]interface{}{
		"status":      "done",
		"newChapters": newAvailable,
	})
}

func (s *Server) failComicCheckJob(job *store.Job, err error, stage string) error {
	if ue := s.Store.UpdateJob(job.ID, map[string]interface{}{"status": "failed", "errorMessage": err.Error()}); ue != nil {
		slog.Error("update job status on check failure", "jobId", job.ID, "stage", stage, "error", ue)
	}
	return fmt.Errorf("%s: %w", stage, err)
}

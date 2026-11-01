package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
	pbrouter "github.com/pocketbase/pocketbase/tools/router"
	"translator-server/internal/store"
)

// sharedComicHandlers exposes the comics resource operations as plain handler
// funcs so the canonical /api/v1/comics routes can register them.
type sharedComicHandlers struct{}

var sharedComics = sharedComicHandlers{}

// comicFieldsSubset projects a Comic onto the requested ?fields= list (sparse
// fieldsets, same contract as the novels list).
func comicFieldsSubset(c *store.Comic, fields []string) map[string]any {
	m := make(map[string]any, len(fields))
	for _, f := range fields {
		switch strings.TrimSpace(f) {
		case "id":
			m["id"] = c.ID
		case "ownerId":
			m["ownerId"] = c.OwnerID
		case "sourceLanguage":
			m["sourceLanguage"] = c.SourceLanguage
		case "targetLanguage":
			m["targetLanguage"] = c.TargetLanguage
		case "sourceTitle":
			m["sourceTitle"] = c.SourceTitle
		case "sourceAuthor":
			m["sourceAuthor"] = c.SourceAuthor
		case "targetTitle":
			m["targetTitle"] = c.TargetTitle
		case "targetAuthor":
			m["targetAuthor"] = c.TargetAuthor
		case "sourceSeries":
			m["sourceSeries"] = c.SourceSeries
		case "url":
			m["url"] = c.URL
		case "status":
			m["status"] = c.Status
		case "tags":
			m["tags"] = c.Tags
		case "coverPath":
			m["coverPath"] = c.CoverPath
		case "thumbnailPath":
			m["thumbnailPath"] = c.ThumbnailPath
		case "isPublic":
			m["isPublic"] = c.IsPublic
		case "chapterCount":
			m["chapterCount"] = c.ChapterCount
		case "downloadedCount":
			m["downloadedCount"] = c.DownloadedCount
		case "maxChapterOrder":
			m["maxChapterOrder"] = c.MaxChapterOrder
		case "lastCheckedAt":
			m["lastCheckedAt"] = c.LastCheckedAt
		case "lastCheckNewChapters":
			m["lastCheckNewChapters"] = c.LastCheckNewChapters
		case "lastReadAt":
			m["lastReadAt"] = c.LastReadAt
		case "readingStatus":
			m["readingStatus"] = c.ReadingStatus
		case "createdAt":
			m["createdAt"] = c.CreatedAt
		case "updatedAt":
			m["updatedAt"] = c.UpdatedAt
		}
	}
	return m
}

// listComics: GET /comics. Supports ?q, ?field, ?sort, ?order, ?page&per_page
// (or ?limit&offset), and the same facet filters as the novels list, with
// ?progress=downloaded replacing the novels' translated.
func (sharedComicHandlers) list(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		q := e.Request.URL.Query()
		page, perPage, limit, offset := parsePagination(q)
		fieldsParam := firstQuery(q, "fields")
		if fieldsParam == "" {
			fieldsParam = firstQuery(q, "select")
		}
		searchQuery := firstQuery(q, "q")
		sortParam := firstQuery(q, "sort")
		orderParam := firstQuery(q, "order")
		opts := store.ListComicOptions{
			Tag:         firstQuery(q, "tag"),
			Author:      firstQuery(q, "author"),
			Series:      firstQuery(q, "series"),
			Shared:      firstQuery(q, "shared"),
			Progress:    firstQuery(q, "progress"),
			SearchField: firstQuery(q, "field"),
		}
		var list []store.Comic
		var hasMore bool
		var err error
		if searchQuery != "" {
			list, hasMore, err = s.Store.SearchComics(e.Auth.Id, searchQuery, limit, offset, sortParam, orderParam, opts)
		} else {
			list, hasMore, err = s.Store.ListComics(e.Auth.Id, limit, offset, sortParam, orderParam, opts)
		}
		if err != nil {
			return e.InternalServerError("failed to list comics", err)
		}
		items := make([]map[string]any, 0, len(list))
		for i := range list {
			if fieldsParam != "" {
				fields := strings.Split(fieldsParam, ",")
				item := comicFieldsSubset(&list[i], fields)
				if containsField(fields, "canUpdate") {
					item["canUpdate"] = s.parserSupportsURL(e.Auth.Id, list[i].URL)
				}
				if containsField(fields, "requiresBrowser") {
					item["requiresBrowser"] = s.parserURLRequiresBrowser(e.Auth.Id, list[i].URL)
				}
				items = append(items, item)
			} else {
				items = append(items, s.comicResponse(&list[i]))
			}
		}
		total := offset + len(items)
		if hasMore {
			total = offset + len(items) + 1
		}
		return v1RespondList(e, http.StatusOK, items, page, perPage, total, hasMore, e.Request.URL.Path)
	}
}

func (sharedComicHandlers) create(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		var in struct {
			SourceLang        string `json:"sourceLanguage"`
			TargetLang        string `json:"targetLanguage"`
			SourceTitle       string `json:"sourceTitle"`
			SourceAuthor      string `json:"sourceAuthor"`
			SourceDescription string `json:"sourceDescription"`
			SourceSeries      string `json:"sourceSeries"`
			SourceNumber      string `json:"sourceNumber"`
			TargetTitle       string `json:"targetTitle"`
			TargetAuthor      string `json:"targetAuthor"`
			TargetDescription string `json:"targetDescription"`
			TargetSeries      string `json:"targetSeries"`
			TargetNumber      string `json:"targetNumber"`
			Notes             string `json:"notes"`
			URL               string `json:"url"`
			Tags              any    `json:"tags"`
		}
		if err := e.BindBody(&in); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		sourceTitle := strings.TrimSpace(in.SourceTitle)
		if sourceTitle == "" {
			return e.BadRequestError("sourceTitle is required", nil)
		}
		comic := &store.Comic{
			SourceLanguage:    in.SourceLang,
			TargetLanguage:    in.TargetLang,
			SourceTitle:       sourceTitle,
			SourceAuthor:      strings.TrimSpace(in.SourceAuthor),
			SourceDescription: strings.TrimSpace(in.SourceDescription),
			SourceSeries:      in.SourceSeries,
			SourceNumber:      in.SourceNumber,
			TargetTitle:       in.TargetTitle,
			TargetAuthor:      in.TargetAuthor,
			TargetDescription: in.TargetDescription,
			TargetSeries:      in.TargetSeries,
			TargetNumber:      in.TargetNumber,
			Notes:             in.Notes,
			URL:               in.URL,
			Status:            "ongoing",
			Tags:              jsonString(in.Tags, "[]"),
		}
		if err := s.Store.CreateComic(e.Auth.Id, comic); err != nil {
			return e.InternalServerError("failed to create comic", err)
		}
		e.Response.Header().Set("Location", "/api/v1/comics/"+comic.ID)
		return v1Respond(e, http.StatusCreated, s.comicResponse(comic), nil, nil)
	}
}

func (sharedComicHandlers) get(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		comic, err := s.Store.GetComicAccessible(e.Auth.Id, e.Request.PathValue("id"))
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		s.Store.PopulateComicReadingStatus(comic, e.Auth.Id)
		return v1Respond(e, http.StatusOK, s.comicResponse(comic), nil, nil)
	}
}

func (sharedComicHandlers) patch(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		patch := map[string]any{}
		if err := e.BindBody(&patch); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		comic, err := s.Store.UpdateComic(e.Auth.Id, e.Request.PathValue("id"), patch)
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		return v1Respond(e, http.StatusOK, s.comicResponse(comic), nil, nil)
	}
}

func (sharedComicHandlers) delete(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		if err := s.Store.DeleteComic(e.Auth.Id, e.Request.PathValue("id")); err != nil {
			return notFoundOrForbidden(e, err)
		}
		return e.NoContent(http.StatusNoContent)
	}
}

func (sharedComicHandlers) recalculateStats(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		comic, err := s.Store.GetOwnedComic(e.Auth.Id, e.Request.PathValue("id"))
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		if err := s.Store.RecalculateComicStats(comic.ID); err != nil {
			return e.InternalServerError("failed to recalculate stats", err)
		}
		reloaded, err := s.Store.GetOwnedComic(e.Auth.Id, comic.ID)
		if err != nil {
			return e.InternalServerError("failed to reload comic", err)
		}
		return v1Respond(e, http.StatusOK, s.comicResponse(reloaded), nil, nil)
	}
}

func (sharedComicHandlers) setVisibility(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		body := struct {
			IsPublic bool `json:"isPublic"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		comic, err := s.Store.SetComicVisibility(e.Auth.Id, e.Request.PathValue("id"), body.IsPublic)
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		return v1Respond(e, http.StatusOK, s.comicResponse(comic), nil, nil)
	}
}

func (sharedComicHandlers) cover(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		if err := e.Request.ParseMultipartForm(32 << 20); err != nil {
			return e.BadRequestError("failed to parse form", err)
		}
		file, header, err := e.Request.FormFile("cover")
		if err != nil {
			return e.BadRequestError("cover file required", err)
		}
		defer file.Close()
		blob, err := io.ReadAll(file)
		if err != nil {
			return e.InternalServerError("failed to read file", err)
		}
		mimeType := header.Header.Get("Content-Type")
		if mimeType == "" {
			mimeType = "image/jpeg"
		}
		comic, err := s.Store.UpdateComicCover(e.Auth.Id, e.Request.PathValue("id"), blob, mimeType)
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		return v1Respond(e, http.StatusOK, s.comicResponse(comic), nil, nil)
	}
}

// coverImage serves the stored cover/thumbnail file. cover and thumbnail are
// Protected file fields, so this cookie-authenticated handler is the
// replacement the frontend's coverPath points at (same contract as the
// novels cover route).
func (sharedComicHandlers) coverImage(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		comicID := e.Request.PathValue("id")
		record, fileName, err := s.Store.GetComicCoverFile(e.Auth.Id, comicID)
		if err != nil {
			if err == store.ErrForbidden {
				return notFoundOrForbidden(e, err)
			}
			// ErrNotFound means either the comic is not visible or it has no
			// stored cover. Only serve the default when the comic itself is
			// accessible.
			if _, aErr := s.Store.GetComicAccessible(e.Auth.Id, comicID); aErr != nil {
				return notFoundOrForbidden(e, aErr)
			}
			return serveDefaultCover(e)
		}
		fsys, err := e.App.NewFilesystem()
		if err != nil {
			return e.InternalServerError("filesystem init failure", err)
		}
		defer fsys.Close()
		// coverPath embeds the PocketBase-generated file name as ?v=, which
		// changes on every cover swap, so URL changes carry the invalidation.
		e.Response.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
		return fsys.Serve(e.Response, e.Request, record.BaseFilesPath()+"/"+fileName, fileName)
	}
}

// full: GET /comics/{id}/full — the comic plus every visible chapter summary.
func (sharedComicHandlers) full(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		comic, err := s.Store.GetComicAccessible(e.Auth.Id, e.Request.PathValue("id"))
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		s.Store.PopulateComicReadingStatus(comic, e.Auth.Id)
		chapters, err := s.Store.ListAllComicChapterSummaries(e.Auth.Id, e.Request.PathValue("id"))
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		return v1Respond(e, http.StatusOK, map[string]any{
			"comic":    s.comicResponse(comic),
			"chapters": comicChapterSummaryRecords(chapters),
		}, nil, nil)
	}
}

// previewFromURL: POST /comics/preview-from-url — fetch the source TOC without
// creating a comic. The snapshot is media-agnostic, so this mirrors the novels
// preview and caches the full chapter list for the import that follows.
func (sharedComicHandlers) previewFromURL(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		body := struct {
			URL string `json:"url"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		if strings.TrimSpace(body.URL) == "" {
			return e.BadRequestError("url is required", nil)
		}
		info, err := s.fetchSourceSnapshot(e.Request.Context(), e.Auth.Id, body.URL)
		if err != nil {
			return writeV1Error(e, http.StatusBadRequest, "bad_request", parserErrorMessage(err))
		}
		cacheKey := e.Auth.Id + ":" + normalizeImportURL(body.URL)
		s.importInfoCacheMu.Lock()
		s.importInfoCache[cacheKey] = importInfoCacheEntry{info: info, createdAt: time.Now()}
		s.importInfoCacheMu.Unlock()
		time.AfterFunc(previewCacheTTL, func() {
			s.importInfoCacheMu.Lock()
			defer s.importInfoCacheMu.Unlock()
			if entry, exists := s.importInfoCache[cacheKey]; exists {
				if time.Since(entry.createdAt) >= previewCacheTTL {
					delete(s.importInfoCache, cacheKey)
				}
			}
		})
		return v1Respond(e, http.StatusOK, map[string]any{
			"title":         info.Title,
			"author":        info.Author,
			"description":   info.Description,
			"coverURL":      info.CoverURL,
			"totalChapters": len(info.Chapters),
			"sourceURL":     body.URL,
		}, nil, nil)
	}
}

// importFromURL: POST /comics/import-from-url — create the comic, download the
// first chapter's pages synchronously, and enqueue a download job for the rest.
func (sharedComicHandlers) importFromURL(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		body := struct {
			URL            string `json:"url"`
			SourceLanguage string `json:"sourceLanguage"`
			TargetLanguage string `json:"targetLanguage"`
			StartChapter   int    `json:"startChapter"`
			EndChapter     int    `json:"endChapter"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		if strings.TrimSpace(body.URL) == "" {
			return e.BadRequestError("url is required", nil)
		}
		sourceLang := strings.TrimSpace(body.SourceLanguage)
		if sourceLang == "" {
			sourceLang = "en"
		}
		targetLang := strings.TrimSpace(body.TargetLanguage)
		if targetLang == "" {
			targetLang = "es"
		}
		cacheKey := e.Auth.Id + ":" + normalizeImportURL(body.URL)
		s.importInfoCacheMu.RLock()
		cachedInfo, cached := s.importInfoCache[cacheKey]
		s.importInfoCacheMu.RUnlock()
		var info *sourceSnapshot
		var err error
		if cached {
			info = cachedInfo.info
			s.importInfoCacheMu.Lock()
			delete(s.importInfoCache, cacheKey)
			s.importInfoCacheMu.Unlock()
		} else {
			info, err = s.fetchSourceSnapshot(e.Request.Context(), e.Auth.Id, body.URL)
			if err != nil {
				return writeV1Error(e, http.StatusBadRequest, "bad_request", parserErrorMessage(err))
			}
		}
		startCh := body.StartChapter
		if startCh < 1 {
			startCh = 1
		}
		endCh := body.EndChapter
		if endCh < startCh || endCh > len(info.Chapters) {
			endCh = len(info.Chapters)
		}
		if startCh < 1 || startCh > len(info.Chapters) {
			return writeV1Error(e, http.StatusBadRequest, "validation_failed", "startChapter is out of range for this comic")
		}

		result, err := s.Store.ImportUrlComic(&store.ImportUrlComicInput{
			OwnerID:           e.Auth.Id,
			URL:               body.URL,
			SourceLanguage:    sourceLang,
			TargetLanguage:    targetLang,
			SourceTitle:       info.Title,
			SourceAuthor:      info.Author,
			SourceDescription: info.Description,
			StartChapter:      startCh,
			EndChapter:        endCh,
		})
		if err != nil {
			return e.InternalServerError("failed to create comic", err)
		}

		// Download the first chapter synchronously (pages → chapter row) so
		// the user lands on a comic with something to read immediately.
		firstRef := info.Chapters[startCh-1]
		firstTitle := ""
		var firstErr error
		if page, perr := s.downloadComicChapterPages(e.Request.Context(), e.Auth.Id, body.URL, store.DownloadComicChapterInfo{
			URL:       firstRef.URL,
			Title:     firstRef.Title,
			SourceKey: firstRef.Key,
		}); perr != nil {
			firstErr = perr
		} else {
			firstTitle = page.Title
			dl := s.ComicDownloaderFactory(e.Auth.Id)
			uploads, uerr := s.downloadComicChapterUploads(e.Request.Context(), dl, page)
			if uerr != nil {
				firstErr = uerr
			} else {
				chTitle := firstTitle
				if chTitle == "" {
					chTitle = firstRef.Title
				}
				if chTitle == "" {
					chTitle = fmt.Sprintf("Capítulo %d", startCh)
				}
				saved, serr := s.Store.UpsertComicChapterWithoutStats(e.Auth.Id, result.Comic.ID, &store.ComicChapter{
					ChapterOrder: startCh,
					Title:        chTitle,
					SourceKey:    firstRef.Key,
					Status:       store.ComicChapterPending,
				})
				if serr != nil {
					firstErr = serr
				} else if rerr := s.Store.ReplaceComicChapterPages(saved.ID, uploads); rerr != nil {
					_ = s.Store.DeleteComicChapterRecord(saved.ID)
					firstErr = rerr
				}
			}
		}
		if firstErr != nil {
			slog.Warn("first comic chapter download failed; it stays pending for the job", "comic", result.Comic.ID, "error", firstErr)
		}

		if info.CoverURL != "" {
			coverBlob, coverMime, coverErr := s.fetchCoverBlob(e.Request.Context(), e.Auth.Id, info.CoverURL)
			if coverErr != nil {
				slog.Warn("failed to download cover", "comic", result.Comic.ID, "error", coverErr)
			} else if err := s.Store.AttachComicCoverBlob(result.Comic.ID, coverBlob, coverMime); err != nil {
				slog.Warn("failed to attach cover", "comic", result.Comic.ID, "error", err)
			}
		}
		if err := s.Store.RecalculateComicStats(result.Comic.ID); err != nil {
			slog.Warn("failed to recalculate comic stats", "comic", result.Comic.ID, "error", err)
		}

		remainingChapters := make([]store.DownloadComicChapterInfo, 0)
		for i := startCh; i < endCh; i++ {
			chURL := info.Chapters[i]
			chTitle := chURL.Title
			if chTitle == "" {
				chTitle = fmt.Sprintf("Capítulo %d", i+1)
			}
			remainingChapters = append(remainingChapters, store.DownloadComicChapterInfo{
				URL:       chURL.URL,
				Title:     chTitle,
				SourceKey: chURL.Key,
			})
		}
		// If the synchronous first chapter failed, it goes back into the job.
		if firstErr != nil {
			remainingChapters = append([]store.DownloadComicChapterInfo{{
				URL:       firstRef.URL,
				Title:     firstRef.Title,
				Order:     startCh,
				SourceKey: firstRef.Key,
			}}, remainingChapters...)
		}
		var downloadJobID string
		if len(remainingChapters) > 0 {
			optionsJSON, _ := json.Marshal(map[string]any{
				"url":            body.URL,
				"chapters":       remainingChapters,
				"startOrder":     startCh + 1,
				"sourceLanguage": sourceLang,
				"targetLanguage": targetLang,
			})
			job := &store.Job{
				ComicID:       result.Comic.ID,
				Status:        "pending",
				Operation:     "download",
				ChapterIDs:    "[]",
				OptionsJSON:   string(optionsJSON),
				TotalChapters: len(remainingChapters),
			}
			if err := s.Store.CreateJob(e.Auth.Id, job); err != nil {
				slog.Error("failed to create download job", "comic", result.Comic.ID, "error", err)
			} else if !s.enqueueJob(job.ID) {
				return e.Error(http.StatusServiceUnavailable, jobQueueFullMessage, nil)
			} else {
				downloadJobID = job.ID
			}
		}
		comic, err := s.Store.GetOwnedComic(e.Auth.Id, result.Comic.ID)
		if err != nil {
			return e.InternalServerError("failed to reload comic", err)
		}
		resp := map[string]any{
			"comic":            s.comicResponse(comic),
			"chaptersImported": 1,
			"totalChapters":    len(info.Chapters),
		}
		if downloadJobID != "" {
			resp["downloadJob"] = map[string]any{
				"id":            downloadJobID,
				"totalChapters": len(remainingChapters),
			}
		}
		e.Response.Header().Set("Location", "/api/v1/comics/"+comic.ID)
		return v1Respond(e, http.StatusCreated, resp, nil, nil)
	}
}

// comicCheckSummary mirrors novelCheckSummary for the comics check-preview.
type comicCheckSummary struct {
	Title           string `json:"title"`
	Author          string `json:"author"`
	CurrentChapters int    `json:"currentChapters"`
	TotalChapters   int    `json:"totalChapters"`
	NewChapters     int    `json:"newChapters"`
	FirstNewChapter int    `json:"firstNewChapter"`
	LastNewChapter  int    `json:"lastNewChapter"`
}

// runComicUpdateCheck fetches the comic's source TOC (priming the preview
// cache), diffs it against the stored chapters and records the check result.
func (s *Server) runComicUpdateCheck(ctx context.Context, userID string, comic *store.Comic) (*comicCheckSummary, *sourceSnapshot, error) {
	snapshot, err := s.fetchSourceSnapshot(ctx, userID, comic.URL)
	if err != nil {
		return nil, nil, err
	}
	cacheKey := userID + ":" + comic.ID
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
	existingOrders, err := s.Store.GetExistingComicChapterOrders(userID, comic.ID)
	if err != nil {
		return nil, nil, newImportStoreError("failed to get existing chapter orders", err)
	}
	existingTitles, err := s.Store.GetExistingComicChapterURLs(userID, comic.ID)
	if err != nil {
		return nil, nil, newImportStoreError("failed to check existing chapters", err)
	}
	existingKeys, err := s.Store.GetExistingComicChapterKeys(userID, comic.ID)
	if err != nil {
		return nil, nil, newImportStoreError("failed to get existing chapter keys", err)
	}
	keyPlan, forceLegacy, err := s.legacySourceKeyPlan(userID, comic.ID, snapshot.Chapters)
	if err != nil {
		return nil, nil, newImportStoreError("failed to plan source keys", err)
	}
	newChapters, _ := diffNovelSnapshot(snapshot, mergeSourceKeys(existingKeys, keyPlan), existingOrders, existingTitles, forceLegacy)
	newAvailable := len(newChapters)
	seen := make(map[int]bool, len(existingOrders))
	for o := range existingOrders {
		seen[o] = true
	}
	firstNew := 0
	lastNew := 0
	for i, ch := range newChapters {
		pos := claimChapterOrder(seen, chapterOrderOf(ch), i+1)
		if firstNew == 0 || pos < firstNew {
			firstNew = pos
		}
		if pos > lastNew {
			lastNew = pos
		}
	}
	if err := s.Store.UpdateComicCheckResult(comic.ID, time.Now().Format(time.RFC3339), newAvailable); err != nil {
		slog.Warn("update comic check result", "comic", comic.ID, "error", err)
	}
	return &comicCheckSummary{
		Title:           snapshot.Title,
		Author:          snapshot.Author,
		CurrentChapters: len(existingTitles),
		TotalChapters:   len(snapshot.Chapters),
		NewChapters:     newAvailable,
		FirstNewChapter: firstNew,
		LastNewChapter:  lastNew,
	}, snapshot, nil
}

func (sharedComicHandlers) checkPreview(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		comic, err := s.Store.GetOwnedComic(e.Auth.Id, e.Request.PathValue("id"))
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		if strings.TrimSpace(comic.URL) == "" {
			return e.BadRequestError("comic has no source URL", nil)
		}
		summary, snapshot, err := s.runComicUpdateCheck(e.Request.Context(), e.Auth.Id, comic)
		if err != nil {
			return mapImportCoreError(e, err)
		}
		return v1Respond(e, http.StatusOK, map[string]any{
			"title":           summary.Title,
			"author":          summary.Author,
			"description":     snapshot.Description,
			"coverURL":        snapshot.CoverURL,
			"sourceURL":       comic.URL,
			"currentChapters": summary.CurrentChapters,
			"totalChapters":   summary.TotalChapters,
			"newChapters":     summary.NewChapters,
			"firstNewChapter": summary.FirstNewChapter,
			"lastNewChapter":  summary.LastNewChapter,
		}, nil, nil)
	}
}

// updateComicFromSource diffs the comic's source TOC against the stored
// chapters and enqueues a download job for the new ones (mirrors
// updateNovelFromSource).
func (s *Server) updateComicFromSource(ctx context.Context, userID string, comic *store.Comic, startChapter, endChapter int) (*novelUpdateResult, error) {
	comicID := comic.ID
	cacheKey := userID + ":" + comicID
	s.previewCacheMu.RLock()
	cached, found := s.previewCache[cacheKey]
	s.previewCacheMu.RUnlock()
	var chapters []sourceChapter
	if found {
		chapters = cached.chapters
		s.previewCacheMu.Lock()
		delete(s.previewCache, cacheKey)
		s.previewCacheMu.Unlock()
	} else {
		snapshot, err := s.fetchSourceSnapshot(ctx, userID, comic.URL)
		if err != nil {
			return nil, err
		}
		chapters = snapshot.Chapters
	}
	existingOrders, err := s.Store.GetExistingComicChapterOrders(userID, comicID)
	if err != nil {
		return nil, newImportStoreError("failed to get existing chapter orders", err)
	}
	existingTitles, err := s.Store.GetExistingComicChapterURLs(userID, comicID)
	if err != nil {
		return nil, newImportStoreError("failed to check existing chapters", err)
	}
	existingKeys, err := s.Store.GetExistingComicChapterKeys(userID, comicID)
	if err != nil {
		return nil, newImportStoreError("failed to get existing chapter keys", err)
	}
	forceLegacy, err := s.backfillLegacySourceKeys(userID, comicID, chapters, existingKeys)
	if err != nil {
		return nil, newImportStoreError("failed to backfill source keys", err)
	}
	newChapters, _ := diffNovelSnapshot(&sourceSnapshot{Chapters: chapters}, existingKeys, existingOrders, existingTitles, forceLegacy)
	sourceToDownload := make([]int, 0)
	for i, ch := range newChapters {
		pos := chapterOrderOf(ch)
		if pos <= 0 {
			pos = i + 1
		}
		if startChapter > 0 && pos < startChapter {
			continue
		}
		if endChapter > 0 && pos > endChapter {
			continue
		}
		sourceToDownload = append(sourceToDownload, i)
	}
	result := &novelUpdateResult{TotalChapters: len(chapters)}
	if len(sourceToDownload) == 0 {
		return result, nil
	}
	downloadChapters := make([]store.DownloadComicChapterInfo, 0, len(sourceToDownload))
	seen := make(map[int]bool, len(existingOrders)+len(sourceToDownload))
	for o := range existingOrders {
		seen[o] = true
	}
	for _, srcIdx := range sourceToDownload {
		ch := newChapters[srcIdx]
		chOrder := claimChapterOrder(seen, chapterOrderOf(ch), srcIdx+1)
		chTitle := ch.Title
		if chTitle == "" {
			chTitle = fmt.Sprintf("Capítulo %d", chOrder)
		}
		downloadChapters = append(downloadChapters, store.DownloadComicChapterInfo{
			URL:       ch.URL,
			Title:     chTitle,
			Order:     chOrder,
			SourceKey: ch.Key,
		})
	}
	result.FirstNewChapter = downloadChapters[0].Order
	result.LastNewChapter = downloadChapters[len(downloadChapters)-1].Order
	optionsJSON, _ := json.Marshal(map[string]any{
		"url":            comic.URL,
		"chapters":       downloadChapters,
		"startOrder":     result.FirstNewChapter,
		"sourceLanguage": comic.SourceLanguage,
		"targetLanguage": comic.TargetLanguage,
	})
	unlock, err := s.acquireComicJobSlot(comicID)
	if err != nil {
		return nil, err
	}
	defer unlock()
	job := &store.Job{
		ComicID:       comicID,
		Status:        "pending",
		Operation:     "download",
		ChapterIDs:    "[]",
		OptionsJSON:   string(optionsJSON),
		TotalChapters: len(downloadChapters),
	}
	if err := s.Store.CreateJob(userID, job); err != nil {
		return nil, newImportStoreError("failed to create download job", err)
	}
	if !s.enqueueJob(job.ID) {
		return nil, errImportQueueFull
	}
	result.JobID = job.ID
	result.ChaptersQueued = len(downloadChapters)
	return result, nil
}

func (sharedComicHandlers) updateFromURL(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		body := struct {
			StartChapter int `json:"startChapter"`
			EndChapter   int `json:"endChapter"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		if body.StartChapter > 0 && body.EndChapter > 0 && body.StartChapter > body.EndChapter {
			return e.BadRequestError("invalid chapter range", nil)
		}
		comic, err := s.Store.GetOwnedComic(e.Auth.Id, e.Request.PathValue("id"))
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		if strings.TrimSpace(comic.URL) == "" {
			return e.BadRequestError("comic has no source URL", nil)
		}
		result, err := s.updateComicFromSource(e.Request.Context(), e.Auth.Id, comic, body.StartChapter, body.EndChapter)
		if err != nil {
			return mapImportCoreError(e, err)
		}
		status := http.StatusAccepted
		if result.ChaptersQueued == 0 {
			status = http.StatusOK
		}
		return v1Respond(e, status, map[string]any{
			"jobId":           result.JobID,
			"totalChapters":   result.TotalChapters,
			"chaptersQueued":  result.ChaptersQueued,
			"firstNewChapter": result.FirstNewChapter,
			"lastNewChapter":  result.LastNewChapter,
		}, nil, nil)
	}
}

// redownloadFromURL: POST /comics/{id}/redownload-from-url — enqueue a job
// that re-fetches the pages of the pending/failed (or explicit) chapters.
func (sharedComicHandlers) redownloadFromURL(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		body := struct {
			ChapterIDs []string `json:"chapterIds"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		comic, err := s.Store.GetOwnedComic(e.Auth.Id, e.Request.PathValue("id"))
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		if strings.TrimSpace(comic.URL) == "" {
			return e.BadRequestError("comic has no source URL", nil)
		}
		chapters, err := s.Store.ListComicChapters(e.Auth.Id, comic.ID)
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		requested := make(map[string]bool, len(body.ChapterIDs))
		for _, id := range body.ChapterIDs {
			requested[id] = true
		}
		planned := make([]store.DownloadComicChapterInfo, 0, len(chapters))
		for _, ch := range chapters {
			if len(requested) > 0 && !requested[ch.ID] {
				continue
			}
			if len(requested) == 0 && ch.Status == store.ComicChapterDone {
				continue
			}
			planned = append(planned, store.DownloadComicChapterInfo{
				URL:       ch.SourceKey,
				Title:     ch.Title,
				Order:     ch.ChapterOrder,
				ChapterID: ch.ID,
				SourceKey: ch.SourceKey,
			})
		}
		// Re-downloads address chapters by their stored source identity; a
		// chapter with no source key cannot be re-fetched from the source.
		filtered := planned[:0]
		for _, ch := range planned {
			if strings.TrimSpace(ch.URL) == "" {
				continue
			}
			filtered = append(filtered, ch)
		}
		if len(filtered) == 0 {
			return e.Error(http.StatusConflict, "No hay capítulos pendientes o fallidos para re-descargar.", nil)
		}
		optionsJSON, _ := json.Marshal(map[string]any{
			"url":        comic.URL,
			"chapters":   filtered,
			"reDownload": true,
		})
		unlock, err := s.acquireComicJobSlot(comic.ID)
		if err != nil {
			return e.Error(http.StatusConflict, comicBusyMessage, nil)
		}
		defer unlock()
		job := &store.Job{
			ComicID:       comic.ID,
			Status:        "pending",
			Operation:     "download",
			ChapterIDs:    "[]",
			OptionsJSON:   string(optionsJSON),
			TotalChapters: len(filtered),
		}
		if err := s.Store.CreateJob(e.Auth.Id, job); err != nil {
			return e.InternalServerError("failed to create download job", err)
		}
		if !s.enqueueJob(job.ID) {
			return e.Error(http.StatusServiceUnavailable, jobQueueFullMessage, nil)
		}
		return v1Respond(e, http.StatusAccepted, map[string]any{
			"jobId":          job.ID,
			"chaptersQueued": len(filtered),
		}, nil, nil)
	}
}

func registerV1ComicRoutes(api *pbrouter.RouterGroup[*core.RequestEvent], s *Server) {
	api.GET("/comics", sharedComics.list(s))
	api.POST("/comics", sharedComics.create(s))
	api.GET("/comics/preview-from-url", sharedComics.previewFromURL(s))
	api.POST("/comics/preview-from-url", sharedComics.previewFromURL(s))
	api.POST("/comics/import-from-url", sharedComics.importFromURL(s))
	api.POST("/comics/import-zip", sharedComicChapters.importZip(s))
	api.GET("/comics/{id}", sharedComics.get(s))
	api.POST("/comics/{id}/recalculate-stats", sharedComics.recalculateStats(s))
	api.PATCH("/comics/{id}", sharedComics.patch(s))
	api.POST("/comics/{id}/cover", sharedComics.cover(s))
	api.GET("/comics/{id}/cover", sharedComics.coverImage(s))
	api.DELETE("/comics/{id}", sharedComics.delete(s))
	api.PATCH("/comics/{id}/visibility", sharedComics.setVisibility(s))
	api.GET("/comics/{id}/full", sharedComics.full(s))
	api.POST("/comics/{id}/check-preview", sharedComics.checkPreview(s))
	api.POST("/comics/{id}/update-from-url", sharedComics.updateFromURL(s))
	api.POST("/comics/{id}/redownload-from-url", sharedComics.redownloadFromURL(s))
	registerV1ComicChapterRoutes(api, s)
}

var _ = errors.New

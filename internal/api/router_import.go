package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
	pbrouter "github.com/pocketbase/pocketbase/tools/router"
	"translator-server/internal/epubimport"
	"translator-server/internal/noveldownloader"
	"translator-server/internal/store"
)

var chapterOrderRegex = regexp.MustCompile(`(\d+)`)

const previewCacheTTL = 30 * time.Minute

type previewCacheEntry struct {
	chapters  []noveldownloader.ChapterURL
	createdAt time.Time
}

type importInfoCacheEntry struct {
	info      *noveldownloader.NovelInfo
	createdAt time.Time
}

// normalizeImportURL returns a canonical key for a source URL so that the
// preview and the import requests (which may differ only in trailing slashes
// or query params) hit the same cache entry.
func normalizeImportURL(url string) string {
	u := strings.TrimSpace(url)
	u = strings.TrimSuffix(u, "/")
	return u
}

type sharedImportHandlers struct{}

var sharedImports = sharedImportHandlers{}

// importEpub: POST /novels:importEpub — multipart upload of an EPUB file.
// Legacy: /api/db/novels/import-epub. Both routes share this handler.
func (sharedImportHandlers) importEpub(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		if err := e.Request.ParseMultipartForm(64 << 20); err != nil {
			return e.BadRequestError("invalid multipart", err)
		}
		file, header, err := e.Request.FormFile("file")
		if err != nil {
			return e.BadRequestError("file required", err)
		}
		defer file.Close()
		blob, err := io.ReadAll(file)
		if err != nil {
			return e.InternalServerError("failed to read file", err)
		}
		parsed, err := epubimport.Parse(blob, header.Filename)
		if err != nil {
			return e.BadRequestError("parse error", err)
		}
		sourceLang := strings.TrimSpace(e.Request.FormValue("sourceLanguage"))
		if sourceLang == "" {
			sourceLang = parsed.Language
		}
		targetLang := strings.TrimSpace(e.Request.FormValue("targetLanguage"))
		if sourceLang == "" || targetLang == "" {
			return e.BadRequestError("sourceLanguage and targetLanguage are required", nil)
		}
		chapters := make([]store.ImportedEpubChapter, len(parsed.Chapters))
		for i, ch := range parsed.Chapters {
			chapters[i] = store.ImportedEpubChapter{Title: ch.Title, Content: ch.Content}
		}
		mimeType := header.Header.Get("Content-Type")
		if mimeType == "" {
			mimeType = mime.TypeByExtension(".epub")
			if mimeType == "" {
				mimeType = "application/epub+zip"
			}
		}
		result, err := s.Store.ImportEpubNovel(&store.ImportEpubNovelInput{OwnerID: e.Auth.Id, FileName: header.Filename, FileBlob: blob, MimeType: mimeType, SourceTitle: parsed.Title, SourceAuthor: parsed.Author, SourceDescription: parsed.Description, SourceLanguage: sourceLang, TargetLanguage: targetLang, SourceSeries: parsed.Series, SourceNumber: parsed.Number, CoverBlob: parsed.CoverBlob, CoverMime: parsed.CoverMime, Chapters: chapters})
		if err != nil {
			return e.InternalServerError("failed to import epub", err)
		}
		body := map[string]any{"novel": s.novelResponse(&result.Novel), "epub": epubRecord(result.Epub), "chaptersImported": result.ChaptersImported}
		if isV1Request(e) {
			e.Response.Header().Set("Location", "/api/v1/novels/"+result.Novel.ID)
			return v1Respond(e, http.StatusCreated, body, nil, nil)
		}
		return e.JSON(http.StatusCreated, body)
	}
}

func (sharedImportHandlers) importZip(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		if err := e.Request.ParseMultipartForm(256 << 20); err != nil {
			return e.BadRequestError("invalid multipart", err)
		}
		file, header, err := e.Request.FormFile("file")
		if err != nil {
			return e.BadRequestError("file required", err)
		}
		defer file.Close()
		blob, err := io.ReadAll(file)
		if err != nil {
			return e.InternalServerError("failed to read file", err)
		}
		reader, err := zip.NewReader(strings.NewReader(string(blob)), int64(len(blob)))
		if err != nil {
			return e.BadRequestError("invalid zip file", err)
		}
		rawEntries := make([]struct {
			name    string
			content []byte
		}, 0)
		for _, f := range reader.File {
			if f.FileInfo().IsDir() {
				continue
			}
			rc, openErr := f.Open()
			if openErr != nil {
				return e.InternalServerError("failed to read zip entry", openErr)
			}
			data, readErr := io.ReadAll(rc)
			rc.Close()
			if readErr != nil {
				return e.InternalServerError("failed to read zip entry", readErr)
			}
			name := strings.TrimLeft(filepath.ToSlash(f.Name), "./")
			rawEntries = append(rawEntries, struct {
				name    string
				content []byte
			}{name, data})
		}
		prefix := detectZipRoot(rawEntries)
		var metadataJSON string
		var coverBlob []byte
		var coverMime string
		type zipFile struct {
			name    string
			content string
		}
		originals := map[string]zipFile{}
		translated := map[string]zipFile{}
		for _, e := range rawEntries {
			normalized := strings.TrimPrefix(e.name, prefix)
			slog.Debug("zip entry", "raw", e.name, "normalized", normalized)
			lower := strings.ToLower(normalized)
			switch {
			case lower == "metadata.json":
				metadataJSON = string(e.content)
			case strings.HasPrefix(lower, "cover."):
				coverBlob = e.content
				ext := strings.ToLower(filepath.Ext(normalized))
				switch ext {
				case ".jpg", ".jpeg":
					coverMime = "image/jpeg"
				case ".png":
					coverMime = "image/png"
				case ".gif":
					coverMime = "image/gif"
				case ".webp":
					coverMime = "image/webp"
				default:
					coverMime = "image/jpeg"
				}
			case strings.HasPrefix(lower, "originals/"):
				name := normalized[len("originals/"):]
				if name != "" {
					originals[name] = zipFile{name: name, content: string(e.content)}
				}
			case strings.HasPrefix(lower, "translated/"):
				name := normalized[len("translated/"):]
				// Empty files (0-byte placeholders) carry no translation: skip
				// them so we never invent a translated title from the filename.
				if name != "" && len(bytes.TrimSpace(e.content)) > 0 {
					translated[name] = zipFile{name: name, content: string(e.content)}
				}
			}
		}
		if metadataJSON == "" {
			return e.BadRequestError("metadata.json is required in the zip", nil)
		}
		if len(originals) == 0 {
			return e.BadRequestError("originals/ directory is required in the zip", nil)
		}
		type namedFile struct {
			name    string
			content string
			number  int
		}
		sorted := make([]namedFile, 0, len(originals))
		for name, f := range originals {
			sorted = append(sorted, namedFile{name: name, content: f.content, number: extractChapterOrder(name)})
		}
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].number < sorted[j].number
		})
		chapters := make([]store.ImportedZipChapter, 0, len(sorted))
		for idx, entry := range sorted {
			title := extractChapterTitle(entry.content, entry.name)
			origContent := contentAfterTitle(entry.content)
			transContent := ""
			transTitle := ""
			if t, ok := translated[entry.name]; ok && strings.TrimSpace(t.content) != "" {
				transContent = contentAfterTitle(t.content)
				transTitle = extractChapterTitle(t.content, entry.name)
			}
			chapters = append(chapters, store.ImportedZipChapter{
				Order:             idx + 1,
				Title:             title,
				TranslatedTitle:   transTitle,
				OriginalContent:   origContent,
				TranslatedContent: transContent,
			})
		}
		result, err := s.Store.ImportZipNovel(&store.ImportZipNovelInput{
			OwnerID:      e.Auth.Id,
			FileName:     header.Filename,
			FileBlob:     blob,
			MetadataJSON: metadataJSON,
			CoverBlob:    coverBlob,
			CoverMime:    coverMime,
			Chapters:     chapters,
		})
		if err != nil {
			return e.InternalServerError("failed to import zip novel", err)
		}
		body := map[string]any{"novel": s.novelResponse(&result.Novel), "chaptersImported": result.ChaptersImported}
		if isV1Request(e) {
			e.Response.Header().Set("Location", "/api/v1/novels/"+result.Novel.ID)
			return v1Respond(e, http.StatusCreated, body, nil, nil)
		}
		return e.JSON(http.StatusCreated, body)
	}
}

// previewFromURL: POST /novels:previewFromUrl — fetch the chapter list without
// creating a novel. Legacy: /api/db/novels/preview-from-url.
func (sharedImportHandlers) previewFromURL(s *Server) func(*core.RequestEvent) error {
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
		info, err := s.getNovelInfoWithFallback(e.Request.Context(), e.Auth.Id, body.URL)
		if err != nil {
			return e.BadRequestError(err.Error(), nil)
		}
		// Cache the full list (with chapter URLs) so the subsequent import
		// request reuses it instead of re-scraping every chapter page.
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
		body2 := map[string]any{
			"title":         info.Title,
			"author":        info.Author,
			"description":   info.Description,
			"coverURL":      info.CoverURL,
			"totalChapters": len(info.Chapters),
			"sourceURL":     info.SourceURL,
		}
		if isV1Request(e) {
			return v1Respond(e, http.StatusOK, body2, nil, nil)
		}
		return e.JSON(http.StatusOK, body2)
	}
}

func (sharedImportHandlers) importFromURL(s *Server) func(*core.RequestEvent) error {
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
		// Reuse the full chapter list cached by the preview request instead
		// of re-scraping every chapter page again.
		cacheKey := e.Auth.Id + ":" + normalizeImportURL(body.URL)
		s.importInfoCacheMu.RLock()
		cachedInfo, cached := s.importInfoCache[cacheKey]
		s.importInfoCacheMu.RUnlock()

		var info *noveldownloader.NovelInfo
		var err error
		if cached {
			info = cachedInfo.info
			s.importInfoCacheMu.Lock()
			delete(s.importInfoCache, cacheKey)
			s.importInfoCacheMu.Unlock()
		} else {
			info, err = s.getNovelInfoWithFallback(e.Request.Context(), e.Auth.Id, body.URL)
			if err != nil {
				return e.BadRequestError(err.Error(), nil)
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

		var firstChapter []noveldownloader.Chapter
		dl := s.DownloaderFactory(e.Auth.Id)
		parser := dl.FindParser(body.URL)

		if parser != nil {
			firstChapter, err = dl.DownloadChapters(e.Request.Context(), info.Chapters, startCh, startCh)
			if err != nil && s.HasBrowserWorkerForUser(e.Auth.Id) {
				slog.Info("direct HTTP chapter download failed, retrying via browser proxy", "error", err)
				proxyDL := s.DownloaderFactoryWithClient(NewProxyHTTPClient(s, e.Auth.Id))
				firstChapter, err = proxyDL.DownloadChapters(e.Request.Context(), info.Chapters, startCh, startCh)
			}
		} else if s.HasBrowserWorkerForUser(e.Auth.Id) {
			proxyDL := s.DownloaderFactoryWithClient(NewProxyHTTPClient(s, e.Auth.Id))
			firstChapter, err = proxyDL.DownloadChapters(e.Request.Context(), info.Chapters, startCh, startCh)
		} else {
			return e.InternalServerError("failed to download first chapter", fmt.Errorf("no download method available"))
		}
		if err != nil {
			return e.InternalServerError("failed to download first chapter", err)
		}
		if len(firstChapter) == 0 {
			return e.InternalServerError("failed to download first chapter", fmt.Errorf("no content returned"))
		}
		result, err := s.Store.ImportUrlNovel(&store.ImportUrlNovelInput{
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
			return e.InternalServerError("failed to create novel", err)
		}
		ch := firstChapter[0]
		chTitle := ch.Title
		if chTitle == "" {
			chTitle = fmt.Sprintf("Capítulo %d", startCh)
		}
		if _, err := s.Store.UpsertChapterWithoutStats(e.Auth.Id, result.Novel.ID, &store.Chapter{
			ChapterOrder:    startCh,
			Title:           chTitle,
			OriginalContent: ch.Markdown,
			Status:          "pending",
		}); err != nil {
			return e.InternalServerError("failed to save chapter", err)
		}

		if info.CoverURL != "" {
			coverBlob, coverMime, coverErr := dl.DownloadCover(e.Request.Context(), info.CoverURL)
			if coverErr != nil && s.HasBrowserWorkerForUser(e.Auth.Id) {
				slog.Info("direct cover download failed, retrying via browser worker", "novel", result.Novel.ID, "error", coverErr)
				coverBlob, coverMime, coverErr = s.FetchImageViaWorker(e.Request.Context(), info.CoverURL, e.Auth.Id, 60)
			}
			if coverErr != nil {
				slog.Warn("failed to download cover", "novel", result.Novel.ID, "error", coverErr)
			} else if err := s.Store.AttachCoverBlob(result.Novel.ID, coverBlob, coverMime); err != nil {
				slog.Warn("failed to attach cover", "novel", result.Novel.ID, "error", err)
			}
		}
		if err := s.Store.RecalculateNovelStats(result.Novel.ID); err != nil {
			slog.Warn("failed to recalculate novel stats", "novel", result.Novel.ID, "error", err)
		}
		remainingChapters := make([]store.DownloadChapterInfo, 0)
		for i := startCh; i < endCh; i++ {
			chURL := info.Chapters[i]
			chTitle := chURL.Title
			if chTitle == "" {
				chTitle = fmt.Sprintf("Capítulo %d", i+1)
			}
			remainingChapters = append(remainingChapters, store.DownloadChapterInfo{
				URL:   chURL.URL,
				Title: chTitle,
			})
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
				NovelID:       result.Novel.ID,
				Status:        "pending",
				Operation:     "download",
				ChapterIDs:    "[]",
				OptionsJSON:   string(optionsJSON),
				TotalChapters: len(remainingChapters),
			}
			if err := s.Store.CreateJob(e.Auth.Id, job); err != nil {
				slog.Error("failed to create download job", "novel", result.Novel.ID, "error", err)
			} else {
				if !s.enqueueJob(job.ID) {
					return e.Error(http.StatusServiceUnavailable, jobQueueFullMessage, nil)
				}
				downloadJobID = job.ID
			}
		}
		novel, err := s.Store.GetOwnedNovel(e.Auth.Id, result.Novel.ID)
		if err != nil {
			return e.InternalServerError("failed to reload novel", err)
		}
		resp := map[string]any{
			"novel":            s.novelResponse(novel),
			"chaptersImported": 1,
			"totalChapters":    len(info.Chapters),
		}
		if downloadJobID != "" {
			resp["downloadJob"] = map[string]any{
				"id":            downloadJobID,
				"totalChapters": len(remainingChapters),
			}
		}
		if isV1Request(e) {
			e.Response.Header().Set("Location", "/api/v1/novels/"+novel.ID)
			return v1Respond(e, http.StatusCreated, resp, nil, nil)
		}
		return e.JSON(http.StatusCreated, resp)
	}
}

// checkPreview: GET /novels/{id}/update-preview (legacy) becomes
// POST /novels/{id}:checkPreview on v1 because the original writes
// lastCheckedAt and must not be a safe verb.
func (sharedImportHandlers) checkPreview(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		novelID := e.Request.PathValue("id")
		novel, err := s.Store.GetOwnedNovel(e.Auth.Id, novelID)
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		if strings.TrimSpace(novel.URL) == "" {
			return e.BadRequestError("novel has no source URL", nil)
		}
		dl := s.DownloaderFactory(e.Auth.Id)
		info, err := dl.GetNovelInfo(e.Request.Context(), novel.URL)
		if err != nil {
			return e.InternalServerError("failed to fetch novel info", err)
		}
		cacheKey := e.Auth.Id + ":" + novelID
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
		existingOrders, err := s.Store.GetExistingChapterOrders(e.Auth.Id, novelID)
		if err != nil {
			return e.InternalServerError("failed to get existing chapter orders", err)
		}
		existingTitles, err := s.Store.GetExistingChapterURLs(e.Auth.Id, novelID)
		if err != nil {
			return e.InternalServerError("failed to check existing chapters", err)
		}
		newAvailable := 0
		firstNew := 0
		lastNew := 0
		for _, ch := range info.Chapters {
			chNum := chapterOrderOf(ch)
			if chNum > 0 && existingOrders[chNum] {
				continue
			}
			if existingTitles[ch.Title] {
				continue
			}
			newAvailable++
			if chNum > 0 {
				if firstNew == 0 || chNum < firstNew {
					firstNew = chNum
				}
				if chNum > lastNew {
					lastNew = chNum
				}
			}
		}
		if err := s.Store.UpdateNovelCheckResult(novelID, time.Now().Format(time.RFC3339), newAvailable); err != nil {
			slog.Warn("update novel check result", "novel", novelID, "error", err)
		}
		body := map[string]any{
			"title":           info.Title,
			"author":          info.Author,
			"description":     info.Description,
			"coverURL":        info.CoverURL,
			"sourceURL":       info.SourceURL,
			"currentChapters": len(existingTitles),
			"totalChapters":   len(info.Chapters),
			"newChapters":     newAvailable,
			"firstNewChapter": firstNew,
			"lastNewChapter":  lastNew,
		}
		if isV1Request(e) {
			return v1Respond(e, http.StatusOK, body, nil, nil)
		}
		return e.JSON(http.StatusOK, body)
	}
}

func (sharedImportHandlers) updateFromURL(s *Server) func(*core.RequestEvent) error {
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
		novelID := e.Request.PathValue("id")
		novel, err := s.Store.GetOwnedNovel(e.Auth.Id, novelID)
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		if strings.TrimSpace(novel.URL) == "" {
			return e.BadRequestError("novel has no source URL", nil)
		}
		cacheKey := e.Auth.Id + ":" + novelID
		s.previewCacheMu.RLock()
		cached, found := s.previewCache[cacheKey]
		s.previewCacheMu.RUnlock()

		var chapters []noveldownloader.ChapterURL
		if found {
			chapters = cached.chapters
			s.previewCacheMu.Lock()
			delete(s.previewCache, cacheKey)
			s.previewCacheMu.Unlock()
		} else {
			dl := s.DownloaderFactory(e.Auth.Id)
			info, err := dl.GetNovelInfo(e.Request.Context(), novel.URL)
			if err != nil {
				return e.InternalServerError("failed to fetch novel info", err)
			}
			chapters = info.Chapters
		}
		existingOrders, err := s.Store.GetExistingChapterOrders(e.Auth.Id, novelID)
		if err != nil {
			return e.InternalServerError("failed to get existing chapter orders", err)
		}
		existingTitles, err := s.Store.GetExistingChapterURLs(e.Auth.Id, novelID)
		if err != nil {
			return e.InternalServerError("failed to check existing chapters", err)
		}
		sourceToDownload := make([]int, 0)
		for i, ch := range chapters {
			chNum := chapterOrderOf(ch)
			if chNum > 0 && existingOrders[chNum] {
				continue
			}
			if existingTitles[ch.Title] {
				continue
			}
			pos := chNum
			if pos <= 0 {
				pos = i + 1
			}
			if body.StartChapter > 0 && pos < body.StartChapter {
				continue
			}
			if body.EndChapter > 0 && pos > body.EndChapter {
				continue
			}
			sourceToDownload = append(sourceToDownload, i)
		}
		if len(sourceToDownload) == 0 {
			resp := map[string]any{"chaptersAdded": 0, "chapters": []map[string]any{}, "totalChapters": len(chapters), "message": "No hay capítulos nuevos. La novela ya está al día."}
			if isV1Request(e) {
				return v1Respond(e, http.StatusOK, resp, nil, nil)
			}
			return e.JSON(http.StatusOK, resp)
		}
		downloadChapters := make([]store.DownloadChapterInfo, 0, len(sourceToDownload))
		for _, srcIdx := range sourceToDownload {
			ch := chapters[srcIdx]
			chTitle := ch.Title
			if chTitle == "" {
				chTitle = fmt.Sprintf("Capítulo %d", srcIdx+1)
			}
			chOrder := chapterOrderOf(ch)
			if chOrder <= 0 {
				chOrder = srcIdx + 1
			}
			downloadChapters = append(downloadChapters, store.DownloadChapterInfo{
				URL:   ch.URL,
				Title: chTitle,
				Order: chOrder,
			})
		}
		firstNewOrder := chapterOrderOf(chapters[sourceToDownload[0]])
		if firstNewOrder <= 0 {
			firstNewOrder = sourceToDownload[0] + 1
		}
		optionsJSON, _ := json.Marshal(map[string]any{
			"url":            novel.URL,
			"chapters":       downloadChapters,
			"startOrder":     firstNewOrder,
			"sourceLanguage": novel.SourceLanguage,
			"targetLanguage": novel.TargetLanguage,
		})
		job := &store.Job{
			NovelID:       novelID,
			Status:        "pending",
			Operation:     "download",
			ChapterIDs:    "[]",
			OptionsJSON:   string(optionsJSON),
			TotalChapters: len(downloadChapters),
		}
		if err := s.Store.CreateJob(e.Auth.Id, job); err != nil {
			return e.InternalServerError("failed to create download job", err)
		}
		if !s.enqueueJob(job.ID) {
			return e.Error(http.StatusServiceUnavailable, jobQueueFullMessage, nil)
		}
		resp := map[string]any{
			"chaptersAdded":   0,
			"chapters":        []map[string]any{},
			"totalChapters":   len(chapters),
			"pendingChapters": len(downloadChapters),
			"downloadJobId":   job.ID,
			"message":         fmt.Sprintf("Descarga iniciada. %d capítulos se están descargando en segundo plano.", len(downloadChapters)),
		}
		if isV1Request(e) {
			return v1Respond(e, http.StatusAccepted, resp, nil, nil)
		}
		return e.JSON(http.StatusOK, resp)
	}
}

func (sharedImportHandlers) redownloadFromURL(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		body := struct {
			StartChapter int  `json:"startChapter"`
			EndChapter   int  `json:"endChapter"`
			Confirm      bool `json:"confirm"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		if body.StartChapter > 0 && body.EndChapter > 0 && body.StartChapter > body.EndChapter {
			return e.BadRequestError("invalid chapter range", nil)
		}
		novelID := e.Request.PathValue("id")
		novel, err := s.Store.GetOwnedNovel(e.Auth.Id, novelID)
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		if strings.TrimSpace(novel.URL) == "" {
			return e.BadRequestError("novel has no source URL", nil)
		}
		existing, err := s.Store.ListChaptersAccessible(e.Auth.Id, novelID)
		if err != nil {
			return e.InternalServerError("failed to load chapters", err)
		}
		byOrder := make(map[int]store.Chapter, len(existing))
		byTitle := make(map[string]store.Chapter, len(existing))
		for _, ch := range existing {
			byOrder[ch.ChapterOrder] = ch
			if ch.Title != "" {
				byTitle[ch.Title] = ch
			}
		}

		// The first request (confirm=false) is a preview: it fetches the source
		// chapter list and, when original titles no longer line up with the
		// stored ones, asks the user to confirm before a job is created. The
		// confirmed request (confirm=true) reuses the cached list.
		cacheKey := e.Auth.Id + ":" + novelID + ":redownload"
		var chapters []noveldownloader.ChapterURL
		loadFresh := func() error {
			dl := s.DownloaderFactory(e.Auth.Id)
			info, err := dl.GetNovelInfo(e.Request.Context(), novel.URL)
			if err != nil {
				return e.InternalServerError("failed to fetch novel info", err)
			}
			chapters = info.Chapters
			return nil
		}
		if !body.Confirm {
			if err := loadFresh(); err != nil {
				return err
			}
		} else {
			s.previewCacheMu.RLock()
			cached, ok := s.previewCache[cacheKey]
			s.previewCacheMu.RUnlock()
			if ok {
				chapters = cached.chapters
				s.previewCacheMu.Lock()
				delete(s.previewCache, cacheKey)
				s.previewCacheMu.Unlock()
			} else if err := loadFresh(); err != nil {
				return err
			}
		}

		plan := planRedownload(chapters, byOrder, byTitle, body.StartChapter, body.EndChapter)
		if len(plan.chapters) == 0 {
			resp := map[string]any{
				"pendingChapters": 0,
				"message":         "No se encontraron capítulos para re-descargar. Verifica que la novela tenga capítulos o que el rango sea válido.",
			}
			if isV1Request(e) {
				return v1Respond(e, http.StatusOK, resp, nil, nil)
			}
			return e.JSON(http.StatusOK, resp)
		}
		if !body.Confirm && len(plan.mismatches) > 0 {
			// Cache the fresh list so the confirmed request does not re-scrape.
			s.previewCacheMu.Lock()
			s.previewCache[cacheKey] = previewCacheEntry{chapters: chapters, createdAt: time.Now()}
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
			resp := map[string]any{
				"pendingChapters":   len(plan.chapters),
				"titleMismatches":   len(plan.mismatches),
				"needsConfirmation": true,
				"chapters":          plan.mismatches,
			}
			if isV1Request(e) {
				return v1Respond(e, http.StatusOK, resp, nil, nil)
			}
			return e.JSON(http.StatusOK, resp)
		}

		// Serialize check + create + enqueue per novel so two concurrent
		// redownload requests cannot both pass the active-jobs check.
		unlockNovel := s.lockNovel(novelID)
		defer unlockNovel()

		activeJobs, err := s.Store.ListActiveNovelJobs(novelID)
		if err != nil {
			return e.InternalServerError("failed to check active jobs", err)
		}
		if len(activeJobs) > 0 {
			return e.Error(http.StatusConflict,
				"No se puede re-descargar: ya hay otro trabajo en curso para esta novela. Espera a que termine e inténtalo de nuevo.", nil)
		}

		optionsJSON, _ := json.Marshal(map[string]any{
			"url":            novel.URL,
			"chapters":       plan.chapters,
			"startOrder":     plan.chapters[0].Order,
			"sourceLanguage": novel.SourceLanguage,
			"targetLanguage": novel.TargetLanguage,
			"reDownload":     true,
		})
		job := &store.Job{
			NovelID:       novelID,
			Status:        "pending",
			Operation:     "download",
			ChapterIDs:    "[]",
			OptionsJSON:   string(optionsJSON),
			TotalChapters: len(plan.chapters),
		}
		if err := s.Store.CreateJob(e.Auth.Id, job); err != nil {
			return e.InternalServerError("failed to create download job", err)
		}
		if !s.enqueueJob(job.ID) {
			return e.Error(http.StatusServiceUnavailable, jobQueueFullMessage, nil)
		}
		resp := map[string]any{
			"pendingChapters": len(plan.chapters),
			"downloadJobId":   job.ID,
			"message":         fmt.Sprintf("Re-descarga iniciada. %d capítulos se actualizarán en segundo plano conservando sus traducciones.", len(plan.chapters)),
		}
		if isV1Request(e) {
			return v1Respond(e, http.StatusAccepted, resp, nil, nil)
		}
		return e.JSON(http.StatusOK, resp)
	}
}

func (sharedImportHandlers) checkBatchUpdates(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		novels, err := s.Store.ListOwnedNovelsWithURL(e.Auth.Id)
		if err != nil {
			return e.InternalServerError("failed to list novels", err)
		}
		if len(novels) == 0 {
			resp := store.BatchCheckResponse{
				Results: []store.BatchCheckNovelResult{},
				Checked: 0, WithUpdates: 0, Errors: 0,
			}
			if isV1Request(e) {
				return v1Respond(e, http.StatusOK, resp, nil, nil)
			}
			return e.JSON(http.StatusOK, resp)
		}
		dl := s.DownloaderFactory(e.Auth.Id)
		supported := make([]store.Novel, 0, len(novels))
		for _, n := range novels {
			if dl.IsSupportedURL(n.URL) {
				supported = append(supported, n)
			}
		}
		if len(supported) == 0 {
			resp := store.BatchCheckResponse{
				Results: []store.BatchCheckNovelResult{},
				Checked: 0, WithUpdates: 0, Errors: 0,
			}
			if isV1Request(e) {
				return v1Respond(e, http.StatusOK, resp, nil, nil)
			}
			return e.JSON(http.StatusOK, resp)
		}
		results := make([]store.BatchCheckNovelResult, 0, len(supported))
		checked := 0
		withUpdates := 0
		errCount := 0
		for i, novel := range supported {
			if i > 0 {
				if err := dl.SleepBetweenChapters(e.Request.Context()); err != nil {
					break
				}
			}
			info, err := dl.GetNovelInfo(e.Request.Context(), novel.URL)
			if err != nil {
				errCount++
				results = append(results, store.BatchCheckNovelResult{
					NovelID: novel.ID, SourceTitle: novel.SourceTitle,
					Error: err.Error(),
				})
				continue
			}
			existingOrders, err := s.Store.GetExistingChapterOrders(e.Auth.Id, novel.ID)
			if err != nil {
				errCount++
				results = append(results, store.BatchCheckNovelResult{
					NovelID: novel.ID, SourceTitle: novel.SourceTitle,
					Error: err.Error(),
				})
				continue
			}
			existingTitles, err := s.Store.GetExistingChapterURLs(e.Auth.Id, novel.ID)
			if err != nil {
				errCount++
				results = append(results, store.BatchCheckNovelResult{
					NovelID: novel.ID, SourceTitle: novel.SourceTitle,
					Error: err.Error(),
				})
				continue
			}
			newCh := make([]store.DownloadChapterInfo, 0)
			newAvailable := 0
			firstNew := 0
			lastNew := 0
			startOrder := 0
			for srcIdx, ch := range info.Chapters {
				chNum := chapterOrderOf(ch)
				if chNum > 0 && existingOrders[chNum] {
					continue
				}
				if existingTitles[ch.Title] {
					continue
				}
				newAvailable++
				pos := chNum
				if pos <= 0 {
					pos = srcIdx + 1
				}
				if startOrder == 0 {
					startOrder = pos
				}
				if chNum > 0 {
					if firstNew == 0 || chNum < firstNew {
						firstNew = chNum
					}
					if chNum > lastNew {
						lastNew = chNum
					}
				}
				chTitle := ch.Title
				if chTitle == "" {
					chTitle = fmt.Sprintf("Capítulo %d", pos)
				}
				chOrder := chapterOrderOf(ch)
				if chOrder <= 0 {
					chOrder = pos
				}
				newCh = append(newCh, store.DownloadChapterInfo{
					URL:   ch.URL,
					Title: chTitle,
					Order: chOrder,
				})
			}
			checked++
			if err := s.Store.UpdateNovelCheckResult(novel.ID, time.Now().Format(time.RFC3339), newAvailable); err != nil {
				slog.Warn("update novel check result", "novel", novel.ID, "error", err)
			}
			if newAvailable > 0 {
				withUpdates++
			}
			if newAvailable == 0 {
				continue
			}
			results = append(results, store.BatchCheckNovelResult{
				NovelID:         novel.ID,
				SourceTitle:     novel.SourceTitle,
				SourceAuthor:    novel.SourceAuthor,
				CoverURL:        info.CoverURL,
				NewChapters:     newAvailable,
				FirstNewChapter: firstNew,
				LastNewChapter:  lastNew,
				StartOrder:      startOrder,
				CurrentChapters: len(existingTitles),
				TotalChapters:   len(info.Chapters),
				NewChapterInfo:  newCh,
			})
		}
		resp := store.BatchCheckResponse{
			Results: results, Checked: checked,
			WithUpdates: withUpdates, Errors: errCount,
		}
		if isV1Request(e) {
			return v1Respond(e, http.StatusOK, resp, nil, nil)
		}
		return e.JSON(http.StatusOK, resp)
	}
}

func (sharedImportHandlers) batchUpdate(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		body := struct {
			Selections []store.BatchUpdateSelection `json:"selections"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		if len(body.Selections) == 0 {
			return e.BadRequestError("selections required", nil)
		}
		for _, sel := range body.Selections {
			if sel.StartChapter > 0 && sel.EndChapter > 0 && sel.StartChapter > sel.EndChapter {
				return e.BadRequestError("invalid chapter range", nil)
			}
		}
		jobs := make([]store.BatchUpdateJobResult, 0, len(body.Selections))
		totalPending := 0
		for _, sel := range body.Selections {
			novel, err := s.Store.GetOwnedNovel(e.Auth.Id, sel.NovelID)
			if err != nil {
				continue
			}
			chaptersToDownload := sel.NewChapterInfo
			if sel.StartChapter > 0 || sel.EndChapter > 0 {
				filtered := make([]store.DownloadChapterInfo, 0)
				for _, ch := range sel.NewChapterInfo {
					order := ch.Order
					if order <= 0 {
						order = extractChapterOrder(ch.Title)
					}
					if order <= 0 {
						order = sel.StartOrder + len(filtered)
					}
					if sel.StartChapter > 0 && order < sel.StartChapter {
						continue
					}
					if sel.EndChapter > 0 && order > sel.EndChapter {
						continue
					}
					filtered = append(filtered, ch)
				}
				chaptersToDownload = filtered
			}
			if len(chaptersToDownload) == 0 {
				continue
			}
			firstOrder := chaptersToDownload[0].Order
			if firstOrder <= 0 {
				firstOrder = extractChapterOrder(chaptersToDownload[0].Title)
			}
			if firstOrder <= 0 {
				firstOrder = sel.StartOrder
			}
			optionsJSON, _ := json.Marshal(map[string]any{
				"url":            novel.URL,
				"chapters":       chaptersToDownload,
				"startOrder":     firstOrder,
				"sourceLanguage": novel.SourceLanguage,
				"targetLanguage": novel.TargetLanguage,
			})
			job := &store.Job{
				NovelID:       sel.NovelID,
				Status:        "pending",
				Operation:     "download",
				ChapterIDs:    "[]",
				OptionsJSON:   string(optionsJSON),
				TotalChapters: len(chaptersToDownload),
			}
			if err := s.Store.CreateJob(e.Auth.Id, job); err != nil {
				continue
			}
			enqueueFailed := !s.enqueueJob(job.ID)
			jobs = append(jobs, store.BatchUpdateJobResult{
				NovelID:         sel.NovelID,
				JobID:           job.ID,
				PendingChapters: len(chaptersToDownload),
				EnqueueFailed:   enqueueFailed,
			})
			if !enqueueFailed {
				totalPending += len(chaptersToDownload)
			}
		}
		resp := store.BatchUpdateResponse{
			Jobs: jobs, TotalPending: totalPending,
		}
		if isV1Request(e) {
			return v1Respond(e, http.StatusAccepted, resp, nil, nil)
		}
		return e.JSON(http.StatusOK, resp)
	}
}

func (sharedImportHandlers) batchTranslatePreview(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		novels, err := s.Store.ListOwnedNovelsWithTranslationStats(e.Auth.Id)
		if err != nil {
			return e.InternalServerError("failed to list novels", err)
		}
		results := make([]store.BatchTranslateNovelResult, 0, len(novels))
		withPending := 0
		for _, novel := range novels {
			pendingChapters := novel.ChapterCount - novel.TranslatedCount
			if pendingChapters < 0 {
				pendingChapters = 0
			}
			hasOriginal := novel.OriginalCharCount > 0
			result := store.BatchTranslateNovelResult{
				NovelID:            novel.ID,
				SourceTitle:        novel.SourceTitle,
				SourceAuthor:       novel.SourceAuthor,
				CoverURL:           novel.CoverPath,
				PendingChapters:    pendingChapters,
				TotalChapters:      novel.ChapterCount,
				TranslatedCount:    novel.TranslatedCount,
				CompletedCount:     novel.CompletedCount,
				HasOriginalContent: hasOriginal,
			}
			if pendingChapters > 0 {
				withPending++
				results = append(results, result)
			}
		}
		resp := store.BatchTranslateResponse{
			Results:     results,
			TotalNovels: len(results),
			WithPending: withPending,
		}
		if isV1Request(e) {
			return v1Respond(e, http.StatusOK, resp, nil, nil)
		}
		return e.JSON(http.StatusOK, resp)
	}
}

func (sharedImportHandlers) batchTranslate(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		body := struct {
			Selections []store.BatchTranslateSelection `json:"selections"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		if len(body.Selections) == 0 {
			return e.BadRequestError("selections required", nil)
		}
		jobs := make([]store.BatchTranslateJobResult, 0, len(body.Selections))
		totalPending := 0
		for _, sel := range body.Selections {
			novel, err := s.Store.GetOwnedNovel(e.Auth.Id, sel.NovelID)
			if err != nil {
				continue
			}
			var chapterIDs []string
			if len(sel.ChapterIDs) > 0 {
				chapterIDs = sel.ChapterIDs
			} else {
				pending, err := s.Store.GetOwnedNovelChapterIDsByStatus(e.Auth.Id, sel.NovelID)
				if err != nil || len(pending) == 0 {
					continue
				}
				chapterIDs = pending
			}
			idsJSON, _ := json.Marshal(chapterIDs)
			job := &store.Job{
				NovelID:       sel.NovelID,
				Status:        "pending",
				Operation:     "translate",
				ChapterIDs:    string(idsJSON),
				TotalChapters: len(chapterIDs),
			}
			if err := s.Store.CreateJob(e.Auth.Id, job); err != nil {
				continue
			}
			if chapters, _, err := s.Store.LoadJobChapters(job); err == nil {
				chIDs := make([]string, 0, len(chapters))
				for _, chapter := range chapters {
					chIDs = append(chIDs, chapter.ID)
				}
				if err := s.Store.UpdateChaptersStatusFast(chIDs, "processing", ""); err != nil {
					slog.Warn("mark chapters processing for batch translate", "novel", sel.NovelID, "jobId", job.ID, "error", err)
				}
			}
			enqueueFailed := !s.enqueueJob(job.ID)
			if enqueueFailed {
				if err := s.Store.ReconcileProcessingChaptersForJob(job.ID); err != nil {
					slog.Warn("reconcile chapters after batch queue rejection", "jobId", job.ID, "error", err)
				}
			}
			jobs = append(jobs, store.BatchTranslateJobResult{
				NovelID:         sel.NovelID,
				JobID:           job.ID,
				PendingChapters: len(chapterIDs),
				EnqueueFailed:   enqueueFailed,
			})
			if !enqueueFailed {
				totalPending += len(chapterIDs)
			}
			_ = novel
		}
		resp := store.BatchTranslateStartResponse{
			Jobs: jobs, TotalPending: totalPending,
		}
		if isV1Request(e) {
			return v1Respond(e, http.StatusAccepted, resp, nil, nil)
		}
		return e.JSON(http.StatusOK, resp)
	}
}

func (sharedImportHandlers) batchCheck(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		body := struct {
			NovelIDs []string `json:"novelIds"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		if len(body.NovelIDs) == 0 {
			return e.BadRequestError("novelIds required", nil)
		}
		type checkResult struct {
			NovelID string `json:"novelId"`
			JobID   string `json:"jobId"`
			Error   string `json:"error,omitempty"`
		}
		results := make([]checkResult, 0, len(body.NovelIDs))
		for _, novelID := range body.NovelIDs {
			novel, err := s.Store.GetOwnedNovel(e.Auth.Id, novelID)
			if err != nil {
				continue
			}
			if novel.URL == "" {
				continue
			}
			optionsJSON, _ := json.Marshal(map[string]any{"url": novel.URL})
			job := &store.Job{
				NovelID:     novelID,
				Status:      "pending",
				Operation:   "check",
				OptionsJSON: string(optionsJSON),
			}
			if err := s.Store.CreateJob(e.Auth.Id, job); err != nil {
				continue
			}
			result := checkResult{NovelID: novelID, JobID: job.ID}
			if !s.enqueueJob(job.ID) {
				result.Error = jobQueueFullMessage
			}
			results = append(results, result)
		}
		resp := map[string]any{"jobs": results}
		if isV1Request(e) {
			return v1Respond(e, http.StatusAccepted, resp, nil, nil)
		}
		return e.JSON(http.StatusOK, resp)
	}
}

func registerImportRoutes(api *pbrouter.RouterGroup[*core.RequestEvent], s *Server) {
	api.POST("/db/novels/import-epub", sharedImports.importEpub(s))
	api.POST("/db/novels/import-from-zip", sharedImports.importZip(s))
	api.POST("/db/novels/preview-from-url", sharedImports.previewFromURL(s))
	api.POST("/db/novels/import-from-url", sharedImports.importFromURL(s))
	api.GET("/db/novels/{id}/update-preview", sharedImports.checkPreview(s))
	api.POST("/db/novels/{id}/update-from-url", sharedImports.updateFromURL(s))
	api.POST("/db/novels/{id}/redownload-from-url", sharedImports.redownloadFromURL(s))
	api.GET("/db/novels/check-batch-updates", sharedImports.checkBatchUpdates(s))
	api.POST("/db/novels/batch-update-from-url", sharedImports.batchUpdate(s))
	api.GET("/db/novels/batch-translate-preview", sharedImports.batchTranslatePreview(s))
	api.POST("/db/novels/batch-translate", sharedImports.batchTranslate(s))
	api.POST("/db/novels/batch-check", sharedImports.batchCheck(s))
}

func registerV1ImportRoutes(api *pbrouter.RouterGroup[*core.RequestEvent], s *Server) {
	// Import verbs at /novels/{action} (and /novels/{id}/{action}) — kept
	// under the /novels resource for discoverability. PocketBase's router
	// inherits Go's net/http pattern syntax which only supports {name}
	// wildcards, not the AIP-style {resource}:{action} colon form.
	api.POST("/novels/import-epub", sharedImports.importEpub(s))
	api.POST("/novels/import-zip", sharedImports.importZip(s))
	api.POST("/novels/preview-from-url", sharedImports.previewFromURL(s))
	api.POST("/novels/import-from-url", sharedImports.importFromURL(s))
	// checkPreview is POST: it writes lastCheckedAt and is not idempotent
	// (calling it twice advances the timestamp).
	api.POST("/novels/{id}/check-preview", sharedImports.checkPreview(s))
	api.POST("/novels/{id}/update-from-url", sharedImports.updateFromURL(s))
	api.POST("/novels/{id}/redownload-from-url", sharedImports.redownloadFromURL(s))
	// Batch operations: a list of novels is the input, and the response is a
	// job list. Grouped under /novels/batch/* to make the relationship to
	// the novel collection explicit.
	api.POST("/novels/batch-check", sharedImports.checkBatchUpdates(s))
	api.POST("/novels/batch-update", sharedImports.batchUpdate(s))
	api.POST("/novels/batch-translate-preview", sharedImports.batchTranslatePreview(s))
	api.POST("/novels/batch-translate", sharedImports.batchTranslate(s))
	api.POST("/novels/batch-check-scheduled", sharedImports.batchCheck(s))
}

func detectZipRoot(entries []struct {
	name    string
	content []byte
}) string {
	if len(entries) == 0 {
		return ""
	}
	candidate := entries[0].name
	for {
		idx := strings.IndexByte(candidate, '/')
		if idx < 0 {
			return ""
		}
		prefix := candidate[:idx+1]
		allMatch := true
		for _, e := range entries {
			if !strings.HasPrefix(e.name, prefix) {
				allMatch = false
				break
			}
		}
		if allMatch {
			if hasFileAtRoot(strings.TrimSuffix(prefix, "/"), entries) {
				return prefix
			}
			candidate = entries[0].name[idx+1:]
			continue
		}
		return ""
	}
}

func hasFileAtRoot(dir string, entries []struct {
	name    string
	content []byte
}) bool {
	for _, e := range entries {
		rest := strings.TrimPrefix(e.name, dir+"/")
		if rest != "" && strings.IndexByte(rest, '/') < 0 {
			if base := strings.ToLower(filepath.Base(rest)); base == "metadata.json" || strings.HasPrefix(base, "originals") || strings.HasPrefix(base, "translated") {
				return true
			}
		}
	}
	return false
}

func extractChapterOrder(filename string) int {
	matches := chapterOrderRegex.FindStringSubmatch(filename)
	if len(matches) >= 2 {
		if n, err := strconv.Atoi(matches[1]); err == nil {
			return n
		}
	}
	return 0
}

// chapterOrderOf returns the chapter number to use for a parsed chapter.
// Parsers that know the canonical position (e.g. SkyDemonOrder reports the
// real episode number) set ChapterURL.Order — trust it first. The title
// fallback is only a heuristic: multi-part titles such as "Some Arc (3)"
// yield the part number, which collides with low episode orders already
// stored and silently hides those chapters from update checks.
func chapterOrderOf(ch noveldownloader.ChapterURL) int {
	if ch.Order > 0 {
		return ch.Order
	}
	return extractChapterOrder(ch.Title)
}

func extractChapterTitle(content, filename string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(content), "\n")
	first = strings.TrimSpace(first)
	first = strings.TrimLeft(first, "# ")
	first = stripMarkdown(first)
	first = strings.TrimSpace(first)
	if first != "" {
		return first
	}
	return filename
}

func stripMarkdown(s string) string {
	s = strings.ReplaceAll(s, "***", "")
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "__", "")
	s = strings.ReplaceAll(s, "*", "")
	s = strings.ReplaceAll(s, "~~", "")
	s = strings.ReplaceAll(s, "`", "")
	return s
}

func contentAfterTitle(content string) string {
	_, rest, found := strings.Cut(strings.TrimSpace(content), "\n")
	if !found || rest == "" {
		return strings.TrimSpace(content)
	}
	return strings.TrimSpace(rest)
}

type redownloadMismatch struct {
	Order       int    `json:"order"`
	SourceTitle string `json:"sourceTitle"`
	StoredTitle string `json:"storedTitle"`
}

type redownloadPlan struct {
	chapters   []store.DownloadChapterInfo
	mismatches []redownloadMismatch
}

// planRedownload matches a fresh chapter list from the source site against the
// stored chapters (by order, falling back to title) and flags chapters whose
// source title no longer matches the stored one — a sign the source renumbered
// or replaced its chapters. Stored titles are the original ones: translated
// titles live in a separate field and re-downloads never touch them, so a
// mismatch here means the pairing of original content to chapters may have
// shifted.
func planRedownload(chapters []noveldownloader.ChapterURL, byOrder map[int]store.Chapter, byTitle map[string]store.Chapter, startChapter, endChapter int) redownloadPlan {
	plan := redownloadPlan{chapters: make([]store.DownloadChapterInfo, 0)}
	for i, ch := range chapters {
		chNum := chapterOrderOf(ch)
		if chNum <= 0 {
			chNum = i + 1
		}
		if startChapter > 0 && chNum < startChapter {
			continue
		}
		if endChapter > 0 && chNum > endChapter {
			continue
		}
		existingCh, ok := byOrder[chNum]
		if !ok && ch.Title != "" {
			existingCh, ok = byTitle[ch.Title]
		}
		if !ok {
			// Not downloaded yet — new chapters are handled by update-from-url.
			continue
		}
		chTitle := ch.Title
		if chTitle == "" {
			chTitle = existingCh.Title
		}
		if chTitle == "" {
			chTitle = fmt.Sprintf("Capítulo %d", chNum)
		}
		plan.chapters = append(plan.chapters, store.DownloadChapterInfo{
			URL:       ch.URL,
			Title:     chTitle,
			Order:     chNum,
			ChapterID: existingCh.ID,
		})
		if ch.Title != "" && existingCh.Title != "" &&
			!strings.EqualFold(strings.TrimSpace(ch.Title), strings.TrimSpace(existingCh.Title)) {
			plan.mismatches = append(plan.mismatches, redownloadMismatch{
				Order:       chNum,
				SourceTitle: ch.Title,
				StoredTitle: existingCh.Title,
			})
		}
	}
	return plan
}

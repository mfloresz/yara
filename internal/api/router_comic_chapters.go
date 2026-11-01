package api

import (
	"archive/zip"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	pbrouter "github.com/pocketbase/pocketbase/tools/router"
	"translator-server/internal/store"
)

// sharedComicChapterHandlers exposes the comic chapter/page sub-resources.
type sharedComicChapterHandlers struct{}

var sharedComicChapters = sharedComicChapterHandlers{}

// list: GET /comics/{id}/chapters — summaries by default;
// ?includeContent=true returns each chapter with its page list (the comic
// analogue of the novel chapter's content bodies).
func (sharedComicChapterHandlers) list(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		comicID := e.Request.PathValue("id")
		includeContent := firstQuery(e.Request.URL.Query(), "includeContent") == "true"
		if includeContent {
			chapters, err := s.Store.ListComicChapters(e.Auth.Id, comicID)
			if err != nil {
				return notFoundOrForbidden(e, err)
			}
			items := make([]map[string]any, 0, len(chapters))
			for _, ch := range chapters {
				pages, perr := s.Store.ListComicChapterPagesAccessible(e.Auth.Id, comicID, ch.ID)
				if perr != nil {
					return notFoundOrForbidden(e, perr)
				}
				items = append(items, comicChapterRecord(ch, pages))
			}
			return v1Respond(e, http.StatusOK, items, nil, nil)
		}
		summaries, err := s.Store.ListAllComicChapterSummaries(e.Auth.Id, comicID)
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		return v1Respond(e, http.StatusOK, comicChapterSummaryRecords(summaries), nil, nil)
	}
}

// chapterSummaries: GET /comics/{id}/chapter-summaries — paginated light list.
func (sharedComicChapterHandlers) chapterSummaries(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		q := e.Request.URL.Query()
		page, perPage, limit, offset := parsePagination(q)
		summaries, total, err := s.Store.ListComicChapterSummaries(e.Auth.Id, e.Request.PathValue("id"), limit, offset)
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		return v1RespondList(e, http.StatusOK, comicChapterSummaryRecords(summaries), page, perPage, total, offset+limit < total, e.Request.URL.Path)
	}
}

// chapterStats: GET /comics/{id}/chapter-stats.
func (sharedComicChapterHandlers) chapterStats(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		stats, err := s.Store.GetComicChapterStats(e.Auth.Id, e.Request.PathValue("id"))
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		return v1Respond(e, http.StatusOK, comicChapterStatsRecord(stats), nil, nil)
	}
}

// get: GET /comics/{id}/chapters/{chapterId} — the chapter with its pages.
func (sharedComicChapterHandlers) get(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		comicID := e.Request.PathValue("id")
		chapterID := e.Request.PathValue("chapterId")
		chapter, err := s.Store.GetComicChapter(e.Auth.Id, comicID, chapterID)
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		pages, err := s.Store.ListComicChapterPagesAccessible(e.Auth.Id, comicID, chapterID)
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		return v1Respond(e, http.StatusOK, comicChapterRecord(*chapter, pages), nil, nil)
	}
}

// neighbors powers the reader's prev/next navigation.
func (sharedComicChapterHandlers) neighbors(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		prev, next, err := s.Store.GetComicChapterNeighbors(e.Auth.Id, e.Request.PathValue("id"), e.Request.PathValue("chapterId"))
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		body := map[string]any{}
		if prev != nil {
			body["prev"] = comicChapterSummaryRecord(*prev)
		}
		if next != nil {
			body["next"] = comicChapterSummaryRecord(*next)
		}
		return v1Respond(e, http.StatusOK, body, nil, nil)
	}
}

// pages: GET /comics/{id}/chapters/{chapterId}/pages.
func (sharedComicChapterHandlers) pages(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		pages, err := s.Store.ListComicChapterPagesAccessible(e.Auth.Id, e.Request.PathValue("id"), e.Request.PathValue("chapterId"))
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		return v1Respond(e, http.StatusOK, comicPageRecords(pages), nil, nil)
	}
}

// pageImage serves one page's image bytes. Page files are Protected, so this
// cookie-authenticated handler is the only serving path. The page id is in the
// URL and never changes content, so the response caches immutably.
func (sharedComicChapterHandlers) pageImage(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		blob, ext, err := s.Store.ReadComicPageBlob(e.Auth.Id, e.Request.PathValue("id"), e.Request.PathValue("chapterId"), e.Request.PathValue("pageId"))
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		contentType := mime.TypeByExtension("." + ext)
		if contentType == "" {
			contentType = "image/jpeg"
		}
		e.Response.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
		e.Response.Header().Set("Content-Type", contentType)
		e.Response.Header().Set("Content-Length", strconv.Itoa(len(blob)))
		return e.Blob(http.StatusOK, contentType, blob)
	}
}

// upsert: POST /comics/{id}/chapters — manual chapter creation (title/order;
// pages arrive through downloads or the ZIP import).
func (sharedComicChapterHandlers) upsert(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		var in struct {
			Title        string `json:"title"`
			ChapterOrder int    `json:"chapterOrder"`
			Position     int    `json:"position"`
		}
		if err := e.BindBody(&in); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		if strings.TrimSpace(in.Title) == "" && in.ChapterOrder == 0 {
			return e.BadRequestError("title or chapterOrder is required", nil)
		}
		chapter, err := s.Store.UpsertComicChapter(e.Auth.Id, e.Request.PathValue("id"), &store.ComicChapter{
			Title:        strings.TrimSpace(in.Title),
			ChapterOrder: in.ChapterOrder,
			Position:     in.Position,
			Status:       store.ComicChapterPending,
		})
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		e.Response.Header().Set("Location", fmt.Sprintf("/api/v1/comics/%s/chapters/%s", e.Request.PathValue("id"), chapter.ID))
		return v1Respond(e, http.StatusCreated, comicChapterSummaryRecord(store.ComicChapterSummary{
			ID: chapter.ID, ComicID: chapter.ComicID, ChapterOrder: chapter.ChapterOrder,
			Position: chapter.Position, Title: chapter.Title, Status: chapter.Status,
			PageCount: chapter.PageCount, HasPages: chapter.HasPages,
			CreatedAt: chapter.CreatedAt, UpdatedAt: chapter.UpdatedAt,
		}), nil, nil)
	}
}

// delete: DELETE /comics/{id}/chapters/{chapterId} — logical delete (exclude).
func (sharedComicChapterHandlers) delete(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		if err := s.Store.ExcludeComicChapter(e.Auth.Id, e.Request.PathValue("id"), e.Request.PathValue("chapterId")); err != nil {
			return notFoundOrForbidden(e, err)
		}
		return e.NoContent(http.StatusNoContent)
	}
}

// bulkDelete: POST /comics/{id}/chapters/bulk-delete.
func (sharedComicChapterHandlers) bulkDelete(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		body := struct {
			ChapterIDs []string `json:"chapterIds"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		count, err := s.Store.BulkExcludeComicChapters(e.Auth.Id, e.Request.PathValue("id"), body.ChapterIDs)
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		return v1Respond(e, http.StatusOK, map[string]any{"excluded": count}, nil, nil)
	}
}

// patchStatus: PATCH /comics/{id}/chapters/{chapterId}/status.
func (sharedComicChapterHandlers) patchStatus(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		body := struct {
			Status       string `json:"status"`
			ErrorMessage string `json:"errorMessage"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		if err := s.Store.UpdateComicChapterStatusForUser(e.Auth.Id, e.Request.PathValue("id"), e.Request.PathValue("chapterId"), body.Status, body.ErrorMessage); err != nil {
			return notFoundOrForbidden(e, err)
		}
		return v1Respond(e, http.StatusOK, map[string]any{"status": body.Status}, nil, nil)
	}
}

// visibility: PATCH /comics/{id}/chapters/{chapterId}/visibility — exclude or
// restore a chapter.
func (sharedComicChapterHandlers) visibility(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		body := struct {
			Excluded bool `json:"excluded"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		if err := s.Store.SetComicChapterExcluded(e.Auth.Id, e.Request.PathValue("id"), e.Request.PathValue("chapterId"), body.Excluded); err != nil {
			return notFoundOrForbidden(e, err)
		}
		return v1Respond(e, http.StatusOK, map[string]any{"excluded": body.Excluded}, nil, nil)
	}
}

// listExcluded: GET /comics/{id}/chapters/excluded — restore-oriented listing.
func (sharedComicChapterHandlers) listExcluded(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		summaries, err := s.Store.ListExcludedComicChapterSummaries(e.Auth.Id, e.Request.PathValue("id"))
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		return v1Respond(e, http.StatusOK, comicChapterSummaryRecords(summaries), nil, nil)
	}
}

// reorder: PATCH /comics/{id}/chapters/order.
func (sharedComicChapterHandlers) reorder(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		body := struct {
			ChapterIDs []string `json:"chapterIds"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		if err := s.Store.ReorderComicChapters(e.Auth.Id, e.Request.PathValue("id"), body.ChapterIDs); err != nil {
			return notFoundOrForbidden(e, err)
		}
		return v1Respond(e, http.StatusOK, map[string]any{"reordered": len(body.ChapterIDs)}, nil, nil)
	}
}

// search: GET /comics/{id}/chapters/search?q= — title search for the reader's
// chapter-list modal.
func (sharedComicChapterHandlers) search(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		q := e.Request.URL.Query()
		limit, _ := strconv.Atoi(firstQuery(q, "limit"))
		hits, err := s.Store.SearchOwnedComicChapters(e.Auth.Id, e.Request.PathValue("id"), firstQuery(q, "q"), limit)
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		items := make([]map[string]any, 0, len(hits))
		for _, hit := range hits {
			items = append(items, map[string]any{
				"chapter":       comicChapterSummaryRecord(hit.ComicChapterSummary),
				"matchedFields": hit.MatchedFields,
			})
		}
		return v1Respond(e, http.StatusOK, items, nil, nil)
	}
}

func getComicReadingProgress(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		progress, err := s.Store.GetComicReadingProgress(e.Auth.Id, e.Request.PathValue("id"))
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		return v1Respond(e, http.StatusOK, map[string]any{
			"comicId":       progress.ComicID,
			"chapterId":     progress.ChapterID,
			"scrollPercent": progress.ScrollPercent,
			"updatedAt":     progress.UpdatedAt,
		}, nil, nil)
	}
}

func putComicReadingProgress(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		body := struct {
			ChapterID     string  `json:"chapterId"`
			ScrollPercent float64 `json:"scrollPercent"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		progress, err := s.Store.UpsertComicReadingProgress(e.Auth.Id, e.Request.PathValue("id"), body.ChapterID, body.ScrollPercent)
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		return v1Respond(e, http.StatusOK, map[string]any{
			"comicId":       progress.ComicID,
			"chapterId":     progress.ChapterID,
			"scrollPercent": progress.ScrollPercent,
			"updatedAt":     progress.UpdatedAt,
		}, nil, nil)
	}
}

// jobs: POST/GET /comics/{id}/jobs — same contract as the novel jobs routes.
func (sharedComicChapterHandlers) jobs(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		q := e.Request.URL.Query()
		failedOnly := firstQuery(q, "failed") == "true"
		jobs, err := s.Store.ListComicJobs(e.Auth.Id, e.Request.PathValue("id"), failedOnly)
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		items := make([]map[string]any, 0, len(jobs))
		for _, job := range jobs {
			items = append(items, map[string]any{
				"id": job.ID, "comicId": job.ComicID, "status": job.Status,
				"operation": job.Operation, "provider": job.Provider, "model": job.Model,
				"totalChapters": job.TotalChapters, "completedChapters": job.CompletedChapters,
				"failedChapters": job.FailedChapters, "errorMessage": job.ErrorMessage,
				"createdAt": job.CreatedAt, "updatedAt": job.UpdatedAt,
			})
		}
		return v1Respond(e, http.StatusOK, items, nil, nil)
	}
}

// importZip: POST /comics/import-zip — a ZIP of page images laid out as one
// folder per chapter ("Capitulo 001/001.jpg", "002.png", ...), plus an
// optional root metadata.json ({sourceLanguage, targetLanguage, sourceTitle,
// ...}) and cover.*. Files inside each folder become that chapter's pages in
// numeric/lexical order; a numeric folder-name prefix sets the chapter order,
// otherwise folders are ordered by name.
func (sharedComicChapterHandlers) importZip(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		if err := e.Request.ParseMultipartForm(128 << 20); err != nil {
			return e.BadRequestError("failed to parse form", err)
		}
		file, header, err := e.Request.FormFile("file")
		if err != nil {
			return e.BadRequestError("file is required", err)
		}
		defer file.Close()
		blob, err := io.ReadAll(file)
		if err != nil {
			return e.InternalServerError("failed to read file", err)
		}
		chapters, metadataJSON, coverBlob, coverMime, perr := parseComicZip(header.Filename, blob)
		if perr != nil {
			return writeV1Error(e, http.StatusBadRequest, "validation_failed", perr.Error())
		}
		if metadataJSON == "" {
			title := strings.TrimSuffix(header.Filename, ".zip")
			metadataJSON = fmt.Sprintf(`{"sourceTitle":%q,"sourceLanguage":"en","targetLanguage":"es"}`, title)
		}
		result, err := s.Store.ImportZipComic(&store.ImportZipComicInput{
			OwnerID:      e.Auth.Id,
			FileName:     header.Filename,
			FileBlob:     blob,
			MetadataJSON: metadataJSON,
			CoverBlob:    coverBlob,
			CoverMime:    coverMime,
			Chapters:     chapters,
		})
		if err != nil {
			return e.InternalServerError("failed to import comic", err)
		}
		e.Response.Header().Set("Location", "/api/v1/comics/"+result.Comic.ID)
		return v1Respond(e, http.StatusCreated, map[string]any{
			"comic":            s.comicResponse(&result.Comic),
			"chaptersImported": result.ChaptersImported,
		}, nil, nil)
	}
}

func registerV1ComicChapterRoutes(api *pbrouter.RouterGroup[*core.RequestEvent], s *Server) {
	api.GET("/comics/{id}/chapters", sharedComicChapters.list(s))
	api.GET("/comics/{id}/chapter-summaries", sharedComicChapters.chapterSummaries(s))
	api.GET("/comics/{id}/chapter-stats", sharedComicChapters.chapterStats(s))
	api.GET("/comics/{id}/chapters/search", sharedComicChapters.search(s))
	api.GET("/comics/{id}/chapters/excluded", sharedComicChapters.listExcluded(s))
	api.PATCH("/comics/{id}/chapters/order", sharedComicChapters.reorder(s))
	api.GET("/comics/{id}/chapters/{chapterId}", sharedComicChapters.get(s))
	api.GET("/comics/{id}/chapters/{chapterId}/neighbors", sharedComicChapters.neighbors(s))
	api.GET("/comics/{id}/chapters/{chapterId}/pages", sharedComicChapters.pages(s))
	api.GET("/comics/{id}/chapters/{chapterId}/pages/{pageId}/image", sharedComicChapters.pageImage(s))
	api.POST("/comics/{id}/chapters", sharedComicChapters.upsert(s))
	api.DELETE("/comics/{id}/chapters/{chapterId}", sharedComicChapters.delete(s))
	api.POST("/comics/{id}/chapters/bulk-delete", sharedComicChapters.bulkDelete(s))
	api.PATCH("/comics/{id}/chapters/{chapterId}/status", sharedComicChapters.patchStatus(s))
	api.PATCH("/comics/{id}/chapters/{chapterId}/visibility", sharedComicChapters.visibility(s))
	api.GET("/comics/{id}/reading-progress", getComicReadingProgress(s))
	api.PUT("/comics/{id}/reading-progress", putComicReadingProgress(s))
	api.GET("/comics/{id}/jobs", sharedComicChapters.jobs(s))
}

// comicZipBudgets bound the archive: per image, total images, total text-ish
// entries (metadata/cover).
const (
	comicZipMaxImageBytes  int64 = 20 << 20
	comicZipMaxImagesTotal int64 = 256 << 20
	comicZipMaxTextTotal   int64 = 1 << 20
)

func comicZipImageMime(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".avif":
		return "image/avif"
	default:
		return ""
	}
}

// parseComicZip walks the archive and builds the chapter/page structure.
func parseComicZip(fileName string, blob []byte) (chapters []store.ImportedComicZipChapter, metadataJSON string, coverBlob []byte, coverMime string, err error) {
	reader, zerr := zip.NewReader(strings.NewReader(string(blob)), int64(len(blob)))
	if zerr != nil {
		return nil, "", nil, "", fmt.Errorf("invalid zip file: %w", zerr)
	}
	// First-level directory → ordered image list.
	folders := map[string][]store.ImportedComicImage{}
	var order []string
	var remainingImages = comicZipMaxImagesTotal
	var remainingText = comicZipMaxTextTotal
	for _, f := range reader.File {
		if f.FileInfo().IsDir() {
			continue
		}
		entry := path.Clean(strings.TrimLeft(filepath.ToSlash(f.Name), "./"))
		rc, oerr := f.Open()
		if oerr != nil {
			return nil, "", nil, "", fmt.Errorf("failed to read zip entry %s: %w", entry, oerr)
		}
		if mime := comicZipImageMime(entry); mime != "" && !strings.HasPrefix(strings.ToLower(path.Base(entry)), "cover.") {
			data, rerr := io.ReadAll(io.LimitReader(rc, comicZipMaxImageBytes+1))
			rc.Close()
			if rerr != nil {
				return nil, "", nil, "", fmt.Errorf("failed to read %s: %w", entry, rerr)
			}
			if int64(len(data)) > comicZipMaxImageBytes {
				return nil, "", nil, "", fmt.Errorf("image %s exceeds the %dMB per-image limit", entry, comicZipMaxImageBytes>>20)
			}
			remainingImages -= int64(len(data))
			if remainingImages < 0 {
				return nil, "", nil, "", fmt.Errorf("zip images exceed the %dMB total limit", comicZipMaxImagesTotal>>20)
			}
			dir := path.Dir(entry)
			if dir == "." || dir == "/" {
				dir = ""
			}
			if _, seen := folders[dir]; !seen {
				order = append(order, dir)
			}
			folders[dir] = append(folders[dir], store.ImportedComicImage{FileName: path.Base(entry), MimeType: mime, Blob: data})
			continue
		}
		data, rerr := io.ReadAll(io.LimitReader(rc, comicZipMaxTextTotal+1))
		rc.Close()
		if rerr != nil {
			return nil, "", nil, "", fmt.Errorf("failed to read %s: %w", entry, rerr)
		}
		if int64(len(data)) > remainingText {
			return nil, "", nil, "", fmt.Errorf("zip text entries exceed the %dMB limit", comicZipMaxTextTotal)
		}
		remainingText -= int64(len(data))
		switch strings.ToLower(path.Base(entry)) {
		case "metadata.json":
			metadataJSON = string(data)
		default:
			if strings.HasPrefix(strings.ToLower(path.Base(entry)), "cover.") {
				coverBlob = data
				coverMime = comicZipImageMime(entry)
			}
		}
	}
	sort.Strings(order)
	chapters = make([]store.ImportedComicZipChapter, 0, len(order))
	autoIndex := 0
	for _, dir := range order {
		images := folders[dir]
		sort.Slice(images, func(i, j int) bool { return images[i].FileName < images[j].FileName })
		title := path.Base(dir)
		if title == "." || title == "/" || dir == "" {
			title = "Capítulo único"
		}
		chOrder := 0
		if prefix := leadingDigits(title); prefix != "" {
			if n, terr := strconv.Atoi(prefix); terr == nil {
				chOrder = n
			}
		}
		if chOrder == 0 {
			autoIndex++
			chOrder = autoIndex
		}
		chapters = append(chapters, store.ImportedComicZipChapter{Order: chOrder, Title: title, Images: images})
	}
	if len(chapters) == 0 {
		return nil, "", nil, "", fmt.Errorf("the zip contains no chapter folders with images")
	}
	return chapters, metadataJSON, coverBlob, coverMime, nil
}

func leadingDigits(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return s[:i]
		}
	}
	return s
}

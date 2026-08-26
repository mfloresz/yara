package api

import (
	"net/http"

	"github.com/pocketbase/pocketbase/core"
	pbrouter "github.com/pocketbase/pocketbase/tools/router"
	"translator-server/internal/store"
)

type sharedChapterHandlers struct{}

var sharedChapters = sharedChapterHandlers{}

// cleanPreview: POST /novels/{novelId}/chapters:cleanPreview
func (sharedChapterHandlers) cleanPreview(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		body := struct {
			ChapterID     string `json:"chapterId"`
			Mode          string `json:"mode"`
			SearchText    string `json:"searchText"`
			ReplaceText   string `json:"replaceText"`
			CaseSensitive bool   `json:"caseSensitive"`
			UseRegex      bool   `json:"useRegex"`
			ApplyTo       string `json:"applyTo"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}

		if !isValidCleanMode(body.Mode) {
			return e.BadRequestError("invalid mode", nil)
		}
		if !isValidApplyTo(body.ApplyTo) {
			return e.BadRequestError("invalid applyTo", nil)
		}
		if body.ChapterID == "" {
			return e.BadRequestError("chapterId is required", nil)
		}

		if _, err := s.Store.GetOwnedNovel(e.Auth.Id, e.Request.PathValue("novelId")); err != nil {
			return notFoundOrForbidden(e, err)
		}

		chapter, err := s.Store.GetChapterAccessible(e.Auth.Id, e.Request.PathValue("novelId"), body.ChapterID)
		if err != nil {
			return notFoundOrForbidden(e, err)
		}

		source := cleaningSource(chapter, body.ApplyTo)
		result := ApplyClean(source, CleanOptions{
			Mode:          CleanMode(body.Mode),
			SearchText:    body.SearchText,
			ReplaceText:   body.ReplaceText,
			CaseSensitive: body.CaseSensitive,
			UseRegex:      body.UseRegex,
		})

		if isV1Request(e) {
			return v1Respond(e, http.StatusOK, CleanPreviewResult{
				ChapterTitle: chapter.Title,
				Changes:      diffLines(result.Original, result.Cleaned),
				CleanResult:  result,
			}, nil, nil)
		}
		return e.JSON(http.StatusOK, CleanPreviewResult{
			ChapterTitle: chapter.Title,
			Changes:      diffLines(result.Original, result.Cleaned),
			CleanResult:  result,
		})
	}
}

func (sharedChapterHandlers) cleanPreviewBulk(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		body := struct {
			ChapterIDs    []string `json:"chapterIds"`
			Mode          string   `json:"mode"`
			SearchText    string   `json:"searchText"`
			ReplaceText   string   `json:"replaceText"`
			CaseSensitive bool     `json:"caseSensitive"`
			UseRegex      bool     `json:"useRegex"`
			ApplyTo       string   `json:"applyTo"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}

		if !isValidCleanMode(body.Mode) {
			return e.BadRequestError("invalid mode", nil)
		}
		if !isValidApplyTo(body.ApplyTo) {
			return e.BadRequestError("invalid applyTo", nil)
		}
		if len(body.ChapterIDs) == 0 {
			return e.BadRequestError("chapterIds is required", nil)
		}

		if _, err := s.Store.GetOwnedNovel(e.Auth.Id, e.Request.PathValue("novelId")); err != nil {
			return notFoundOrForbidden(e, err)
		}

		opts := CleanOptions{
			Mode:          CleanMode(body.Mode),
			SearchText:    body.SearchText,
			ReplaceText:   body.ReplaceText,
			CaseSensitive: body.CaseSensitive,
			UseRegex:      body.UseRegex,
		}

		items := make([]CleanPreviewBulkItem, 0, len(body.ChapterIDs))
		for _, chapterID := range body.ChapterIDs {
			chapter, err := s.Store.GetChapterAccessible(e.Auth.Id, e.Request.PathValue("novelId"), chapterID)
			if err != nil {
				continue
			}

			result := ApplyClean(cleaningSource(chapter, body.ApplyTo), opts)
			if !result.Changed {
				continue
			}
			items = append(items, CleanPreviewBulkItem{
				ChapterID:    chapter.ID,
				ChapterOrder: chapter.ChapterOrder,
				ChapterTitle: chapter.Title,
				Changes:      diffLines(result.Original, result.Cleaned),
				CleanResult:  result,
			})
		}

		body2 := map[string]any{
			"items":   items,
			"total":   len(body.ChapterIDs),
			"changed": len(items),
		}
		if isV1Request(e) {
			return v1Respond(e, http.StatusOK, body2, nil, nil)
		}
		return e.JSON(http.StatusOK, body2)
	}
}

func (sharedChapterHandlers) clean(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		body := struct {
			ChapterIDs    []string `json:"chapterIds"`
			Mode          string   `json:"mode"`
			SearchText    string   `json:"searchText"`
			ReplaceText   string   `json:"replaceText"`
			CaseSensitive bool     `json:"caseSensitive"`
			UseRegex      bool     `json:"useRegex"`
			ApplyTo       string   `json:"applyTo"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}

		if !isValidCleanMode(body.Mode) {
			return e.BadRequestError("invalid mode", nil)
		}
		if !isValidApplyTo(body.ApplyTo) {
			return e.BadRequestError("invalid applyTo", nil)
		}
		if len(body.ChapterIDs) == 0 {
			return e.BadRequestError("chapterIds is required", nil)
		}

		if _, err := s.Store.GetOwnedNovel(e.Auth.Id, e.Request.PathValue("novelId")); err != nil {
			return notFoundOrForbidden(e, err)
		}

		opts := CleanOptions{
			Mode:          CleanMode(body.Mode),
			SearchText:    body.SearchText,
			ReplaceText:   body.ReplaceText,
			CaseSensitive: body.CaseSensitive,
			UseRegex:      body.UseRegex,
		}

		modified, skipped, notFound, failed := 0, 0, 0, 0
		for _, chapterID := range body.ChapterIDs {
			chapter, err := s.Store.GetChapterAccessible(e.Auth.Id, e.Request.PathValue("novelId"), chapterID)
			if err != nil {
				notFound++
				continue
			}

			patch := &store.Chapter{
				ID:                chapterID,
				ChapterOrder:      chapter.ChapterOrder,
				Title:             chapter.Title,
				TranslatedTitle:   chapter.TranslatedTitle,
				OriginalContent:   chapter.OriginalContent,
				TranslatedContent: chapter.TranslatedContent,
				RefinedContent:    chapter.RefinedContent,
				Status:            chapter.Status,
				ErrorMessage:      chapter.ErrorMessage,
			}
			changed := false
			hasApplicableContent := false

			if body.ApplyTo == "original" || body.ApplyTo == "all" {
				if chapter.OriginalContent != "" {
					hasApplicableContent = true
					res := ApplyClean(chapter.OriginalContent, opts)
					if res.Changed {
						patch.OriginalContent = res.Cleaned
						changed = true
					}
				}
			}
			if body.ApplyTo == "translated" || body.ApplyTo == "all" {
				if chapter.TranslatedContent != "" {
					hasApplicableContent = true
					res := ApplyClean(chapter.TranslatedContent, opts)
					if res.Changed {
						patch.TranslatedContent = res.Cleaned
						changed = true
					}
				}
			}
			if body.ApplyTo == "refined" || body.ApplyTo == "all" {
				if chapter.RefinedContent != "" {
					hasApplicableContent = true
					res := ApplyClean(chapter.RefinedContent, opts)
					if res.Changed {
						patch.RefinedContent = res.Cleaned
						changed = true
					}
				}
			}

			if !changed {
				if !hasApplicableContent {
					skipped++
				}
				continue
			}

			if _, err := s.Store.UpsertChapterWithoutStats(e.Auth.Id, e.Request.PathValue("novelId"), patch); err != nil {
				failed++
				continue
			}
			modified++
		}

		if modified > 0 {
			if err := s.Store.RecalculateNovelStats(e.Request.PathValue("novelId")); err != nil {
				return e.InternalServerError("failed to recalculate stats", err)
			}
		}

		summary := map[string]any{
			"modified": modified,
			"total":    len(body.ChapterIDs),
			"skipped":  skipped,
			"notFound": notFound,
			"failed":   failed,
		}
		if isV1Request(e) {
			return v1Respond(e, http.StatusOK, summary, nil, nil)
		}
		return e.JSON(http.StatusOK, summary)
	}
}

// listChapters: GET /novels/{novelId}/chapters — legacy returns bare array; v1
// returns {data,meta,links}. To keep the page small, v1 also honors
// ?includeContent=false (default false on v1 — only summaries) and ?summary=true
// (always returns chapterSummaryRecord). The legacy /chapters path keeps
// returning the full chapter array so the offline cache keeps working.
func (sharedChapterHandlers) list(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		// Legacy behavior: full chapters, no content filter, bare array.
		chapters, err := s.Store.ListAllChapterSummariesAccessible(e.Auth.Id, e.Request.PathValue("novelId"))
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		if isV1Request(e) {
			q := e.Request.URL.Query()
			includeContent := firstQuery(q, "includeContent") == "true"
			page, perPage, limit, offset := parsePagination(q)
			_ = page
			_ = perPage
			_ = limit
			_ = offset
			out := make([]map[string]any, 0, len(chapters))
			if includeContent {
				full, err := s.Store.ListChaptersAccessible(e.Auth.Id, e.Request.PathValue("novelId"))
				if err != nil {
					return notFoundOrForbidden(e, err)
				}
				for _, ch := range full {
					out = append(out, chapterRecord(ch))
				}
			} else {
				for _, ch := range chapters {
					out = append(out, chapterSummaryRecord(ch))
				}
			}
			return v1RespondList(e, http.StatusOK, out, 1, len(out), len(out), false, e.Request.URL.Path)
		}
		out := make([]map[string]any, 0, len(chapters))
		for _, ch := range chapters {
			out = append(out, chapterSummaryRecord(ch))
		}
		return e.JSON(http.StatusOK, out)
	}
}

func (sharedChapterHandlers) eligible(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		operation := e.Request.URL.Query().Get("operation")
		if operation != "translate" && operation != "refine" {
			operation = "translate"
		}
		chapters, err := s.Store.ListEligibleChapterSummariesAccessible(e.Auth.Id, e.Request.PathValue("novelId"), operation)
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		out := make([]map[string]any, 0, len(chapters))
		for _, ch := range chapters {
			out = append(out, chapterSummaryRecord(ch))
		}
		if isV1Request(e) {
			return v1RespondList(e, http.StatusOK, out, 1, len(out), len(out), false, e.Request.URL.Path)
		}
		return e.JSON(http.StatusOK, out)
	}
}

// chaptersFull: legacy GET /db/novels/{novelId}/chapters/full — returns full
// chapter array (no v1 equivalent; v1 uses ?includeContent=true on /chapters).
func (sharedChapterHandlers) full(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		chapters, err := s.Store.ListChaptersAccessible(e.Auth.Id, e.Request.PathValue("novelId"))
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		out := make([]map[string]any, 0, len(chapters))
		for _, ch := range chapters {
			out = append(out, chapterRecord(ch))
		}
		return e.JSON(http.StatusOK, out)
	}
}

func (sharedChapterHandlers) chapterSummaries(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		q := e.Request.URL.Query()
		page, perPage, limit, offset := parsePagination(q)
		items, total, err := s.Store.ListChapterSummariesAccessible(e.Auth.Id, e.Request.PathValue("novelId"), limit, offset)
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		out := make([]map[string]any, 0, len(items))
		for _, ch := range items {
			out = append(out, chapterSummaryRecord(ch))
		}
		if isV1Request(e) {
			hasMore := offset+len(items) < total
			return v1RespondList(e, http.StatusOK, out, page, perPage, total, hasMore, e.Request.URL.Path)
		}
		return e.JSON(http.StatusOK, map[string]any{"items": out, "total": total, "limit": limit, "offset": offset})
	}
}

func (sharedChapterHandlers) chapterStats(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		stats, err := s.Store.GetChapterStatsAccessible(e.Auth.Id, e.Request.PathValue("novelId"))
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		if isV1Request(e) {
			return v1Respond(e, http.StatusOK, chapterStatsRecord(stats), nil, nil)
		}
		return e.JSON(http.StatusOK, chapterStatsRecord(stats))
	}
}

func (sharedChapterHandlers) get(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		chapter, err := s.Store.GetChapterAccessible(e.Auth.Id, e.Request.PathValue("novelId"), e.Request.PathValue("chapterId"))
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		if isV1Request(e) {
			return v1Respond(e, http.StatusOK, chapterRecord(*chapter), nil, nil)
		}
		return e.JSON(http.StatusOK, chapterRecord(*chapter))
	}
}

func (sharedChapterHandlers) upsert(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		body := store.Chapter{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		chapter, err := s.Store.UpsertChapter(e.Auth.Id, e.Request.PathValue("novelId"), &body)
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		if isV1Request(e) {
			e.Response.Header().Set("Location", "/api/v1/novels/"+e.Request.PathValue("novelId")+"/chapters/"+chapter.ID)
			return v1Respond(e, http.StatusCreated, chapterRecord(*chapter), nil, nil)
		}
		return e.JSON(http.StatusCreated, chapterRecord(*chapter))
	}
}

func (sharedChapterHandlers) delete(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		if err := s.Store.DeleteChapter(e.Auth.Id, e.Request.PathValue("novelId"), e.Request.PathValue("chapterId")); err != nil {
			return notFoundOrForbidden(e, err)
		}
		if isV1Request(e) {
			return e.NoContent(http.StatusNoContent)
		}
		return e.JSON(http.StatusOK, map[string]any{"ok": true})
	}
}

func (sharedChapterHandlers) bulkDelete(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		body := struct {
			IDs []string `json:"ids"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		deleted, err := s.Store.BulkDeleteChapters(e.Auth.Id, e.Request.PathValue("novelId"), body.IDs)
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		summary := map[string]any{"deleted": deleted, "requested": len(body.IDs)}
		if isV1Request(e) {
			return v1Respond(e, http.StatusOK, summary, nil, nil)
		}
		return e.JSON(http.StatusOK, summary)
	}
}

// patchStatus: PATCH /novels/{novelId}/chapters/{chapterId}/status — legacy
// only. On v1 the equivalent is POST /novels/{novelId}/chapters/{chapterId}:reset
// for "pending" or a separate PATCH for translation status. To avoid splitting
// the logic, v1 also routes this through patchStatus but with stricter
// validation.
func (sharedChapterHandlers) patchStatus(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		body := struct {
			Status       string `json:"status"`
			ErrorMessage string `json:"errorMessage"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		if err := s.Store.UpdateChapterStatusForUser(e.Auth.Id, e.Request.PathValue("novelId"), e.Request.PathValue("chapterId"), body.Status, body.ErrorMessage); err != nil {
			return notFoundOrForbidden(e, err)
		}
		chapter, err := s.Store.GetChapterAccessible(e.Auth.Id, e.Request.PathValue("novelId"), e.Request.PathValue("chapterId"))
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		if isV1Request(e) {
			return v1Respond(e, http.StatusOK, chapterRecord(*chapter), nil, nil)
		}
		return e.JSON(http.StatusOK, chapterRecord(*chapter))
	}
}

func (sharedChapterHandlers) gaps(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		novelID := e.Request.PathValue("novelId")
		if _, err := s.Store.GetOwnedNovel(e.Auth.Id, novelID); err != nil {
			return notFoundOrForbidden(e, err)
		}
		gaps, err := s.Store.GetChapterGaps(e.Auth.Id, novelID)
		if err != nil {
			return e.InternalServerError("failed to get chapter gaps", err)
		}
		body := map[string]any{"gaps": gaps}
		if isV1Request(e) {
			return v1Respond(e, http.StatusOK, body, nil, nil)
		}
		return e.JSON(http.StatusOK, body)
	}
}

func registerChapterRoutes(api *pbrouter.RouterGroup[*core.RequestEvent], s *Server) {
	api.POST("/db/novels/{novelId}/chapters/clean-preview", sharedChapters.cleanPreview(s))
	api.POST("/db/novels/{novelId}/chapters/clean-preview-bulk", sharedChapters.cleanPreviewBulk(s))
	api.POST("/db/novels/{novelId}/chapters/clean", sharedChapters.clean(s))
	api.GET("/db/novels/{novelId}/chapters", sharedChapters.list(s))
	api.GET("/db/novels/{novelId}/chapters/eligible", sharedChapters.eligible(s))
	api.GET("/db/novels/{novelId}/chapters/full", sharedChapters.full(s))
	api.GET("/db/novels/{novelId}/chapter-summaries", sharedChapters.chapterSummaries(s))
	api.GET("/db/novels/{novelId}/chapter-stats", sharedChapters.chapterStats(s))
	api.GET("/db/novels/{novelId}/chapters/{chapterId}", sharedChapters.get(s))
	api.POST("/db/novels/{novelId}/chapters", sharedChapters.upsert(s))
	api.DELETE("/db/novels/{novelId}/chapters/{chapterId}", sharedChapters.delete(s))
	api.POST("/db/novels/{novelId}/chapters/bulk-delete", sharedChapters.bulkDelete(s))
	api.PATCH("/db/novels/{novelId}/chapters/{chapterId}/status", sharedChapters.patchStatus(s))
	api.GET("/db/novels/{novelId}/chapters/gaps", sharedChapters.gaps(s))
}

func registerV1ChapterRoutes(api *pbrouter.RouterGroup[*core.RequestEvent], s *Server) {
	api.POST("/novels/{novelId}/chapters/clean-preview", sharedChapters.cleanPreview(s))
	api.POST("/novels/{novelId}/chapters/clean-preview-bulk", sharedChapters.cleanPreviewBulk(s))
	api.POST("/novels/{novelId}/chapters/clean", sharedChapters.clean(s))
	api.GET("/novels/{novelId}/chapters", sharedChapters.list(s))
	api.GET("/novels/{novelId}/chapters/eligible", sharedChapters.eligible(s))
	api.GET("/novels/{novelId}/chapter-summaries", sharedChapters.chapterSummaries(s))
	api.GET("/novels/{novelId}/chapter-stats", sharedChapters.chapterStats(s))
	api.GET("/novels/{novelId}/chapters/{chapterId}", sharedChapters.get(s))
	api.POST("/novels/{novelId}/chapters", sharedChapters.upsert(s))
	api.DELETE("/novels/{novelId}/chapters/{chapterId}", sharedChapters.delete(s))
	api.POST("/novels/{novelId}/chapters/bulk-delete", sharedChapters.bulkDelete(s))
	api.PATCH("/novels/{novelId}/chapters/{chapterId}/status", sharedChapters.patchStatus(s))
	api.GET("/novels/{novelId}/chapters/gaps", sharedChapters.gaps(s))
}

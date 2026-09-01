package api

import (
	"errors"
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

		return v1Respond(e, http.StatusOK, CleanPreviewResult{
			ChapterTitle: chapter.Title,
			Changes:      diffLines(result.Original, result.Cleaned),
			CleanResult:  result,
		}, nil, nil)
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
		return v1Respond(e, http.StatusOK, body2, nil, nil)
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
		return v1Respond(e, http.StatusOK, summary, nil, nil)
	}
}

// listChapters: GET /novels/{novelId}/chapters — v1 honors
// ?includeContent=true (default false — only summaries) and uses the
// {data,meta,links} envelope.
func (sharedChapterHandlers) list(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		chapters, err := s.Store.ListAllChapterSummariesAccessible(e.Auth.Id, e.Request.PathValue("novelId"))
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
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
		return v1RespondList(e, http.StatusOK, out, 1, len(out), len(out), false, e.Request.URL.Path)
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
		hasMore := offset+len(items) < total
		return v1RespondList(e, http.StatusOK, out, page, perPage, total, hasMore, e.Request.URL.Path)
	}
}

func (sharedChapterHandlers) chapterStats(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		stats, err := s.Store.GetChapterStatsAccessible(e.Auth.Id, e.Request.PathValue("novelId"))
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		return v1Respond(e, http.StatusOK, chapterStatsRecord(stats), nil, nil)
	}
}

func (sharedChapterHandlers) get(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		chapter, err := s.Store.GetChapterAccessible(e.Auth.Id, e.Request.PathValue("novelId"), e.Request.PathValue("chapterId"))
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		return v1Respond(e, http.StatusOK, chapterRecord(*chapter), nil, nil)
	}
}

func (sharedChapterHandlers) upsert(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		body := store.Chapter{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		novelID := e.Request.PathValue("novelId")
		unlock := s.lockNovel(novelID)
		defer unlock()
		chapter, err := s.Store.UpsertChapter(e.Auth.Id, novelID, &body)
		if err != nil {
			switch {
			case errors.Is(err, store.ErrActiveJobs):
				return e.Error(http.StatusConflict, "cannot reorder chapters while jobs are active on this novel", err)
			case errors.Is(err, store.ErrInvalidReorder):
				return e.BadRequestError(err.Error(), err)
			case errors.Is(err, store.ErrNotFound), errors.Is(err, store.ErrForbidden):
				return notFoundOrForbidden(e, err)
			default:
				return e.InternalServerError("failed to save chapter", err)
			}
		}
		e.Response.Header().Set("Location", "/api/v1/novels/"+novelID+"/chapters/"+chapter.ID)
		return v1Respond(e, http.StatusCreated, chapterRecord(*chapter), nil, nil)
	}
}

func (sharedChapterHandlers) delete(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		novelID := e.Request.PathValue("novelId")
		unlock := s.lockNovel(novelID)
		defer unlock()
		// Logical delete: retain record/content/order/id; hidden until restored.
		if err := s.Store.ExcludeChapter(e.Auth.Id, novelID, e.Request.PathValue("chapterId")); err != nil {
			if errors.Is(err, store.ErrActiveJobs) {
				return e.Error(http.StatusConflict, "cannot exclude chapters while jobs are active on this novel", err)
			}
			return notFoundOrForbidden(e, err)
		}
		return e.NoContent(http.StatusNoContent)
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
		novelID := e.Request.PathValue("novelId")
		unlock := s.lockNovel(novelID)
		defer unlock()
		excluded, err := s.Store.BulkExcludeChapters(e.Auth.Id, novelID, body.IDs)
		if err != nil {
			if errors.Is(err, store.ErrActiveJobs) {
				return e.Error(http.StatusConflict, "cannot exclude chapters while jobs are active on this novel", err)
			}
			return notFoundOrForbidden(e, err)
		}
		summary := map[string]any{"deleted": excluded, "requested": len(body.IDs)}
		return v1Respond(e, http.StatusOK, summary, nil, nil)
	}
}

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
		return v1Respond(e, http.StatusOK, chapterRecord(*chapter), nil, nil)
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
		excludedOrders, err := s.Store.GetExcludedChapterOrders(e.Auth.Id, novelID)
		if err != nil {
			return e.InternalServerError("failed to get excluded chapter orders", err)
		}
		body := map[string]any{"gaps": gaps, "excludedOrders": excludedOrders}
		return v1Respond(e, http.StatusOK, body, nil, nil)
	}
}

func (sharedChapterHandlers) listExcluded(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		chapters, err := s.Store.ListExcludedChapterSummariesAccessible(e.Auth.Id, e.Request.PathValue("novelId"))
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		out := make([]map[string]any, 0, len(chapters))
		for _, ch := range chapters {
			out = append(out, chapterSummaryRecord(ch))
		}
		return v1RespondList(e, http.StatusOK, out, 1, len(out), len(out), false, e.Request.URL.Path)
	}
}

func (sharedChapterHandlers) reorder(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		body := struct {
			ChapterIDs []string `json:"chapterIds"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		novelID := e.Request.PathValue("novelId")
		unlock := s.lockNovel(novelID)
		defer unlock()
		if err := s.Store.ReorderChapters(e.Auth.Id, novelID, body.ChapterIDs); err != nil {
			switch {
			case errors.Is(err, store.ErrActiveJobs):
				return e.Error(http.StatusConflict, "cannot reorder chapters while jobs are active on this novel", err)
			case errors.Is(err, store.ErrInvalidReorder):
				return e.BadRequestError(err.Error(), err)
			case errors.Is(err, store.ErrNotFound), errors.Is(err, store.ErrForbidden):
				return notFoundOrForbidden(e, err)
			default:
				return e.InternalServerError("failed to reorder chapters", err)
			}
		}
		chapters, err := s.Store.ListAllChapterSummariesAccessible(e.Auth.Id, novelID)
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		out := make([]map[string]any, 0, len(chapters))
		for _, ch := range chapters {
			out = append(out, chapterSummaryRecord(ch))
		}
		return v1Respond(e, http.StatusOK, map[string]any{"items": out}, nil, nil)
	}
}

func (sharedChapterHandlers) visibility(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		body := struct {
			Excluded bool `json:"excluded"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		novelID := e.Request.PathValue("novelId")
		unlock := s.lockNovel(novelID)
		defer unlock()
		if err := s.Store.SetChapterExcluded(e.Auth.Id, novelID, e.Request.PathValue("chapterId"), body.Excluded); err != nil {
			if errors.Is(err, store.ErrActiveJobs) {
				return e.Error(http.StatusConflict, "cannot exclude chapters while jobs are active on this novel", err)
			}
			return notFoundOrForbidden(e, err)
		}
		chapter, err := s.Store.GetChapterAccessible(e.Auth.Id, novelID, e.Request.PathValue("chapterId"))
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		return v1Respond(e, http.StatusOK, chapterRecord(*chapter), nil, nil)
	}
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
	api.GET("/novels/{novelId}/chapters/excluded", sharedChapters.listExcluded(s))
	api.PATCH("/novels/{novelId}/chapters/order", sharedChapters.reorder(s))
	api.PATCH("/novels/{novelId}/chapters/{chapterId}/visibility", sharedChapters.visibility(s))
}

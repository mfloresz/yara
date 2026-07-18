package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/pocketbase/pocketbase/core"
	pbrouter "github.com/pocketbase/pocketbase/tools/router"
	"translator-server/internal/store"
)

func registerGlossaryRoutes(api *pbrouter.RouterGroup[*core.RequestEvent], s *Server) {
	api.POST("/db/novels/{novelId}/generate-glossary", func(e *core.RequestEvent) error {
		novelID := e.Request.PathValue("novelId")
		userID := e.Auth.Id

		// Ownership check: GetOwnedNovel returns an error unless the novel
		// belongs to the authenticated user.
		if _, err := s.Store.GetOwnedNovel(userID, novelID); err != nil {
			return notFoundOrForbidden(e, err)
		}

		var body struct {
			ChapterFrom       int    `json:"chapterFrom"`
			ChapterTo         int    `json:"chapterTo"`
			Mode              string `json:"mode"`
			MaxTokensPerBatch int    `json:"maxTokensPerBatch"`
			Provider          string `json:"provider"`
			Model             string `json:"model"`
		}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}

		if body.ChapterFrom <= 0 {
			return e.BadRequestError("chapterFrom must be positive", nil)
		}
		if body.ChapterTo > 0 && body.ChapterTo < body.ChapterFrom {
			return e.BadRequestError("chapterTo must be >= chapterFrom", nil)
		}
		if body.Mode != "" && body.Mode != "together" && body.Mode != "batch" {
			return e.BadRequestError("mode must be 'together' or 'batch'", nil)
		}
		if body.Mode == "" {
			body.Mode = "together"
		}
		if body.MaxTokensPerBatch < 0 {
			return e.BadRequestError("maxTokensPerBatch must not be negative", nil)
		}
		if body.MaxTokensPerBatch > maxAllowedTokensPerBatch {
			return e.BadRequestError("maxTokensPerBatch exceeds the allowed maximum", nil)
		}

		chapters, err := s.Store.ListChaptersAccessible(userID, novelID)
		if err != nil {
			return e.InternalServerError("failed to load chapters", err)
		}

		chapterCount := 0
		for _, ch := range chapters {
			if chapterInRangeWithContent(ch, body.ChapterFrom, body.ChapterTo) {
				chapterCount++
			}
		}
		if chapterCount == 0 {
			return e.BadRequestError("no chapters found in the specified range with content", nil)
		}

		options := glossaryJobOptions{
			ChapterFrom:       body.ChapterFrom,
			ChapterTo:         body.ChapterTo,
			Mode:              body.Mode,
			MaxTokensPerBatch: body.MaxTokensPerBatch,
			Provider:          body.Provider,
			Model:             body.Model,
		}
		optionsJSON, err := json.Marshal(options)
		if err != nil {
			return e.InternalServerError("failed to marshal options", err)
		}

		job := &store.Job{
			NovelID:    novelID,
			Status:     "pending",
			Operation:  "generate-glossary",
			Provider:   body.Provider,
			Model:      body.Model,
			OptionsJSON: string(optionsJSON),
		}

		if err := s.Store.CreateJob(userID, job); err != nil {
			return e.InternalServerError("failed to create job", err)
		}

		s.enqueueJob(job.ID)

		return e.JSON(http.StatusOK, map[string]any{
			"jobId": job.ID,
			"status": job.Status,
			"operation": job.Operation,
		})
	})

	// GET /db/novels/{novelId}/estimate-glossary-tokens?from=N&to=M
	// Returns estimated token count for a chapter range so the frontend can
	// show the user an estimate before generating the glossary.
	api.GET("/db/novels/{novelId}/estimate-glossary-tokens", func(e *core.RequestEvent) error {
		novelID := e.Request.PathValue("novelId")
		userID := e.Auth.Id

		if _, err := s.Store.GetOwnedNovel(userID, novelID); err != nil {
			return notFoundOrForbidden(e, err)
		}

		fromStr := e.Request.URL.Query().Get("from")
		if fromStr == "" {
			return e.BadRequestError("from is required", nil)
		}
		from, err := strconv.Atoi(fromStr)
		if err != nil || from <= 0 {
			return e.BadRequestError("from must be a positive integer", nil)
		}

		to := 0
		if toStr := e.Request.URL.Query().Get("to"); toStr != "" {
			to, err = strconv.Atoi(toStr)
			if err != nil || to < 0 {
				return e.BadRequestError("to must be a non-negative integer", nil)
			}
			if to > 0 && to < from {
				return e.BadRequestError("to must be >= from", nil)
			}
		}

		chapters, err := s.Store.ListChaptersAccessible(userID, novelID)
		if err != nil {
			return e.InternalServerError("failed to load chapters", err)
		}

		totalTokens := 0
		chapterCount := 0
		for _, ch := range chapters {
			if chapterInRangeWithContent(ch, from, to) {
				totalTokens += estimateTokens(ch.OriginalContent)
				chapterCount++
			}
		}

		return e.JSON(http.StatusOK, map[string]any{
			"totalTokens": totalTokens,
			"chapterCount": chapterCount,
		})
	})
}

package api

import (
	"encoding/json"
	"net/http"

	"github.com/pocketbase/pocketbase/core"
	pbrouter "github.com/pocketbase/pocketbase/tools/router"
	"translator-server/internal/store"
)

func registerGlossaryRoutes(api *pbrouter.RouterGroup[*core.RequestEvent], s *Server) {
	api.POST("/db/novels/{novelId}/generate-glossary", func(e *core.RequestEvent) error {
		novelID := e.Request.PathValue("novelId")
		userID := e.Auth.Id

		novel, err := s.Store.GetOwnedNovel(userID, novelID)
		if err != nil {
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

		_ = novel

		chapters, err := s.Store.ListChaptersAccessible(userID, novelID)
		if err != nil {
			return e.InternalServerError("failed to load chapters", err)
		}

		chapterCount := 0
		for _, ch := range chapters {
			if ch.ChapterOrder >= body.ChapterFrom && (body.ChapterTo <= 0 || ch.ChapterOrder <= body.ChapterTo) {
				if ch.OriginalContent != "" {
					chapterCount++
				}
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
}

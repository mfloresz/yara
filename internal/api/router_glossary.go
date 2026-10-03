package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/pocketbase/pocketbase/core"
	pbrouter "github.com/pocketbase/pocketbase/tools/router"
	"translator-server/internal/store"
)

func registerV1GlossaryRoutes(api *pbrouter.RouterGroup[*core.RequestEvent], s *Server) {
	api.POST("/novels/{novelId}/glossary/generate", generateGlossaryHandler(s))
	api.GET("/novels/{novelId}/glossary/estimate-tokens", estimateGlossaryTokensHandler(s))
}

func generateGlossaryHandler(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
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
			IncludeExisting   *bool  `json:"includeExisting"`
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

		job, err := s.submitGlossaryJob(userID, novelID, glossaryJobOptions{
			ChapterFrom:       body.ChapterFrom,
			ChapterTo:         body.ChapterTo,
			Mode:              body.Mode,
			MaxTokensPerBatch: body.MaxTokensPerBatch,
			Provider:          body.Provider,
			Model:             body.Model,
			IncludeExisting:   body.IncludeExisting,
		})
		if err != nil {
			switch {
			case errors.Is(err, errGlossaryNoChapters):
				return e.BadRequestError(err.Error(), nil)
			case errors.Is(err, errNovelBusy):
				return e.Error(http.StatusConflict, novelBusyMessage, nil)
			case errors.Is(err, errImportQueueFull):
				return e.Error(http.StatusServiceUnavailable, jobQueueFullMessage, nil)
			}
			return e.InternalServerError("failed to create job", err)
		}

		body2 := map[string]any{
			"jobId":     job.ID,
			"status":    job.Status,
			"operation": job.Operation,
		}
		e.Response.Header().Set("Location", "/api/v1/jobs/"+job.ID)
		return v1Respond(e, http.StatusAccepted, body2, nil, nil)
	}
}

// errGlossaryNoChapters marks a range without chapters that have original
// content; the HTTP handler renders it 400, the agent tool as tool error.
var errGlossaryNoChapters = errors.New("no chapters found in the specified range with content")

// submitGlossaryJob counts the chapters with original content inside the
// requested range, then enqueues a generate-glossary job with the admission
// lock held (one active job per novel: the glossary write must not race
// another job on the same novel). Caller owns argument validation and the
// GetOwnedNovel check. Shared by POST /novels/{novelId}/glossary/generate and
// the agent's generate_glossary tool.
func (s *Server) submitGlossaryJob(userID, novelID string, options glossaryJobOptions) (*store.Job, error) {
	chapters, err := s.Store.ListChaptersAccessible(userID, novelID)
	if err != nil {
		return nil, err
	}
	chapterCount := 0
	for _, ch := range chapters {
		if chapterInRangeWithContent(ch, options.ChapterFrom, options.ChapterTo) {
			chapterCount++
		}
	}
	if chapterCount == 0 {
		return nil, errGlossaryNoChapters
	}
	if options.IncludeExisting == nil {
		v := true
		options.IncludeExisting = &v
	}
	optionsJSON, err := json.Marshal(options)
	if err != nil {
		return nil, err
	}
	job := &store.Job{
		NovelID:     novelID,
		Status:      "pending",
		Operation:   "generate-glossary",
		Provider:    options.Provider,
		Model:       options.Model,
		OptionsJSON: string(optionsJSON),
	}
	unlock, err := s.acquireNovelJobSlot(novelID)
	if err != nil {
		return nil, err // errNovelBusy — callers map it
	}
	defer unlock()
	if err := s.Store.CreateJob(userID, job); err != nil {
		return nil, err
	}
	if !s.enqueueJob(job.ID) {
		return nil, errImportQueueFull
	}
	return job, nil
}

// GET /novels/{novelId}/glossary/estimate-tokens?from=N&to=M
// Returns estimated token count for a chapter range so the frontend can
// show the user an estimate before generating the glossary.
func estimateGlossaryTokensHandler(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
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

		body2 := map[string]any{
			"totalTokens":  totalTokens,
			"chapterCount": chapterCount,
		}
		return v1Respond(e, http.StatusOK, body2, nil, nil)
	}
}

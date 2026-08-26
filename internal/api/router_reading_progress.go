package api

import (
	"net/http"

	"github.com/pocketbase/pocketbase/core"
	pbrouter "github.com/pocketbase/pocketbase/tools/router"
)

func registerV1ReadingProgressRoutes(api *pbrouter.RouterGroup[*core.RequestEvent], s *Server) {
	api.GET("/novels/{novelId}/reading-progress", getReadingProgress(s))
	// PUT is fine for the user's own per-novel progress slot: it is the
	// single canonical entrypoint and the body always represents the full
	// desired state.
	api.PUT("/novels/{novelId}/reading-progress", putReadingProgress(s))
}

func getReadingProgress(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		novelID := e.Request.PathValue("novelId")
		if novelID == "" {
			return e.BadRequestError("novelId is required", nil)
		}
		if _, err := s.Store.GetNovelAccessible(e.Auth.Id, novelID); err != nil {
			return notFoundOrForbidden(e, err)
		}
		rp, err := s.Store.GetReadingProgress(e.Auth.Id, novelID)
		if err != nil {
			return v1Respond(e, http.StatusOK, map[string]any{}, nil, nil)
		}
		return v1Respond(e, http.StatusOK, readingProgressRecord(*rp), nil, nil)
	}
}

func putReadingProgress(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		novelID := e.Request.PathValue("novelId")
		if novelID == "" {
			return e.BadRequestError("novelId is required", nil)
		}
		if _, err := s.Store.GetOwnedNovel(e.Auth.Id, novelID); err != nil {
			return notFoundOrForbidden(e, err)
		}
		body := struct {
			ChapterID     string  `json:"chapterId"`
			ScrollPercent float64 `json:"scrollPercent"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		if body.ChapterID == "" {
			return e.BadRequestError("chapterId is required", nil)
		}
		rp, err := s.Store.UpsertReadingProgress(e.Auth.Id, novelID, body.ChapterID, body.ScrollPercent)
		if err != nil {
			return e.InternalServerError("failed to save reading progress", err)
		}
		return v1Respond(e, http.StatusOK, readingProgressRecord(*rp), nil, nil)
	}
}

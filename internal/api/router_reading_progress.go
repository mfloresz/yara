package api

import (
	"net/http"

	"github.com/pocketbase/pocketbase/core"
	pbrouter "github.com/pocketbase/pocketbase/tools/router"
	"translator-server/internal/store"
)

func registerV1ReadingProgressRoutes(api *pbrouter.RouterGroup[*core.RequestEvent], s *Server) {
	api.GET("/novels/{novelId}/reading-progress", getReadingProgress(s))
	// PUT is fine for the user's own per-novel progress slot: it is the
	// single canonical entrypoint and the body always represents the full
	// desired state. Reading is not ownership-restricted (same as the comics
	// tree), so the slot is reachable for any accessible novel.
	api.PUT("/novels/{novelId}/reading-progress", putReadingProgress(s))
	// DELETE clears the slot entirely (resume point plus reading mark) so the
	// work drops out of "continue reading" without deleting the novel.
	api.DELETE("/novels/{novelId}/reading-progress", deleteReadingProgress(s))
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
		if _, err := s.Store.GetNovelAccessible(e.Auth.Id, novelID); err != nil {
			return notFoundOrForbidden(e, err)
		}
		body := struct {
			ChapterID     string  `json:"chapterId"`
			ScrollPercent float64 `json:"scrollPercent"`
			Status        string  `json:"status"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		if body.ChapterID == "" && body.Status == "" {
			return e.BadRequestError("chapterId or status is required", nil)
		}
		if body.Status != "" && !isValidReadingStatus(body.Status) {
			return e.BadRequestError("invalid status; expected reading, paused or finished", nil)
		}
		rp, err := s.Store.UpsertReadingProgress(e.Auth.Id, novelID, body.ChapterID, body.ScrollPercent, body.Status)
		if err != nil {
			return e.InternalServerError("failed to save reading progress", err)
		}
		return v1Respond(e, http.StatusOK, readingProgressRecord(*rp), nil, nil)
	}
}

func deleteReadingProgress(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		novelID := e.Request.PathValue("novelId")
		if novelID == "" {
			return e.BadRequestError("novelId is required", nil)
		}
		if _, err := s.Store.GetNovelAccessible(e.Auth.Id, novelID); err != nil {
			return notFoundOrForbidden(e, err)
		}
		if err := s.Store.DeleteReadingProgress(e.Auth.Id, novelID); err != nil {
			return e.InternalServerError("failed to delete reading progress", err)
		}
		return e.NoContent(http.StatusNoContent)
	}
}

func isValidReadingStatus(status string) bool {
	switch status {
	case store.ReadingStatusReading, store.ReadingStatusPaused, store.ReadingStatusFinished:
		return true
	}
	return false
}

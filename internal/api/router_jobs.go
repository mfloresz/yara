package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/pocketbase/pocketbase/core"
	pbrouter "github.com/pocketbase/pocketbase/tools/router"
	"translator-server/internal/store"
)

type sharedJobHandlers struct{}

var sharedJobs = sharedJobHandlers{}

// createJob: POST /novels/{novelId}/jobs. Used by both /api/db/novels/{id}/translation-jobs
// (legacy) and /api/v1/novels/{id}/jobs. Async job creation — return 201 (the
// job is a resource) plus Location so clients can poll its status.
func (sharedJobHandlers) create(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		body := struct {
			ChapterIDs []string       `json:"chapterIds"`
			Operation  string         `json:"operation"`
			Options    map[string]any `json:"options"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		provider, _ := body.Options["provider"].(string)
		model, _ := body.Options["model"].(string)
		idsJSON, _ := json.Marshal(body.ChapterIDs)
		optionsJSON, _ := json.Marshal(body.Options)
		job := &store.Job{NovelID: e.Request.PathValue("novelId"), Status: "pending", Operation: defaultString(body.Operation, "translate"), Provider: provider, Model: model, ChapterIDs: string(idsJSON), OptionsJSON: string(optionsJSON), TotalChapters: len(body.ChapterIDs)}
		if err := s.Store.CreateJob(e.Auth.Id, job); err != nil {
			return notFoundOrForbidden(e, err)
		}
		if chapters, _, err := s.Store.LoadJobChapters(job); err != nil {
			slog.Error("load job chapters for processing mark", "jobId", job.ID, "error", err)
		} else {
			chapterIDs := make([]string, 0, len(chapters))
			for _, chapter := range chapters {
				chapterIDs = append(chapterIDs, chapter.ID)
			}
			if err := s.Store.UpdateChaptersStatusFast(chapterIDs, "processing", ""); err != nil {
				slog.Error("mark job chapters processing", "jobId", job.ID, "error", err)
			}
		}
		if !s.enqueueJob(job.ID) {
			if err := s.Store.ReconcileProcessingChaptersForJob(job.ID); err != nil {
				slog.Warn("reconcile chapters after queue rejection", "jobId", job.ID, "error", err)
			}
			return e.Error(http.StatusServiceUnavailable, jobQueueFullMessage, nil)
		}
		// 429 (queue full) is the spec-correct status for "resource created
		// but couldn't be queued". We return 503 to match existing behavior;
		// the legacy /translation-jobs route does the same.
		if isV1Request(e) {
			e.Response.Header().Set("Location", "/api/v1/jobs/"+job.ID)
			e.Response.Header().Set("Retry-After", "30")
			return v1Respond(e, http.StatusCreated, jobRecord(*job), nil, nil)
		}
		return e.JSON(http.StatusCreated, jobRecord(*job))
	}
}

func (sharedJobHandlers) list(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		failedOnly := e.Request.URL.Query().Get("failedOnly") == "1"
		jobs, err := s.Store.ListJobs(e.Auth.Id, e.Request.PathValue("novelId"), failedOnly)
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		out := make([]map[string]any, 0, len(jobs))
		for _, job := range jobs {
			out = append(out, jobRecord(job))
		}
		if isV1Request(e) {
			q := e.Request.URL.Query()
			page, perPage, _, _ := parsePagination(q)
			return v1RespondList(e, http.StatusOK, out, page, perPage, len(out), false, e.Request.URL.Path)
		}
		return e.JSON(http.StatusOK, out)
	}
}

// activeJobs: GET /jobs/active. Legacy: /api/db/translation-jobs/active.
// The /api/db/translation-jobs/active/status endpoint is folded here as a
// sub-resource: GET /api/v1/jobs/active returns {data:[...], meta:{has_active}}
// and the legacy /status path returns the bare {hasActive:true} shape so the
// existing frontend polling code keeps working.
func (sharedJobHandlers) active(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		jobs, err := s.Store.ListActiveJobs(e.Auth.Id)
		if err != nil {
			return e.InternalServerError("failed to list active jobs", err)
		}
		out := make([]map[string]any, 0, len(jobs))
		for _, job := range jobs {
			out = append(out, jobRecord(job))
		}
		if isV1Request(e) {
			return v1Respond(e, http.StatusOK, map[string]any{
				"jobs":      out,
				"hasActive": len(out) > 0,
			}, &v1Meta{Total: len(out), HasMore: false}, nil)
		}
		return e.JSON(http.StatusOK, out)
	}
}

// activeStatus: legacy /api/db/translation-jobs/active/status. Only registered
// on the legacy group; v1 clients read hasActive from the active endpoint.
func (sharedJobHandlers) activeStatus(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		hasActive, err := s.Store.HasActiveJobs(e.Auth.Id)
		if err != nil {
			return e.InternalServerError("failed to read active job status", err)
		}
		return e.JSON(http.StatusOK, map[string]any{"hasActive": hasActive})
	}
}

func (sharedJobHandlers) get(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		job, err := s.Store.GetOwnedJob(e.Auth.Id, e.Request.PathValue("jobId"))
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		if isV1Request(e) {
			return v1Respond(e, http.StatusOK, jobRecord(*job), nil, nil)
		}
		return e.JSON(http.StatusOK, jobRecord(*job))
	}
}

// patchJob: legacy PATCH /db/translation-jobs/{jobId}. Accepts {status:
// "cancelled"|"pending"} (retry) plus any other patchable fields. v1 splits
// this into explicit :cancel and :retry sub-routes; the shared handler still
// understands the legacy body for backward compat.
func (sharedJobHandlers) patch(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		patch := map[string]any{}
		if err := e.BindBody(&patch); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		jobId := e.Request.PathValue("jobId")
		status, hasStatus := patch["status"].(string)
		if hasStatus {
			job, err := s.Store.GetOwnedJob(e.Auth.Id, jobId)
			if err != nil {
				return notFoundOrForbidden(e, err)
			}
			// Cancelling is always allowed; re-queueing is a retry and must not
			// race an in-flight execution, which would run the job twice.
			switch status {
			case "cancelled":
			case "pending":
				if job.Status == "pending" || job.Status == "running" {
					return e.Error(http.StatusConflict, "job is already active and cannot be re-queued", nil)
				}
			default:
				return e.BadRequestError("invalid job status", nil)
			}
		}
		if err := s.Store.UpdateJobForUser(e.Auth.Id, jobId, patch); err != nil {
			return notFoundOrForbidden(e, err)
		}
		if hasStatus && status == "pending" {
			if !s.enqueueJob(jobId) {
				return e.Error(http.StatusServiceUnavailable, jobQueueFullMessage, nil)
			}
		} else if hasStatus && status == "cancelled" {
			s.cancelJob(jobId)
			if err := s.Store.ReconcileProcessingChaptersForJob(jobId); err != nil {
				slog.Error("reconcile cancelled job chapters", "jobId", jobId, "error", err)
			}
			if job, jErr := s.Store.GetOwnedJob(e.Auth.Id, jobId); jErr == nil {
				if err := s.Store.RecalculateNovelStats(job.NovelID); err != nil {
					slog.Error("recalculate novel stats on cancel", "jobId", jobId, "error", err)
				}
			}
		}
		job, err := s.Store.GetOwnedJob(e.Auth.Id, e.Request.PathValue("jobId"))
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		if isV1Request(e) {
			return v1Respond(e, http.StatusOK, jobRecord(*job), nil, nil)
		}
		return e.JSON(http.StatusOK, jobRecord(*job))
	}
}

// cancelJob: POST /jobs/{jobId}:cancel — v1 only. Maps to status=cancelled.
func (sharedJobHandlers) cancel(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		jobId := e.Request.PathValue("jobId")
		job, err := s.Store.GetOwnedJob(e.Auth.Id, jobId)
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		if err := s.Store.UpdateJobForUser(e.Auth.Id, jobId, map[string]any{"status": "cancelled"}); err != nil {
			return notFoundOrForbidden(e, err)
		}
		s.cancelJob(jobId)
		if err := s.Store.ReconcileProcessingChaptersForJob(jobId); err != nil {
			slog.Error("reconcile cancelled job chapters", "jobId", jobId, "error", err)
		}
		if err := s.Store.RecalculateNovelStats(job.NovelID); err != nil {
			slog.Error("recalculate novel stats on cancel", "jobId", jobId, "error", err)
		}
		updated, err := s.Store.GetOwnedJob(e.Auth.Id, jobId)
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		return v1Respond(e, http.StatusOK, jobRecord(*updated), nil, nil)
	}
}

// retryJob: POST /jobs/{jobId}:retry — v1 only. Re-queues a failed/cancelled
// job. Returns 409 if the job is already active.
func (sharedJobHandlers) retry(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		jobId := e.Request.PathValue("jobId")
		job, err := s.Store.GetOwnedJob(e.Auth.Id, jobId)
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		if job.Status == "pending" || job.Status == "running" {
			return e.Error(http.StatusConflict, "job is already active and cannot be re-queued", nil)
		}
		if err := s.Store.UpdateJobForUser(e.Auth.Id, jobId, map[string]any{"status": "pending"}); err != nil {
			return notFoundOrForbidden(e, err)
		}
		if !s.enqueueJob(jobId) {
			return e.Error(http.StatusServiceUnavailable, jobQueueFullMessage, nil)
		}
		updated, err := s.Store.GetOwnedJob(e.Auth.Id, jobId)
		if err != nil {
			return notFoundOrForbidden(e, err)
		}
		return v1Respond(e, http.StatusOK, jobRecord(*updated), nil, nil)
	}
}

func registerJobRoutes(api *pbrouter.RouterGroup[*core.RequestEvent], s *Server) {
	api.POST("/db/novels/{novelId}/translation-jobs", sharedJobs.create(s))
	api.GET("/db/novels/{novelId}/translation-jobs", sharedJobs.list(s))
	api.GET("/db/translation-jobs/active/status", sharedJobs.activeStatus(s))
	api.GET("/db/translation-jobs/active", sharedJobs.active(s))
	api.PATCH("/db/translation-jobs/{jobId}", sharedJobs.patch(s))
}

func registerV1JobRoutes(api *pbrouter.RouterGroup[*core.RequestEvent], s *Server) {
	api.POST("/novels/{novelId}/jobs", sharedJobs.create(s))
	api.GET("/novels/{novelId}/jobs", sharedJobs.list(s))
	api.GET("/jobs/active", sharedJobs.active(s))
	api.GET("/jobs/{jobId}", sharedJobs.get(s))
	// v1 RPC-style lifecycle endpoints. PATCH /jobs/{jobId} is no longer used
	// on v1 — clients call :cancel or :retry instead.
	api.POST("/jobs/{jobId}/cancel", sharedJobs.cancel(s))
	api.POST("/jobs/{jobId}/retry", sharedJobs.retry(s))
}

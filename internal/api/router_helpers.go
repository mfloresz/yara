package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"translator-server/internal/store"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

// requireAdmin rejects the request with a v1 problem+json 403 unless the
// authenticated user has the admin role. Mounted on the /api/v1/admin group.
func requireAdmin() *hook.Handler[*core.RequestEvent] {
	return &hook.Handler[*core.RequestEvent]{
		Id: "requireAdmin",
		Func: func(e *core.RequestEvent) error {
			if e.Auth == nil || e.Auth.GetString("role") != store.RoleAdmin {
				return writeV1Error(e, http.StatusForbidden, "forbidden", "admin role required")
			}
			return e.Next()
		},
	}
}

// rejectBlocked rejects authenticated requests from blocked users. Mounted on
// every authed v1 group after RequireAuth so a block takes effect without
// waiting for the JWT to expire (block also rotates the tokenKey, this is the
// safety net for already-issued tokens).
func rejectBlocked() *hook.Handler[*core.RequestEvent] {
	return &hook.Handler[*core.RequestEvent]{
		Id: "rejectBlocked",
		Func: func(e *core.RequestEvent) error {
			if e.Auth != nil && e.Auth.GetBool("blocked") {
				return writeV1Error(e, http.StatusForbidden, "account_blocked", "account is blocked")
			}
			return e.Next()
		},
	}
}

// v1ServiceError maps store-level sentinel errors to v1 problem+json
// responses for admin endpoints (notFoundOrForbidden doesn't know about
// ErrLastAdmin / validation failures).
func v1ServiceError(e *core.RequestEvent, err error) error {
	switch err {
	case store.ErrNotFound:
		return writeV1Error(e, http.StatusNotFound, "not_found", "resource not found")
	case store.ErrForbidden:
		return writeV1Error(e, http.StatusForbidden, "forbidden", "forbidden")
	case store.ErrLastAdmin:
		return writeV1Error(e, http.StatusConflict, "last_admin", "at least one admin user is required")
	case store.ErrActiveJobs:
		return writeV1Error(e, http.StatusConflict, "active_jobs", "user has active jobs; cancel them first")
	case store.ErrInvalidInput:
		return writeV1Error(e, http.StatusBadRequest, "validation_failed", "invalid input")
	default:
		slog.Error("admin endpoint internal error", "error", err)
		return writeV1Error(e, http.StatusInternalServerError, "internal_error", "internal error")
	}
}

func jsonString(value any, fallback string) string {
	if value == nil {
		return fallback
	}
	b, err := json.Marshal(value)
	if err != nil || string(b) == "null" {
		return fallback
	}
	return string(b)
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func notFoundOrForbidden(e *core.RequestEvent, err error) error {
	switch err {
	case store.ErrForbidden:
		return e.ForbiddenError("forbidden", err)
	case store.ErrNotFound:
		return e.NotFoundError("not found", err)
	default:
		return e.InternalServerError("internal error", err)
	}
}

func defaultTheme(theme string) string {
	switch theme {
	case "light", "dark", "system":
		return theme
	default:
		return "system"
	}
}

func cleaningSource(ch *store.Chapter, applyTo string) string {
	switch applyTo {
	case "original":
		return ch.OriginalContent
	case "translated":
		return ch.TranslatedContent
	case "refined":
		return ch.RefinedContent
	case "all":
		if ch.RefinedContent != "" {
			return ch.RefinedContent
		}
		if ch.TranslatedContent != "" {
			return ch.TranslatedContent
		}
		return ch.OriginalContent
	default:
		return ""
	}
}

func isValidCleanMode(mode string) bool {
	switch CleanMode(mode) {
	case CleanModeRemoveAfter, CleanModeRemoveDuplicates, CleanModeRemoveLine, CleanModeRemoveMultipleBlanks, CleanModeSearchReplace:
		return true
	default:
		return false
	}
}

func isValidApplyTo(applyTo string) bool {
	switch applyTo {
	case "original", "translated", "refined", "all":
		return true
	default:
		return false
	}
}

func bearerToken(r *http.Request) string {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(header), "bearer ") {
		return strings.TrimSpace(header[7:])
	}
	if cookie, err := r.Cookie(authCookieName); err == nil && cookie.Value != "" {
		return cookie.Value
	}
	return ""
}

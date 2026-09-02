package api

import (
	"log/slog"
	"net/http"

	"github.com/pocketbase/pocketbase/core"
	pbrouter "github.com/pocketbase/pocketbase/tools/router"
	"translator-server/internal/store"
)

// registerV1AdminRoutes mounts the admin panel endpoints under
// /api/v1/admin/*. The group is already wrapped by requireAdmin(); every
// mutation writes an audit line via slog.
func registerV1AdminRoutes(admin *pbrouter.RouterGroup[*core.RequestEvent], s *Server) {
	admin.GET("/users", adminListUsers(s))
	admin.PATCH("/users/{userId}", adminUpdateUserRole(s))
}

func adminListUsers(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		users, err := s.Store.ListUsers()
		if err != nil {
			return v1ServiceError(e, err)
		}
		out := make([]map[string]any, 0, len(users))
		for _, user := range users {
			out = append(out, adminUserRecord(user))
		}
		return v1RespondList(e, http.StatusOK, out, 1, len(out), len(out), false, e.Request.URL.Path)
	}
}

func adminUpdateUserRole(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		userID := e.Request.PathValue("userId")
		body := struct {
			Role string `json:"role"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return writeV1Error(e, http.StatusBadRequest, "validation_failed", "invalid body")
		}
		user, err := s.Store.UpdateUserRole(userID, body.Role)
		if err != nil {
			return v1ServiceError(e, err)
		}
		slog.Info("user role changed", "actorId", e.Auth.Id, "userId", userID, "role", body.Role)
		return v1Respond(e, http.StatusOK, adminUserRecord(user), nil, nil)
	}
}

func adminUserRecord(user store.User) map[string]any {
	return map[string]any{
		"id":        user.ID,
		"email":     user.Email,
		"name":      user.Name,
		"role":      user.Role,
		"createdAt": user.CreatedAt,
		"updatedAt": user.UpdatedAt,
	}
}

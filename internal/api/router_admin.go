package api

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

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

	admin.GET("/invitations", adminListInvitations(s))
	admin.POST("/invitations", adminCreateInvitation(s))
	admin.DELETE("/invitations/{invitationId}", adminDeleteInvitation(s))
}

func adminListInvitations(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		invitations, err := s.Store.ListInvitations()
		if err != nil {
			return v1ServiceError(e, err)
		}
		out := make([]store.Invitation, 0, len(invitations))
		out = append(out, invitations...)
		return v1RespondList(e, http.StatusOK, out, 1, len(out), len(out), false, e.Request.URL.Path)
	}
}

func adminCreateInvitation(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		body := struct {
			Email string `json:"email"`
			Role  string `json:"role"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return writeV1Error(e, http.StatusBadRequest, "validation_failed", "invalid body")
		}
		invitation, rawToken, err := s.Store.CreateInvitation(e.Auth.Id, body.Email, body.Role)
		if err != nil {
			switch err {
			case store.ErrEmailTaken:
				return writeV1Error(e, http.StatusConflict, "email_taken", "this email is already registered")
			case store.ErrInvalidInput:
				return writeV1Error(e, http.StatusBadRequest, "validation_failed", "email and role are required")
			default:
				return v1ServiceError(e, err)
			}
		}
		slog.Info("invitation created", "actorId", e.Auth.Id, "invitationId", invitation.ID, "role", invitation.Role)

		scheme := "http"
		if e.Request.TLS != nil || strings.HasPrefix(e.Request.Header.Get("X-Forwarded-Proto"), "https") {
			scheme = "https"
		}
		invitationURL := fmt.Sprintf("%s://%s/invite/%s", scheme, e.Request.Host, rawToken)
		return v1Respond(e, http.StatusCreated, map[string]any{
			"invitation":    invitation,
			"invitationUrl": invitationURL,
		}, nil, nil)
	}
}

func adminDeleteInvitation(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		id := e.Request.PathValue("invitationId")
		if err := s.Store.DeleteInvitation(id); err != nil {
			return v1ServiceError(e, err)
		}
		slog.Info("invitation deleted", "actorId", e.Auth.Id, "invitationId", id)
		return e.NoContent(http.StatusNoContent)
	}
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

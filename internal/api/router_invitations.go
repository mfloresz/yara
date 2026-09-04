package api

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	pbrouter "github.com/pocketbase/pocketbase/tools/router"
	"translator-server/internal/store"
)

// Public (unauthenticated) invitation endpoints, mounted under
// /api/v1/auth/invitations/*. Validate never explains WHY an invitation is
// invalid (unknown / used / expired) to avoid leaking invitation state.
func registerV1InvitationPublicRoutes(auth *pbrouter.RouterGroup[*core.RequestEvent], s *Server) {
	auth.POST("/invitations/validate", withJSONBodyLimit(maxAuthBodyBytes, withIPRateLimit(s.invitationLimiter, handleInvitationValidate(s))))
	auth.POST("/invitations/accept", withJSONBodyLimit(maxAuthBodyBytes, withIPRateLimit(s.invitationLimiter, handleInvitationAccept(s))))
}

func handleInvitationValidate(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		body := struct {
			Token string `json:"token"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return writeV1Error(e, http.StatusBadRequest, "validation_failed", "invalid body")
		}
		if strings.TrimSpace(body.Token) == "" {
			return writeV1Error(e, http.StatusBadRequest, "validation_failed", "token is required")
		}
		invitation, err := s.Store.FindInvitationByToken(body.Token)
		if err != nil {
			return v1Respond(e, http.StatusOK, map[string]any{"valid": false}, nil, nil)
		}
		return v1Respond(e, http.StatusOK, map[string]any{
			"valid":     true,
			"email":     invitation.Email,
			"role":      invitation.Role,
			"expiresAt": invitation.ExpiresAt,
		}, nil, nil)
	}
}

func handleInvitationAccept(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		body := struct {
			Token    string `json:"token"`
			Password string `json:"password"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return writeV1Error(e, http.StatusBadRequest, "validation_failed", "invalid body")
		}
		if strings.TrimSpace(body.Token) == "" || body.Password == "" {
			return writeV1Error(e, http.StatusBadRequest, "validation_failed", "token and password are required")
		}
		if len(body.Password) < 8 {
			return writeV1Error(e, http.StatusBadRequest, "validation_failed", "password must be at least 8 characters")
		}

		// Serialize redemption: two concurrent accepts of the same token must
		// not both pass the check-then-create sequence.
		s.invitationMu.Lock()
		user, err := s.Store.RedeemInvitation(body.Token, body.Password)
		s.invitationMu.Unlock()
		if err != nil {
			switch err {
			case store.ErrNotFound, store.ErrInvitationUsed, store.ErrInvitationExpired:
				return writeV1Error(e, http.StatusBadRequest, "invalid_invitation", "invitation is invalid, already used or expired")
			case store.ErrEmailTaken:
				return writeV1Error(e, http.StatusConflict, "email_taken", "this email is already registered")
			default:
				slog.Error("invitation redemption failed", "error", err)
				return writeV1Error(e, http.StatusInternalServerError, "internal_error", "internal error")
			}
		}
		slog.Info("invitation redeemed", "userId", user.ID, "role", user.Role)
		return v1Respond(e, http.StatusCreated, map[string]any{"email": user.Email, "role": user.Role}, nil, nil)
	}
}

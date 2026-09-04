package api

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	pbrouter "github.com/pocketbase/pocketbase/tools/router"
	"translator-server/internal/store"
)

// Public (unauthenticated) password-reset endpoints, mounted under
// /api/v1/auth/password-reset/*. Mirror the invitation surface: validate
// never explains WHY a token is invalid, bodies are capped and rate-limited.
func registerV1PasswordResetPublicRoutes(auth *pbrouter.RouterGroup[*core.RequestEvent], s *Server) {
	auth.POST("/password-reset/validate", withJSONBodyLimit(maxAuthBodyBytes, withIPRateLimit(s.invitationLimiter, handlePasswordResetValidate(s))))
	auth.POST("/password-reset/accept", withJSONBodyLimit(maxAuthBodyBytes, withIPRateLimit(s.invitationLimiter, handlePasswordResetAccept(s))))
}

func handlePasswordResetValidate(s *Server) func(*core.RequestEvent) error {
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
		reset, err := s.Store.FindPasswordResetByToken(body.Token)
		if err != nil {
			return v1Respond(e, http.StatusOK, map[string]any{"valid": false}, nil, nil)
		}
		return v1Respond(e, http.StatusOK, map[string]any{
			"valid":     true,
			"email":     reset.Email,
			"expiresAt": reset.ExpiresAt,
		}, nil, nil)
	}
}

func handlePasswordResetAccept(s *Server) func(*core.RequestEvent) error {
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
		s.invitationMu.Lock()
		user, err := s.Store.RedeemPasswordReset(body.Token, body.Password)
		s.invitationMu.Unlock()
		if err != nil {
			switch err {
			case store.ErrNotFound, store.ErrInvitationUsed, store.ErrInvitationExpired:
				return writeV1Error(e, http.StatusBadRequest, "invalid_reset", "reset link is invalid, already used or expired")
			case store.ErrInvalidInput:
				return writeV1Error(e, http.StatusBadRequest, "validation_failed", "password must be at least 8 characters")
			default:
				slog.Error("password reset redemption failed", "error", err)
				return writeV1Error(e, http.StatusInternalServerError, "internal_error", "internal error")
			}
		}
		slog.Info("password reset redeemed", "userId", user.ID)
		clearAuthCookie(e)
		return v1Respond(e, http.StatusOK, map[string]any{"email": user.Email}, nil, nil)
	}
}

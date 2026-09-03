package api

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	"translator-server/internal/store"
)

const authCookieName = "auth.token"

func setAuthCookie(e *core.RequestEvent, token string) {
	secure := e.Request.TLS != nil || strings.HasPrefix(e.Request.Header.Get("X-Forwarded-Proto"), "https")
	e.SetCookie(&http.Cookie{
		Name:     authCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   30 * 24 * 60 * 60, // 30 days
	})
}

func clearAuthCookie(e *core.RequestEvent) {
	e.SetCookie(&http.Cookie{
		Name:     authCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

// loadAuthFromCookie populates e.Auth from the HttpOnly auth cookie when the
// request has no (or an invalid) Authorization header. PocketBase's built-in
// loadAuthToken middleware only reads the Authorization header, so without
// this the cookie-based token is never picked up and apis.RequireAuth()
// rejects every request once the token is no longer readable by JS.
func loadAuthFromCookie() *hook.Handler[*core.RequestEvent] {
	return &hook.Handler[*core.RequestEvent]{
		Id: "loadAuthFromCookie",
		Func: func(e *core.RequestEvent) error {
			if e.Auth != nil {
				return e.Next()
			}
			cookie, err := e.Request.Cookie(authCookieName)
			if err != nil || cookie.Value == "" {
				return e.Next()
			}
			record, err := e.App.FindAuthRecordByToken(cookie.Value, core.TokenTypeAuth)
			if err == nil && record != nil {
				e.Auth = record
			}
			return e.Next()
		},
	}
}

// Shared auth handler functions. Mounted under /api/v1/auth/*.
func handleAuthRegister(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		body := struct {
			Email    string `json:"email"`
			Password string `json:"password"`
			Name     string `json:"name"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		if strings.TrimSpace(body.Email) == "" || strings.TrimSpace(body.Password) == "" {
			return e.BadRequestError("email and password are required", nil)
		}
		if len(body.Password) < 8 {
			return writeV1Error(e, http.StatusBadRequest, "validation_failed", "password must be at least 8 characters")
		}

		// First user on a fresh install becomes the admin. Everything after
		// that requires an invitation (handleInvitationAccept creates the
		// invited users itself and never goes through this handler).
		s.bootstrapMu.Lock()
		userCount, err := s.Store.CountUsers()
		if err != nil {
			s.bootstrapMu.Unlock()
			return e.InternalServerError("failed to count users", err)
		}
		if userCount > 0 {
			s.bootstrapMu.Unlock()
			return writeV1Error(e, http.StatusForbidden, "forbidden", "registration requires an invitation")
		}
		result, err := s.Store.CreateUser(body.Email, body.Password, body.Name)
		if err != nil {
			s.bootstrapMu.Unlock()
			return writeV1Error(e, http.StatusBadRequest, "validation_failed", "failed to create user")
		}
		promoted, err := s.Store.UpdateUserRole(result.User.ID, store.RoleAdmin)
		if err != nil {
			s.bootstrapMu.Unlock()
			return e.InternalServerError("failed to promote first user", err)
		}
		s.bootstrapMu.Unlock()

		result.User = promoted
		setAuthCookie(e, result.Token)
		slog.Info("first user registered as admin", "userId", result.User.ID)
		// The session token is only delivered via the HttpOnly cookie; it is
		// never echoed in the response body where JS-readable XSS payloads
		// could exfiltrate it.
		return v1Respond(e, http.StatusCreated, map[string]any{"user": result.User}, nil, nil)
	}
}

// handleAuthSetupStatus tells a fresh install's UI that the first (admin)
// user can still register directly. Public by necessity: it is called before
// any user exists. It leaks nothing beyond "this install has no users".
func handleAuthSetupStatus(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		userCount, err := s.Store.CountUsers()
		if err != nil {
			return e.InternalServerError("failed to count users", err)
		}
		return v1Respond(e, http.StatusOK, map[string]any{"needsSetup": userCount == 0}, nil, nil)
	}
}

func handleAuthLogin(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		body := struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		result, err := s.Store.AuthenticateUser(body.Email, body.Password)
		if err != nil {
			return e.BadRequestError("invalid credentials", nil)
		}
		setAuthCookie(e, result.Token)
		return v1Respond(e, http.StatusOK, map[string]any{"user": result.User}, nil, nil)
	}
}

func handleAuthMe(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		user := store.User{ID: e.Auth.Id, Email: e.Auth.Email(), Name: e.Auth.GetString("name"), Role: store.RoleUser, Theme: defaultTheme(e.Auth.GetString("theme")), CreatedAt: e.Auth.GetString("created"), UpdatedAt: e.Auth.GetString("updated")}
		if e.Auth.GetString("role") == store.RoleAdmin {
			user.Role = store.RoleAdmin
		}
		return v1Respond(e, http.StatusOK, user, nil, nil)
	}
}

func handleAuthRefresh(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		token := bearerToken(e.Request)
		result, err := s.Store.RefreshAuth(token)
		if err != nil {
			return e.UnauthorizedError("invalid token", err)
		}
		setAuthCookie(e, result.Token)
		return v1Respond(e, http.StatusOK, map[string]any{"user": result.User}, nil, nil)
	}
}

func handleAuthLogout(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		clearAuthCookie(e)
		return e.NoContent(http.StatusNoContent)
	}
}

// handleAuthLogoutAll invalidates every outstanding session token for the
// calling user. PocketBase signs auth JWTs with the record's tokenKey plus
// the collection secret, so rotating the tokenKey (RefreshTokenKey + save)
// kills all previously issued tokens at once — this is the deliberate
// "logout everywhere" action; the plain logout above only clears the
// calling client's cookie.
func handleAuthLogoutAll(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		e.Auth.RefreshTokenKey()
		if err := e.App.Save(e.Auth); err != nil {
			return e.InternalServerError("failed to invalidate sessions", err)
		}
		slog.Info("all sessions invalidated", "userId", e.Auth.Id)
		clearAuthCookie(e)
		return e.NoContent(http.StatusNoContent)
	}
}

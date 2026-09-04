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

	admin.GET("/provider-keys", adminListProviderKeys(s))
	admin.PUT("/provider-keys/{providerKey}", adminUpsertProviderKey(s))
	admin.DELETE("/provider-keys/{providerKey}", adminDeleteProviderKey(s))

	admin.GET("/prompt-overrides", adminListPromptOverrides(s))
	admin.PUT("/prompt-overrides/{promptKey}", adminUpsertPromptOverride(s))
	admin.DELETE("/prompt-overrides/{promptKey}", adminDeletePromptOverride(s))
	admin.GET("/prompts", adminListEffectivePrompts(s))

	admin.POST("/backups/export", backupDownload(s))
}

func adminListPromptOverrides(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		overrides, err := s.Store.ListPromptOverrides()
		if err != nil {
			return v1ServiceError(e, err)
		}
		out := make([]store.Prompt, 0, len(overrides))
		out = append(out, overrides...)
		return v1RespondList(e, http.StatusOK, out, 1, len(out), len(out), false, e.Request.URL.Path)
	}
}

func adminUpsertPromptOverride(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		key := e.Request.PathValue("promptKey")
		body := struct {
			Prompt struct {
				SystemPrompt string `json:"systemPrompt"`
				UserPrompt   string `json:"userPrompt"`
			} `json:"prompt"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return writeV1Error(e, http.StatusBadRequest, "validation_failed", "invalid body")
		}
		prompt, err := s.Store.UpsertPromptOverride(store.Prompt{Key: key, SystemPrompt: body.Prompt.SystemPrompt, UserPrompt: body.Prompt.UserPrompt})
		if err != nil {
			switch err {
			case store.ErrInvalidInput:
				return writeV1Error(e, http.StatusBadRequest, "validation_failed", "unknown prompt key or prompt too long")
			default:
				return v1ServiceError(e, err)
			}
		}
		slog.Info("prompt override set", "actorId", e.Auth.Id, "promptKey", key)
		return v1Respond(e, http.StatusOK, prompt, nil, nil)
	}
}

func adminDeletePromptOverride(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		key := e.Request.PathValue("promptKey")
		if err := s.Store.DeletePromptOverride(key); err != nil {
			return v1ServiceError(e, err)
		}
		slog.Info("prompt override reset to embedded default", "actorId", e.Auth.Id, "promptKey", key)
		return e.NoContent(http.StatusNoContent)
	}
}

// adminListEffectivePrompts returns the 5 known prompts with the admin's
// global override applied on top of the embedded defaults, so the admin UI
// can show what is actually in effect today (same precedence the user sees,
// minus the per-user layer which the admin cannot edit).
func adminListEffectivePrompts(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		prompts, err := s.Store.ListEffectiveAdminPrompts()
		if err != nil {
			return v1ServiceError(e, err)
		}
		return v1RespondList(e, http.StatusOK, prompts, 1, len(prompts), len(prompts), false, e.Request.URL.Path)
	}
}

func adminListProviderKeys(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		providers, err := s.Store.App.FindRecordsByFilter(store.ProvidersCollection, "", "key", 200, 0)
		if err != nil {
			return v1ServiceError(e, err)
		}
		sharedKeys, err := s.Store.ListSharedProviderKeys()
		if err != nil {
			return v1ServiceError(e, err)
		}
		sharedByKey := map[string]store.SharedProviderKey{}
		for _, item := range sharedKeys {
			sharedByKey[item.Provider] = item
		}
		out := make([]map[string]any, 0, len(providers))
		for _, provider := range providers {
			key := provider.GetString("key")
			entry := map[string]any{
				"provider":   key,
				"label":      provider.GetString("label"),
				"enabled":    provider.GetBool("enabled"),
				"configured": false,
				"shared":     false,
			}
			if shared := sharedByKey[key]; shared.Provider != "" {
				entry["configured"] = shared.Configured
				entry["shared"] = shared.Shared
				entry["apiKeyUpdatedAt"] = shared.APIKeyUpdatedAt
			}
			out = append(out, entry)
		}
		return v1RespondList(e, http.StatusOK, out, 1, len(out), len(out), false, e.Request.URL.Path)
	}
}

func adminUpsertProviderKey(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		providerKey := e.Request.PathValue("providerKey")
		body := struct {
			APIKey string `json:"apiKey"`
			Shared bool   `json:"shared"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return writeV1Error(e, http.StatusBadRequest, "validation_failed", "invalid body")
		}
		entry, err := s.Store.UpsertSharedProviderKey(providerKey, body.APIKey, body.Shared, e.Auth.Id)
		if err != nil {
			switch err {
			case store.ErrNotFound:
				return writeV1Error(e, http.StatusNotFound, "not_found", "unknown provider")
			case store.ErrInvalidInput:
				return writeV1Error(e, http.StatusBadRequest, "validation_failed", "apiKey is required when configuring a provider key for the first time")
			default:
				return v1ServiceError(e, err)
			}
		}
		slog.Info("shared provider key updated", "actorId", e.Auth.Id, "provider", providerKey, "shared", body.Shared, "configured", entry.Configured)
		return v1Respond(e, http.StatusOK, entry, nil, nil)
	}
}

func adminDeleteProviderKey(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		providerKey := e.Request.PathValue("providerKey")
		if err := s.Store.DeleteSharedProviderKey(providerKey); err != nil {
			return v1ServiceError(e, err)
		}
		slog.Info("shared provider key deleted", "actorId", e.Auth.Id, "provider", providerKey)
		return e.NoContent(http.StatusNoContent)
	}
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

		invitationURL := invitationURLFor(s, e, rawToken)
		return v1Respond(e, http.StatusCreated, map[string]any{
			"invitation":    invitation,
			"invitationUrl": invitationURL,
		}, nil, nil)
	}
}

// invitationURLFor builds the absolute invite link shown to the admin. The
// configured public origin wins; the request Host is a dev-only fallback
// because Host / X-Forwarded-Proto are client-controlled on direct
// connections and could otherwise produce a poisoned link.
func invitationURLFor(s *Server, e *core.RequestEvent, rawToken string) string {
	if base := strings.TrimSuffix(strings.TrimSpace(s.Cfg.PublicBaseURL), "/"); base != "" {
		return base + "/invite/" + rawToken
	}
	scheme := "http"
	if e.Request.TLS != nil || strings.HasPrefix(e.Request.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/invite/%s", scheme, e.Request.Host, rawToken)
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

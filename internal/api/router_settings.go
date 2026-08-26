package api

import (
	"net/http"

	"github.com/pocketbase/pocketbase/core"
	pbrouter "github.com/pocketbase/pocketbase/tools/router"
	"translator-server/internal/store"
)

func registerV1SettingsRoutes(api *pbrouter.RouterGroup[*core.RequestEvent], s *Server) {
	api.GET("/defaults", getDefaults(s))
	api.GET("/settings", getSettings(s))
	api.PUT("/settings", putSettings(s))
}

func getDefaults(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		defaults := store.DefaultTranslationDefaults
		body := map[string]any{"translation": defaults}
		return v1Respond(e, http.StatusOK, body, nil, nil)
	}
}

func getSettings(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		settings, err := s.Store.GetAppSettings(e.Auth.Id)
		if err != nil {
			return e.InternalServerError("failed to load settings", err)
		}
		theme, _ := s.Store.GetTheme(e.Auth.Id)
		resp := map[string]any{
			"theme":       theme,
			"ai":          settings.AI,
			"translation": settings.Translation,
		}
		if settings.TitleProvider != "" {
			resp["titleProvider"] = settings.TitleProvider
		}
		if settings.TitleModel != "" {
			resp["titleModel"] = settings.TitleModel
		}
		return v1Respond(e, http.StatusOK, resp, nil, nil)
	}
}

func putSettings(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		body := struct {
			Theme         string                    `json:"theme"`
			AI            store.AISettings          `json:"ai"`
			TitleProvider string                    `json:"titleProvider"`
			TitleModel    string                    `json:"titleModel"`
			Translation   store.TranslationDefaults `json:"translation"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		if body.Theme != "" {
			if err := s.Store.SaveTheme(e.Auth.Id, body.Theme); err != nil {
				return e.InternalServerError("failed to save theme", err)
			}
		}
		settings, err := s.Store.SaveAppSettings(e.Auth.Id, store.AppSettings{
			AI:            body.AI,
			TitleProvider: body.TitleProvider,
			TitleModel:    body.TitleModel,
			Translation:   body.Translation,
		})
		if err != nil {
			return e.InternalServerError("failed to save settings", err)
		}
		theme, _ := s.Store.GetTheme(e.Auth.Id)
		resp := map[string]any{
			"theme":       theme,
			"ai":          settings.AI,
			"translation": settings.Translation,
		}
		if settings.TitleProvider != "" {
			resp["titleProvider"] = settings.TitleProvider
		}
		if settings.TitleModel != "" {
			resp["titleModel"] = settings.TitleModel
		}
		return v1Respond(e, http.StatusOK, resp, nil, nil)
	}
}

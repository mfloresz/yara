package api

import (
	"net/http"

	"github.com/pocketbase/pocketbase/core"
	pbrouter "github.com/pocketbase/pocketbase/tools/router"
)

func registerProviderRoutes(api *pbrouter.RouterGroup[*core.RequestEvent], s *Server) {
	api.GET("/user/providers", func(e *core.RequestEvent) error {
		providers, err := s.Store.ListProviderSettings(e.Auth.Id)
		if err != nil {
			return e.InternalServerError("failed to load providers", err)
		}
		return e.JSON(http.StatusOK, map[string]any{"providers": providers})
	})
	api.PUT("/user/providers/{providerKey}", func(e *core.RequestEvent) error {
		providerKey := e.Request.PathValue("providerKey")
		body := struct {
			Model       string `json:"model"`
			BaseURL     string `json:"baseUrl"`
			TimeoutMs   int    `json:"timeoutMs"`
			Concurrency int    `json:"concurrency"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		timeout := 0
		if body.TimeoutMs > 0 {
			timeout = body.TimeoutMs
		}
		concurrency := 0
		if body.Concurrency > 0 {
			concurrency = body.Concurrency
		}
		provider, err := s.Store.UpsertProviderSettingsWithConcurrency(e.Auth.Id, providerKey, body.Model, body.BaseURL, timeout, concurrency)
		if err != nil {
			return e.InternalServerError("failed to update provider settings", err)
		}
		return e.JSON(http.StatusOK, provider)
	})
	api.PUT("/user/providers/{providerKey}/key", func(e *core.RequestEvent) error {
		providerKey := e.Request.PathValue("providerKey")
		body := struct {
			APIKey string `json:"apiKey"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		provider, err := s.Store.ReplaceProviderAPIKey(e.Auth.Id, providerKey, body.APIKey)
		if err != nil {
			return e.InternalServerError("failed to replace api key", err)
		}
		return e.JSON(http.StatusOK, provider)
	})
	api.DELETE("/user/providers/{providerKey}/key", func(e *core.RequestEvent) error {
		providerKey := e.Request.PathValue("providerKey")
		if err := s.Store.DeleteProviderAPIKey(e.Auth.Id, providerKey); err != nil {
			return e.InternalServerError("failed to delete api key", err)
		}
		return e.NoContent(http.StatusNoContent)
	})
}

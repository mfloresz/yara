package api

import (
	"net/http"

	"github.com/pocketbase/pocketbase/core"
	pbrouter "github.com/pocketbase/pocketbase/tools/router"
	"translator-server/internal/store"
)

func registerV1PromptRoutes(api *pbrouter.RouterGroup[*core.RequestEvent], s *Server) {
	api.GET("/prompts", listPrompts(s))
	api.PUT("/prompts/{key}", upsertPrompt(s))
}

func listPrompts(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		prompts, err := s.Store.ListPrompts(e.Auth.Id)
		if err != nil {
			return e.InternalServerError("failed to load prompts", err)
		}
		out := promptsToResponse(prompts)
		return v1RespondList(e, http.StatusOK, out, 1, len(out), len(out), false, e.Request.URL.Path)
	}
}

func upsertPrompt(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		key := e.Request.PathValue("key")
		body := struct {
			Label       string `json:"label"`
			Description string `json:"description"`
			Prompt      struct {
				SystemPrompt string `json:"systemPrompt"`
				UserPrompt   string `json:"userPrompt"`
			} `json:"prompt"`
			Active *bool `json:"active"`
		}{}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid body", err)
		}
		active := 1
		if body.Active != nil && !*body.Active {
			active = 0
		}
		prompt, err := s.Store.UpsertPrompt(e.Auth.Id, store.Prompt{Key: key, Label: body.Label, Description: body.Description, SystemPrompt: body.Prompt.SystemPrompt, UserPrompt: body.Prompt.UserPrompt, Active: active})
		if err != nil {
			return e.InternalServerError("failed to update prompt", err)
		}
		out := promptToResponse(prompt)
		return v1Respond(e, http.StatusOK, out, nil, nil)
	}
}

package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zendev-sh/goai"
	"github.com/zendev-sh/goai/provider/openai"
)

func TestProvidersContainKnownEntries(t *testing.T) {
	ids := map[string]bool{}
	for _, p := range Providers() {
		if p.ID == "" {
			t.Fatalf("provider with empty id: %+v", p)
		}
		if p.BaseURL == "" {
			t.Fatalf("provider %q has empty base url", p.ID)
		}
		if len(p.Models) == 0 {
			t.Fatalf("provider %q has no models", p.ID)
		}
		if p.DefaultModel == "" {
			t.Fatalf("provider %q has empty default model", p.ID)
		}
		found := false
		for _, m := range p.Models {
			if m == p.DefaultModel {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("provider %q default model %q not in models list %v", p.ID, p.DefaultModel, p.Models)
		}
		if ids[p.ID] {
			t.Fatalf("duplicate provider id %q", p.ID)
		}
		ids[p.ID] = true
	}
	for _, want := range []string{"venice", "opencode-go", "groq"} {
		if !ids[want] {
			t.Fatalf("missing known provider %q", want)
		}
	}
}

func TestProviderByIDOpenCodeGo(t *testing.T) {
	info, ok := ProviderByID("opencode-go")
	if !ok {
		t.Fatal("opencode-go provider not registered")
	}
	if info.BaseURL != "https://opencode.ai/zen/go/v1" {
		t.Fatalf("unexpected base url: %q", info.BaseURL)
	}
	if !info.OpenAICompat {
		t.Fatal("opencode-go should be OpenAI compatible")
	}
	if got, _ := info.GoAIOptions["useResponsesAPI"].(bool); got {
		t.Fatal("opencode-go should force chat/completions instead of responses API")
	}
	if got, _ := info.GoAIOptions["strictJsonSchema"].(bool); !got {
		t.Fatal("opencode-go should enable strict JSON schema")
	}
	wantModels := map[string]bool{"mimo-v2.5": true, "deepseek-v4-flash": true}
	if len(info.Models) != len(wantModels) {
		t.Fatalf("unexpected model list: %v", info.Models)
	}
	for _, m := range info.Models {
		if !wantModels[m] {
			t.Fatalf("unexpected model %q in opencode-go", m)
		}
	}
}

func TestModelNameSuffixPassthrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		model, _ := body["model"].(string)
		if model != "e2ee-gemma-4-26b-a4b-uncensored-p:disable_thinking=true" {
			t.Fatalf("model name suffix was stripped or modified:\n  want: e2ee-gemma-4-26b-a4b-uncensored-p:disable_thinking=true\n  got:  %q", model)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"test","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer srv.Close()

	model := openai.Chat("e2ee-gemma-4-26b-a4b-uncensored-p:disable_thinking=true",
		openai.WithBaseURL(srv.URL),
		openai.WithAPIKey("test-key"),
	)
	_, err := goai.GenerateText(context.Background(), model,
		goai.WithPrompt("hi"),
	)
	if err != nil {
		t.Fatalf("GenerateText failed: %v", err)
	}
}

func TestProviderByIDUnknown(t *testing.T) {
	if _, ok := ProviderByID("does-not-exist"); ok {
		t.Fatal("unknown provider should not be found")
	}
}

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
	for _, want := range []string{"venice", "meta", "opencode-go", "openrouter"} {
		if !ids[want] {
			t.Fatalf("missing known provider %q", want)
		}
	}
}

func TestProviderByIDMeta(t *testing.T) {
	info, ok := ProviderByID("meta")
	if !ok {
		t.Fatal("meta provider not registered")
	}
	if info.Name != "Meta" {
		t.Fatalf("unexpected provider name: %q", info.Name)
	}
	if info.BaseURL != "https://api.meta.ai/v1" {
		t.Fatalf("unexpected base url: %q", info.BaseURL)
	}
	if !info.OpenAICompat {
		t.Fatal("meta should be OpenAI compatible")
	}
	if got, _ := info.GoAIOptions["useResponsesAPI"].(bool); got {
		t.Fatal("meta should force chat/completions instead of responses API")
	}
	if info.DefaultModel != "muse-spark-1.2-contributor" {
		t.Fatalf("unexpected default model: %q", info.DefaultModel)
	}
	if len(info.Models) != 1 || info.Models[0] != "muse-spark-1.2-contributor" {
		t.Fatalf("unexpected model list: %v", info.Models)
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
	wantModels := map[string]bool{
		"openai/gpt-5.6-luna (reasoning: none)":   true,
		"openai/gpt-5.6-luna (reasoning: low)":    true,
		"openai/gpt-5.6-luna (reasoning: medium)": true,
		"mimo-v2.5":                  true,
		"deepseek-v4-flash":          true,
		"muse-spark-1.2-contributor": true,
	}
	if len(info.Models) != len(wantModels) {
		t.Fatalf("unexpected model list: %v", info.Models)
	}
	for _, m := range info.Models {
		if !wantModels[m] {
			t.Fatalf("unexpected model %q in opencode-go", m)
		}
	}
	if got, _ := info.ModelOptions["muse-spark-1.2-contributor"]["useResponsesAPI"].(bool); !got {
		t.Fatal("muse-spark-1.2-contributor on opencode-go should use the responses API")
	}
}

func TestOpenCodeGoLunaVariantWireFormat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		if body["model"] != "gpt-5.6-luna" {
			t.Fatalf("unexpected model: %v", body["model"])
		}
		reasoning, ok := body["reasoning"].(map[string]any)
		if !ok || reasoning["effort"] != "medium" {
			t.Fatalf("unexpected reasoning options: %v", body["reasoning"])
		}
		if _, ok := body["service_tier"]; ok {
			t.Fatal("service_tier must not be set for OpenCode Go")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"test","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	provider := &OpenAIProvider{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Model:   "openai/gpt-5.6-luna (reasoning: medium)",
		ProviderOptions: map[string]any{
			"useResponsesAPI": false,
		},
	}
	if _, err := provider.TranslateText(context.Background(), TranslateTextInput{TextToTranslate: "hello"}); err != nil {
		t.Fatalf("TranslateText failed: %v", err)
	}
}

func TestOpenCodeGoMuseSparkUsesResponsesAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("expected requests against the responses endpoint, got %q", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		if body["model"] != "muse-spark-1.2-contributor" {
			t.Fatalf("unexpected model: %v", body["model"])
		}
		if _, ok := body["input"]; !ok {
			t.Fatalf("responses request should carry an input array, got: %v", body)
		}
		if _, ok := body["messages"]; ok {
			t.Fatalf("responses request must not carry a chat-completions messages array: %v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"test","model":"muse-spark-1.2-contributor","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`))
	}))
	defer srv.Close()

	provider := &OpenAIProvider{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Model:   "muse-spark-1.2-contributor",
		ProviderOptions: map[string]any{
			"useResponsesAPI": true,
		},
	}
	if _, err := provider.TranslateText(context.Background(), TranslateTextInput{TextToTranslate: "hello"}); err != nil {
		t.Fatalf("TranslateText failed: %v", err)
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

func TestProviderByIDOpenRouter(t *testing.T) {
	info, ok := ProviderByID("openrouter")
	if !ok {
		t.Fatal("openrouter provider not registered")
	}
	if info.BaseURL != "https://openrouter.ai/api/v1" {
		t.Fatalf("unexpected base url: %q", info.BaseURL)
	}
	wantModels := []string{
		"openai/gpt-5.6-luna (reasoning: none)",
		"openai/gpt-5.6-luna (reasoning: low)",
		"openai/gpt-5.6-luna (reasoning: medium)",
		"deepseek/deepseek-v4-flash-0731",
		"google/gemini-3.5-flash-lite",
	}
	if len(info.Models) != len(wantModels) {
		t.Fatalf("unexpected model list: %v", info.Models)
	}
	for i, want := range wantModels {
		if info.Models[i] != want {
			t.Fatalf("model %d = %q, want %q", i, info.Models[i], want)
		}
	}
	if got, _ := info.GoAIOptions["useResponsesAPI"].(bool); got {
		t.Fatal("openrouter should use chat/completions")
	}
}

func TestOpenRouterReasoningVariantWireFormat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		if body["model"] != "openai/gpt-5.6-luna" {
			t.Fatalf("unexpected model: %v", body["model"])
		}
		reasoning, ok := body["reasoning"].(map[string]any)
		if !ok || reasoning["effort"] != "medium" {
			t.Fatalf("unexpected reasoning options: %v", body["reasoning"])
		}
		if tier, ok := body["service_tier"].(string); !ok || tier != "flex" {
			t.Fatalf("luna models should ride the flex tier, got: %v", body["service_tier"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"test","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	provider := &OpenAIProvider{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Model:   "openai/gpt-5.6-luna (reasoning: medium)",
		ProviderOptions: map[string]any{
			"useResponsesAPI": false,
		},
		OpenRouter: true,
	}
	if _, err := provider.TranslateText(context.Background(), TranslateTextInput{TextToTranslate: "hello"}); err != nil {
		t.Fatalf("TranslateText failed: %v", err)
	}
}

func TestOpenRouterNonLunaModelOmitsServiceTier(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		if _, ok := body["service_tier"]; ok {
			t.Fatalf("service_tier must not be set for non-luna models: %v", body["service_tier"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"test","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	provider := &OpenAIProvider{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Model:   "deepseek/deepseek-v4-flash-0731",
		ProviderOptions: map[string]any{
			"useResponsesAPI": false,
		},
		OpenRouter: true,
	}
	if _, err := provider.TranslateText(context.Background(), TranslateTextInput{TextToTranslate: "hello"}); err != nil {
		t.Fatalf("TranslateText failed: %v", err)
	}
}

func TestProviderByIDUnknown(t *testing.T) {
	if _, ok := ProviderByID("does-not-exist"); ok {
		t.Fatal("unknown provider should not be found")
	}
}

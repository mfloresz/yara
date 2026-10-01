package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
			t.Fatalf("provider %q default model %q not in models list %v", p.DefaultModel, p.ID, p.Models)
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
	if got, _ := info.GoAIOptions["strictJsonSchema"].(bool); !got {
		t.Fatal("meta should enable strict JSON schema")
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
	if got, _ := info.GoAIOptions["strictJsonSchema"].(bool); !got {
		t.Fatal("opencode-go should enable strict JSON schema")
	}
	wantModels := map[string]bool{
		"openai/gpt-5.6-luna (reasoning: none)":   true,
		"openai/gpt-5.6-luna (reasoning: low)":    true,
		"openai/gpt-5.6-luna (reasoning: medium)": true,
		"mimo-v2.5":           true,
		"deepseek-v4.1-flash": true,
	}
	if len(info.Models) != len(wantModels) {
		t.Fatalf("unexpected model list: %v", info.Models)
	}
	for _, m := range info.Models {
		if !wantModels[m] {
			t.Fatalf("unexpected model %q in opencode-go", m)
		}
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
		respondChatCompletion(w, "ok")
	}))
	defer srv.Close()

	provider := &OpenAIProvider{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Model:   "openai/gpt-5.6-luna (reasoning: medium)",
	}
	if _, err := provider.TranslateText(context.Background(), TranslateTextInput{TextToTranslate: "hello"}); err != nil {
		t.Fatalf("TranslateText failed: %v", err)
	}
}

// TestOpenAIProviderAlwaysUsesChatCompletions pins the eino migration
// behavior: every OpenAI-compatible request goes to /chat/completions with a
// messages array, regardless of legacy provider options. The useResponsesAPI
// switch is gone, so a catalog endpoint that only speaks the OpenAI Responses
// API (e.g. muse-spark) will not work until that provider exposes
// /chat/completions; the catalog entry is kept so the provider stays
// selectable, and the limitation is called out in the API docs.
func TestOpenAIProviderAlwaysUsesChatCompletions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("expected chat completions endpoint, got %q", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		if body["model"] != "muse-spark-1.2-contributor" {
			t.Fatalf("unexpected model: %v", body["model"])
		}
		if _, ok := body["messages"]; !ok {
			t.Fatalf("chat completions request must carry a messages array, got: %v", body)
		}
		if _, ok := body["input"]; ok {
			t.Fatalf("chat completions request must not carry a responses input array: %v", body)
		}
		respondChatCompletion(w, "ok")
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
		respondChatCompletion(w, "ok")
	}))
	defer srv.Close()

	provider := &OpenAIProvider{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Model:   "e2ee-gemma-4-26b-a4b-uncensored-p:disable_thinking=true",
	}
	if _, err := provider.TranslateText(context.Background(), TranslateTextInput{TextToTranslate: "hi"}); err != nil {
		t.Fatalf("TranslateText failed: %v", err)
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
		"tencent/hy-mt2-30b-a3b",
		"tencent/hy-mt2-1.8b",
		"mistralai/ministral-8b-2512",
		"mistralai/mistral-small-2603",
		"mistralai/ministral-14b-2512",
		"inception/mercury-2.5",
	}
	if len(info.Models) != len(wantModels) {
		t.Fatalf("unexpected model list: %v", info.Models)
	}
	for i, want := range wantModels {
		if info.Models[i] != want {
			t.Fatalf("model %d = %q, want %q", i, info.Models[i], want)
		}
	}
	if got, _ := info.GoAIOptions["strictJsonSchema"].(bool); !got {
		t.Fatal("openrouter should enable strict JSON schema")
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
		respondChatCompletion(w, "ok")
	}))
	defer srv.Close()

	provider := &OpenAIProvider{
		APIKey:     "test-key",
		BaseURL:    srv.URL,
		Model:      "openai/gpt-5.6-luna (reasoning: medium)",
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
		respondChatCompletion(w, "ok")
	}))
	defer srv.Close()

	provider := &OpenAIProvider{
		APIKey:     "test-key",
		BaseURL:    srv.URL,
		Model:      "deepseek/deepseek-v4-flash-0731",
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

// TestOptionsForModelMerge pins the per-model override mechanism. It is the
// extension point for provider quirks that only apply to one model of a
// provider, and it is exercised by runtime_config.go when building a provider
// for the selected model. No catalog entry populates it today, so without this
// test the merge branch could rot silently.
func TestOptionsForModelMerge(t *testing.T) {
	info := ProviderInfo{
		GoAIOptions: map[string]any{
			"strictJsonSchema": true,
			"shared":           "base",
		},
		ModelOptions: map[string]map[string]any{
			"model-a": {"strictJsonSchema": false, "extra": 1},
		},
	}

	// A model with overrides: keys merge, and the override wins.
	got := info.OptionsForModel("model-a")
	if v, _ := got["strictJsonSchema"].(bool); v {
		t.Error("per-model override should disable strictJsonSchema")
	}
	if got["shared"] != "base" {
		t.Errorf("base option lost during merge: %v", got)
	}
	if got["extra"] != 1 {
		t.Errorf("per-model extra option missing: %v", got)
	}

	// A model without overrides: the base map is returned as-is.
	if got := info.OptionsForModel("model-b"); got["strictJsonSchema"] != true {
		t.Errorf("model without overrides should keep base options: %v", got)
	}
}

// TestOpenRouterAttributionHeaders pins the HTTP-Referer/X-Title pair that
// OpenRouter uses for app attribution. goai's OpenRouter provider injected
// them unconditionally and the eino migration dropped them, so they are now
// set explicitly for OpenRouter requests.
func TestOpenRouterAttributionHeaders(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		respondChatCompletion(w, "ok")
	}))
	defer srv.Close()

	provider := &OpenAIProvider{
		APIKey:     "test-key",
		BaseURL:    srv.URL,
		Model:      "deepseek/deepseek-v4-flash-0731",
		OpenRouter: true,
	}
	if _, err := provider.TranslateText(context.Background(), TranslateTextInput{TextToTranslate: "hello"}); err != nil {
		t.Fatalf("TranslateText failed: %v", err)
	}
	if got.Get("HTTP-Referer") != openRouterReferer {
		t.Errorf("HTTP-Referer = %q, want %q", got.Get("HTTP-Referer"), openRouterReferer)
	}
	if got.Get("X-Title") == "" {
		t.Error("X-Title attribution header is missing")
	}

	// Non-OpenRouter providers must not claim OpenRouter attribution.
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		respondChatCompletion(w, "ok")
	}))
	defer other.Close()
	provider.BaseURL = other.URL
	provider.OpenRouter = false
	if _, err := provider.TranslateText(context.Background(), TranslateTextInput{TextToTranslate: "hello"}); err != nil {
		t.Fatalf("TranslateText failed: %v", err)
	}
	if got.Get("HTTP-Referer") != "" {
		t.Errorf("HTTP-Referer should only be set for OpenRouter, got %q", got.Get("HTTP-Referer"))
	}
}

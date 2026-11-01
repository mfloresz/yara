package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecisionClientDecideSendsContractAndParsesAnswers(t *testing.T) {
	var gotPath, gotMethod, gotAuth, gotTitle string
	var gotBody decisionRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		gotTitle = r.Header.Get("X-Title")
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &gotBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"answers": {
				"recommended_action": {"type": "choice", "choice": "refine", "confidence": 0.93, "probabilities": {"accept": 0.05, "refine": 0.93, "retranslate": 0.02}},
				"structurally_intact": {"type": "noul", "noul": 0.98},
				"untranslated_content_present": {"type": "noul", "noul": 0.91},
				"has_meaning_error": {"type": "noul", "noul": 0.04}
			},
			"usage": {"input_tokens": 3200, "output_tokens": 12, "cost": 0.00007}
		}`))
	}))
	defer srv.Close()

	client := &DecisionClient{APIKey: "sk-test", BaseURL: srv.URL}
	resp, err := client.Decide(context.Background(), DecisionRequest{
		Model: "perplexity/pplx-decider-v1.2",
		State: map[string]any{"original": "Hello", "translation": "Hola"},
		Questions: map[string]DecisionQuestion{
			"recommended_action": {Type: "choice", Instructions: "pick one"},
		},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Fatalf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/alpha/decisions" {
		t.Fatalf("path = %q, want /alpha/decisions", gotPath)
	}
	if gotAuth != "Bearer sk-test" {
		t.Fatalf("Authorization = %q, want bearer token", gotAuth)
	}
	if gotTitle != yaraUserAgent {
		t.Fatalf("X-Title = %q, want %q", gotTitle, yaraUserAgent)
	}
	if gotBody.Model != "perplexity/pplx-decider-v1.2" {
		t.Fatalf("model = %q", gotBody.Model)
	}
	if gotBody.State["original"] != "Hello" {
		t.Fatalf("state.original = %v", gotBody.State["original"])
	}
	if gotBody.Questions["recommended_action"].Type != "choice" {
		t.Fatalf("question type = %q", gotBody.Questions["recommended_action"].Type)
	}

	action := resp.Answers["recommended_action"]
	if action.Choice != "refine" || action.Confidence != 0.93 {
		t.Fatalf("unexpected action answer: %+v", action)
	}
	if got := resp.Answers["structurally_intact"].Noul; got != 0.98 {
		t.Fatalf("structurally_intact = %v, want 0.98", got)
	}
	if resp.Usage.InputTokens != 3200 {
		t.Fatalf("usage.input_tokens = %d", resp.Usage.InputTokens)
	}
}

func TestDecisionClientEndpoint(t *testing.T) {
	cases := []struct {
		base string
		want string
	}{
		{"", defaultDecisionEndpoint},
		{"https://openrouter.ai/api", "https://openrouter.ai/api/alpha/decisions"},
		{"https://openrouter.ai/api/", "https://openrouter.ai/api/alpha/decisions"},
		{"https://openrouter.ai/api/v1", "https://openrouter.ai/api/alpha/decisions"},
		{"https://openrouter.ai/api/alpha/decisions", "https://openrouter.ai/api/alpha/decisions"},
	}
	for _, tc := range cases {
		client := &DecisionClient{BaseURL: tc.base}
		if got := client.endpoint(); got != tc.want {
			t.Fatalf("endpoint(%q) = %q, want %q", tc.base, got, tc.want)
		}
	}
}

func TestDecisionClientValidation(t *testing.T) {
	ctx := context.Background()

	if _, err := (&DecisionClient{APIKey: "k"}).Decide(ctx, DecisionRequest{}); err == nil {
		t.Fatal("expected missing model to fail")
	}
	if _, err := (&DecisionClient{}).Decide(ctx, DecisionRequest{Model: "m"}); err == nil {
		t.Fatal("expected missing API key to fail")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"no key"}}`))
	}))
	defer srv.Close()
	if _, err := (&DecisionClient{APIKey: "k", BaseURL: srv.URL}).Decide(ctx, DecisionRequest{Model: "m"}); err == nil ||
		!strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("expected HTTP 401 error, got %v", err)
	}

	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"answers":{}}`))
	}))
	defer empty.Close()
	if _, err := (&DecisionClient{APIKey: "k", BaseURL: empty.URL}).Decide(ctx, DecisionRequest{Model: "m"}); err == nil ||
		!strings.Contains(err.Error(), "no answers") {
		t.Fatalf("expected empty answers error, got %v", err)
	}
}

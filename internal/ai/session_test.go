package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSessionForJobIsStableOpaqueAndShort(t *testing.T) {
	first := SessionForJob("job-123")
	second := SessionForJob("job-123")
	if first != second {
		t.Fatalf("expected stable session for same job, got %q vs %q", first, second)
	}
	if len(first) != opencodeSessionLength {
		t.Fatalf("expected session length %d, got %q", opencodeSessionLength, first)
	}
	for _, r := range first {
		if !strings.ContainsRune("0123456789abcdef", r) {
			t.Fatalf("expected lowercase hex session, got %q", first)
		}
	}
	if other := SessionForJob("job-456"); other == first {
		t.Fatalf("expected different jobs to map to different sessions, both %q", first)
	}
	if strings.Contains(first, "job-123") {
		t.Fatalf("session must not expose the job id, got %q", first)
	}
}

func TestOpenAIProviderHeadersAlwaysIdentifyApp(t *testing.T) {
	h := (&OpenAIProvider{}).headers()
	if h["User-Agent"] != yaraUserAgent {
		t.Fatalf("expected User-Agent %q, got %q", yaraUserAgent, h["User-Agent"])
	}
	if _, ok := h[opencodeSessionHeader]; ok {
		t.Fatalf("expected no session header when SessionID is empty, got %v", h)
	}

	h = (&OpenAIProvider{SessionID: "abc12345"}).headers()
	if h[opencodeSessionHeader] != "abc12345" {
		t.Fatalf("expected session header to carry the session id, got %v", h)
	}
}

func TestOpenAIProviderSendsSessionAndUserAgent(t *testing.T) {
	var gotUA, gotSession string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		gotSession = r.Header.Get(opencodeSessionHeader)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"test","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	provider := &OpenAIProvider{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Model:   "test-model",
		ProviderOptions: map[string]any{
			"useResponsesAPI": false,
		},
		SessionID: "abc12345",
	}
	if _, err := provider.TranslateText(context.Background(), TranslateTextInput{TextToTranslate: "hello"}); err != nil {
		t.Fatalf("TranslateText failed: %v", err)
	}
	if gotUA != yaraUserAgent {
		t.Fatalf("expected User-Agent %q, got %q", yaraUserAgent, gotUA)
	}
	if gotSession != "abc12345" {
		t.Fatalf("expected %s %q, got %q", opencodeSessionHeader, "abc12345", gotSession)
	}
}

func TestOpenAIProviderOmitsSessionWhenEmpty(t *testing.T) {
	var gotSession string
	var sessionPresent bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, sessionPresent = r.Header[http.CanonicalHeaderKey(opencodeSessionHeader)]
		gotSession = r.Header.Get(opencodeSessionHeader)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"test","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	provider := &OpenAIProvider{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Model:   "test-model",
		ProviderOptions: map[string]any{
			"useResponsesAPI": false,
		},
	}
	if _, err := provider.TranslateText(context.Background(), TranslateTextInput{TextToTranslate: "hello"}); err != nil {
		t.Fatalf("TranslateText failed: %v", err)
	}
	if sessionPresent || gotSession != "" {
		t.Fatalf("expected no session header for non-opencode providers, got %q", gotSession)
	}
}

func TestSessionForJobDecodesAsJSONRoundTrip(t *testing.T) {
	// Guards the 8-hex-char contract against accidental format changes.
	raw, _ := json.Marshal(map[string]string{"session": SessionForJob("job-123")})
	var decoded map[string]string
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("session id must stay JSON-safe: %v", err)
	}
	if len(decoded["session"]) != opencodeSessionLength {
		t.Fatalf("expected session length %d, got %q", opencodeSessionLength, decoded["session"])
	}
}

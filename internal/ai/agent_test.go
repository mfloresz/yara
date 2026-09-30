package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cloudwego/eino-ext/components/model/openai"
)

// sseWrite emits one OpenAI-style SSE data line and flushes it.
func sseWrite(t *testing.T, w http.ResponseWriter, payload map[string]any) {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal chunk: %v", err)
	}
	if _, err := w.Write(append(append([]byte("data: "), b...), '\n', '\n')); err != nil {
		t.Fatalf("write chunk: %v", err)
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func sseChunk(delta map[string]any, finish any) map[string]any {
	return map[string]any{
		"id":      "chatcmpl-test",
		"object":  "chat.completion.chunk",
		"created": 1,
		"model":   "test-model",
		"choices": []map[string]any{{"index": 0, "delta": delta, "finish_reason": finish}},
	}
}

func toolCallDelta(index int, id, name, args string) map[string]any {
	return map[string]any{
		"index": index,
		"id":    id,
		"type":  "function",
		"function": map[string]any{
			"name":      name,
			"arguments": args,
		},
	}
}

const askUserArgs = `{"question":"¿De cuál novela?","options":[{"label":"The Guardian (de Evil_Warlord)","value":"gp7kkn"},{"label":"Cultivating Clan","value":"cxg6ar"}]}`

// TestAgentChatTerminalToolEndsTurn drives the real streaming loop against a
// scripted OpenAI-compatible endpoint: a terminal tool call (ask_user) must
// end the turn after one model call, persisting a replayable trail of
// assistant tool call + tool result, with no second model invocation.
func TestAgentChatTerminalToolEndsTurn(t *testing.T) {
	requests := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if !strings.Contains(string(body), `"stream":true`) {
			t.Fatalf("AgentChat must stream, request body: %s", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		sseWrite(t, w, sseChunk(map[string]any{"role": "assistant", "content": ""}, nil))
		sseWrite(t, w, sseChunk(map[string]any{"tool_calls": []map[string]any{toolCallDelta(0, "call-1", "ask_user", askUserArgs)}}, nil))
		sseWrite(t, w, sseChunk(map[string]any{}, "tool_calls"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer ts.Close()

	provider := &OpenAIProvider{APIKey: "test-key", BaseURL: ts.URL, Model: "test-model"}
	askUser := AgentTool{
		Name:        "ask_user",
		Description: "ask",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"question":{"type":"string"},"options":{"type":"array","items":{"type":"object","properties":{"label":{"type":"string"},"value":{"type":"string"}},"required":["label","value"]}}},"required":["question","options"]}`),
		Terminal:    true,
		Execute: func(_ context.Context, _ json.RawMessage) (string, error) {
			return `{"awaitingUser":true}`, nil
		},
	}

	var events []AgentEvent
	out, err := provider.AgentChat(context.Background(), AgentChatInput{
		System:   "sys",
		Messages: []AgentMessage{{Role: "user", Content: "dame las estadísticas"}},
		Tools:    []AgentTool{askUser},
		OnEvent:  func(ev AgentEvent) { events = append(events, ev) },
	})
	if err != nil {
		t.Fatalf("AgentChat returned error: %v", err)
	}

	if requests != 1 {
		t.Fatalf("terminal tool must end the turn after 1 model call, got %d requests", requests)
	}
	if out.Text != "" || out.Steps != 1 {
		t.Fatalf("unexpected output text/steps: %q/%d", out.Text, out.Steps)
	}
	if len(out.Messages) != 3 {
		t.Fatalf("trail must be user + assistant tool call + tool result, got %d messages: %#v", len(out.Messages), out.Messages)
	}
	if out.Messages[1].Role != "assistant" || len(out.Messages[1].ToolCalls) != 1 || out.Messages[1].ToolCalls[0].Name != "ask_user" {
		t.Fatalf("assistant message must carry the ask_user call, got %#v", out.Messages[1])
	}
	if out.Messages[2].Role != "tool" || out.Messages[2].ToolName != "ask_user" || !strings.Contains(out.Messages[2].Content, "awaitingUser") {
		t.Fatalf("tool result must be the awaitingUser marker, got %#v", out.Messages[2])
	}
	if out.Messages[2].ToolCallID != "call-1" {
		t.Fatalf("tool result must reference the tool call id, got %q", out.Messages[2].ToolCallID)
	}

	var kinds []string
	for _, ev := range events {
		kinds = append(kinds, ev.Type)
	}
	if strings.Join(kinds, ",") != "tool_call,tool_result" {
		t.Fatalf("unexpected events: %#v", kinds)
	}
}

// TestAgentChatNonTerminalToolContinuesLoop pins the other side of the
// Terminal flag: an ordinary tool call must keep the loop running until the
// model answers with plain text.
func TestAgentChatNonTerminalToolContinuesLoop(t *testing.T) {
	requests := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "text/event-stream")
		if requests == 1 {
			sseWrite(t, w, sseChunk(map[string]any{"role": "assistant", "content": ""}, nil))
			sseWrite(t, w, sseChunk(map[string]any{"tool_calls": []map[string]any{toolCallDelta(0, "call-1", "list_novels", `{"query":"Guardian"}`)}}, nil))
			sseWrite(t, w, sseChunk(map[string]any{}, "tool_calls"))
		} else {
			sseWrite(t, w, sseChunk(map[string]any{"role": "assistant", "content": "Dos novelas coinciden."}, "stop"))
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer ts.Close()

	provider := &OpenAIProvider{APIKey: "test-key", BaseURL: ts.URL, Model: "test-model"}
	listNovels := AgentTool{
		Name:        "list_novels",
		Description: "list",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`),
		Execute: func(_ context.Context, _ json.RawMessage) (string, error) {
			return `[{"id":"gp7kkn","sourceTitle":"The Guardian"}]`, nil
		},
	}

	out, err := provider.AgentChat(context.Background(), AgentChatInput{
		System:   "sys",
		Messages: []AgentMessage{{Role: "user", Content: "¿cuál guardian?"}},
		Tools:    []AgentTool{listNovels},
	})
	if err != nil {
		t.Fatalf("AgentChat returned error: %v", err)
	}

	if requests != 2 {
		t.Fatalf("non-terminal tool must continue the loop (2 model calls), got %d", requests)
	}
	if out.Text != "Dos novelas coinciden." || out.Steps != 2 {
		t.Fatalf("unexpected output text/steps: %q/%d", out.Text, out.Steps)
	}
	if len(out.Messages) != 4 {
		t.Fatalf("trail must be user + assistant tool call + tool + assistant, got %d", len(out.Messages))
	}
}

// TestTruncateToolResultRespectsRuneLimit pins the unit the cap is expressed
// in. Gating on bytes while cutting on runes returned a result LARGER than the
// input for multi-byte text (6000 CJK runes: 18000 bytes in, 18015 bytes out
// against an 8000 limit), so the cap did not cap for exactly the content this
// app serves.
func TestTruncateToolResultRespectsRuneLimit(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{"cjk", strings.Repeat("漢", maxToolResultChars*2)},
		{"accented", strings.Repeat("á", maxToolResultChars*2)},
		{"emoji", strings.Repeat("🙂", maxToolResultChars*2)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := truncateToolResult(tc.text)
			if n := utf8.RuneCountInString(out); n > maxToolResultChars+len("\n…[truncated]") {
				t.Errorf("result is %d runes, cap is %d plus the marker", n, maxToolResultChars)
			}
			if !utf8.ValidString(out) {
				t.Error("truncation produced invalid UTF-8")
			}
			if !strings.Contains(out, "[truncated]") {
				t.Error("truncated result is missing its marker")
			}
		})
	}

	// Short results pass through untouched.
	short := "hola"
	if got := truncateToolResult(short); got != short {
		t.Errorf("short result was modified: %q", got)
	}
}

// TestToolResultPreviewRespectsRuneLimit is the same guarantee for the UI
// preview, which had the identical byte-gate/rune-cut mismatch.
func TestToolResultPreviewRespectsRuneLimit(t *testing.T) {
	preview := toolResultPreview(strings.Repeat("漢", maxEventToolResultChars*3))
	if n := utf8.RuneCountInString(preview); n > maxEventToolResultChars+1 {
		t.Errorf("preview is %d runes, cap is %d plus the ellipsis", n, maxEventToolResultChars)
	}
	if !utf8.ValidString(preview) {
		t.Error("preview produced invalid UTF-8")
	}
}

func TestTruncateForFeedbackKeepsValidUTF8(t *testing.T) {
	out := truncateForFeedback(strings.Repeat("漢", 500))
	if !utf8.ValidString(out) {
		t.Error("truncateForFeedback produced invalid UTF-8 (byte-wise cut)")
	}
	if n := utf8.RuneCountInString(out); n > 201 {
		t.Errorf("truncateForFeedback returned %d runes, want at most 201", n)
	}
}

// TestRetryClassifier pins which failures are worth another attempt.
// Deterministic errors (auth, bad request, unsupported model) must fail fast
// so a typo does not burn the user's quota three times over.
func TestRetryClassifier(t *testing.T) {
	retryable := []error{
		&openai.APIError{HTTPStatusCode: 429, Message: "x"},
		&openai.APIError{HTTPStatusCode: 500, Message: "x"},
		&openai.APIError{HTTPStatusCode: 503, Message: "x"},
		&openai.APIError{HTTPStatusCode: 408, Message: "x"},
		errors.New("dial tcp: connection reset by peer"),
		errors.New("unexpected EOF"),
		errors.New("server closed idle connection"),
	}
	for _, err := range retryable {
		if !isRetryableModelError(err) {
			t.Errorf("should retry: %v", err)
		}
	}

	terminal := []error{
		nil,
		context.Canceled,
		context.DeadlineExceeded,
		&openai.APIError{HTTPStatusCode: 401, Message: "x"},
		&openai.APIError{HTTPStatusCode: 400, Message: "x"},
		&openai.APIError{HTTPStatusCode: 404, Message: "x"},
		fmt.Errorf("wrapped: %w", errNoRetry),
	}
	for _, err := range terminal {
		if isRetryableModelError(err) {
			t.Errorf("should not retry: %v", err)
		}
	}
}

// TestWithRetryStopsOnNonRetryable pins that a deterministic failure is
// attempted once, not three times.
func TestWithRetryStopsOnNonRetryable(t *testing.T) {
	attempts := 0
	err := withRetry(context.Background(), defaultAgentRetries+1, func(context.Context) error {
		attempts++
		return errors.New("invalid api key")
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if attempts != 1 {
		t.Errorf("deterministic error attempted %d times, want 1", attempts)
	}
}

// TestWithRetryRetriesTransientThenSucceeds pins the recovery path.
func TestWithRetryRetriesTransientThenSucceeds(t *testing.T) {
	attempts := 0
	err := withRetry(context.Background(), defaultAgentRetries+1, func(context.Context) error {
		attempts++
		if attempts < 3 {
			return &openai.APIError{HTTPStatusCode: 429, Message: "x"}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected recovery, got %v", err)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

// TestWithRetryRespectsCanceledContext pins that an aborted turn stops
// retrying immediately instead of sleeping through the backoff.
func TestWithRetryRespectsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	err := withRetry(ctx, defaultAgentRetries+1, func(context.Context) error {
		attempts++
		cancel()
		return &openai.APIError{HTTPStatusCode: 500, Message: "x"}
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1 (must not sleep after cancel)", attempts)
	}
}

// TestAgentChatAbortsOnStalledStream pins the idle guard. A provider that
// accepts the request and then goes silent produces no error and no output, so
// without a bound on the gap between chunks the chat hangs with nothing on
// screen until the whole-turn timeout (minutes) expires.
func TestAgentChatAbortsOnStalledStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Send one chunk, then hold the connection open forever without
		// another chunk and without closing: the classic stalled upstream.
		sseWrite(t, w, map[string]any{
			"id":      "chatcmpl-stall",
			"object":  "chat.completion.chunk",
			"created": 1,
			"model":   "test",
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": "hola"}, "finish_reason": nil}},
		})
		// Block until the client goes away. A bare select{} would keep the
		// handler alive after the test returns and stall srv.Close().
		<-r.Context().Done()
	}))
	defer srv.Close()

	provider := &OpenAIProvider{APIKey: "test-key", BaseURL: srv.URL, Model: "test-model"}
	// Shorten the guard so the test does not wait the production 60s.
	restore := agentStreamIdleTimeoutForTest(120 * time.Millisecond)
	defer restore()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := time.Now()
	_, err := provider.AgentChat(ctx, AgentChatInput{System: "s", Messages: []AgentMessage{{Role: "user", Content: "hola"}}})
	if err == nil {
		t.Fatal("expected the stalled stream to abort the turn")
	}
	if !IsStreamStalled(err) {
		t.Errorf("err = %v, want a stalled-stream error", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("abort took %s, expected the idle guard to fire quickly", elapsed)
	}
}

// TestAgentChatDoesNotAbortALongButActiveStream pins the other half of the
// contract: a step that keeps delivering is never cut off, no matter how long
// the total. A fixed per-call deadline (what the provider Timeout used to be)
// would fail this.
func TestAgentChatDoesNotAbortALongButActiveStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// Total duration well past the idle guard, but a chunk every 20ms.
		for i := 0; i < 20; i++ {
			sseWrite(t, w, map[string]any{
				"id":      "chatcmpl-slow",
				"object":  "chat.completion.chunk",
				"created": 1,
				"model":   "test",
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": "x"}, "finish_reason": nil}},
			})
			time.Sleep(20 * time.Millisecond)
		}
		sseWrite(t, w, map[string]any{
			"id": "chatcmpl-slow", "object": "chat.completion.chunk", "created": 1, "model": "test",
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
		})
	}))
	defer srv.Close()

	provider := &OpenAIProvider{APIKey: "test-key", BaseURL: srv.URL, Model: "test-model"}
	restore := agentStreamIdleTimeoutForTest(120 * time.Millisecond)
	defer restore()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out, err := provider.AgentChat(ctx, AgentChatInput{System: "s", Messages: []AgentMessage{{Role: "user", Content: "hola"}}})
	if err != nil {
		t.Fatalf("a slow but active stream must not be aborted: %v", err)
	}
	if out.Text == "" {
		t.Error("expected the accumulated text")
	}
}

// agentStreamIdleTimeoutForTest overrides the production idle guard and
// returns a function that restores it.
func agentStreamIdleTimeoutForTest(d time.Duration) func() {
	prev := agentStreamIdleTimeout
	agentStreamIdleTimeout = d
	return func() { agentStreamIdleTimeout = prev }
}

// TestSettingsTimeoutAppliesToTranslation pins that the timeout configured in
// Settings (or in a novel's AI settings) still bounds a translation call.
//
// The job worker builds its context with context.WithCancel, which carries NO
// deadline, so the provider is the only place this timeout can be applied. An
// earlier version of the eino migration dropped it entirely, which left
// translate/refine jobs unbounded: a wedged provider kept a job running until
// the process died. It is applied as a context deadline, not as
// http.Client.Timeout, so a response that is actively streaming is not cut off
// mid-flight the way a client-level deadline would.
func TestSettingsTimeoutAppliesToTranslation(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Accept the request and never answer: only the deadline ends this.
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer func() {
		close(release)
		srv.Close()
	}()

	provider := &OpenAIProvider{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Model:   "test-model",
		Timeout: 300 * time.Millisecond,
	}
	start := time.Now()
	_, err := provider.TranslateText(context.Background(), TranslateTextInput{TextToTranslate: "hola"})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected the configured timeout to abort the call")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want context.DeadlineExceeded", err)
	}
	if elapsed > 3*time.Second {
		t.Errorf("call took %s; the Settings timeout is not being applied", elapsed)
	}
}

// TestTranslationWithoutTimeoutStillRespectsCallerContext pins the other
// direction: a provider with no configured timeout must not invent one, and
// the caller's context remains the authority.
func TestTranslationWithoutTimeoutRespectsCallerContext(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer func() {
		close(release)
		srv.Close()
	}()

	provider := &OpenAIProvider{APIKey: "test-key", BaseURL: srv.URL, Model: "test-model"}
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	if _, err := provider.TranslateText(ctx, TranslateTextInput{TextToTranslate: "hola"}); err == nil {
		t.Fatal("expected the caller's deadline to abort the call")
	}
}

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

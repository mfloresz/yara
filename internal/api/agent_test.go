package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"translator-server/internal/ai"
	"translator-server/internal/store"
)

// fakeAgentProvider implements ai.Provider via the embedded OpenAIProvider
// (whose job methods fail if ever called) plus ai.AgentProvider with a
// scripted turn: one real list_novels tool execution against the store, then
// a final answer quoting the tool result.
type fakeAgentProvider struct {
	*ai.OpenAIProvider
	finalText string
}

func (f *fakeAgentProvider) AgentChat(ctx context.Context, in ai.AgentChatInput) (ai.AgentChatOutput, error) {
	toolResult := ""
	for _, tool := range in.Tools {
		if tool.Name == "list_novels" {
			result, err := tool.Execute(ctx, json.RawMessage(`{"query":"Sin"}`))
			if err != nil {
				toolResult = "error: " + err.Error()
			} else {
				toolResult = result
			}
		}
	}
	// Pad the tool result past PocketBase's 5000-char TextField default: the
	// persisted trail must survive it (regression for the agent session save
	// failure on real libraries).
	toolResult += "\n" + strings.Repeat("x", 6000)
	if in.OnEvent != nil {
		in.OnEvent(ai.AgentEvent{Type: "tool_call", Step: 1, ToolName: "list_novels", ToolArgs: `{"query":"Sin"}`})
		in.OnEvent(ai.AgentEvent{Type: "tool_result", Step: 1, ToolName: "list_novels", ToolResult: toolResult})
		in.OnEvent(ai.AgentEvent{Type: "text_delta", Step: 2, Text: f.finalText})
	}
	trail := append([]ai.AgentMessage{}, in.Messages...)
	trail = append(trail,
		ai.AgentMessage{Role: "assistant", ToolCalls: []ai.AgentToolCall{{ID: "call-1", Name: "list_novels", Args: `{"query":"Sin"}`}}},
		ai.AgentMessage{Role: "tool", Content: toolResult, ToolCallID: "call-1", ToolName: "list_novels"},
		ai.AgentMessage{Role: "assistant", Content: f.finalText},
	)
	return ai.AgentChatOutput{Messages: trail, Text: f.finalText, Steps: 2}, nil
}

func parseNDJSON(t *testing.T, body []byte) []map[string]any {
	t.Helper()
	lines := []map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("invalid NDJSON line %q: %v", line, err)
		}
		lines = append(lines, ev)
	}
	return lines
}

func TestAgentChatRunsToolsAndPersistsSession(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "alice-agent@example.com", "secret123", "Alice")
	novel := createNovel(t, env.handler, alice.Token, "Sin descripción", "es", "en")

	env.server.NewAIProvider = func(store.AISettings, string) (ai.Provider, error) {
		return &fakeAgentProvider{finalText: "Encontré 1 novela."}, nil
	}

	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/agent/chat", alice.Token, map[string]any{
		"message": "¿Qué novelas no tienen descripción?",
		"novelId": novel.ID,
	})
	assertStatus(t, resp, http.StatusOK)
	if ct := resp.Header().Get("Content-Type"); ct != "application/x-ndjson" {
		t.Fatalf("unexpected content type: %q", ct)
	}

	events := parseNDJSON(t, []byte(resp.Body.String()))

	if got := events[0]["type"]; got != "session" {
		t.Fatalf("first event should announce the session, got %v", got)
	}
	sessionID, _ := events[0]["sessionId"].(string)
	if sessionID == "" {
		t.Fatal("session event must carry a sessionId")
	}

	var toolResult string
	var deltas []string
	var done map[string]any
	for _, ev := range events {
		switch ev["type"] {
		case "tool_result":
			toolResult, _ = ev["result"].(string)
		case "text_delta":
			text, _ := ev["text"].(string)
			deltas = append(deltas, text)
		case "done":
			done = ev
		}
	}
	if !strings.Contains(toolResult, novel.ID) {
		t.Fatalf("tool_result should reference the created novel id %q, got %q", novel.ID, toolResult)
	}
	if strings.Join(deltas, "") != "Encontré 1 novela." {
		t.Fatalf("unexpected text deltas: %#v", deltas)
	}
	if done == nil {
		t.Fatal("stream must end with a done event")
	}
	if msg, ok := done["message"].(map[string]any); !ok || msg["content"] != "Encontré 1 novela." {
		t.Fatalf("done event must carry the assistant message, got %v", done["message"])
	}

	// The session must persist the whole trail: user, assistant tool call,
	// tool result, final assistant message.
	getResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/agent/session", alice.Token, nil)
	assertStatus(t, getResp, http.StatusOK)
	var session struct {
		ID       string `json:"id"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	decodeData(t, getResp, &session)
	if session.ID != sessionID {
		t.Fatalf("persisted session id %q should match streamed %q", session.ID, sessionID)
	}
	if len(session.Messages) != 4 {
		t.Fatalf("expected 4 persisted messages (user, assistant tool call, tool, assistant), got %d", len(session.Messages))
	}
	if session.Messages[0].Role != "user" || !strings.Contains(session.Messages[0].Content, "descripción") {
		t.Fatalf("unexpected first message: %#v", session.Messages[0])
	}
	if session.Messages[3].Content != "Encontré 1 novela." {
		t.Fatalf("unexpected final assistant message: %#v", session.Messages[3])
	}

	// A second user must not see alice's session.
	bob := registerUser(t, env, "bob-agent@example.com", "secret123", "Bob")
	bobResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/agent/session", bob.Token, nil)
	assertStatus(t, bobResp, http.StatusOK)
	if !strings.Contains(bobResp.Body.String(), `"data":null`) {
		t.Fatalf("bob should have no session, got %s", bobResp.Body.String())
	}

	// Reset wipes the sessions.
	delResp := doJSONRequest(t, env.handler, http.MethodDelete, "/api/v1/agent/session", alice.Token, nil)
	assertStatus(t, delResp, http.StatusNoContent)
	afterResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/agent/session", alice.Token, nil)
	assertStatus(t, afterResp, http.StatusOK)
	if !strings.Contains(afterResp.Body.String(), `"data":null`) {
		t.Fatalf("session should be gone after reset, got %s", afterResp.Body.String())
	}
}

func TestAgentChatRejectsUnsupportedProviderAndBadInput(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "alice-agent-2@example.com", "secret123", "Alice")

	// A provider that does not implement ai.AgentProvider (the Google
	// provider is the real-world case) must fail with a clear 400 before any
	// streaming starts.
	env.server.NewAIProvider = func(store.AISettings, string) (ai.Provider, error) {
		return &ai.GoogleProvider{APIKey: "k", Model: "m"}, nil
	}
	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/agent/chat", alice.Token, map[string]any{
		"message": "hola",
	})
	assertStatus(t, resp, http.StatusBadRequest)
	if ct := resp.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("expected problem+json error, got %q", ct)
	}

	// Empty messages are rejected.
	env.server.NewAIProvider = nil
	emptyResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/agent/chat", alice.Token, map[string]any{
		"message": "   ",
	})
	assertStatus(t, emptyResp, http.StatusBadRequest)

	// Unknown session ids are a 404, not a silent new session.
	unknownResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/agent/chat", alice.Token, map[string]any{
		"sessionId": "does-not-exist",
		"message":   "hola",
	})
	assertStatus(t, unknownResp, http.StatusNotFound)
}

func TestAgentChatRequiresOwnershipOfNovel(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "alice-agent-3@example.com", "secret123", "Alice")
	bob := registerUser(t, env, "bob-agent-3@example.com", "secret123", "Bob")
	novel := createNovel(t, env.handler, alice.Token, "Privada", "es", "en")

	env.server.NewAIProvider = func(store.AISettings, string) (ai.Provider, error) {
		return &fakeAgentProvider{finalText: "ok"}, nil
	}

	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/agent/chat", bob.Token, map[string]any{
		"message": "cuéntame de esta novela",
		"novelId": novel.ID,
	})
	// GetNovelAccessible masks another user's novel as not found.
	assertStatus(t, resp, http.StatusNotFound)
}

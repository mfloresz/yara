package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

// TestAgentChatAskUserEmitsQuestionEvent drives the ask_user flow end to end:
// the terminal tool call must surface as a "question" NDJSON event (never as a
// tool chip), persist a replayable trail, and accept the picked option as the
// next turn's message.
func TestAgentChatAskUserEmitsQuestionEvent(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "alice-ask@example.com", "secret123", "Alice")
	createNovel(t, env.handler, alice.Token, "The Guardian", "es", "en")

	const askArgs = `{"question":"¿De cuál necesitas las estadísticas?","options":[` +
		`{"label":"The Guardian (de Evil_Warlord)","value":"gp7kkn"},` +
		`{"label":"Cultivating Clan","value":"cxg6ar"}]}`

	// questionAgentProvider mimics the real loop's ask_user turn: one terminal
	// tool call, the awaitingUser tool result, no final assistant text. It
	// records the replayed history so the test can assert the trail survives.
	provider := &questionAgentProvider{askArgs: askArgs}
	env.server.NewAIProvider = func(store.AISettings, string) (ai.Provider, error) {
		return provider, nil
	}

	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/agent/chat", alice.Token, map[string]any{
		"message": "Dame las estadísticas de la novela The Guardian",
	})
	assertStatus(t, resp, http.StatusOK)
	events := parseNDJSON(t, []byte(resp.Body.String()))

	var question, done map[string]any
	for _, ev := range events {
		switch ev["type"] {
		case "question":
			question = ev
		case "tool_call", "tool_result":
			if ev["tool"] == agentAskUserToolName {
				t.Fatalf("ask_user must not leak as a tool event, got %v", ev)
			}
		case "done":
			done = ev
		}
	}
	if question == nil {
		t.Fatalf("stream must contain a question event, got %#v", events)
	}
	if question["question"] != "¿De cuál necesitas las estadísticas?" {
		t.Fatalf("unexpected question payload: %v", question)
	}
	options, ok := question["options"].([]any)
	if !ok || len(options) != 2 {
		t.Fatalf("question must carry two options, got %v", question["options"])
	}
	first, _ := options[0].(map[string]any)
	if first["label"] != "The Guardian (de Evil_Warlord)" || first["value"] != "gp7kkn" {
		t.Fatalf("unexpected first option: %v", first)
	}
	if done == nil {
		t.Fatal("stream must still end with a done event")
	}
	if msg, ok := done["message"].(map[string]any); !ok || msg["content"] != "" {
		t.Fatalf("question turn must end without assistant text, got %v", done["message"])
	}

	// The trail must persist the ask_user call and its awaitingUser result so
	// the next turn replays a valid conversation.
	getResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/agent/session", alice.Token, nil)
	assertStatus(t, getResp, http.StatusOK)
	var session struct {
		Messages []struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			ToolCalls []struct {
				Name string `json:"name"`
				Args string `json:"args"`
			} `json:"toolCalls"`
		} `json:"messages"`
	}
	decodeData(t, getResp, &session)
	if len(session.Messages) != 3 {
		t.Fatalf("expected 3 persisted messages (user, assistant tool call, tool), got %d", len(session.Messages))
	}
	if len(session.Messages[1].ToolCalls) != 1 || session.Messages[1].ToolCalls[0].Name != agentAskUserToolName {
		t.Fatalf("assistant message must persist the ask_user call, got %#v", session.Messages[1])
	}
	if !strings.Contains(session.Messages[1].ToolCalls[0].Args, "gp7kkn") {
		t.Fatalf("persisted args must carry the options, got %q", session.Messages[1].ToolCalls[0].Args)
	}
	if session.Messages[2].Role != "tool" || !strings.Contains(session.Messages[2].Content, "awaitingUser") {
		t.Fatalf("tool result must persist the awaitingUser marker, got %#v", session.Messages[2])
	}

	// The user's click arrives as a normal message; the replayed history the
	// provider receives must contain the whole ask_user trail.
	resp2 := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/agent/chat", alice.Token, map[string]any{
		"message": "gp7kkn",
	})
	assertStatus(t, resp2, http.StatusOK)
	seen := provider.seen
	if len(seen) != 4 {
		t.Fatalf("second turn must replay user + ask_user trail + new message, got %d messages: %#v", len(seen), seen)
	}
	if seen[1].ToolCalls[0].Name != agentAskUserToolName || seen[2].Role != "tool" || seen[3].Content != "gp7kkn" {
		t.Fatalf("replayed history is not the question trail plus the answer: %#v", seen)
	}
}

// questionAgentProvider mimics the real loop's ask_user turn: one terminal
// tool call, the awaitingUser tool result, no final assistant text. It
// records the replayed history it receives each turn.
type questionAgentProvider struct {
	*ai.OpenAIProvider
	askArgs string
	seen    []ai.AgentMessage
}

func (q *questionAgentProvider) AgentChat(ctx context.Context, in ai.AgentChatInput) (ai.AgentChatOutput, error) {
	q.seen = append([]ai.AgentMessage{}, in.Messages...)
	trail := append([]ai.AgentMessage{}, in.Messages...)
	trail = append(trail,
		ai.AgentMessage{Role: "assistant", ToolCalls: []ai.AgentToolCall{{ID: "call-1", Name: agentAskUserToolName, Args: q.askArgs}}},
		ai.AgentMessage{Role: "tool", Content: `{"awaitingUser":true}`, ToolCallID: "call-1", ToolName: agentAskUserToolName},
	)
	if in.OnEvent != nil {
		in.OnEvent(ai.AgentEvent{Type: "tool_call", Step: 1, ToolName: agentAskUserToolName, ToolArgs: q.askArgs})
		in.OnEvent(ai.AgentEvent{Type: "tool_result", Step: 1, ToolName: agentAskUserToolName, ToolResult: `{"awaitingUser":true}`})
	}
	return ai.AgentChatOutput{Messages: trail, Steps: 1}, nil
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

// scriptedAgentProvider runs a fixed sequence of tool calls against the real
// tool catalog and answers once they all executed.
type scriptedAgentProvider struct {
	*ai.OpenAIProvider
	toolCalls []string // "_tool" + args envelopes, in order
}

func (f *scriptedAgentProvider) AgentChat(ctx context.Context, in ai.AgentChatInput) (ai.AgentChatOutput, error) {
	byName := map[string]ai.AgentTool{}
	for _, tool := range in.Tools {
		byName[tool.Name] = tool
	}
	toolMsgs := make([]ai.AgentMessage, 0, len(f.toolCalls))
	for i, rawArgs := range f.toolCalls {
		var envelope struct {
			Tool string          `json:"_tool"`
			Args json.RawMessage `json:"args"`
		}
		if err := json.Unmarshal([]byte(rawArgs), &envelope); err != nil {
			return ai.AgentChatOutput{}, err
		}
		tool, ok := byName[envelope.Tool]
		if !ok {
			return ai.AgentChatOutput{}, fmt.Errorf("scripted tool %q not in catalog", envelope.Tool)
		}
		result, err := tool.Execute(ctx, envelope.Args)
		content := result
		if err != nil {
			content = "error: " + err.Error()
		}
		callID := fmt.Sprintf("call-%d", i+1)
		if in.OnEvent != nil {
			in.OnEvent(ai.AgentEvent{Type: "tool_call", Step: i + 1, ToolName: envelope.Tool, ToolArgs: string(envelope.Args)})
			in.OnEvent(ai.AgentEvent{Type: "tool_result", Step: i + 1, ToolName: envelope.Tool, ToolResult: content})
		}
		toolMsgs = append(toolMsgs,
			ai.AgentMessage{Role: "assistant", ToolCalls: []ai.AgentToolCall{{ID: callID, Name: envelope.Tool, Args: string(envelope.Args)}}},
			ai.AgentMessage{Role: "tool", Content: content, ToolCallID: callID, ToolName: envelope.Tool},
		)
	}
	final := "Hecho."
	trail := append([]ai.AgentMessage{}, in.Messages...)
	trail = append(trail, toolMsgs...)
	trail = append(trail, ai.AgentMessage{Role: "assistant", Content: final})
	return ai.AgentChatOutput{Messages: trail, Text: final, Steps: len(f.toolCalls) + 1}, nil
}

// TestAgentChatChapterToolsExercisesNewCatalog drives the four chapter tools
// added after the first agent release: search_chapters, update_chapter,
// set_chapter_status and set_chapter_excluded.
func TestAgentChatChapterToolsExercisesNewCatalog(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "alice-chtools@example.com", "secret123", "Alice")
	novel := createNovel(t, env.handler, alice.Token, "Capítulos", "es", "en")

	// One chapter with body content to search and edit.
	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/"+novel.ID+"/chapters", alice.Token, map[string]any{
		"chapterOrder":      1,
		"title":             "El comienzo",
		"originalContent":   "The hidden dragon awakens beneath the mountain.",
		"translatedContent": "El dragón oculto despierta bajo la montaña.",
		"status":            "translated",
	})
	assertStatus(t, resp, http.StatusCreated)
	var chapter struct {
		ID string `json:"id"`
	}
	decodeData(t, resp, &chapter)

	env.server.NewAIProvider = func(store.AISettings, string) (ai.Provider, error) {
		return &scriptedAgentProvider{toolCalls: []string{
			`{"_tool":"search_chapters","args":{"novelId":"` + novel.ID + `","query":"dragon"}}`,
			`{"_tool":"update_chapter","args":{"novelId":"` + novel.ID + `","chapterId":"` + chapter.ID + `","translatedTitle":"El inicio","translatedContent":"El dragón oculto despierta bajo la montaña, furioso."}}`,
			`{"_tool":"set_chapter_status","args":{"novelId":"` + novel.ID + `","chapterId":"` + chapter.ID + `","status":"refined"}}`,
			`{"_tool":"set_chapter_excluded","args":{"novelId":"` + novel.ID + `","chapterId":"` + chapter.ID + `","excluded":false}}`,
		}}, nil
	}

	chatResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/agent/chat", alice.Token, map[string]any{
		"message": "busca dragon, edita el capítulo y marca refined",
		"novelId": novel.ID,
	})
	assertStatus(t, chatResp, http.StatusOK)

	var searchResult, updateResult, statusResult, excludeResult string
	for _, ev := range parseNDJSON(t, []byte(chatResp.Body.String())) {
		if ev["type"] != "tool_result" {
			continue
		}
		switch ev["tool"] {
		case "search_chapters":
			searchResult, _ = ev["result"].(string)
		case "update_chapter":
			updateResult, _ = ev["result"].(string)
		case "set_chapter_status":
			statusResult, _ = ev["result"].(string)
		case "set_chapter_excluded":
			excludeResult, _ = ev["result"].(string)
		}
	}

	if !strings.Contains(searchResult, chapter.ID) || !strings.Contains(searchResult, "originalContent") || !strings.Contains(searchResult, "hidden dragon") {
		t.Fatalf("search_chapters should hit the body with a snippet, got %q", searchResult)
	}
	if !strings.Contains(updateResult, `"ok":true`) || !strings.Contains(updateResult, "El inicio") {
		t.Fatalf("update_chapter should apply the edit, got %q", updateResult)
	}
	if !strings.Contains(statusResult, `"status":"refined"`) {
		t.Fatalf("set_chapter_status should apply, got %q", statusResult)
	}
	if !strings.Contains(excludeResult, `"excluded":false`) {
		t.Fatalf("set_chapter_excluded should apply, got %q", excludeResult)
	}

	// The edits must be visible through the normal REST surface.
	getResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels/"+novel.ID+"/chapters/"+chapter.ID, alice.Token, nil)
	assertStatus(t, getResp, http.StatusOK)
	var got struct {
		TranslatedTitle   string `json:"translatedTitle"`
		TranslatedContent string `json:"translatedContent"`
		Status            string `json:"status"`
	}
	decodeData(t, getResp, &got)
	if got.TranslatedTitle != "El inicio" {
		t.Fatalf("translatedTitle not persisted: %q", got.TranslatedTitle)
	}
	if !strings.Contains(got.TranslatedContent, "furioso") {
		t.Fatalf("translatedContent not persisted: %q", got.TranslatedContent)
	}
	if got.Status != "refined" {
		t.Fatalf("status not persisted: %q", got.Status)
	}
}

// TestAgentChatChapterToolsRejectsForeignNovel pins ownership: every chapter
// tool resolves the novel through the requesting user, so another user's
// chapter ids are indistinguishable from unknown ones.
func TestAgentChatChapterToolsRejectsForeignNovel(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "alice-chown@example.com", "secret123", "Alice")
	bob := registerUser(t, env, "bob-chown@example.com", "secret123", "Bob")
	novel := createNovel(t, env.handler, alice.Token, "Privada", "es", "en")
	chResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/"+novel.ID+"/chapters", alice.Token, map[string]any{
		"chapterOrder":    1,
		"title":           "Uno",
		"originalContent": "contenido",
	})
	assertStatus(t, chResp, http.StatusCreated)
	var chapter struct {
		ID string `json:"id"`
	}
	decodeData(t, chResp, &chapter)

	env.server.NewAIProvider = func(store.AISettings, string) (ai.Provider, error) {
		return &scriptedAgentProvider{toolCalls: []string{
			`{"_tool":"update_chapter","args":{"novelId":"` + novel.ID + `","chapterId":"` + chapter.ID + `","title":"hackeado"}}`,
			`{"_tool":"set_chapter_status","args":{"novelId":"` + novel.ID + `","chapterId":"` + chapter.ID + `","status":"done"}}`,
			`{"_tool":"set_chapter_excluded","args":{"novelId":"` + novel.ID + `","chapterId":"` + chapter.ID + `","excluded":true}}`,
			`{"_tool":"search_chapters","args":{"novelId":"` + novel.ID + `","query":"contenido"}}`,
		}}, nil
	}

	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/agent/chat", bob.Token, map[string]any{
		"message": "tócame los capítulos",
	})
	assertStatus(t, resp, http.StatusOK)

	for _, ev := range parseNDJSON(t, []byte(resp.Body.String())) {
		if ev["type"] != "tool_result" {
			continue
		}
		result, _ := ev["result"].(string)
		if !strings.HasPrefix(result, "error: ") {
			t.Fatalf("tool %v against a foreign novel must fail, got %q", ev["tool"], result)
		}
		if strings.Contains(result, "hackeado") {
			t.Fatalf("update must not leak the applied title, got %q", result)
		}
	}
}

// TestAgentChatQueryLibraryTool drives the read-only SQL analytics tool end
// to end: the model's SELECT goes through the scoped views and returns only
// the requesting user's rows.
func TestAgentChatQueryLibraryTool(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "alice-sql@example.com", "secret123", "Alice")
	novel := createNovel(t, env.handler, alice.Token, "Analytics", "en", "es")
	chResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/"+novel.ID+"/chapters", alice.Token, map[string]any{
		"chapterOrder":      1,
		"title":             "Uno",
		"originalContent":   "body",
		"translatedContent": "cuerpo",
		"status":            "translated",
	})
	assertStatus(t, chResp, http.StatusCreated)

	// The motivating query from the feature request: novels missing fewer
	// than 10 chapters to be complete.
	env.server.NewAIProvider = func(store.AISettings, string) (ai.Provider, error) {
		return &scriptedAgentProvider{toolCalls: []string{
			`{"_tool":"query_library","args":{"sql":"SELECT novel_id, title, total, pending FROM v_agent_novel_progress WHERE pending < 10 ORDER BY pending"}}`,
		}}, nil
	}

	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/agent/chat", alice.Token, map[string]any{
		"message": "¿qué novelas les faltan menos de 10 capítulos?",
	})
	assertStatus(t, resp, http.StatusOK)

	var result string
	for _, ev := range parseNDJSON(t, []byte(resp.Body.String())) {
		if ev["type"] == "tool_result" && ev["tool"] == "query_library" {
			result, _ = ev["result"].(string)
		}
	}
	if !strings.Contains(result, novel.ID) || !strings.Contains(result, "Analytics") {
		t.Fatalf("query_library should return alice's novel, got %q", result)
	}
	if !strings.Contains(result, `"pending":0`) && !strings.Contains(result, "0") {
		t.Fatalf("unexpected pending value in %q", result)
	}

	// A destructive query arrives back as a tool error, never as data loss.
	env.server.NewAIProvider = func(store.AISettings, string) (ai.Provider, error) {
		return &scriptedAgentProvider{toolCalls: []string{
			`{"_tool":"query_library","args":{"sql":"DELETE FROM v_agent_novel_progress"}}`,
		}}, nil
	}
	badResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/agent/chat", alice.Token, map[string]any{
		"message": "borra todo",
	})
	assertStatus(t, badResp, http.StatusOK)
	var badResult string
	for _, ev := range parseNDJSON(t, []byte(badResp.Body.String())) {
		if ev["type"] == "tool_result" && ev["tool"] == "query_library" {
			badResult, _ = ev["result"].(string)
		}
	}
	if !strings.HasPrefix(badResult, "error: ") {
		t.Fatalf("DELETE attempt must surface as a tool error, got %q", badResult)
	}
}

// runAgentTools executes one chat turn and returns the concatenated tool
// results, so a test can assert on what the model actually read or wrote.
func runAgentTools(t *testing.T, env *apiTestEnv, token string, calls []string, body map[string]any) []string {
	t.Helper()
	env.server.NewAIProvider = func(store.AISettings, string) (ai.Provider, error) {
		return &scriptedAgentProvider{toolCalls: calls}, nil
	}
	payload := map[string]any{"message": "hazlo"}
	for k, v := range body {
		payload[k] = v
	}
	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/agent/chat", token, payload)
	assertStatus(t, resp, http.StatusOK)
	results := []string{}
	for _, ev := range parseNDJSON(t, []byte(resp.Body.String())) {
		if ev["type"] == "tool_result" {
			if s, ok := ev["result"].(string); ok {
				results = append(results, s)
			}
		}
	}
	return results
}

// TestAgentToolsCannotReadAnotherUsersLibrary is the core ownership
// guarantee: no assistant tool, however the model asks, can read a novel,
// chapter or body that belongs to someone else — not even when the novel is
// public, which the REST surface deliberately exposes to other users.
func TestAgentToolsCannotReadAnotherUsersLibrary(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "alice-iso@example.com", "secret123", "Alice")
	bob := registerUser(t, env, "bob-iso@example.com", "secret123", "Bob")

	aliceNovel := createNovel(t, env.handler, alice.Token, "Secreto de Alice", "es", "en")
	chResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/"+aliceNovel.ID+"/chapters", alice.Token, map[string]any{
		"chapterOrder":      1,
		"title":             "Uno",
		"originalContent":   "contenido original de alice",
		"translatedContent": "traducción secreta de alice",
		"status":            "translated",
	})
	assertStatus(t, chResp, http.StatusCreated)
	chapterID := chapterIDFrom(t, chResp)

	// Make it public: the REST API exposes it to every user, so the assistant
	// must not become a back door around that.
	pubResp := doJSONRequest(t, env.handler, http.MethodPatch, "/api/v1/novels/"+aliceNovel.ID, alice.Token, map[string]any{
		"isPublic": true,
	})
	assertStatus(t, pubResp, http.StatusOK)

	readTools := []string{
		`{"_tool":"list_novels","args":{"query":"Secreto"}}`,
		`{"_tool":"get_novel","args":{"id":"` + aliceNovel.ID + `"}}`,
		`{"_tool":"get_novel_stats","args":{"id":"` + aliceNovel.ID + `"}}`,
		`{"_tool":"get_novel_chapters","args":{"id":"` + aliceNovel.ID + `"}}`,
		`{"_tool":"get_chapter","args":{"novelId":"` + aliceNovel.ID + `","chapterId":"` + chapterID + `","content":"original"}}`,
		`{"_tool":"get_chapter","args":{"novelId":"` + aliceNovel.ID + `","chapterId":"` + chapterID + `","content":"translated"}}`,
		`{"_tool":"search_chapters","args":{"novelId":"` + aliceNovel.ID + `","query":"secreta"}}`,
		`{"_tool":"query_library","args":{"sql":"SELECT novel_id, title FROM v_agent_novel_progress"}}`,
		`{"_tool":"query_library","args":{"sql":"SELECT chapter_id, title FROM v_agent_chapter_overview"}}`,
	}
	for _, call := range readTools {
		for _, result := range runAgentTools(t, env, bob.Token, []string{call}, nil) {
			for _, secret := range []string{
				aliceNovel.ID,
				"Secreto de Alice",
				chapterID,
				"contenido original de alice",
				"traducción secreta de alice",
			} {
				if strings.Contains(result, secret) {
					t.Errorf("tool %s leaked %q to another user: %s", call, secret, result)
				}
			}
		}
	}
}

// TestAgentToolsCannotWriteAnotherUsersLibrary pins the write side of the same
// guarantee: the mutation tools are refused for a novel the caller does not
// own, and the target record is unchanged afterwards.
func TestAgentToolsCannotWriteAnotherUsersLibrary(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "alice-w@example.com", "secret123", "Alice")
	bob := registerUser(t, env, "bob-w@example.com", "secret123", "Bob")
	novel := createNovel(t, env.handler, alice.Token, "Intocable", "es", "en")
	chResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/"+novel.ID+"/chapters", alice.Token, map[string]any{
		"chapterOrder":    1,
		"title":           "Original",
		"originalContent": "intocable",
		"status":          "pending",
	})
	assertStatus(t, chResp, http.StatusCreated)
	chapterID := chapterIDFrom(t, chResp)
	pubResp := doJSONRequest(t, env.handler, http.MethodPatch, "/api/v1/novels/"+novel.ID, alice.Token, map[string]any{"isPublic": true})
	assertStatus(t, pubResp, http.StatusOK)

	writeTools := []string{
		`{"_tool":"update_novel","args":{"id":"` + novel.ID + `","targetTitle":"Secuestrada"}}`,
		`{"_tool":"update_chapter","args":{"novelId":"` + novel.ID + `","chapterId":"` + chapterID + `","translatedContent":"hackeado"}}`,
		`{"_tool":"set_chapter_status","args":{"novelId":"` + novel.ID + `","chapterId":"` + chapterID + `","status":"done"}}`,
		`{"_tool":"set_chapter_excluded","args":{"novelId":"` + novel.ID + `","chapterId":"` + chapterID + `","excluded":true}}`,
	}
	for _, call := range writeTools {
		results := runAgentTools(t, env, bob.Token, []string{call}, nil)
		if len(results) != 1 || !strings.HasPrefix(results[0], "error: ") {
			t.Errorf("write tool %s must be refused for a non-owner, got %v", call, results)
		}
	}

	// Nothing changed on alice's side.
	novelResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels/"+novel.ID, alice.Token, nil)
	assertStatus(t, novelResp, http.StatusOK)
	if strings.Contains(novelResp.Body.String(), "Secuestrada") {
		t.Fatal("another user renamed the novel through the assistant")
	}
	chapterResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels/"+novel.ID+"/chapters?includeContent=true", alice.Token, nil)
	assertStatus(t, chapterResp, http.StatusOK)
	if strings.Contains(chapterResp.Body.String(), "hackeado") {
		t.Fatal("another user edited the chapter through the assistant")
	}
	if !strings.Contains(chapterResp.Body.String(), `"status":"pending"`) {
		t.Fatalf("another user changed the chapter status through the assistant: %s", chapterResp.Body.String())
	}
}

// TestAgentSessionIsPerUser pins that each user gets an independent chat and
// never sees another's history.
func TestAgentSessionIsPerUser(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "alice-sess@example.com", "secret123", "Alice")
	bob := registerUser(t, env, "bob-sess@example.com", "secret123", "Bob")

	env.server.NewAIProvider = func(store.AISettings, string) (ai.Provider, error) {
		return &fakeAgentProvider{finalText: "respuesta"}, nil
	}
	aliceResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/agent/chat", alice.Token, map[string]any{
		"message": "mi secreto de alice",
	})
	assertStatus(t, aliceResp, http.StatusOK)
	bobResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/agent/chat", bob.Token, map[string]any{
		"message": "hola bob",
	})
	assertStatus(t, bobResp, http.StatusOK)

	aliceSession := sessionIDFrom(t, aliceResp)
	bobSession := sessionIDFrom(t, bobResp)
	if aliceSession == "" || bobSession == "" {
		t.Fatal("both users must get a session id")
	}
	if aliceSession == bobSession {
		t.Fatal("users must not share a chat session")
	}

	// Alice's history holds her message; bob's must not contain it.
	aliceGet := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/agent/session", alice.Token, nil)
	assertStatus(t, aliceGet, http.StatusOK)
	if !strings.Contains(aliceGet.Body.String(), "mi secreto de alice") {
		t.Fatalf("alice's session lost her message: %s", aliceGet.Body.String())
	}
	bobGet := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/agent/session", bob.Token, nil)
	assertStatus(t, bobGet, http.StatusOK)
	if strings.Contains(bobGet.Body.String(), "mi secreto de alice") {
		t.Fatalf("bob can read alice's session: %s", bobGet.Body.String())
	}
	if strings.Contains(bobGet.Body.String(), aliceSession) {
		t.Fatalf("bob's session points at alice's session id: %s", bobGet.Body.String())
	}

	// Sending an explicit foreign sessionId must be refused, not adopted.
	crossResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/agent/chat", bob.Token, map[string]any{
		"sessionId": aliceSession,
		"message":   "intento",
	})
	assertStatus(t, crossResp, http.StatusNotFound)

	// Resetting bob's chat leaves alice's intact.
	delResp := doJSONRequest(t, env.handler, http.MethodDelete, "/api/v1/agent/session", bob.Token, nil)
	assertStatus(t, delResp, http.StatusNoContent)
	bobAfterReset := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/agent/session", bob.Token, nil)
	assertStatus(t, bobAfterReset, http.StatusOK)
	if !strings.Contains(bobAfterReset.Body.String(), `"data":null`) {
		t.Fatalf("bob's session should be gone after reset, got %s", bobAfterReset.Body.String())
	}
	aliceAfterReset := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/agent/session", alice.Token, nil)
	assertStatus(t, aliceAfterReset, http.StatusOK)
	if !strings.Contains(aliceAfterReset.Body.String(), "mi secreto de alice") {
		t.Fatalf("bob's reset must not clear alice's session: %s", aliceAfterReset.Body.String())
	}
}

// TestAgentChapterStatusAcceptsOnlySchemaValues pins the closed status set to
// the chapters collection values: "failed" works, the previously advertised
// "error" is refused instead of failing PocketBase validation.
func TestAgentChapterStatusAcceptsOnlySchemaValues(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "alice-status@example.com", "secret123", "Alice")
	novel := createNovel(t, env.handler, alice.Token, "Estados", "es", "en")
	chResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/"+novel.ID+"/chapters", alice.Token, map[string]any{
		"chapterOrder":    1,
		"title":           "Uno",
		"originalContent": "x",
		"status":          "pending",
	})
	assertStatus(t, chResp, http.StatusCreated)
	chapterID := chapterIDFrom(t, chResp)

	ok := runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"set_chapter_status","args":{"novelId":"` + novel.ID + `","chapterId":"` + chapterID + `","status":"failed","errorMessage":"boom"}}`,
	}, nil)
	if len(ok) != 1 || strings.HasPrefix(ok[0], "error: ") {
		t.Fatalf("failed must be an accepted status, got %v", ok)
	}
	chapterResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels/"+novel.ID+"/chapters?includeContent=true", alice.Token, nil)
	assertStatus(t, chapterResp, http.StatusOK)
	if !strings.Contains(chapterResp.Body.String(), `"status":"failed"`) {
		t.Fatalf("status was not persisted: %s", chapterResp.Body.String())
	}

	rejected := runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"set_chapter_status","args":{"novelId":"` + novel.ID + `","chapterId":"` + chapterID + `","status":"error"}}`,
		`{"_tool":"set_chapter_status","args":{"novelId":"` + novel.ID + `","chapterId":"` + chapterID + `","status":"inventado"}}`,
	}, nil)
	for _, result := range rejected {
		if !strings.HasPrefix(result, "error: ") {
			t.Errorf("unknown status must be refused, got %q", result)
		}
	}
}

// TestTrimAgentHistoryCutsAtTurnBoundary guards the replay invariant: a
// trimmed trail must start with a user message, never with an orphaned tool
// result, which every OpenAI-compatible API rejects with a 400.
func TestTrimAgentHistoryCutsAtTurnBoundary(t *testing.T) {
	build := func(n int) []ai.AgentMessage {
		msgs := make([]ai.AgentMessage, 0, n*3)
		for i := 0; i < n; i++ {
			msgs = append(msgs,
				ai.AgentMessage{Role: "user", Content: fmt.Sprintf("pregunta %d", i)},
				ai.AgentMessage{Role: "assistant", ToolCalls: []ai.AgentToolCall{{ID: fmt.Sprintf("c%d", i), Name: "list_novels", Args: "{}"}}},
				ai.AgentMessage{Role: "tool", Content: "[]", ToolCallID: fmt.Sprintf("c%d", i), ToolName: "list_novels"},
			)
		}
		return msgs
	}

	trimmed := trimAgentHistory(build(100))
	if len(trimmed) == 0 {
		t.Fatal("trimming dropped the whole history")
	}
	if trimmed[0].Role != "user" {
		t.Fatalf("trimmed history must start at a user message, got %q", trimmed[0].Role)
	}
	if len(trimmed) > agentMaxHistoryMessages {
		t.Fatalf("trimmed history is still over the cap: %d", len(trimmed))
	}
	// Every tool result must be preceded by its assistant tool call.
	pending := map[string]bool{}
	for _, m := range trimmed {
		switch m.Role {
		case "assistant":
			for _, call := range m.ToolCalls {
				pending[call.ID] = true
			}
		case "tool":
			if !pending[m.ToolCallID] {
				t.Fatalf("orphaned tool result for %q survived the trim", m.ToolCallID)
			}
			delete(pending, m.ToolCallID)
		}
	}

	// A short history is left untouched.
	short := build(3)
	if got := trimAgentHistory(short); len(got) != len(short) {
		t.Fatalf("short history must not be trimmed, got %d of %d", len(got), len(short))
	}
}

func chapterIDFrom(t *testing.T, resp *httptest.ResponseRecorder) string {
	t.Helper()
	var payload struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode chapter response: %v", err)
	}
	if payload.Data.ID == "" {
		t.Fatalf("chapter response carried no id: %s", resp.Body.String())
	}
	return payload.Data.ID
}

func sessionIDFrom(t *testing.T, resp *httptest.ResponseRecorder) string {
	t.Helper()
	for _, ev := range parseNDJSON(t, []byte(resp.Body.String())) {
		if id, ok := ev["sessionId"].(string); ok && id != "" {
			return id
		}
	}
	return ""
}

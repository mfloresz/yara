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

type capturedChatRequest struct {
	Model          string `json:"model"`
	ResponseFormat any    `json:"response_format"`
	Messages       []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

func systemContent(req capturedChatRequest) string {
	for _, m := range req.Messages {
		if m.Role == "system" {
			return m.Content
		}
	}
	return ""
}

func userContent(req capturedChatRequest) string {
	for _, m := range req.Messages {
		if m.Role == "user" {
			return m.Content
		}
	}
	return ""
}

func respondChatCompletion(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":      "chatcmpl-test",
		"object":  "chat.completion",
		"model":   "test-model",
		"choices": []map[string]any{{"index": 0, "message": map[string]any{"role": "assistant", "content": text}, "finish_reason": "stop"}},
		"usage":   map[string]int{"prompt_tokens": 10, "completion_tokens": 20, "total_tokens": 30},
	})
}

func TestOpenAITranslateTitle_SendsTitleContextAndReturnsPlainText(t *testing.T) {
	var captured capturedChatRequest

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed reading request body: %v", err)
		}
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Fatalf("failed decoding request body: %v\nbody: %s", err, string(body))
		}

		respondChatCompletion(w, "Capítulo Siete")
	}))
	defer ts.Close()

	provider := &OpenAIProvider{
		APIKey:  "test-key",
		BaseURL: ts.URL,
		Model:   "test-model",
	}

	translatedTitle, err := provider.TranslateTitle(context.Background(), TranslateTitleInput{
		SystemPrompt:   "Translate from English to Spanish.\n\nGlossary:\nhouse → casa",
		TitleOriginal:  "Chapter Seven",
		SourceLanguage: "en",
		TargetLanguage: "es",
	})
	if err != nil {
		t.Fatalf("TranslateTitle returned error: %v", err)
	}

	if translatedTitle != "Capítulo Siete" {
		t.Fatalf("unexpected translated title\nwant: %q\ngot:  %q", "Capítulo Siete", translatedTitle)
	}

	if captured.Model != "test-model" {
		t.Fatalf("unexpected model: %q", captured.Model)
	}
	if strings.TrimSpace(systemContent(captured)) == "" {
		t.Fatal("system message should not be empty")
	}

	userText := userContent(captured)
	if userText == "" {
		t.Fatal("user message content should not be empty")
	}

	var userPayload map[string]any
	if err := json.Unmarshal([]byte(userText), &userPayload); err != nil {
		t.Fatalf("user message is not valid JSON: %v\ncontent: %s", err, userText)
	}
	if len(userPayload) != 1 {
		t.Fatalf("user payload should have exactly 1 field, got %d: %#v", len(userPayload), userPayload)
	}
	if got := userPayload["title_original"]; got != "Chapter Seven" {
		t.Fatalf("unexpected title_original\nwant: %q\ngot:  %#v", "Chapter Seven", got)
	}
	for _, forbidden := range []string{
		"text_original",
		"source_language", "target_language",
		"previous_title_original", "previous_title_translated",
	} {
		if _, ok := userPayload[forbidden]; ok {
			t.Fatalf("user payload must not include %q", forbidden)
		}
	}

	if captured.ResponseFormat != nil {
		t.Fatalf("plain-text title translation must not request a response_format, got: %#v", captured.ResponseFormat)
	}
}

func TestOpenAITranslateTitle_IncludesPreviousTitleInUserPayload(t *testing.T) {
	var captured capturedChatRequest

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Fatalf("failed decoding request body: %v\nbody: %s", err, string(body))
		}
		respondChatCompletion(w, "Capítulo Siete")
	}))
	defer ts.Close()

	provider := &OpenAIProvider{
		APIKey:  "test-key",
		BaseURL: ts.URL,
		Model:   "test-model",
	}

	if _, err := provider.TranslateTitle(context.Background(), TranslateTitleInput{
		SystemPrompt:       "Traduce fielmente.",
		TitleOriginal:      "Chapter Seven",
		PreviousTitleOrig:  "Chapter Six",
		PreviousTitleTrans: "Capítulo Seis",
		SourceLanguage:     "en",
		TargetLanguage:     "es",
	}); err != nil {
		t.Fatalf("TranslateTitle returned error: %v", err)
	}

	userText := userContent(captured)
	var userPayload map[string]any
	if err := json.Unmarshal([]byte(userText), &userPayload); err != nil {
		t.Fatalf("user message is not valid JSON: %v\ncontent: %s", err, userText)
	}

	if got := userPayload["title_original"]; got != "Chapter Seven" {
		t.Fatalf("unexpected title_original: %#v", got)
	}
	if got, ok := userPayload["previous_title_original"]; !ok || got != "Chapter Six" {
		t.Fatalf("previous_title_original missing or wrong: %#v (ok=%v)", got, ok)
	}
	if got, ok := userPayload["previous_title_translated"]; !ok || got != "Capítulo Seis" {
		t.Fatalf("previous_title_translated missing or wrong: %#v (ok=%v)", got, ok)
	}
}

func TestOpenAITranslateText_SendsPlainTextPromptWithoutSchema(t *testing.T) {
	var captured capturedChatRequest

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed reading request body: %v", err)
		}
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Fatalf("failed decoding request body: %v\nbody: %s", err, string(body))
		}
		respondChatCompletion(w, "Texto del cuerpo traducido")
	}))
	defer ts.Close()

	provider := &OpenAIProvider{
		APIKey:  "test-key",
		BaseURL: ts.URL,
		Model:   "test-model",
	}

	translatedText, err := provider.TranslateText(context.Background(), TranslateTextInput{
		SystemPrompt:    "Translate faithfully.",
		TextToTranslate: "Body text",
		SourceLanguage:  "en",
		TargetLanguage:  "es",
	})
	if err != nil {
		t.Fatalf("TranslateText returned error: %v", err)
	}

	if translatedText != "Texto del cuerpo traducido" {
		t.Fatalf("unexpected translated text\nwant: %q\ngot:  %q", "Texto del cuerpo traducido", translatedText)
	}

	if userContent(captured) != "Body text" {
		t.Fatalf("plain-text user prompt mismatch\nwant: %q\ngot:  %q", "Body text", userContent(captured))
	}
	if strings.Contains(systemContent(captured), "structured output schema") {
		t.Fatalf("content instructions must not require structured output:\n%s", systemContent(captured))
	}
	if captured.ResponseFormat != nil {
		t.Fatalf("plain-text translation must not request a response_format, got: %#v", captured.ResponseFormat)
	}
}

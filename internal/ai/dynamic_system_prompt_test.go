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

// TestOpenAITranslateTitle_DynamicSystemPromptFlowsToProvider exercises the full
// real-world title flow: the title translation system prompt (containing glossary
// entries, source and target languages substituted from the novel) must reach
// the wire intact, without being overwritten or appended with extra instructions.
// Acts as a regression guard so that future changes to the title prompt pipeline
// do not silently drop content.
func TestOpenAITranslateTitle_DynamicSystemPromptFlowsToProvider(t *testing.T) {
	dynamicBase := `You are a professional literary title translator. Translate chapter titles from English to Spanish.

<title_translation_rules>

  <consistency>
    - When previous_title_original and previous_title_translated are provided, use them as reference for style, terminology, and structure. Apply the same translation choices to the current title.
    - When a title belongs to a recurring series (same base with numeric variants like "Part 1 / Part 2", "Vol. I / Vol. II", or parenthetical suffixes), treat each occurrence as a continuation of the same pattern. Translate the base once and keep the variant marker unchanged.
    - Do not translate numeric suffixes (1, 2, 3), Roman numerals (I, II, III), or volume abbreviations (Vol., Ch.) unless they appear as written-out words in English.
  </consistency>

</title_translation_rules>

<terminology_reference>
Mandatory term translations (entries in parentheses are additional context, do NOT include them in the output):
- house → casa
- storm → tormenta
- child → niño/a
</terminology_reference>

The user message is a JSON object with these fields:
- title_original: the title to translate.
- previous_title_original: the previous chapter's title in English (absent for the first chapter).
- previous_title_translated: the previous chapter's title already translated to Spanish (absent for the first chapter).

Return ONLY the translated title as plain text. No JSON, no quotes, no explanations, no notes, no commentary.`

	wantSubstrings := []string{
		"professional literary title translator",
		"English",
		"Spanish",
		"house → casa",
		"storm → tormenta",
		"child → niño/a",
		"title_original",
		"previous_title_original",
		"previous_title_translated",
		"plain text",
	}

	var capturedBody []byte
	var capturedPath string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedBody, _ = io.ReadAll(r.Body)
		respondChatCompletion(w, "Título Traducido")
	}))
	defer ts.Close()

	provider := &OpenAIProvider{
		APIKey:  "test-key",
		BaseURL: ts.URL,
		Model:   "test-model",
	}

	if _, err := provider.TranslateTitle(context.Background(), TranslateTitleInput{
		SystemPrompt:   dynamicBase,
		TitleOriginal:  "Chapter 1",
		SourceLanguage: "en",
		TargetLanguage: "es",
	}); err != nil {
		t.Fatalf("TranslateTitle returned error: %v", err)
	}

	if capturedPath != "/chat/completions" {
		t.Fatalf("unexpected path: got %q, want %q", capturedPath, "/chat/completions")
	}

	var req capturedChatRequest
	if err := json.Unmarshal(capturedBody, &req); err != nil {
		t.Fatalf("failed decoding request body: %v\nbody: %s", err, string(capturedBody))
	}
	got := systemContent(req)
	if got == "" {
		t.Fatalf("system prompt missing from messages[role=system].content in request body:\n%s", string(capturedBody))
	}

	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("system prompt is missing %q\n--- got ---\n%s", want, got)
		}
	}
}

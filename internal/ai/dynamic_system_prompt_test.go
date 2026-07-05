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

	cases := []struct {
		name           string
		providerOpts   map[string]any
		wantPath       string
		extractSystem  func(body []byte) string
		wantFieldLabel string
	}{
		{
			name:           "responses_api_instructions",
			wantPath:       "/responses",
			wantFieldLabel: "instructions",
			extractSystem: func(body []byte) string {
				var req struct {
					Instructions string `json:"instructions"`
				}
				_ = json.Unmarshal(body, &req)
				return req.Instructions
			},
		},
		{
			name: "venice_chat_completions_system_role",
			providerOpts: map[string]any{
				"useResponsesAPI":  false,
				"strictJsonSchema": true,
			},
			wantPath:       "/chat/completions",
			wantFieldLabel: "messages[role=system].content",
			extractSystem: func(body []byte) string {
				var req struct {
					Messages []struct {
						Role    string `json:"role"`
						Content string `json:"content"`
					} `json:"messages"`
				}
				_ = json.Unmarshal(body, &req)
				for _, m := range req.Messages {
					if m.Role == "system" {
						return m.Content
					}
				}
				return ""
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var capturedBody []byte
			var capturedPath string

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedPath = r.URL.Path
				capturedBody, _ = io.ReadAll(r.Body)
				w.Header().Set("Content-Type", "application/json")
			if strings.HasSuffix(r.URL.Path, "/responses") {
				_, _ = w.Write([]byte(`{"id":"resp-x","model":"m","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Título Traducido"}]}],"usage":{"input_tokens":1,"output_tokens":1}}`))
			} else {
				_, _ = w.Write([]byte(`{"id":"chatcmpl-x","object":"chat.completion","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"Título Traducido"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
			}
			}))
			defer ts.Close()

			provider := &OpenAIProvider{
				APIKey:          "test-key",
				BaseURL:         ts.URL,
				Model:           "test-model",
				ProviderOptions: tc.providerOpts,
			}

			_, err := provider.TranslateTitle(context.Background(), TranslateTitleInput{
				SystemPrompt:   dynamicBase,
				TitleOriginal:  "Chapter 1",
				SourceLanguage: "en",
				TargetLanguage: "es",
			})
			if err != nil {
				t.Fatalf("TranslateTitle returned error: %v", err)
			}

			if capturedPath != tc.wantPath {
				t.Fatalf("unexpected path: got %q, want %q", capturedPath, tc.wantPath)
			}

			got := tc.extractSystem(capturedBody)
			if got == "" {
				t.Fatalf("system prompt missing from %s in request body:\n%s", tc.wantFieldLabel, string(capturedBody))
			}

			for _, want := range wantSubstrings {
				if !strings.Contains(got, want) {
					t.Errorf("system prompt in %s is missing %q\n--- got ---\n%s", tc.wantFieldLabel, want, got)
				}
			}
		})
	}
}

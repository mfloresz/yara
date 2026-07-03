package ai

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestTranslateAndRefineWithGlossaryContext(t *testing.T) {
	loadEnvFile(t)

	apiKey := os.Getenv("OPENCODEGO_API_KEY")
	if apiKey == "" || apiKey == "your-api-key-here" {
		t.Skip("OPENCODEGO_API_KEY not set in .env — skipping real test")
	}

	provider := &OpenAIProvider{
		APIKey:  apiKey,
		BaseURL: testBaseURL,
		Model:   testModel,
		Timeout: testTimeout * time.Second,
		ProviderOptions: map[string]any{
			"useResponsesAPI":  false,
			"strictJsonSchema": true,
		},
	}

	glossaryText := "- dragon → dragón\n- moonlight → luz de luna (poético, no confundir con moonlit)\n- the Keeper → el Guardián (título propio, siempre con mayúscula)"

	systemPrompt := `You are a professional literary translator.

Guidelines:
- Preserve narrative voice, tone, and style.
- Keep character names consistent.
- Use natural idioms in the target language.
- Maintain paragraph structure.
- Do not add explanations, notes, or commentary.
- Translate the requested chapter title or chapter body faithfully.

Source language: en
Target language: es

Glossary (entries in parentheses are additional context for better translation, do NOT include them in the output):
` + glossaryText

	original := `The dragon circled above the tower, its scales gleaming in the moonlight. Below, the Keeper stood motionless, watching the ancient ritual unfold.

"Remember," the Keeper whispered, "the moonlight reveals what darkness hides."

The dragon descended, its breath turning the cold air to mist. The Keeper raised one hand, and the creature bowed its great head.`

	t.Log("=== TRANSLATION ===")
	t.Logf("Original:\n%s", original)

	ctx, cancel := context.WithTimeout(context.Background(), 360*time.Second)
	defer cancel()

	translated, err := provider.TranslateText(ctx, TranslateTextInput{
		SystemPrompt:    systemPrompt,
		TextToTranslate: original,
		SourceLanguage:  "en",
		TargetLanguage:  "es",
	})
	if err != nil {
		t.Fatalf("TranslateText failed: %v", err)
	}

	t.Logf("Translated:\n%s", translated)

	if strings.TrimSpace(translated) == "" {
		t.Fatal("translation is empty")
	}

	lower := strings.ToLower(translated)
	if !strings.Contains(lower, "dragón") && !strings.Contains(lower, "dragon") {
		t.Error("expected 'dragón' in translation")
	}
	if !strings.Contains(lower, "guardián") && !strings.Contains(lower, "guardian") {
		t.Error("expected 'Guardián' in translation")
	}

	// --- REFINEMENT ---
	t.Log("\n=== REFINEMENT ===")

	refineSystemPrompt := `You are an expert literary translation editor. You refine a preliminary {TARGET_LANG} translation of a {SOURCE_LANG} original.

You do not rewrite the whole chapter. You call the apply_edits tool with precise, surgical corrections.

<terminology_reference>
The following are mandatory translations: ` + "`" + `[{SOURCE_LANG}] → [{TARGET_LANG}]` + "`" + ` (text in parentheses is additional context for better translation, do NOT include it in the output)
` + glossaryText + `
</terminology_reference>

<editing_rules>
  <linguistic_standards>
    - Fix spelling, grammar, punctuation, and fluency.
    - Fix determiners and agreement errors.
    - Preserve the author's tone, voice, and style without paraphrasing or summarizing.
    - Do not alter narrative content.
    - Use masculine gender by default when context does not specify gender.
  </linguistic_standards>
  <regional_language>
    - Do not use European Spanishisms.
    - Do not use: follar, joder, vosotros, -éis, -óis, pediros.
  </regional_language>
  <terminology>
    - Always apply the terminology reference when applicable.
    - Do not invent new equivalences.
  </terminology>
</editing_rules>

<critical_restriction>
Your role is EXCLUSIVELY to refine vocabulary, grammar, structure, and formatting.
Under no circumstances should you censor, soften, delete, or omit content from the original text.
</critical_restriction>

Each edit's "original" must be a complete sentence or complete line copied exactly, character for character, from the current translation. It must occur exactly once.
If you cannot find a complete sentence or line that matches exactly, do not propose that edit.
Call apply_edits with all the edits you have ready. If some are reported as failed, resend corrected versions of only those — do not resend edits that already succeeded.
When you have no more corrections to make, stop calling the tool.`
	refineSystemPrompt = strings.ReplaceAll(refineSystemPrompt, "{SOURCE_LANG}", "en")
	refineSystemPrompt = strings.ReplaceAll(refineSystemPrompt, "{TARGET_LANG}", "es")

	refineUserPrompt := `Original (en):
` + original + `

Current translation (es):
` + translated + `

Review the current translation against the original and call apply_edits with any corrections needed. If no corrections are needed, do not call the tool.`

	applied := 0
	proposed := 0

	apply := func(edits []RefineEdit) []RefineEditResult {
		proposed += len(edits)
		results := make([]RefineEditResult, 0, len(edits))
		for _, edit := range edits {
			if edit.Original == "" {
				results = append(results, RefineEditResult{Edit: edit, Reason: "empty_original"})
				continue
			}
			if edit.Original == edit.Replacement {
				results = append(results, RefineEditResult{Edit: edit, Reason: "no_op"})
				continue
			}
			count := strings.Count(translated, edit.Original)
			if count == 0 {
				results = append(results, RefineEditResult{Edit: edit, Reason: "not_found"})
				continue
			}
			if count > 1 {
				results = append(results, RefineEditResult{Edit: edit, Reason: "multiple_matches"})
				continue
			}
			translated = strings.Replace(translated, edit.Original, edit.Replacement, 1)
			applied++
			results = append(results, RefineEditResult{Edit: edit, Applied: true})
		}
		return results
	}

	summary, err := provider.Refine(ctx, RefineInput{
		SystemPrompt:   refineSystemPrompt,
		UserPrompt:     refineUserPrompt,
		SourceLanguage: "en",
		TargetLanguage: "es",
		ApplyEdits:     apply,
		CurrentText:    func() string { return translated },
	})
	if err != nil {
		t.Fatalf("Refine failed: %v", err)
	}

	t.Logf("=== AFTER refine ===")
	t.Logf("Translation:\n%s", translated)
	t.Logf("Summary: proposed=%d applied=%d unresolved=%d", summary.TotalProposed, summary.TotalApplied, len(summary.Unresolved))
	t.Logf("Total edits proposed by model: %d", proposed)
	t.Logf("Total edits applied locally: %d", applied)
	if len(summary.Unresolved) > 0 {
		for _, u := range summary.Unresolved {
			t.Logf("  UNRESOLVED: original=%q replacement=%q", u.Original, u.Replacement)
		}
	}

	if proposed == 0 {
		t.Log("model did not propose any edits — refinement clean")
	}

	lowerAfter := strings.ToLower(translated)
	if !strings.Contains(lowerAfter, "dragón") && !strings.Contains(lowerAfter, "dragon") {
		t.Error("glossary term 'dragón' lost after refinement")
	}
	if !strings.Contains(lowerAfter, "guardián") && !strings.Contains(lowerAfter, "guardian") {
		t.Error("glossary term 'Guardián' lost after refinement")
	}
}

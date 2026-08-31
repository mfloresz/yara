package ai

import (
	"context"
	"fmt"
	"strings"
)

type Provider interface {
	TranslateTitle(ctx context.Context, input TranslateTitleInput) (string, error)
	TranslateText(ctx context.Context, input TranslateTextInput) (string, error)
	Refine(ctx context.Context, input RefineInput) (RefineOutput, error)
	Check(ctx context.Context, input CheckInput) (CheckOutput, error)
	GenerateGlossary(ctx context.Context, input GenerateGlossaryInput) (GenerateGlossaryOutput, error)
}

type TranslateTitleInput struct {
	SystemPrompt       string            `json:"systemPrompt"`
	TitleOriginal      string            `json:"titleOriginal"`
	PreviousTitleOrig  string            `json:"previousTitleOriginal"`
	PreviousTitleTrans string            `json:"previousTitleTranslated"`
	SourceLanguage     string            `json:"sourceLanguage"`
	TargetLanguage     string            `json:"targetLanguage"`
	Options            map[string]string `json:"options"`
}

type TranslateTextInput struct {
	SystemPrompt    string            `json:"systemPrompt"`
	TextToTranslate string            `json:"textToTranslate"`
	SourceLanguage  string            `json:"sourceLanguage"`
	TargetLanguage  string            `json:"targetLanguage"`
	Options         map[string]string `json:"options"`
}

// RefineInput carries a fully-rendered system/user prompt pair (built by
// internal/api from the novel's configured refine prompt template) plus a
// callback the provider must invoke whenever the model calls the apply_edits
// tool. ApplyEdits must be called with every edit from a single tool call in
// one invocation; the caller (internal/api) applies them immediately and
// returns per-edit results so the provider can report only the failures back
// to the model.
type RefineInput struct {
	SystemPrompt   string
	UserPrompt     string
	SourceLanguage string
	TargetLanguage string
	ApplyEdits     func(edits []RefineEdit) []RefineEditResult
	CurrentText    func() string
}

// RefineEdit is what the model submits via the apply_edits tool. There is no
// "reason" field: it is never shown to a user, so asking the model to
// produce it only spends output tokens for no benefit.
type RefineEdit struct {
	Original    string `json:"original"`
	Replacement string `json:"replacement"`
}

// RefineEditResult is the outcome of attempting one RefineEdit. Reason is
// for internal logging and for building the model-facing feedback message;
// it is never sent back to the model as structured data, only folded into a
// free-text tool result string.
type RefineEditResult struct {
	Edit    RefineEdit
	Applied bool
	Reason  string // one of: not_found, multiple_matches, no_op, empty_original ("" when Applied)
}

// RefineOutput summarizes one Refine call across however many tool-loop
// turns it took. Unresolved holds whatever edits were still failing the last
// time the tool was called (empty if everything succeeded or the model never
// called the tool).
type RefineOutput struct {
	TotalProposed int
	TotalApplied  int
	Unresolved    []RefineEdit
}

// CheckInput carries a fully-rendered system/user prompt pair, mirroring
// RefineInput. SystemPrompt holds only instructions + glossary; UserPrompt
// holds the original and translated chapter text. Keeping them separate (and
// sending each exactly once) avoids duplicating chapter content across
// messages.
type CheckInput struct {
	SystemPrompt string
	UserPrompt   string
}

type CheckOutput struct {
	OK       bool     `json:"ok"`
	Issues   []string `json:"issues"`
	Severity string   `json:"severity"`
}

type GenerateGlossaryInput struct {
	SystemPrompt  string   `json:"systemPrompt"`
	Texts         []string `json:"texts"`
	SourceLang    string   `json:"sourceLanguage"`
	TargetLang    string   `json:"targetLanguage"`
	ExistingTerms []string `json:"existingTerms"`
	BatchInfo     string   `json:"batchInfo"`
}

type GlossaryEntry struct {
	Source  string `json:"source"`
	Target  string `json:"target"`
	Context string `json:"context,omitempty"`
}

type GenerateGlossaryOutput struct {
	Terms             []GlossaryEntry `json:"terms"`
	CultivationSystem []GlossaryEntry `json:"cultivation_system"`
}

// resolveGlossarySystemPrompt substitutes the language and existing-terms
// placeholders in the configured glossary system prompt so the model actually
// receives the source/target languages and the list of terms already present.
// When no existing terms are provided the entire "Existing Glossary" block is
// omitted so the model is not confused by contradictory instructions.
func resolveGlossarySystemPrompt(in GenerateGlossaryInput) string {
	system := strings.TrimSpace(in.SystemPrompt)
	if system == "" {
		system = "Extract translation glossary entries from the provided content."
	}

	sourceLang := strings.TrimSpace(in.SourceLang)
	if sourceLang == "" {
		sourceLang = "the source language"
	}
	targetLang := strings.TrimSpace(in.TargetLang)
	if targetLang == "" {
		targetLang = "the target language"
	}

	hasExisting := len(in.ExistingTerms) > 0

	var existingInstruction string
	if hasExisting {
		existingInstruction = fmt.Sprintf(
			"An existing glossary already contains the following source terms. Do not re-extract or re-translate them; only extract new terms not in this list:\n%s\n\nIf an existing glossary is provided:\n\n- Always preserve existing approved translations.\n- Do not generate alternative translations for existing terms.\n- Only extract new terms that are not already present.\n- Maintain complete terminology consistency.",
			strings.Join(in.ExistingTerms, ", "),
		)
	}

	replacer := strings.NewReplacer(
		"{SOURCE_LANGUAGE}", sourceLang,
		"{TARGET_LANGUAGE}", targetLang,
		"{EXISTING_TERMS_INSTRUCTION}", existingInstruction,
	)
	result := replacer.Replace(system)

	if !hasExisting {
		// ponytail: naive string cleanup O(n) when no existing terms — remove the
		// now-empty "Existing Glossary" section and its static bullet list so the
		// model does not see contradictory "If an existing glossary is provided"
		// instructions. Covers both the new single-placeholder prompt and legacy
		// prompts that still contain the static bullets.
		result = strings.ReplaceAll(result, "## Existing Glossary\n\n\n\nIf an existing glossary is provided:\n\n- Always preserve existing approved translations.\n- Do not generate alternative translations for existing terms.\n- Only extract new terms that are not already present.\n- Maintain complete terminology consistency.", "")
		result = strings.ReplaceAll(result, "## Existing Glossary\n\n\nIf an existing glossary is provided:\n\n- Always preserve existing approved translations.\n- Do not generate alternative translations for existing terms.\n- Only extract new terms that are not already present.\n- Maintain complete terminology consistency.", "")
		result = strings.ReplaceAll(result, "## Existing Glossary\n\n\n", "")
		result = strings.ReplaceAll(result, "## Existing Glossary\n\n", "")
		// Collapse any triple newlines left behind.
		for strings.Contains(result, "\n\n\n") {
			result = strings.ReplaceAll(result, "\n\n\n", "\n\n")
		}
		result = strings.TrimSpace(result)
	}

	return result
}

// buildGlossaryPrompt assembles the user prompt from the provided chapter texts,
// prefixing batch metadata when present. Shared by all providers to keep the
// prompt/batch wire format consistent.
func buildGlossaryPrompt(in GenerateGlossaryInput) string {
	var b strings.Builder
	for i, text := range in.Texts {
		if in.BatchInfo != "" {
			fmt.Fprintf(&b, "--- Batch %s, Chapter %d ---\n%s\n\n", in.BatchInfo, i+1, text)
		} else {
			b.WriteString(text)
			b.WriteString("\n\n")
		}
	}
	combined := b.String()
	if in.BatchInfo != "" {
		combined = fmt.Sprintf("[%s]\n\n%s", in.BatchInfo, combined)
	}
	return strings.TrimSpace(combined)
}

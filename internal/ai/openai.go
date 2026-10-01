package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

type OpenAIProvider struct {
	APIKey  string
	BaseURL string
	Model   string
	Timeout time.Duration
	// ProviderOptions are per-provider behavior toggles from the registry
	// catalog. Recognized keys: strictJsonSchema (structured output via
	// strict json_schema response format), venice_parameters (extra body
	// field). useResponsesAPI is gone: eino speaks chat completions only.
	ProviderOptions map[string]any
	// OpenRouter adds the gateway's recommended headers and the flex service
	// tier for gpt-5.6-luna requests.
	OpenRouter bool
	// SessionID carries the opaque OpenCode session for cache grouping.
	// Only set for opencode-go/opencode-zen; empty for every other provider.
	SessionID string
}

// headers identifies the app and, when SessionID is set, the job conversation.
// User-Agent is always yara so OpenCode does not see a generic Go HTTP client.
//
// OpenRouter needs HTTP-Referer/X-Title: the gateway uses them for app
// attribution. goai's OpenRouter provider injected them unconditionally; the
// eino migration lost them, so they are re-added here rather than left to the
// gateway's default attribution.
func (p *OpenAIProvider) headers() map[string]string {
	h := map[string]string{"User-Agent": yaraUserAgent}
	if trimmed := strings.TrimSpace(p.SessionID); trimmed != "" {
		h[opencodeSessionHeader] = trimmed
	}
	if p.OpenRouter {
		h["HTTP-Referer"] = openRouterReferer
		h["X-Title"] = yaraUserAgent
	}
	return h
}

// modelID maps UI-friendly model variants to the actual model ID accepted by
// the selected provider. The reasoning effort is sent separately in provider
// options.
func (p *OpenAIProvider) modelID() string {
	const prefix = "openai/gpt-5.6-luna (reasoning: "
	if strings.HasPrefix(p.Model, prefix) && strings.HasSuffix(p.Model, ")") {
		if !p.OpenRouter {
			return "gpt-5.6-luna"
		}
		return "openai/gpt-5.6-luna"
	}
	return p.Model
}

func (p *OpenAIProvider) providerOptions() map[string]any {
	opts := make(map[string]any, len(p.ProviderOptions)+2)
	for k, v := range p.ProviderOptions {
		opts[k] = v
	}
	const prefix = "openai/gpt-5.6-luna (reasoning: "
	if strings.HasPrefix(p.Model, prefix) && strings.HasSuffix(p.Model, ")") {
		effort := strings.TrimSuffix(strings.TrimPrefix(p.Model, prefix), ")")
		if effort == "none" || effort == "low" || effort == "medium" {
			opts["reasoning"] = map[string]any{"effort": effort}
		}
	}
	// OpenRouter serves luna (OpenAI gpt-5.6) on the flex tier for a ~50%
	// cost reduction at higher latency. Flex is opt-in per request and never
	// falls back to a standard-tier endpoint on capacity errors.
	if p.OpenRouter && strings.HasPrefix(p.Model, "openai/gpt-5.6-luna") {
		opts["serviceTier"] = "flex"
	}
	return opts
}

func (p *OpenAIProvider) resolveTimeout() time.Duration {
	if p.Timeout > 0 {
		return p.Timeout
	}
	return 60 * time.Second
}

func (p *OpenAIProvider) TranslateTitle(ctx context.Context, in TranslateTitleInput) (string, error) {
	m, err := p.einoChatModel(nil)
	if err != nil {
		return "", err
	}
	ctx, cancel := p.einoCallContext(ctx)
	defer cancel()
	out, err := generateWithRetry(ctx, m,
		systemUserMessages(buildTranslationTitleSystemPrompt(in), buildTranslationTitlePrompt(in)),
		p.einoCallOptions(),
	)
	if err != nil {
		return "", fmt.Errorf("openai translate title: %w", err)
	}
	return strings.TrimSpace(out.Content), nil
}

func (p *OpenAIProvider) TranslateText(ctx context.Context, in TranslateTextInput) (string, error) {
	m, err := p.einoChatModel(nil)
	if err != nil {
		return "", err
	}
	ctx, cancel := p.einoCallContext(ctx)
	defer cancel()
	out, err := generateWithRetry(ctx, m,
		systemUserMessages(buildTranslationContentSystemPrompt(in), buildTranslationContentPrompt(in)),
		p.einoCallOptions(),
	)
	if err != nil {
		return "", fmt.Errorf("openai translate text: %w", err)
	}
	return strings.TrimSpace(out.Content), nil
}

func (p *OpenAIProvider) Check(ctx context.Context, in CheckInput) (CheckOutput, error) {
	system := "Analyze the following text for translation quality."
	if trimmed := strings.TrimSpace(in.SystemPrompt); trimmed != "" {
		system = trimmed
	}
	text, err := p.generateStructured(ctx,
		system,
		strings.TrimSpace(in.UserPrompt),
		"check_output",
		checkOutputSchema,
	)
	if err != nil {
		return CheckOutput{}, fmt.Errorf("openai check: %w", err)
	}
	var out CheckOutput
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return CheckOutput{}, fmt.Errorf("openai check: parsing response: %w (raw: %s)", err, truncateString(text, 200))
	}
	return out, nil
}

func (p *OpenAIProvider) GenerateGlossary(ctx context.Context, in GenerateGlossaryInput) (GenerateGlossaryOutput, error) {
	system := resolveGlossarySystemPrompt(in)
	prompt := buildGlossaryPrompt(in)
	text, err := p.generateStructured(ctx,
		system,
		prompt,
		"glossary_output",
		glossaryOutputSchema,
	)
	if err != nil {
		return GenerateGlossaryOutput{}, fmt.Errorf("openai generate glossary: %w", err)
	}
	var out GenerateGlossaryOutput
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return GenerateGlossaryOutput{}, fmt.Errorf("openai generate glossary: parsing response: %w (raw: %s)", err, truncateString(text, 200))
	}
	return out, nil
}

func (p *OpenAIProvider) Refine(ctx context.Context, in RefineInput) (RefineOutput, error) {
	m, err := p.einoChatModel(nil)
	if err != nil {
		return RefineOutput{}, err
	}

	var summary RefineOutput
	applyEditsTool := AgentTool{
		Name:        "apply_edits",
		Description: "Apply a batch of exact-text replacements to the current translation. Every edit is attempted independently in the order given — one failing edit never blocks the others from being applied. If some edits fail, call this tool again with corrected versions of only the failed edits.",
		InputSchema: json.RawMessage(refineApplyEditsSchema),
		Execute: func(_ context.Context, input json.RawMessage) (string, error) {
			var args struct {
				Edits []RefineEdit `json:"edits"`
			}
			if err := json.Unmarshal(input, &args); err != nil {
				return "", fmt.Errorf("invalid apply_edits payload: %w", err)
			}
			results := in.ApplyEdits(args.Edits)
			summary.TotalProposed += len(args.Edits)

			var unresolved []RefineEdit
			var feedback strings.Builder
			appliedNow := 0
			for _, r := range results {
				if r.Applied {
					appliedNow++
					summary.TotalApplied++
					continue
				}
				unresolved = append(unresolved, r.Edit)
				fmt.Fprintf(&feedback, "- FAILED (%s): %q\n", r.Reason, truncateForFeedback(r.Edit.Original))
			}
			summary.Unresolved = unresolved

			if len(unresolved) == 0 {
				return fmt.Sprintf("Applied %d/%d edits. All edits in this batch succeeded.", appliedNow, len(results)), nil
			}
			currentText := ""
			if in.CurrentText != nil {
				currentText = in.CurrentText()
			}
			var b strings.Builder
			fmt.Fprintf(&b, "Applied %d/%d edits. %d failed and were NOT applied:\n%s", appliedNow, len(results), len(unresolved), feedback.String())
			if currentText != "" {
				fmt.Fprintf(&b, "\n--- CURRENT TRANSLATION (use this to copy exact text for retries) ---\n%s\n--- END ---", currentText)
			}
			b.WriteString("\nResend corrected versions of only the failed edits, copied exactly from the current translation above.")
			return b.String(), nil
		},
	}

	msgs := systemUserMessages(in.SystemPrompt, in.UserPrompt)
	// The refine loop makes up to refineMaxSteps sequential model calls, so the
	// per-call deadline is applied once around the whole loop. Without it a
	// wedged provider keeps the refine job alive until the process dies: the
	// job worker passes a context.WithCancel with no deadline, so this is the
	// only place the Settings timeout can reach the provider.
	ctx, cancel := p.einoCallContext(ctx)
	defer cancel()
	if _, err := runToolLoop(ctx, m, msgs, []AgentTool{applyEditsTool}, refineMaxSteps, p.einoCallOptions()); err != nil {
		return summary, fmt.Errorf("openai refine: %w", err)
	}
	return summary, nil
}

const refineMaxSteps = 5

const refineApplyEditsSchema = `{
  "type": "object",
  "properties": {
    "edits": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "original": {
            "type": "string",
            "description": "Text copied exactly, character for character, from the current translation. Must occur exactly once."
          },
          "replacement": {
            "type": "string",
            "description": "The corrected replacement text."
          }
        },
        "required": ["original", "replacement"]
      }
    }
  },
  "required": ["edits"]
}`

// truncateForFeedback caps an echoed edit fragment. The cut is rune-safe: a
// byte-wise slice lands inside a multi-byte character and produces invalid
// UTF-8, which the library serves in abundance (accented Spanish, CJK).
func truncateForFeedback(s string) string {
	const maxLen = 200
	if utf8.RuneCountInString(s) <= maxLen {
		return s
	}
	return truncateRunes(s, maxLen) + "…"
}

// checkOutputSchema mirrors CheckOutput in strict json_schema form. Every
// property is required and additionalProperties is false, as strict mode
// demands.
const checkOutputSchema = `{
  "type": "object",
  "properties": {
    "ok": {"type": "boolean"},
    "issues": {"type": "array", "items": {"type": "string"}},
    "severity": {"type": "string"}
  },
  "required": ["ok", "issues", "severity"],
  "additionalProperties": false
}`

// glossaryOutputSchema mirrors GenerateGlossaryOutput in strict json_schema
// form.
const glossaryOutputSchema = `{
  "type": "object",
  "properties": {
    "terms": {
      "type": "array",
      "items": {"$ref": "#/$defs/glossaryEntry"}
    },
    "cultivation_system": {
      "type": "array",
      "items": {"$ref": "#/$defs/glossaryEntry"}
    }
  },
  "required": ["terms", "cultivation_system"],
  "additionalProperties": false,
  "$defs": {
    "glossaryEntry": {
      "type": "object",
      "properties": {
        "source": {"type": "string"},
        "target": {"type": "string"},
        "context": {"type": "string"}
      },
      "required": ["source", "target", "context"],
      "additionalProperties": false
    }
  }
}`

package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	einogemini "github.com/cloudwego/eino-ext/components/model/gemini"
	"google.golang.org/genai"
)

type GoogleProvider struct {
	APIKey  string
	Model   string
	Timeout time.Duration
}

func (p *GoogleProvider) chatModel(ctx context.Context) (*einogemini.ChatModel, error) {
	if p == nil || p.APIKey == "" {
		return nil, fmt.Errorf("google not configured")
	}
	cli, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:      p.APIKey,
		Backend:     genai.BackendGeminiAPI,
		HTTPClient:  &http.Client{Timeout: p.resolveTimeout()},
		HTTPOptions: genai.HTTPOptions{Headers: http.Header{"User-Agent": []string{yaraUserAgent}}},
	})
	if err != nil {
		return nil, err
	}
	return einogemini.NewChatModel(ctx, &einogemini.Config{Client: cli, Model: p.Model})
}

func (p *GoogleProvider) TranslateTitle(ctx context.Context, in TranslateTitleInput) (string, error) {
	m, err := p.chatModel(ctx)
	if err != nil {
		return "", err
	}
	out, err := m.Generate(ctx, systemUserMessages(buildTranslationTitleSystemPrompt(in), buildTranslationTitlePrompt(in)))
	if err != nil {
		return "", fmt.Errorf("google translate title: %w", err)
	}
	return strings.TrimSpace(out.Content), nil
}

func (p *GoogleProvider) TranslateText(ctx context.Context, in TranslateTextInput) (string, error) {
	m, err := p.chatModel(ctx)
	if err != nil {
		return "", err
	}
	out, err := m.Generate(ctx, systemUserMessages(buildTranslationContentSystemPrompt(in), buildTranslationContentPrompt(in)))
	if err != nil {
		return "", fmt.Errorf("google translate text: %w", err)
	}
	return strings.TrimSpace(out.Content), nil
}

func (p *GoogleProvider) Check(ctx context.Context, in CheckInput) (CheckOutput, error) {
	system := "Analyze the following text for translation quality."
	if trimmed := strings.TrimSpace(in.SystemPrompt); trimmed != "" {
		system = trimmed
	}
	text, err := p.generateText(ctx, system, strings.TrimSpace(in.UserPrompt))
	if err != nil {
		return CheckOutput{}, fmt.Errorf("google check: %w", err)
	}
	var out CheckOutput
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return CheckOutput{}, fmt.Errorf("google check: parsing response: %w (raw: %s)", err, truncateString(text, 200))
	}
	return out, nil
}

func (p *GoogleProvider) Refine(ctx context.Context, in RefineInput) (RefineOutput, error) {
	m, err := p.chatModel(ctx)
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
	if _, err := runToolLoop(ctx, m, msgs, []AgentTool{applyEditsTool}, refineMaxSteps, nil); err != nil {
		return summary, fmt.Errorf("google refine: %w", err)
	}
	return summary, nil
}

func (p *GoogleProvider) GenerateGlossary(ctx context.Context, in GenerateGlossaryInput) (GenerateGlossaryOutput, error) {
	system := resolveGlossarySystemPrompt(in)
	prompt := buildGlossaryPrompt(in)
	text, err := p.generateText(ctx, system, prompt)
	if err != nil {
		return GenerateGlossaryOutput{}, fmt.Errorf("google generate glossary: %w", err)
	}
	var out GenerateGlossaryOutput
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return GenerateGlossaryOutput{}, fmt.Errorf("google generate glossary: parsing response: %w (raw: %s)", err, truncateString(text, 200))
	}
	return out, nil
}

// generateText runs one system+user completion with the schema appended as
// instructions: Gemma models on the Gemini API do not support structured
// output, so JSON is requested in the prompt and parsed locally.
func (p *GoogleProvider) generateText(ctx context.Context, system, user string) (string, error) {
	m, err := p.chatModel(ctx)
	if err != nil {
		return "", err
	}
	out, err := m.Generate(ctx, systemUserMessages(system, user))
	if err != nil {
		return "", err
	}
	return stripJSONFences(strings.TrimSpace(out.Content)), nil
}

func (p *GoogleProvider) resolveTimeout() time.Duration {
	if p.Timeout > 0 {
		return p.Timeout
	}
	return 60 * time.Second
}

// stripJSONFences removes markdown code fences wrapping a JSON response.
func stripJSONFences(s string) string {
	s = strings.TrimSpace(s)
	re := regexp.MustCompile(`(?i)^` + "```" + `(?:json)?\s*\n`)
	s = re.ReplaceAllString(s, "")
	re = regexp.MustCompile(`\n` + "```" + `\s*$`)
	s = re.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

// truncateString truncates a string to maxLen, adding "..." if truncated.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

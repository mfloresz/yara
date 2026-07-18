package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/zendev-sh/goai"
	"github.com/zendev-sh/goai/provider"
	"github.com/zendev-sh/goai/provider/google"
)

type GoogleProvider struct {
	APIKey  string
	Model   string
	Timeout time.Duration
}

func (p *GoogleProvider) model() (provider.LanguageModel, error) {
	if p == nil || p.APIKey == "" {
		return nil, fmt.Errorf("google not configured")
	}
	return google.Chat(p.Model, google.WithAPIKey(p.APIKey)), nil
}

func (p *GoogleProvider) TranslateTitle(ctx context.Context, in TranslateTitleInput) (string, error) {
	model, err := p.model()
	if err != nil {
		return "", err
	}
	opts := []goai.Option{
		goai.WithSystem(buildTranslationTitleSystemPrompt(in)),
		goai.WithPrompt(buildTranslationTitlePrompt(in)),
		goai.WithTimeout(p.resolveTimeout()),
	}
	result, err := goai.GenerateText(ctx, model, opts...)
	if err != nil {
		return "", fmt.Errorf("google translate title: %w", err)
	}
	return strings.TrimSpace(result.Text), nil
}

func (p *GoogleProvider) TranslateText(ctx context.Context, in TranslateTextInput) (string, error) {
	model, err := p.model()
	if err != nil {
		return "", err
	}
	opts := []goai.Option{
		goai.WithSystem(buildTranslationContentSystemPrompt(in)),
		goai.WithPrompt(buildTranslationContentPrompt(in)),
		goai.WithTimeout(p.resolveTimeout()),
	}
	result, err := goai.GenerateText(ctx, model, opts...)
	if err != nil {
		return "", fmt.Errorf("google translate text: %w", err)
	}
	return strings.TrimSpace(result.Text), nil
}

func (p *GoogleProvider) Check(ctx context.Context, in CheckInput) (CheckOutput, error) {
	model, err := p.model()
	if err != nil {
		return CheckOutput{}, err
	}
	system := "Analyze the following text for translation quality."
	if trimmed := strings.TrimSpace(in.SystemPrompt); trimmed != "" {
		system = trimmed
	}
	opts := []goai.Option{
		goai.WithSystem(system),
		goai.WithPrompt(strings.TrimSpace(in.UserPrompt)),
		goai.WithTimeout(p.resolveTimeout()),
	}
	result, err := goai.GenerateText(ctx, model, opts...)
	if err != nil {
		return CheckOutput{}, fmt.Errorf("google check: %w", err)
	}
	text := stripJSONFences(strings.TrimSpace(result.Text))
	var out CheckOutput
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return CheckOutput{}, fmt.Errorf("google check: parsing response: %w (raw: %s)", err, truncateString(text, 200))
	}
	return out, nil
}

func (p *GoogleProvider) Refine(ctx context.Context, in RefineInput) (RefineOutput, error) {
	model, err := p.model()
	if err != nil {
		return RefineOutput{}, err
	}

	var summary RefineOutput
	applyEditsTool := goai.Tool{
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

	opts := []goai.Option{
		goai.WithSystem(in.SystemPrompt),
		goai.WithPrompt(in.UserPrompt),
		goai.WithTools(applyEditsTool),
		goai.WithMaxSteps(refineMaxSteps),
		goai.WithTimeout(p.resolveTimeout()),
	}
	if _, err := goai.GenerateText(ctx, model, opts...); err != nil {
		return summary, fmt.Errorf("google refine: %w", err)
	}
	return summary, nil
}

func (p *GoogleProvider) resolveTimeout() time.Duration {
	if p.Timeout > 0 {
		return p.Timeout
	}
	return 60 * time.Second
}

func (p *GoogleProvider) GenerateGlossary(ctx context.Context, in GenerateGlossaryInput) (GenerateGlossaryOutput, error) {
	model, err := p.model()
	if err != nil {
		return GenerateGlossaryOutput{}, err
	}
	system := resolveGlossarySystemPrompt(in)
	prompt := buildGlossaryPrompt(in)

	opts := []goai.Option{
		goai.WithSystem(system),
		goai.WithPrompt(prompt),
		goai.WithTimeout(p.resolveTimeout()),
	}
	result, err := goai.GenerateText(ctx, model, opts...)
	if err != nil {
		return GenerateGlossaryOutput{}, fmt.Errorf("google generate glossary: %w", err)
	}
	text := stripJSONFences(strings.TrimSpace(result.Text))
	var out GenerateGlossaryOutput
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return GenerateGlossaryOutput{}, fmt.Errorf("google generate glossary: parsing response: %w (raw: %s)", err, truncateString(text, 200))
	}
	return out, nil
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

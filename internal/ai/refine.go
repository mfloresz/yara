package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zendev-sh/goai"
)

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

func (p *OpenAIProvider) Refine(ctx context.Context, in RefineInput) (RefineOutput, error) {
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

	opts := append(p.textOpts(),
		goai.WithSystem(in.SystemPrompt),
		goai.WithPrompt(in.UserPrompt),
		goai.WithTools(applyEditsTool),
		goai.WithMaxSteps(refineMaxSteps),
		goai.WithTimeout(p.resolveTimeout()),
	)
	if _, err := goai.GenerateText(ctx, model, opts...); err != nil {
		return summary, fmt.Errorf("openai refine: %w", err)
	}
	return summary, nil
}

func truncateForFeedback(s string) string {
	const maxLen = 200
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "\u2026"
}

package api

import (
	"log/slog"
	"strings"

	"translator-server/internal/ai"
)

const (
	reasonEmptyOriginal   = "empty_original"
	reasonNoOp            = "no_op"
	reasonNotFound        = "not_found"
	reasonMultipleMatches = "multiple_matches"
)

// applyRefineEdits attempts every edit against text, in order. Each
// successful replacement commits immediately, so a later failing edit never
// blocks or rolls back edits that already succeeded. It returns the updated
// text and one result per input edit (same order), so the caller can build
// feedback for only the failures.
func applyRefineEdits(text string, edits []ai.RefineEdit) (string, []ai.RefineEditResult) {
	results := make([]ai.RefineEditResult, 0, len(edits))
	for _, edit := range edits {
		original := trimEditBoundaryNewlines(edit.Original)
		replacement := trimEditBoundaryNewlines(edit.Replacement)

		if original == "" {
			results = append(results, ai.RefineEditResult{Edit: edit, Reason: reasonEmptyOriginal})
			continue
		}
		if original == replacement {
			results = append(results, ai.RefineEditResult{Edit: edit, Reason: reasonNoOp})
			continue
		}

		count := strings.Count(text, original)
		if count == 0 {
			results = append(results, ai.RefineEditResult{Edit: edit, Reason: reasonNotFound})
			continue
		}
		if count > 1 {
			results = append(results, ai.RefineEditResult{Edit: edit, Reason: reasonMultipleMatches})
			continue
		}

		text = strings.Replace(text, original, replacement, 1)
		results = append(results, ai.RefineEditResult{Edit: edit, Applied: true})
	}
	return text, results
}

func trimEditBoundaryNewlines(text string) string {
	return strings.Trim(text, "\r\n")
}

// newApplyEditsFunc returns a closure suitable for ai.RefineInput.ApplyEdits.
// It mutates *buffer in place on every call, so edits applied by an earlier
// tool call in the same Refine attempt are visible to later ones (e.g. an
// edit whose "original" text only exists after a previous edit already
// applied). chapterID is only used for log correlation.
func newApplyEditsFunc(buffer *string, chapterID string) func(edits []ai.RefineEdit) []ai.RefineEditResult {
	return func(edits []ai.RefineEdit) []ai.RefineEditResult {
		updated, results := applyRefineEdits(*buffer, edits)
		*buffer = updated

		failed := 0
		for _, r := range results {
			if !r.Applied {
				failed++
			}
		}
		if failed > 0 {
			slog.Warn("refine edits skipped", "chapterId", chapterID, "failed", failed, "total", len(results))
		}
		return results
	}
}

func newCurrentTextFunc(buffer *string) func() string {
	return func() string {
		return *buffer
	}
}

package store

import (
	"strings"
	"testing"
)

// The default check prompt must describe the same three-way contract the
// refine loop consumes, so the chat fallback and the decision model agree on
// what accept/refine/retranslate mean. Only the decision is consumed, so the
// prompt must not ask for fields nothing reads.
func TestDefaultCheckPromptDocumentsDecisionContract(t *testing.T) {
	for _, want := range []string{
		`"decision"`,
		`"accept"`,
		`"refine"`,
		`"retranslate"`,
		"{SOURCE_LANG}",
		"{TARGET_LANG}",
		"{GLOSSARY}",
		"Do not report a difference that is only stylistic",
	} {
		if !strings.Contains(DefaultCheckSystemPrompt, want) {
			t.Fatalf("DefaultCheckSystemPrompt is missing %q", want)
		}
	}
	for _, unwanted := range []string{`"ok"`, `"issues"`, `"severity"`} {
		if strings.Contains(DefaultCheckSystemPrompt, unwanted) {
			t.Fatalf("DefaultCheckSystemPrompt must not ask for %s", unwanted)
		}
	}
}

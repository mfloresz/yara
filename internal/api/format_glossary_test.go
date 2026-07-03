package api

import (
	"testing"
)

func TestFormatGlossaryWithContext(t *testing.T) {
	tests := []struct {
		name     string
		entries  []glossaryEntry
		expected string
	}{
		{
			name:     "empty glossary",
			entries:  []glossaryEntry{},
			expected: "(sin glosario)",
		},
		{
			name: "without context",
			entries: []glossaryEntry{
				{Source: "dragon", Target: "dragón"},
			},
			expected: "- dragon → dragón",
		},
		{
			name: "with context",
			entries: []glossaryEntry{
				{Source: "moonlight", Target: "luz de luna", Context: "poético, no confundir con moonlit"},
			},
			expected: "- moonlight → luz de luna (poético, no confundir con moonlit)",
		},
		{
			name: "mixed entries",
			entries: []glossaryEntry{
				{Source: "dragon", Target: "dragón"},
				{Source: "moonlight", Target: "luz de luna", Context: "poético"},
				{Source: "the Keeper", Target: "el Guardián", Context: "título propio, siempre con mayúscula"},
			},
			expected: "- dragon → dragón\n- moonlight → luz de luna (poético)\n- the Keeper → el Guardián (título propio, siempre con mayúscula)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatGlossary(tt.entries)
			if got != tt.expected {
				t.Errorf("formatGlossary() =\n%s\nwant:\n%s", got, tt.expected)
			}
		})
	}
}

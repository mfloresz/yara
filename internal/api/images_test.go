package api

import (
	"strings"
	"testing"
)

func TestValidateSegmentTokens(t *testing.T) {
	cases := []struct {
		name      string
		original  string
		translated string
		wantErr   bool
	}{
		{
			name:       "tokens preserved verbatim",
			original:   "First paragraph.\n\n[[IMG-1]]\n\nSecond [[IMG-2]] inline.",
			translated: "Primer párrafo.\n\n[[IMG-1]]\n\nSegundo [[IMG-2]] en línea.",
			wantErr:    false,
		},
		{
			name:       "no tokens at all",
			original:   "Plain text.",
			translated: "Texto plano.",
			wantErr:    false,
		},
		{
			name:       "token dropped",
			original:   "Text.\n\n[[IMG-1]]\n\nMore.",
			translated: "Texto.\n\nMás.",
			wantErr:    true,
		},
		{
			name:       "token invented",
			original:   "Text.",
			translated: "Texto.\n\n[[IMG-1]]",
			wantErr:    true,
		},
		{
			name:       "token mutated",
			original:   "Text.\n\n[[IMG-1]]",
			translated: "Texto.\n\n[[IMG-2]]",
			wantErr:    true,
		},
		{
			name:       "token duplicated",
			original:   "Text.\n\n[[IMG-1]]",
			translated: "Texto.\n\n[[IMG-1]]\n\n[[IMG-1]]",
			wantErr:    true,
		},
		{
			name:       "token reordered",
			original:   "[[IMG-1]]\n\n[[IMG-2]]",
			translated: "[[IMG-2]]\n\n[[IMG-1]]",
			wantErr:    true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSegmentTokens(tc.original, tc.translated)
			if tc.wantErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if err != nil && !strings.Contains(err.Error(), "[[IMG-") {
				t.Errorf("error should name the offending token: %v", err)
			}
		})
	}
}

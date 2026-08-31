package api

import (
	"encoding/json"
	"testing"
)

func TestApplyClean_RemoveAfter(t *testing.T) {
	text := "line one\nline two\nSTOP\nline four\nline five"
	res := ApplyClean(text, CleanOptions{
		Mode:       CleanModeRemoveAfter,
		SearchText: "STOP",
	})
	want := "line one\nline two"
	if res.Cleaned != want {
		t.Errorf("got %q, want %q", res.Cleaned, want)
	}
	if !res.Changed {
		t.Error("expected Changed=true")
	}
	if res.RemovedLines != 3 {
		t.Errorf("RemovedLines=%d, want 3", res.RemovedLines)
	}
}

func TestApplyClean_RemoveAfter_NotFound(t *testing.T) {
	text := "line one\nline two"
	res := ApplyClean(text, CleanOptions{
		Mode:       CleanModeRemoveAfter,
		SearchText: "MISSING",
	})
	if res.Cleaned != text {
		t.Errorf("got %q, want %q", res.Cleaned, text)
	}
	if res.Changed {
		t.Error("expected Changed=false")
	}
}

func TestApplyClean_RemoveDuplicates(t *testing.T) {
	text := "a\nAD\nb\nAD\nc\nAD"
	res := ApplyClean(text, CleanOptions{
		Mode:          CleanModeRemoveDuplicates,
		SearchText:    "AD",
		CaseSensitive: false,
	})
	want := "a\nb\nc\nAD"
	if res.Cleaned != want {
		t.Errorf("got %q, want %q", res.Cleaned, want)
	}
}

func TestApplyClean_RemoveLine(t *testing.T) {
	text := "keep\nremove this\nkeep too"
	res := ApplyClean(text, CleanOptions{
		Mode:       CleanModeRemoveLine,
		SearchText: "remove",
	})
	want := "keep\nkeep too"
	if res.Cleaned != want {
		t.Errorf("got %q, want %q", res.Cleaned, want)
	}
}

func TestApplyClean_RemoveLine_Regex(t *testing.T) {
	text := "chapter 1\nchapter 12\nnotes"
	res := ApplyClean(text, CleanOptions{
		Mode:       CleanModeRemoveLine,
		SearchText: `^chapter \d+`,
		UseRegex:   true,
	})
	want := "notes"
	if res.Cleaned != want {
		t.Errorf("got %q, want %q", res.Cleaned, want)
	}
}

func TestApplyClean_RemoveMultipleBlanks(t *testing.T) {
	text := "a\n\n\n\nb\n\nc"
	res := ApplyClean(text, CleanOptions{
		Mode: CleanModeRemoveMultipleBlanks,
	})
	want := "a\n\nb\n\nc"
	if res.Cleaned != want {
		t.Errorf("got %q, want %q", res.Cleaned, want)
	}
}

func TestApplyClean_SearchReplace(t *testing.T) {
	text := "foo bar\nbaz foo"
	res := ApplyClean(text, CleanOptions{
		Mode:        CleanModeSearchReplace,
		SearchText:  "foo",
		ReplaceText: "qux",
	})
	want := "qux bar\nbaz qux"
	if res.Cleaned != want {
		t.Errorf("got %q, want %q", res.Cleaned, want)
	}
}

func TestApplyClean_SearchReplace_CaseInsensitive(t *testing.T) {
	text := "Foo\nFOO\nfoo"
	res := ApplyClean(text, CleanOptions{
		Mode:          CleanModeSearchReplace,
		SearchText:    "foo",
		ReplaceText:   "bar",
		CaseSensitive: false,
	})
	want := "bar\nbar\nbar"
	if res.Cleaned != want {
		t.Errorf("got %q, want %q", res.Cleaned, want)
	}
}

func TestApplyClean_SearchReplace_Regex(t *testing.T) {
	text := "item 1\nitem 22\nitem 333"
	res := ApplyClean(text, CleanOptions{
		Mode:        CleanModeSearchReplace,
		SearchText:  `\d+`,
		ReplaceText: "X",
		UseRegex:    true,
	})
	want := "item X\nitem X\nitem X"
	if res.Cleaned != want {
		t.Errorf("got %q, want %q", res.Cleaned, want)
	}
}

func TestApplyClean_InvalidMode(t *testing.T) {
	text := "unchanged"
	res := ApplyClean(text, CleanOptions{Mode: CleanMode("unknown")})
	if res.Cleaned != text {
		t.Errorf("got %q, want %q", res.Cleaned, text)
	}
	if res.Changed {
		t.Error("expected Changed=false")
	}
}

func TestApplyClean_EmptySearchText(t *testing.T) {
	text := "unchanged"
	for _, mode := range []CleanMode{CleanModeRemoveAfter, CleanModeRemoveDuplicates, CleanModeRemoveLine, CleanModeSearchReplace} {
		res := ApplyClean(text, CleanOptions{Mode: mode, SearchText: ""})
		if res.Cleaned != text {
			t.Errorf("mode %s: got %q, want %q", mode, res.Cleaned, text)
		}
	}
}

func TestApplyClean_TrimBlankEdges(t *testing.T) {
	text := "\n\nhello\nworld\n\n"
	res := ApplyClean(text, CleanOptions{Mode: CleanModeRemoveMultipleBlanks})
	want := "hello\nworld"
	if res.Cleaned != want {
		t.Errorf("got %q, want %q", res.Cleaned, want)
	}
}

func TestApplyClean_NormalizeLineEndings(t *testing.T) {
	text := "a\r\nb\r"
	res := ApplyClean(text, CleanOptions{Mode: CleanModeRemoveMultipleBlanks})
	want := "a\nb"
	if res.Cleaned != want {
		t.Errorf("got %q, want %q", res.Cleaned, want)
	}
}

func TestDiffLines_SearchReplace(t *testing.T) {
	original := "foo bar\nbaz foo"
	cleaned := "qux bar\nbaz qux"
	hunks := diffLines(original, cleaned)
	if len(hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d (%+v)", len(hunks), hunks)
	}
	h := hunks[0]
	if len(h.Before) != 2 || h.Before[0] != "foo bar" || h.Before[1] != "baz foo" {
		t.Errorf("unexpected before lines: %+v", h.Before)
	}
	if len(h.After) != 2 || h.After[0] != "qux bar" || h.After[1] != "baz qux" {
		t.Errorf("unexpected after lines: %+v", h.After)
	}
}

func TestDiffLines_RemoveLine(t *testing.T) {
	original := "keep\nremove this\nkeep too"
	cleaned := "keep\nkeep too"
	hunks := diffLines(original, cleaned)
	if len(hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d (%+v)", len(hunks), hunks)
	}
	if len(hunks[0].Before) != 1 || hunks[0].Before[0] != "remove this" {
		t.Errorf("unexpected before lines: %+v", hunks[0].Before)
	}
	if len(hunks[0].After) != 0 {
		t.Errorf("expected no after lines, got %+v", hunks[0].After)
	}
}

func TestDiffLines_RemoveAfter(t *testing.T) {
	original := "line one\nline two\nSTOP\nline four"
	cleaned := "line one\nline two"
	hunks := diffLines(original, cleaned)
	if len(hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d (%+v)", len(hunks), hunks)
	}
	want := []string{"STOP", "line four"}
	if len(hunks[0].Before) != len(want) {
		t.Fatalf("unexpected before lines: %+v", hunks[0].Before)
	}
	for i, line := range want {
		if hunks[0].Before[i] != line {
			t.Errorf("before[%d]=%q, want %q", i, hunks[0].Before[i], line)
		}
	}
	if len(hunks[0].After) != 0 {
		t.Errorf("expected no after lines, got %+v", hunks[0].After)
	}
	encoded, err := json.Marshal(hunks)
	if err != nil {
		t.Fatalf("marshal hunks: %v", err)
	}
	if string(encoded) != `[{"before":["STOP","line four"],"after":[]}]` {
		t.Errorf("expected empty after array in JSON, got %s", encoded)
	}
}

func TestDiffLines_NoChange(t *testing.T) {
	hunks := diffLines("same\nlines", "same\nlines")
	if len(hunks) != 0 {
		t.Errorf("expected no hunks, got %+v", hunks)
	}
}

func TestDiffLines_IgnoresLineEndingNormalization(t *testing.T) {
	hunks := diffLines("a\r\nb", "a\nb")
	if len(hunks) != 0 {
		t.Errorf("expected no hunks for CRLF-only differences, got %+v", hunks)
	}
}

package api

import (
	"testing"

	"translator-server/internal/ai"
)

func TestApplyRefineEditsCommitsSuccessesAndReportsOnlyFailures(t *testing.T) {
	text := "La casa era roja.\nLa casa era roja.\nEl cielo era azul."
	edits := []ai.RefineEdit{
		{Original: "El cielo era azul.", Replacement: "El cielo estaba azul."},
		{Original: "La casa era roja.", Replacement: "La casa estaba roja."},
		{Original: "No existe.", Replacement: "No existía."},
	}

	updated, results := applyRefineEdits(text, edits)

	want := "La casa era roja.\nLa casa era roja.\nEl cielo estaba azul."
	if updated != want {
		t.Fatalf("unexpected text:\nwant: %q\n got: %q", want, updated)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if !results[0].Applied {
		t.Fatalf("expected first edit to be applied, got reason %q", results[0].Reason)
	}
	if results[1].Applied || results[1].Reason != reasonMultipleMatches {
		t.Fatalf("expected second edit to fail as multiple_matches, got %+v", results[1])
	}
	if results[2].Applied || results[2].Reason != reasonNotFound {
		t.Fatalf("expected third edit to fail as not_found, got %+v", results[2])
	}
}

func TestNewApplyEditsFuncMutatesBufferAcrossCalls(t *testing.T) {
	buffer := "one\ntwo\nthree"
	apply := newApplyEditsFunc(&buffer, "chapter-1")

	results := apply([]ai.RefineEdit{{Original: "two", Replacement: "TWO"}})
	if !results[0].Applied {
		t.Fatalf("expected edit to apply, got %+v", results[0])
	}
	if buffer != "one\nTWO\nthree" {
		t.Fatalf("expected buffer to be mutated, got %q", buffer)
	}

	results = apply([]ai.RefineEdit{{Original: "TWO", Replacement: "2"}})
	if !results[0].Applied {
		t.Fatalf("expected second edit to apply against mutated buffer, got %+v", results[0])
	}
	if buffer != "one\n2\nthree" {
		t.Fatalf("expected buffer to reflect both edits, got %q", buffer)
	}
}

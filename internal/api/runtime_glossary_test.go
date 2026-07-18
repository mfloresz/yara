package api

import (
	"testing"

	"translator-server/internal/ai"
)

func TestMergeGlossaryEmpty(t *testing.T) {
	result := mergeGlossary(nil, nil)
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d entries", len(result))
	}
}

func TestMergeGlossaryNewEntriesOnly(t *testing.T) {
	newEntries := []ai.GlossaryEntry{
		{Source: "dragon", Target: "dragón", Context: "criatura mítica"},
		{Source: "shire", Target: "la Comarca", Context: "lugar de la obra"},
	}
	result := mergeGlossary(nil, newEntries)
	if len(result) != 2 {
		t.Errorf("expected 2 entries, got %d", len(result))
	}
	if result[0].Source != "dragon" || result[0].Target != "dragón" {
		t.Errorf("unexpected first entry: %+v", result[0])
	}
	if result[1].Source != "shire" || result[1].Target != "la Comarca" {
		t.Errorf("unexpected second entry: %+v", result[1])
	}
}

func TestMergeGlossaryDeduplication(t *testing.T) {
	existing := []glossaryEntry{
		{Source: "dragon", Target: "dragón viejo", Context: "criatura mítica"},
		{Source: "knight", Target: "caballero"},
	}
	newEntries := []ai.GlossaryEntry{
		{Source: "dragon", Target: "dragón actualizado"},
		{Source: "sword", Target: "espada"},
	}
	result := mergeGlossary(existing, newEntries)
	// dragon (preserved), knight (preserved), sword (new) => 3 entries
	if len(result) != 3 {
		t.Errorf("expected 3 entries (dragon preserved, knight preserved, sword new), got %d", len(result))
	}
	// dragon should appear once, keeping its existing approved translation and context
	found := false
	for _, e := range result {
		if e.Source == "dragon" {
			if e.Target != "dragón viejo" {
				t.Errorf("expected preserved target for dragon, got %s", e.Target)
			}
			if e.Context != "criatura mítica" {
				t.Errorf("expected preserved context for dragon, got %s", e.Context)
			}
			found = true
		}
	}
	if !found {
		t.Error("dragon entry not found in result")
	}
}

func TestMergeGlossaryPreservesExistingEntries(t *testing.T) {
	// Regression test: generating a glossary must NOT wipe out existing
	// (including manually-added) entries.
	existing := []glossaryEntry{
		{Source: "manual-term", Target: "término manual", Context: "added by hand"},
	}
	newEntries := []ai.GlossaryEntry{
		{Source: "sword", Target: "espada"},
	}
	result := mergeGlossary(existing, newEntries)
	if len(result) != 2 {
		t.Fatalf("expected 2 entries (existing preserved + new), got %d", len(result))
	}
	if result[0].Source != "manual-term" || result[0].Target != "término manual" || result[0].Context != "added by hand" {
		t.Errorf("existing manual entry not preserved: %+v", result[0])
	}
	if result[1].Source != "sword" || result[1].Target != "espada" {
		t.Errorf("new entry not appended correctly: %+v", result[1])
	}
}

func TestMergeGlossarySkipsEmptyEntries(t *testing.T) {
	newEntries := []ai.GlossaryEntry{
		{Source: "dragon", Target: "dragón"},
		{Source: "", Target: "something"},
		{Source: "sword", Target: ""},
		{Source: "knight", Target: "caballero", Context: "warrior"},
	}
	result := mergeGlossary(nil, newEntries)
	if len(result) != 2 {
		t.Errorf("expected 2 entries (skip empty), got %d", len(result))
	}
}

func TestMergeGlossaryPreservesContext(t *testing.T) {
	newEntries := []ai.GlossaryEntry{
		{Source: "dragon", Target: "dragón", Context: "criatura mítica"},
		{Source: "sword", Target: "espada"},
	}
	result := mergeGlossary(nil, newEntries)
	if result[0].Context != "criatura mítica" {
		t.Errorf("expected context preserved, got %s", result[0].Context)
	}
	if result[1].Context != "" {
		t.Errorf("expected empty context, got %s", result[1].Context)
	}
}

func TestEstimateTokens(t *testing.T) {
	short := "Hello world"
	tokens := estimateTokens(short)
	if tokens <= 0 {
		t.Errorf("expected positive token count for short text, got %d", tokens)
	}

	long := "This is a longer text that should have more tokens than the short one. It contains multiple sentences and should be significantly larger in terms of token count."
	longTokens := estimateTokens(long)
	if longTokens <= tokens {
		t.Errorf("expected more tokens for long text (%d) than short text (%d)", longTokens, tokens)
	}
}

func TestFlattenGlossaryOutput(t *testing.T) {
	out := ai.GenerateGlossaryOutput{
		Terms: []ai.GlossaryEntry{
			{Source: "dragon", Target: "dragón"},
		},
		CultivationSystem: []ai.GlossaryEntry{
			{Source: "Qi Condensation", Target: "Condensación de Qi"},
		},
	}
	result := flattenGlossaryOutput(out)
	if len(result) != 2 {
		t.Errorf("expected 2 entries, got %d", len(result))
	}
}

func TestFlattenGlossaryOutputEmpty(t *testing.T) {
	out := ai.GenerateGlossaryOutput{}
	result := flattenGlossaryOutput(out)
	if len(result) != 0 {
		t.Errorf("expected 0 entries, got %d", len(result))
	}
}

package ai

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestCheckOutputSchemaRequiresDecision(t *testing.T) {
	var schema struct {
		Properties map[string]struct {
			Type string   `json:"type"`
			Enum []string `json:"enum"`
		} `json:"properties"`
		Required             []string `json:"required"`
		AdditionalProperties bool     `json:"additionalProperties"`
	}
	if err := json.Unmarshal([]byte(checkOutputSchema), &schema); err != nil {
		t.Fatalf("checkOutputSchema is not valid JSON: %v", err)
	}

	if !slices.Equal(schema.Required, []string{"decision"}) {
		t.Fatalf("strict schema must require only decision, got %v", schema.Required)
	}
	if len(schema.Properties) != 1 {
		t.Fatalf("strict schema must expose only decision, got %v", schema.Properties)
	}
	if _, ok := schema.Properties["decision"]; !ok {
		t.Fatal("strict schema is missing the decision property")
	}
	if schema.AdditionalProperties {
		t.Fatal("strict schema must not allow additional properties")
	}

	decision := schema.Properties["decision"]
	if decision.Type != "string" {
		t.Fatalf("decision type = %q, want string", decision.Type)
	}
	want := []string{CheckDecisionAccept, CheckDecisionRefine, CheckDecisionRetranslate}
	if !slices.Equal(decision.Enum, want) {
		t.Fatalf("decision enum = %v, want %v", decision.Enum, want)
	}
}

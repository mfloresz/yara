package ai

import (
	"encoding/json"
	"testing"
)

func TestRefineApplyEditsSchemaIsValidJSON(t *testing.T) {
	var v any
	if err := json.Unmarshal([]byte(refineApplyEditsSchema), &v); err != nil {
		t.Fatalf("refineApplyEditsSchema is not valid JSON: %v", err)
	}
	obj, ok := v.(map[string]any)
	if !ok {
		t.Fatal("refineApplyEditsSchema root must be a JSON object")
	}
	if obj["type"] != "object" {
		t.Fatalf("refineApplyEditsSchema type = %v, want 'object'", obj["type"])
	}
	props, ok := obj["properties"].(map[string]any)
	if !ok {
		t.Fatal("refineApplyEditsSchema must have a 'properties' object")
	}
	edits, ok := props["edits"]
	if !ok {
		t.Fatal("refineApplyEditsSchema must have an 'edits' property")
	}
	t.Logf("schema parses OK: %s", refineApplyEditsSchema[:40])
	_ = edits
}

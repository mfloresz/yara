package store

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"

	"translator-server/internal/secure"
)

// TestEnsureSchemaRaisesAgentMessagesMax reproduces the 5000-char truncation:
// collections created before the explicit Max existed run on PocketBase's
// TextField default and reject real chat histories. EnsureSchema must raise
// the cap on existing databases, and saving a >5000 char trail must work.
func TestEnsureSchemaRaisesAgentMessagesMax(t *testing.T) {
	dataDir := t.TempDir()
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: dataDir})
	if err := app.Bootstrap(); err != nil {
		t.Fatalf("bootstrap pocketbase: %v", err)
	}
	t.Cleanup(func() {
		app.ResetBootstrapState()
	})

	encryptor, err := secure.NewEncryptorFromConfig("", filepath.Join(dataDir, "app.key"))
	if err != nil {
		t.Fatalf("create encryptor: %v", err)
	}
	st := New(app, encryptor)
	if err := st.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	// Simulate a database created before the explicit Max existed.
	collection, err := app.FindCollectionByNameOrId(AgentSessionsCollection)
	if err != nil {
		t.Fatalf("find agent sessions collection: %v", err)
	}
	field := collection.Fields.GetByName("messages")
	textField, ok := field.(*core.TextField)
	if !ok {
		t.Fatalf("messages field should be a TextField, got %T", field)
	}
	textField.Max = 0
	if err := app.Save(collection); err != nil {
		t.Fatalf("save legacy messages field: %v", err)
	}

	if err := st.EnsureSchema(); err != nil {
		t.Fatalf("re-run ensure schema: %v", err)
	}

	collection, err = app.FindCollectionByNameOrId(AgentSessionsCollection)
	if err != nil {
		t.Fatalf("re-find agent sessions collection: %v", err)
	}
	field = collection.Fields.GetByName("messages")
	textField, ok = field.(*core.TextField)
	if !ok {
		t.Fatalf("messages field should still be a TextField, got %T", field)
	}
	if textField.Max != agentMessagesFieldMax {
		t.Fatalf("expected messages Max %d after migration, got %d", agentMessagesFieldMax, textField.Max)
	}

	// End to end: a trail bigger than the old 5000-char default must persist.
	user, err := st.CreateUser("agent-max@example.com", "secret123", "Alice")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	bigTrail := `["` + strings.Repeat("x", 6000) + `"]`
	session, err := st.CreateAgentSession(user.User.ID, "[]")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := st.SaveAgentSessionMessages(user.User.ID, session.ID, bigTrail); err != nil {
		t.Fatalf("save >5000 char trail: %v", err)
	}
	saved, err := st.GetAgentSession(user.User.ID, session.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if saved.Messages != bigTrail {
		t.Fatalf("saved trail was altered (len got %d, want %d)", len(saved.Messages), len(bigTrail))
	}
}

package store

import (
	"path/filepath"
	"testing"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"translator-server/internal/secure"
)

// A last-used write must not overwrite a revocation committed after validation
// loaded the token. The hook deterministically interleaves those operations.
func TestValidateWorkerTokenPreservesPasswordChangeRevocation(t *testing.T) {
	dataDir := t.TempDir()
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: dataDir})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.ResetBootstrapState() })
	encryptor, err := secure.NewEncryptorFromConfig("", filepath.Join(dataDir, "app.key"))
	if err != nil {
		t.Fatal(err)
	}
	st := New(app, encryptor)
	if err := st.EnsureSchema(); err != nil {
		t.Fatal(err)
	}
	alice, err := st.CreateUser("worker-revocation@example.com", "secret123", "Alice")
	if err != nil {
		t.Fatal(err)
	}
	auth, err := st.FindAuthRecord(alice.Token)
	if err != nil {
		t.Fatal(err)
	}
	worker, token, err := st.CreateWorkerToken(alice.User.ID, "revocation-test", "Test")
	if err != nil {
		t.Fatal(err)
	}
	interleaved := false
	app.OnRecordUpdate(WorkerTokensCollection).BindFunc(func(e *core.RecordEvent) error {
		if !interleaved && e.Record.Id == worker.ID && !e.Record.GetBool("revoked") && e.Record.GetString("last_used_at") != "" {
			// Set first because ChangePassword itself updates this worker token.
			interleaved = true
			if err := st.ChangePassword(alice.User.ID, auth.TokenKey(), "secret123", "new-secret456", "new-secret456"); err != nil {
				return err
			}
		}
		return e.Next()
	})
	if _, err := st.ValidateWorkerToken(token); err != nil {
		t.Fatalf("in-flight validation: %v", err)
	}
	if !interleaved {
		t.Fatal("last-used hook did not exercise concurrent password change")
	}
	saved, err := app.FindRecordById(WorkerTokensCollection, worker.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.GetBool("revoked") {
		t.Fatal("last-used save restored revoked=false after password change")
	}
	if saved.GetDateTime("last_used_at").IsZero() {
		t.Fatal("last-used timestamp was not saved")
	}
	if _, err := st.ValidateWorkerToken(token); err == nil {
		t.Fatal("revoked worker token remained usable")
	}
}

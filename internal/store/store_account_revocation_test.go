package store

import (
	"errors"
	"fmt"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func newAccountRevocationStore(t *testing.T) (*Store, string, string) {
	t.Helper()
	st, ownerID, _ := newImagesTestStore(t)
	t.Cleanup(func() { st.App.ResetBootstrapState() })
	other, err := st.CreateUser("revocation-other@example.com", "secret123", "Other")
	if err != nil {
		t.Fatal(err)
	}
	return st, ownerID, other.User.ID
}

func TestAccountRevocationBeyondPageLimit(t *testing.T) {
	for _, action := range []string{"helper", "reset", "block"} {
		t.Run(action, func(t *testing.T) {
			st, ownerID, otherID := newAccountRevocationStore(t)
			_, unrelated, err := st.CreateWorkerToken(otherID, "unrelated", "Test")
			if err != nil {
				t.Fatal(err)
			}
			plaintexts := make([]string, 0, 501)
			err = st.App.RunInTransaction(func(tx core.App) error {
				txStore := New(tx, st.Encryptor)
				for i := 0; i < 501; i++ {
					_, raw, err := txStore.CreateWorkerToken(ownerID, fmt.Sprintf("worker-%d", i), "Test")
					if err != nil {
						return err
					}
					plaintexts = append(plaintexts, raw)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			switch action {
			case "helper":
				err = st.revokeAllWorkerTokens(ownerID)
			case "reset":
				_, raw, createErr := st.CreatePasswordReset(otherID, ownerID)
				if createErr != nil {
					t.Fatal(createErr)
				}
				_, err = st.RedeemPasswordReset(raw, "newsecret123")
			case "block":
				_, err = st.BlockUser(otherID, ownerID)
			}
			if err != nil {
				t.Fatal(err)
			}
			remaining, err := st.App.CountRecords(WorkerTokensCollection, dbx.HashExp{"owner": ownerID, "revoked": false})
			if err != nil || remaining != 0 {
				t.Fatalf("unrevoked tokens=%d error=%v", remaining, err)
			}
			for _, raw := range plaintexts {
				if _, err := st.ValidateWorkerToken(raw); err == nil {
					t.Fatal("revoked token still validates")
				}
			}
			if _, err := st.ValidateWorkerToken(unrelated); err != nil {
				t.Fatalf("unrelated token revoked: %v", err)
			}
		})
	}
}

func TestAccountRevocationRollsBackOnTokenFailure(t *testing.T) {
	for _, action := range []string{"reset", "block"} {
		t.Run(action, func(t *testing.T) {
			st, ownerID, otherID := newAccountRevocationStore(t)
			before, err := st.App.FindRecordById(UsersCollection, ownerID)
			if err != nil {
				t.Fatal(err)
			}
			_, raw, err := st.CreateWorkerToken(ownerID, "rollback", "Test")
			if err != nil {
				t.Fatal(err)
			}
			_, reset, err := st.CreatePasswordReset(otherID, ownerID)
			if err != nil {
				t.Fatal(err)
			}
			st.App.OnRecordUpdate(WorkerTokensCollection).BindFunc(func(e *core.RecordEvent) error {
				if e.Record.GetBool("revoked") {
					return errors.New("simulated revocation failure")
				}
				return e.Next()
			})
			if action == "reset" {
				_, err = st.RedeemPasswordReset(reset, "newsecret123")
			} else {
				_, err = st.BlockUser(otherID, ownerID)
			}
			if err == nil {
				t.Fatal("expected revocation failure")
			}
			after, err := st.App.FindRecordById(UsersCollection, ownerID)
			if err != nil {
				t.Fatal(err)
			}
			if after.TokenKey() != before.TokenKey() || after.GetBool("blocked") != before.GetBool("blocked") || after.GetString("password") != before.GetString("password") {
				t.Fatal("failed revocation changed account")
			}
			if _, err := st.FindPasswordResetByToken(reset); err != nil {
				t.Fatalf("failed transaction consumed reset: %v", err)
			}
			if _, err := st.ValidateWorkerToken(raw); err != nil {
				t.Fatalf("failed transaction revoked token: %v", err)
			}
		})
	}
}

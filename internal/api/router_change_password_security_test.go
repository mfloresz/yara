package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"translator-server/internal/store"
)

func TestChangePasswordConsumesOnlyOwnResetLinks(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "reset-change-alice@example.com", "secret123", "Alice")
	bob := registerUser(t, env, "reset-change-bob@example.com", "secret123", "Bob")
	_, aliceToken, err := env.store.CreatePasswordReset("", alice.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, bobToken, err := env.store.CreatePasswordReset("", bob.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Exercise more than one batch without paying 500 separate transactions.
	err = env.store.App.RunInTransaction(func(tx core.App) error {
		s := store.New(tx, env.store.Encryptor)
		for i := 0; i < 500; i++ {
			if _, _, err := s.CreatePasswordReset("", alice.User.ID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/auth/change-password", alice.Token, map[string]string{"currentPassword": "secret123", "newPassword": "new-secret456", "newPasswordConfirm": "new-secret456"})
	assertStatus(t, resp, http.StatusNoContent)
	if _, err := env.store.RedeemPasswordReset(aliceToken, "reset-secret789"); !errors.Is(err, store.ErrInvitationUsed) {
		t.Fatalf("old reset redeemed or wrong error: %v", err)
	}
	remaining, err := env.store.App.CountRecords(store.PasswordResetsCollection, dbx.HashExp{"user": alice.User.ID, "used_at": ""})
	if err != nil || remaining != 0 {
		t.Fatalf("unused own reset links: %d, error: %v", remaining, err)
	}
	if _, err := env.store.RedeemPasswordReset(bobToken, "bob-reset-secret789"); err != nil {
		t.Fatalf("Bob's reset affected: %v", err)
	}
	if _, err := env.store.AuthenticateUser(alice.User.Email, "new-secret456"); err != nil {
		t.Fatalf("old reset changed Alice's password: %v", err)
	}
}

func TestChangePasswordResetRevocationRollback(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "reset-rollback@example.com", "secret123", "Alice")
	_, token, err := env.store.CreatePasswordReset("", alice.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	env.store.App.OnRecordUpdate(store.PasswordResetsCollection).BindFunc(func(e *core.RecordEvent) error { return errors.New("simulated reset consumption failure") })
	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/auth/change-password", alice.Token, map[string]string{"currentPassword": "secret123", "newPassword": "new-secret456", "newPasswordConfirm": "new-secret456"})
	assertStatus(t, resp, http.StatusInternalServerError)
	if _, err := env.store.AuthenticateUser(alice.User.Email, "secret123"); err != nil {
		t.Fatal("failed reset consumption changed password")
	}
	if _, err := env.store.FindAuthRecord(alice.Token); err != nil {
		t.Fatal("failed reset consumption revoked session")
	}
	if _, err := env.store.FindPasswordResetByToken(token); err != nil {
		t.Fatal("failed reset consumption used reset")
	}
}

func TestChangePasswordClosesRealWorkerWebsocket(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "socket-password@example.com", "secret123", "Alice")
	_, token, err := env.store.CreateWorkerToken(alice.User.ID, "password-socket", "Test")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(env.handler)
	defer srv.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/ws/browser-worker", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(map[string]any{"type": "register", "payload": map[string]any{"token": token}}); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var registered struct {
		Type    string `json:"type"`
		Payload struct {
			OK bool `json:"ok"`
		} `json:"payload"`
	}
	if err := conn.ReadJSON(&registered); err != nil {
		t.Fatal(err)
	}
	if registered.Type != "register_response" || !registered.Payload.OK {
		t.Fatalf("registration rejected: %+v", registered)
	}
	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/auth/change-password", alice.Token, map[string]string{"currentPassword": "secret123", "newPassword": "new-secret456", "newPasswordConfirm": "new-secret456"})
	assertStatus(t, resp, http.StatusNoContent)
	_, _, err = conn.ReadMessage()
	if err == nil {
		t.Fatal("worker socket remains open")
	}
	var closeErr *websocket.CloseError
	if !errors.As(err, &closeErr) {
		t.Fatalf("expected closed connection, not timeout or read failure: %v", err)
	}
}

func TestChangePasswordRejectsOtherAuthCollections(t *testing.T) {
	for _, name := range []string{"_superusers", "other_password_users"} {
		t.Run(name, func(t *testing.T) {
			env := newAPITestEnv(t)
			collection, err := env.store.App.FindCollectionByNameOrId(name)
			if name != "_superusers" {
				collection = core.NewAuthCollection(name)
				err = env.store.App.Save(collection)
			}
			if err != nil {
				t.Fatal(err)
			}
			record := core.NewRecord(collection)
			record.SetEmail("other-password@example.com")
			record.SetPassword("secret123")
			if err := env.store.App.Save(record); err != nil {
				t.Fatal(err)
			}
			token, err := record.NewAuthToken()
			if err != nil {
				t.Fatal(err)
			}
			resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/auth/change-password", token, map[string]string{"currentPassword": "secret123", "newPassword": "new-secret456", "newPasswordConfirm": "new-secret456"})
			if resp.Code != http.StatusUnauthorized && resp.Code != http.StatusForbidden {
				t.Fatalf("non-user auth status %d: %s", resp.Code, resp.Body.String())
			}
		})
	}
}

func TestChangePasswordSweepWaitsForRegistration(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "registration-password@example.com", "secret123", "Alice")
	_, token, err := env.store.CreateWorkerToken(alice.User.ID, "registration", "Test")
	if err != nil {
		t.Fatal(err)
	}
	worker := &BrowserWorker{ID: "password-registration-order", State: "connected"}
	browserWorkersMu.Lock()
	browserWorkers[worker.ID] = worker
	browserWorkersMu.Unlock()
	t.Cleanup(func() { evictWorker(worker) })
	validated := make(chan struct{})
	resume := make(chan struct{})
	env.store.App.OnRecordUpdate(store.WorkerTokensCollection).BindFunc(func(e *core.RecordEvent) error {
		if e.Record.GetBool("revoked") {
			return e.Next()
		}
		close(validated)
		<-resume
		return e.Next()
	})
	payload, _ := json.Marshal(map[string]string{"token": token})
	registered := make(chan struct{})
	go func() {
		defer close(registered)
		env.server.handleWorkerMessage(worker, &BrowserWorkerMessage{Type: "register", Payload: payload})
	}()
	select {
	case <-validated:
	case <-time.After(3 * time.Second):
		t.Fatal("registration did not validate")
	}
	swept := make(chan struct{})
	go func() { closePasswordChangeWorkers(alice.User.ID); close(swept) }()
	select {
	case <-swept:
		close(resume)
		<-registered
		t.Fatal("sweep passed registration before identity was published")
	case <-time.After(30 * time.Millisecond):
	}
	close(resume)
	select {
	case <-registered:
	case <-time.After(3 * time.Second):
		t.Fatal("registration stuck")
	}
	select {
	case <-swept:
	case <-time.After(3 * time.Second):
		t.Fatal("sweep stuck")
	}
	browserWorkersMu.RLock()
	_, exists := browserWorkers[worker.ID]
	browserWorkersMu.RUnlock()
	if exists {
		t.Fatal("pre-sweep registration escaped eviction")
	}
}

func TestChangePasswordSweepDoesNotHoldRegistryWhileWaiting(t *testing.T) {
	worker := &BrowserWorker{ID: "password-locked-worker", UserID: "password-locked-user"}
	browserWorkersMu.Lock()
	browserWorkers[worker.ID] = worker
	browserWorkersMu.Unlock()
	worker.mu.Lock()
	swept := make(chan struct{})
	go func() { closePasswordChangeWorkers(worker.UserID); close(swept) }()
	// The sweep may wait on this worker, but must not prevent unrelated registry access.
	time.Sleep(30 * time.Millisecond)
	accessed := make(chan struct{})
	go func() { browserWorkersMu.Lock(); browserWorkersMu.Unlock(); close(accessed) }()
	select {
	case <-accessed:
	case <-time.After(time.Second):
		worker.mu.Unlock()
		<-swept
		t.Fatal("worker mutex froze global registry")
	}
	worker.mu.Unlock()
	select {
	case <-swept:
	case <-time.After(3 * time.Second):
		t.Fatal("sweep stuck")
	}
}

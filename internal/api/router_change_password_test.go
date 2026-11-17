package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"translator-server/internal/store"
)

func TestChangePasswordRevokesSessions(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "password-alice@example.com", "secret123", "Alice")
	bob := registerUser(t, env, "password-bob@example.com", "secret123", "Bob")
	browserWorkersMu.Lock()
	browserWorkers["password-test-worker"] = &BrowserWorker{ID: "password-test-worker", UserID: alice.User.ID, State: "authenticated"}
	browserWorkersMu.Unlock()
	t.Cleanup(func() {
		browserWorkersMu.Lock()
		delete(browserWorkers, "password-test-worker")
		browserWorkersMu.Unlock()
	})
	second, err := env.store.AuthenticateUser(alice.User.Email, "secret123")
	if err != nil {
		t.Fatal(err)
	}
	_, workerToken, err := env.store.CreateWorkerToken(alice.User.ID, "password-test", "Test")
	if err != nil {
		t.Fatal(err)
	}
	response := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/auth/change-password", alice.Token, map[string]any{
		"currentPassword": "secret123", "newPassword": "new-secret456", "newPasswordConfirm": "new-secret456",
		// These must never select a target or be mass-assigned.
		"userId": bob.User.ID, "email": bob.User.Email, "role": "admin",
	})
	assertStatus(t, response, http.StatusNoContent)
	if env.server.HasBrowserWorkerForUser(alice.User.ID) {
		t.Fatal("authenticated worker connection remains available")
	}
	if response.Body.Len() != 0 {
		t.Fatal("password change must not return credentials")
	}
	cleared := false
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == authCookieName && cookie.MaxAge < 0 && cookie.HttpOnly {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("auth cookie was not cleared")
	}
	for _, token := range []string{alice.Token, second.Token} {
		assertStatus(t, doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/auth/me", token, nil), http.StatusUnauthorized)
		assertStatus(t, doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/auth/refresh", token, nil), http.StatusUnauthorized)
	}
	if _, err := env.store.ValidateWorkerToken(workerToken); err == nil {
		t.Fatal("old worker token remains valid")
	}
	if _, err := env.store.AuthenticateUser(alice.User.Email, "secret123"); err == nil {
		t.Fatal("old password remains valid")
	}
	fresh, err := env.store.AuthenticateUser(alice.User.Email, "new-secret456")
	if err != nil {
		t.Fatalf("new password rejected: %v", err)
	}
	assertStatus(t, doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/auth/me", fresh.Token, nil), http.StatusOK)
	if fresh.User.Role != alice.User.Role {
		t.Fatal("password change altered role")
	}
	if _, err := env.store.AuthenticateUser(bob.User.Email, "secret123"); err != nil {
		t.Fatal("another account was changed")
	}
}

func TestChangePasswordValidation(t *testing.T) {
	cases := []struct{ name, current, password, confirm string }{
		{"wrong current", "wrong", "new-secret456", "new-secret456"},
		{"missing current", "", "new-secret456", "new-secret456"},
		{"short", "secret123", "short", "short"},
		{"missing confirmation", "secret123", "new-secret456", ""},
		{"mismatch", "secret123", "new-secret456", "other-secret456"},
		{"unchanged", "secret123", "secret123", "secret123"},
		{"too long", "secret123", strings.Repeat("x", 73), strings.Repeat("x", 73)},
		{"PocketBase maximum", "secret123", strings.Repeat("x", 72), strings.Repeat("x", 72)},
		{"too few unicode characters", "secret123", "密码测试五六七", "密码测试五六七"},
		{"bcrypt byte maximum", "secret123", strings.Repeat("密", 25), strings.Repeat("密", 25)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newAPITestEnv(t)
			alice := registerUser(t, env, "password-validation@example.com", "secret123", "Alice")
			resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/auth/change-password", alice.Token, map[string]string{
				"currentPassword": tc.current, "newPassword": tc.password, "newPasswordConfirm": tc.confirm,
			})
			assertStatus(t, resp, http.StatusBadRequest)
			if _, err := env.store.AuthenticateUser(alice.User.Email, "secret123"); err != nil {
				t.Fatal("failed attempt changed password")
			}
			assertStatus(t, doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/auth/me", alice.Token, nil), http.StatusOK)
		})
	}
}

func TestChangePasswordCookieAndNavigationGuard(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "password-cookie@example.com", "secret123", "Alice")
	body := `{"currentPassword":"secret123","newPassword":"new-secret456","newPasswordConfirm":"new-secret456"}`
	request := func(mode string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/change-password", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Sec-Fetch-Mode", mode)
		req.Header.Set("X-Forwarded-Proto", "https")
		req.AddCookie(&http.Cookie{Name: authCookieName, Value: alice.Token})
		resp := httptest.NewRecorder()
		env.handler.ServeHTTP(resp, req)
		return resp
	}
	assertStatus(t, request("navigate"), http.StatusForbidden)
	resp := request("cors")
	assertStatus(t, resp, http.StatusNoContent)
	for _, cookie := range resp.Result().Cookies() {
		if cookie.Name == authCookieName && (!cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.MaxAge >= 0) {
			t.Fatal("cleared cookie lost security attributes")
		}
	}
}

func TestChangePasswordRevokesEveryWorkerToken(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "password-many-workers@example.com", "secret123", "Alice")
	err := env.store.App.RunInTransaction(func(tx core.App) error {
		s := store.New(tx, env.store.Encryptor)
		for i := 0; i < 501; i++ {
			if _, _, err := s.CreateWorkerToken(alice.User.ID, "many-workers", "Test"); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := env.store.FindAuthRecord(alice.Token)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.store.ChangePassword(alice.User.ID, record.TokenKey(), "secret123", "new-secret456", "new-secret456"); err != nil {
		t.Fatal(err)
	}
	remaining, err := env.store.App.CountRecords(store.WorkerTokensCollection, dbx.HashExp{"revoked": false})
	if err != nil || remaining != 0 {
		t.Fatalf("unrevoked worker tokens: %d, error: %v", remaining, err)
	}
}

func TestChangePasswordAtomicRollback(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "password-rollback@example.com", "secret123", "Alice")
	_, workerToken, err := env.store.CreateWorkerToken(alice.User.ID, "rollback", "Test")
	if err != nil {
		t.Fatal(err)
	}
	env.store.App.OnRecordUpdate(store.WorkerTokensCollection).BindFunc(func(e *core.RecordEvent) error {
		if e.Record.GetBool("revoked") {
			return errors.New("simulated token revocation failure")
		}
		return e.Next()
	})
	record, err := env.store.FindAuthRecord(alice.Token)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.store.ChangePassword(alice.User.ID, record.TokenKey(), "secret123", "new-secret456", "new-secret456"); err == nil {
		t.Fatal("expected token revocation failure")
	}
	if _, err := env.store.AuthenticateUser(alice.User.Email, "secret123"); err != nil {
		t.Fatal("failed transaction changed password")
	}
	if _, err := env.store.FindAuthRecord(alice.Token); err != nil {
		t.Fatal("failed transaction revoked session")
	}
	if _, err := env.store.ValidateWorkerToken(workerToken); err != nil {
		t.Fatal("failed transaction revoked worker")
	}
}

func TestChangePasswordRejectsStaleAuthenticatedRecord(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "password-stale@example.com", "secret123", "Alice")
	record, err := env.store.FindAuthRecord(alice.Token)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.store.ChangePassword(alice.User.ID, record.TokenKey(), "secret123", "new-secret456", "new-secret456"); err != nil {
		t.Fatal(err)
	}
	if err := env.store.ChangePassword(alice.User.ID, record.TokenKey(), "new-secret456", "another-secret789", "another-secret789"); !errors.Is(err, store.ErrForbidden) {
		t.Fatalf("stale authenticated record accepted: %v", err)
	}
}

func TestChangePasswordBodyAndBlockedAccount(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "password-body@example.com", "secret123", "Alice")
	for _, body := range []string{`{`, `{"currentPassword":"` + strings.Repeat("x", int(maxAuthBodyBytes)) + `"}`} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/change-password", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", alice.Token)
		resp := httptest.NewRecorder()
		env.handler.ServeHTTP(resp, req)
		assertStatus(t, resp, http.StatusBadRequest)
	}
	record, err := env.store.FindAuthRecord(alice.Token)
	if err != nil {
		t.Fatal(err)
	}
	record.Set("blocked", true)
	if err := env.store.App.Save(record); err != nil {
		t.Fatal(err)
	}
	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/auth/change-password", alice.Token, map[string]string{"currentPassword": "secret123", "newPassword": "new-secret456", "newPasswordConfirm": "new-secret456"})
	assertStatus(t, resp, http.StatusForbidden)
}

func TestChangePasswordIPRateLimit(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "password-ip@example.com", "secret123", "Alice")
	for i := 0; i < 6; i++ {
		resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/auth/change-password", alice.Token, map[string]string{"currentPassword": "wrong", "newPassword": "new-secret456", "newPasswordConfirm": "new-secret456"})
		want := http.StatusBadRequest
		if i == 5 {
			want = http.StatusTooManyRequests
		}
		assertStatus(t, resp, want)
	}
}

func TestChangePasswordRequiresAuth(t *testing.T) {
	env := newAPITestEnv(t)
	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/auth/change-password", "", map[string]string{"currentPassword": "secret123", "newPassword": "new-secret456", "newPasswordConfirm": "new-secret456"})
	assertStatus(t, resp, http.StatusUnauthorized)
}

func TestChangePasswordRateLimitedPerAccount(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "password-limit@example.com", "secret123", "Alice")
	// Remove the IP ceiling to prove the account ceiling independently.
	env.server.loginLimiter.capacity = 100
	env.server.loginLimiter.perMinute = 100
	for i := 0; i < loginFailureLimit; i++ {
		resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/auth/change-password", alice.Token, map[string]string{"currentPassword": "wrong", "newPassword": "new-secret456", "newPasswordConfirm": "new-secret456"})
		assertStatus(t, resp, http.StatusBadRequest)
	}
	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/auth/change-password", alice.Token, map[string]string{"currentPassword": "secret123", "newPassword": "new-secret456", "newPasswordConfirm": "new-secret456"})
	assertStatus(t, resp, http.StatusTooManyRequests)
	if resp.Header().Get("Retry-After") == "" {
		t.Fatal("missing Retry-After")
	}
	if _, err := env.store.AuthenticateUser(alice.User.Email, "secret123"); err != nil {
		t.Fatal("rate limited attempt changed password")
	}
}

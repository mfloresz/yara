package api

import (
	"net/http"
	"strings"
	"testing"
)

func TestAdminBlockUnblock(t *testing.T) {
	env := newAPITestEnv(t)
	admin := bootstrapAdmin(t, env, "admin@example.com")
	bob := registerUser(t, env, "bob@example.com", "secret123", "Bob")

	block := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/admin/users/"+bob.User.ID+"/block", admin.Token, map[string]any{})
	assertStatus(t, block, http.StatusOK)

	login := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/auth/login", "", map[string]any{
		"email": "bob@example.com", "password": "secret123",
	})
	assertStatus(t, login, http.StatusForbidden)
	if !strings.Contains(login.Body.String(), "account_blocked") {
		t.Fatalf("expected account_blocked, got %s", login.Body.String())
	}

	// Blocked session cannot use the API.
	me := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/auth/me", bob.Token, nil)
	if me.Code != http.StatusForbidden && me.Code != http.StatusUnauthorized {
		t.Fatalf("expected blocked session rejected, got %d: %s", me.Code, me.Body.String())
	}

	unblock := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/admin/users/"+bob.User.ID+"/unblock", admin.Token, map[string]any{})
	assertStatus(t, unblock, http.StatusOK)

	// Admin cannot block themselves.
	self := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/admin/users/"+admin.User.ID+"/block", admin.Token, map[string]any{})
	assertStatus(t, self, http.StatusBadRequest)
}

func TestAdminDeleteUserWithNovels(t *testing.T) {
	env := newAPITestEnv(t)
	admin := bootstrapAdmin(t, env, "admin@example.com")
	bob := registerUser(t, env, "bob@example.com", "secret123", "Bob")
	novel := createNovel(t, env.handler, bob.Token, "Bob Novel", "en", "es")

	stats := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/admin/users/"+bob.User.ID+"/stats", admin.Token, nil)
	assertStatus(t, stats, http.StatusOK)
	if !strings.Contains(stats.Body.String(), novel.ID) {
		t.Fatalf("expected stats to list novel %s, got %s", novel.ID, stats.Body.String())
	}

	del := doJSONRequest(t, env.handler, http.MethodDelete, "/api/v1/admin/users/"+bob.User.ID, admin.Token, map[string]any{"mode": "with-novels"})
	assertStatus(t, del, http.StatusNoContent)

	get := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels/"+novel.ID, admin.Token, nil)
	assertStatus(t, get, http.StatusNotFound)
}

func TestAdminTransferNovelsAndDelete(t *testing.T) {
	env := newAPITestEnv(t)
	admin := bootstrapAdmin(t, env, "admin@example.com")
	bob := registerUser(t, env, "bob@example.com", "secret123", "Bob")
	novel := createNovel(t, env.handler, bob.Token, "Bob Novel", "en", "es")

	del := doJSONRequest(t, env.handler, http.MethodDelete, "/api/v1/admin/users/"+bob.User.ID, admin.Token, map[string]any{
		"mode": "transfer", "transferToUserId": admin.User.ID,
	})
	assertStatus(t, del, http.StatusNoContent)

	get := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels/"+novel.ID, admin.Token, nil)
	assertStatus(t, get, http.StatusOK)
}

func TestPasswordResetLifecycle(t *testing.T) {
	env := newAPITestEnv(t)
	admin := bootstrapAdmin(t, env, "admin@example.com")
	bob := registerUser(t, env, "bob@example.com", "secret123", "Bob")

	created := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/admin/users/"+bob.User.ID+"/password-resets", admin.Token, map[string]any{})
	assertStatus(t, created, http.StatusCreated)
	var out struct {
		ResetURL string `json:"resetUrl"`
	}
	decodeResponse(t, created, &out)
	idx := strings.LastIndex(out.ResetURL, "/reset-password/")
	if idx < 0 {
		t.Fatalf("reset URL missing token: %q", out.ResetURL)
	}
	rawToken := out.ResetURL[idx+len("/reset-password/"):]

	validate := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/auth/password-reset/validate", "", map[string]any{"token": rawToken})
	assertStatus(t, validate, http.StatusOK)
	var validated struct {
		Valid bool `json:"valid"`
	}
	decodeResponse(t, validate, &validated)
	if !validated.Valid {
		t.Fatal("expected reset token valid")
	}

	accept := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/auth/password-reset/accept", "", map[string]any{
		"token": rawToken, "password": "newsecret123",
	})
	assertStatus(t, accept, http.StatusOK)

	// Old password stops working, new one works.
	oldLogin := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/auth/login", "", map[string]any{
		"email": "bob@example.com", "password": "secret123",
	})
	assertStatus(t, oldLogin, http.StatusBadRequest)
	newLogin := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/auth/login", "", map[string]any{
		"email": "bob@example.com", "password": "newsecret123",
	})
	assertStatus(t, newLogin, http.StatusOK)

	// Old session was killed by the tokenKey rotation.
	me := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/auth/me", bob.Token, nil)
	assertStatus(t, me, http.StatusUnauthorized)

	// Single use: second accept fails.
	reuse := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/auth/password-reset/accept", "", map[string]any{
		"token": rawToken, "password": "another123",
	})
	assertStatus(t, reuse, http.StatusBadRequest)
}

package api

import (
	"net/http"
	"testing"

	"translator-server/internal/store"
)

func TestFirstUserBecomesAdmin(t *testing.T) {
	env := newAPITestEnv(t)

	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email":    "founder@example.com",
		"password": "secret123",
		"name":     "Founder",
	})
	assertStatus(t, resp, http.StatusCreated)
	var out authPayload
	decodeResponse(t, resp, &out)
	if out.Token == "" {
		t.Fatal("expected auth token")
	}
	if out.User.Role != store.RoleAdmin {
		t.Fatalf("expected first user role %q, got %q", store.RoleAdmin, out.User.Role)
	}

	meResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/auth/me", out.Token, nil)
	assertStatus(t, meResp, http.StatusOK)
	var me store.User
	decodeResponse(t, meResp, &me)
	if me.Role != store.RoleAdmin {
		t.Fatalf("expected /me role %q, got %q", store.RoleAdmin, me.Role)
	}
}

func TestRegisterRequiresInvitationAfterFirstUser(t *testing.T) {
	env := newAPITestEnv(t)
	registerUser(t, env, "first@example.com", "secret123", "First")

	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email":    "second@example.com",
		"password": "secret123",
		"name":     "Second",
	})
	assertStatus(t, resp, http.StatusForbidden)
	var errBody struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	decodeRaw(t, resp, &errBody)
	if errBody.Error.Code != "forbidden" {
		t.Fatalf("expected error code forbidden, got %q (%s)", errBody.Error.Code, resp.Body.String())
	}
}

func TestRegisterPasswordTooShort(t *testing.T) {
	env := newAPITestEnv(t)
	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email":    "founder@example.com",
		"password": "short",
		"name":     "Founder",
	})
	assertStatus(t, resp, http.StatusBadRequest)
}

func TestAdminEndpointsRequireAdminRole(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "alice@example.com", "secret123", "Alice")

	resp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/admin/users", alice.Token, nil)
	assertStatus(t, resp, http.StatusForbidden)

	patch := doJSONRequest(t, env.handler, http.MethodPatch, "/api/v1/admin/users/"+alice.User.ID, alice.Token, map[string]any{"role": "admin"})
	assertStatus(t, patch, http.StatusForbidden)
}

func TestAdminUserRoleManagement(t *testing.T) {
	env := newAPITestEnv(t)
	alice := promoteToAdmin(t, env, registerUser(t, env, "alice@example.com", "secret123", "Alice"))
	bob := registerUser(t, env, "bob@example.com", "secret123", "Bob")

	list := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/admin/users", alice.Token, nil)
	assertStatus(t, list, http.StatusOK)
	var users []struct {
		ID   string `json:"id"`
		Role string `json:"role"`
	}
	decodeResponse(t, list, &users)
	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(users))
	}
	roleByID := map[string]string{}
	for _, user := range users {
		roleByID[user.ID] = user.Role
	}
	if roleByID[alice.User.ID] != store.RoleAdmin || roleByID[bob.User.ID] != store.RoleUser {
		t.Fatalf("unexpected roles: %+v", roleByID)
	}

	// Demote the only admin -> rejected with 409.
	demote := doJSONRequest(t, env.handler, http.MethodPatch, "/api/v1/admin/users/"+alice.User.ID, alice.Token, map[string]any{"role": "user"})
	assertStatus(t, demote, http.StatusConflict)

	// Promote bob, then demoting alice succeeds.
	promote := doJSONRequest(t, env.handler, http.MethodPatch, "/api/v1/admin/users/"+bob.User.ID, alice.Token, map[string]any{"role": "admin"})
	assertStatus(t, promote, http.StatusOK)
	demote = doJSONRequest(t, env.handler, http.MethodPatch, "/api/v1/admin/users/"+alice.User.ID, alice.Token, map[string]any{"role": "user"})
	assertStatus(t, demote, http.StatusOK)

	// Invalid role -> 400.
	invalid := doJSONRequest(t, env.handler, http.MethodPatch, "/api/v1/admin/users/"+alice.User.ID, bob.Token, map[string]any{"role": "superadmin"})
	assertStatus(t, invalid, http.StatusBadRequest)
}

func TestSuperuserUIBlocked(t *testing.T) {
	env := newAPITestEnv(t)

	for _, path := range []string{"/_/", "/_/settings", "/_"} {
		resp := doJSONRequest(t, env.handler, http.MethodGet, path, "", nil)
		if resp.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for %s, got %d", path, resp.Code)
		}
	}
}

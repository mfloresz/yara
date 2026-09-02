package api

import (
	"strings"
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

// admin creates a fresh install's first user (admin) via HTTP registration.
func bootstrapAdmin(t *testing.T, env *apiTestEnv, email string) authPayload {
	t.Helper()
	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email":    email,
		"password": "secret123",
		"name":     "Admin",
	})
	assertStatus(t, resp, http.StatusCreated)
	var out authPayload
	decodeResponse(t, resp, &out)
	return out
}

func createInvitation(t *testing.T, env *apiTestEnv, admin authPayload, email, role string) (invitationPayload, string) {
	t.Helper()
	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/admin/invitations", admin.Token, map[string]any{
		"email": email,
		"role":  role,
	})
	assertStatus(t, resp, http.StatusCreated)
	var out struct {
		Invitation    invitationPayload `json:"invitation"`
		InvitationURL string            `json:"invitationUrl"`
	}
	decodeResponse(t, resp, &out)
	idx := strings.LastIndex(out.InvitationURL, "/invite/")
	if idx < 0 {
		t.Fatalf("invitation URL missing token: %q", out.InvitationURL)
	}
	return out.Invitation, out.InvitationURL[idx+len("/invite/"):]
}

type invitationPayload struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	ExpiresAt string `json:"expiresAt"`
	UsedAt    string `json:"usedAt"`
}

func TestInvitationLifecycle(t *testing.T) {
	env := newAPITestEnv(t)
	admin := bootstrapAdmin(t, env, "admin@example.com")

	// Admin creates an invitation; URL carries the raw token.
	invitation, rawToken := createInvitation(t, env, admin, "invited@example.com", "user")
	if invitation.Role != "user" || invitation.Email != "invited@example.com" {
		t.Fatalf("unexpected invitation payload: %+v", invitation)
	}
	if invitation.UsedAt != "" {
		t.Fatalf("expected unused invitation, got usedAt=%q", invitation.UsedAt)
	}

	// Validation succeeds and reveals email + role.
	validate := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/auth/invitations/validate", "", map[string]any{"token": rawToken})
	assertStatus(t, validate, http.StatusOK)
	var validated struct {
		Valid bool   `json:"valid"`
		Email string `json:"email"`
	}
	decodeResponse(t, validate, &validated)
	if !validated.Valid || validated.Email != "invited@example.com" {
		t.Fatalf("unexpected validation result: %+v", validated)
	}

	// Unknown token reports valid:false, not an error.
	unknown := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/auth/invitations/validate", "", map[string]any{"token": "nope"})
	assertStatus(t, unknown, http.StatusOK)
	var unknownOut struct {
		Valid bool `json:"valid"`
	}
	decodeResponse(t, unknown, &unknownOut)
	if unknownOut.Valid {
		t.Fatal("expected valid=false for unknown token")
	}

	// Accept creates the user with the invited role.
	accept := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/auth/invitations/accept", "", map[string]any{
		"token":    rawToken,
		"password": "secret123",
	})
	assertStatus(t, accept, http.StatusCreated)

	login := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/auth/login", "", map[string]any{
		"email":    "invited@example.com",
		"password": "secret123",
	})
	assertStatus(t, login, http.StatusOK)
	var invited authPayload
	decodeResponse(t, login, &invited)
	if invited.User.Role != "user" {
		t.Fatalf("expected invited user role user, got %q", invited.User.Role)
	}

	// The token is now used: validate and accept both fail.
	revalidate := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/auth/invitations/validate", "", map[string]any{"token": rawToken})
	var revalidated struct {
		Valid bool `json:"valid"`
	}
	decodeResponse(t, revalidate, &revalidated)
	if revalidated.Valid {
		t.Fatal("expected used invitation to validate as false")
	}
	reaccept := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/auth/invitations/accept", "", map[string]any{
		"token":    rawToken,
		"password": "secret456",
	})
	assertStatus(t, reaccept, http.StatusBadRequest)
}

func TestInvitationAcceptAdminRole(t *testing.T) {
	env := newAPITestEnv(t)
	admin := bootstrapAdmin(t, env, "admin@example.com")

	_, rawToken := createInvitation(t, env, admin, "second-admin@example.com", "admin")
	accept := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/auth/invitations/accept", "", map[string]any{
		"token":    rawToken,
		"password": "secret123",
	})
	assertStatus(t, accept, http.StatusCreated)
	var out struct {
		Role string `json:"role"`
	}
	decodeResponse(t, accept, &out)
	if out.Role != "admin" {
		t.Fatalf("expected admin role, got %q", out.Role)
	}
}

func TestInvitationRevoke(t *testing.T) {
	env := newAPITestEnv(t)
	admin := bootstrapAdmin(t, env, "admin@example.com")
	invitation, rawToken := createInvitation(t, env, admin, "revoked@example.com", "user")

	del := doJSONRequest(t, env.handler, http.MethodDelete, "/api/v1/admin/invitations/"+invitation.ID, admin.Token, nil)
	assertStatus(t, del, http.StatusNoContent)

	validate := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/auth/invitations/validate", "", map[string]any{"token": rawToken})
	var out struct {
		Valid bool `json:"valid"`
	}
	decodeResponse(t, validate, &out)
	if out.Valid {
		t.Fatal("expected revoked invitation to validate as false")
	}
}

func TestInvitationDuplicateEmailRejected(t *testing.T) {
	env := newAPITestEnv(t)
	admin := bootstrapAdmin(t, env, "admin@example.com")
	registerUser(t, env, "taken@example.com", "secret123", "Taken")

	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/admin/invitations", admin.Token, map[string]any{
		"email": "taken@example.com",
		"role":  "user",
	})
	assertStatus(t, resp, http.StatusConflict)
}

func TestInvitationAcceptShortPasswordRejected(t *testing.T) {
	env := newAPITestEnv(t)
	admin := bootstrapAdmin(t, env, "admin@example.com")
	_, rawToken := createInvitation(t, env, admin, "shortpass@example.com", "user")

	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/auth/invitations/accept", "", map[string]any{
		"token":    rawToken,
		"password": "short",
	})
	assertStatus(t, resp, http.StatusBadRequest)

	// The invitation must still be redeemable after a failed accept.
	retry := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/auth/invitations/accept", "", map[string]any{
		"token":    rawToken,
		"password": "secret123",
	})
	assertStatus(t, retry, http.StatusCreated)
}

func TestInvitationListAdminOnly(t *testing.T) {
	env := newAPITestEnv(t)
	admin := bootstrapAdmin(t, env, "admin@example.com")
	createInvitation(t, env, admin, "someone@example.com", "user")
	nonAdmin := registerUser(t, env, "peasant@example.com", "secret123", "Peasant")

	resp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/admin/invitations", nonAdmin.Token, nil)
	assertStatus(t, resp, http.StatusForbidden)

	list := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/admin/invitations", admin.Token, nil)
	assertStatus(t, list, http.StatusOK)
	var out []invitationPayload
	decodeResponse(t, list, &out)
	if len(out) != 1 {
		t.Fatalf("expected 1 invitation, got %d", len(out))
	}
}

func TestSharedProviderKeysLifecycle(t *testing.T) {
	env := newAPITestEnv(t)
	admin := bootstrapAdmin(t, env, "admin@example.com")
	user := registerUser(t, env, "reader@example.com", "secret123", "Reader")

	// Unknown provider -> 404.
	unknown := doJSONRequest(t, env.handler, http.MethodPut, "/api/v1/admin/provider-keys/nope", admin.Token, map[string]any{"apiKey": "sk-123", "shared": true})
	assertStatus(t, unknown, http.StatusNotFound)

	// First configuration requires a key.
	noKey := doJSONRequest(t, env.handler, http.MethodPut, "/api/v1/admin/provider-keys/venice", admin.Token, map[string]any{"apiKey": "", "shared": true})
	assertStatus(t, noKey, http.StatusBadRequest)

	// Configure + share.
	put := doJSONRequest(t, env.handler, http.MethodPut, "/api/v1/admin/provider-keys/venice", admin.Token, map[string]any{"apiKey": "sk-shared-1", "shared": true})
	assertStatus(t, put, http.StatusOK)

	// Non-admin cannot manage provider keys.
	forbidden := doJSONRequest(t, env.handler, http.MethodPut, "/api/v1/admin/provider-keys/venice", user.Token, map[string]any{"apiKey": "sk-x", "shared": true})
	assertStatus(t, forbidden, http.StatusForbidden)

	// The admin list reports configured+shared, never the key itself.
	list := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/admin/provider-keys", admin.Token, nil)
	assertStatus(t, list, http.StatusOK)
	type keyEntry struct {
		Provider   string `json:"provider"`
		Configured bool   `json:"configured"`
		Shared     bool   `json:"shared"`
	}
	var entries []keyEntry
	decodeResponse(t, list, &entries)
	var venice *keyEntry
	for i := range entries {
		if entries[i].Provider == "venice" {
			venice = &entries[i]
		}
	}
	if venice == nil || !venice.Configured || !venice.Shared {
		t.Fatalf("expected venice configured+shared, got %+v", venice)
	}
	if strings.Contains(list.Body.String(), "sk-shared-1") {
		t.Fatal("shared key plaintext leaked in admin list response")
	}

	// User sees sharedKeyAvailable and usingSharedKey flags.
	providers := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/providers", user.Token, nil)
	assertStatus(t, providers, http.StatusOK)
	var providerOut struct {
		Providers []struct {
			Provider           string `json:"provider"`
			APIKeyConfigured   bool   `json:"apiKeyConfigured"`
			SharedKeyAvailable bool   `json:"sharedKeyAvailable"`
			UsingSharedKey     bool   `json:"usingSharedKey"`
		} `json:"providers"`
	}
	decodeResponse(t, providers, &providerOut)
	var userVenice *struct {
		Provider           string `json:"provider"`
		APIKeyConfigured   bool   `json:"apiKeyConfigured"`
		SharedKeyAvailable bool   `json:"sharedKeyAvailable"`
		UsingSharedKey     bool   `json:"usingSharedKey"`
	}
	for i := range providerOut.Providers {
		if providerOut.Providers[i].Provider == "venice" {
			userVenice = &providerOut.Providers[i]
		}
	}
	if userVenice == nil || !userVenice.SharedKeyAvailable || !userVenice.UsingSharedKey {
		t.Fatalf("expected venice shared+using for user, got %+v", userVenice)
	}

	// Resolution order: shared key used when the user has none.
	settings, err := env.store.ResolveProviderAISettings(user.User.ID, "venice")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if settings.APIKey != "sk-shared-1" {
		t.Fatalf("expected shared key fallback, got %q", settings.APIKey)
	}

	// The user's own key takes precedence.
	if _, err := env.store.ReplaceProviderAPIKey(user.User.ID, "venice", "sk-own-1"); err != nil {
		t.Fatalf("replace user key: %v", err)
	}
	settings, err = env.store.ResolveProviderAISettings(user.User.ID, "venice")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if settings.APIKey != "sk-own-1" {
		t.Fatalf("expected own key to win, got %q", settings.APIKey)
	}

	// Un-sharing removes the fallback for the user.
	unshare := doJSONRequest(t, env.handler, http.MethodPut, "/api/v1/admin/provider-keys/venice", admin.Token, map[string]any{"shared": false})
	assertStatus(t, unshare, http.StatusOK)
	if err := env.store.DeleteProviderAPIKey(user.User.ID, "venice"); err != nil {
		t.Fatalf("delete user key: %v", err)
	}
	settings, err = env.store.ResolveProviderAISettings(user.User.ID, "venice")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if settings.APIKey != "" {
		t.Fatalf("expected no key after unshare, got %q", settings.APIKey)
	}

	// Deleting the shared entry removes it from the admin list.
	del := doJSONRequest(t, env.handler, http.MethodDelete, "/api/v1/admin/provider-keys/venice", admin.Token, nil)
	assertStatus(t, del, http.StatusNoContent)
	list = doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/admin/provider-keys", admin.Token, nil)
	if strings.Contains(list.Body.String(), `"shared":true`) {
		t.Fatal("expected no shared entries after delete")
	}
}

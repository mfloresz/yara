package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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
	// The session token must only travel in the HttpOnly cookie, never in the
	// response body where XSS-readable payloads could exfiltrate it.
	if out.Token != "" {
		t.Fatal("expected no token in the register response body")
	}
	if out.User.Role != store.RoleAdmin {
		t.Fatalf("expected first user role %q, got %q", store.RoleAdmin, out.User.Role)
	}

	cookie := findAuthCookie(t, resp)
	meResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/auth/me", cookie, nil)
	assertStatus(t, meResp, http.StatusOK)
	var me store.User
	decodeResponse(t, meResp, &me)
	if me.Role != store.RoleAdmin {
		t.Fatalf("expected /me role %q, got %q", store.RoleAdmin, me.Role)
	}
}

// findAuthCookie returns the session token from the auth.token Set-Cookie
// header of a login/register/refresh response.
func findAuthCookie(t *testing.T, resp *httptest.ResponseRecorder) string {
	t.Helper()
	for _, cookie := range resp.Result().Cookies() {
		if cookie.Name == authCookieName && cookie.Value != "" {
			return cookie.Value
		}
	}
	t.Fatalf("expected %s cookie in response", authCookieName)
	return ""
}

// TestUserCannotSelfPromoteViaPocketBaseAPI locks in the fix for the role
// escalation: users.UpdateRule allows self-updates, so the role field must be
// hidden — PocketBase strips hidden fields from non-superuser writes, and the
// native REST surface must not be able to grant admin.
func TestUserCannotSelfPromoteViaPocketBaseAPI(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "alice-escalate@example.com", "secret123", "Alice")

	resp := doJSONRequest(t, env.handler, http.MethodPatch,
		"/api/collections/users/records/"+alice.User.ID, alice.Token,
		map[string]any{"role": store.RoleAdmin})
	assertStatus(t, resp, http.StatusOK)
	if strings.Contains(resp.Body.String(), `"role"`) {
		t.Fatalf("expected hidden role field to be absent from the response, got %s", resp.Body.String())
	}

	record, err := env.store.App.FindRecordById("users", alice.User.ID)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if got := record.GetString("role"); got != store.RoleUser {
		t.Fatalf("expected role to remain %q after self-PATCH, got %q", store.RoleUser, got)
	}
}

// TestRateLimitIgnoresSpoofedForwardedHeaders verifies that a direct (non-
// loopback) connection cannot rotate its rate-limit key by faking
// CF-Connecting-IP / X-Forwarded-For: after burning the login budget, a
// request with a fresh spoofed IP must still be rejected.
func TestRateLimitIgnoresSpoofedForwardedHeaders(t *testing.T) {
	env := newAPITestEnv(t)

	login := func(spoofedIP string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"email":"nobody@example.com","password":"wrong"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("CF-Connecting-IP", spoofedIP)
		req.Header.Set("X-Forwarded-For", spoofedIP)
		resp := httptest.NewRecorder()
		env.handler.ServeHTTP(resp, req)
		return resp
	}

	for i := 0; i < 5; i++ {
		if resp := login(fmt.Sprintf("10.0.0.%d", i+1)); resp.Code == http.StatusTooManyRequests {
			t.Fatalf("unexpected 429 on attempt %d", i+1)
		}
	}
	if resp := login("10.0.0.200"); resp.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 with spoofed forwarded IP after budget exhausted, got %d", resp.Code)
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
	// The session token is only delivered in the HttpOnly cookie now.
	out.Token = findAuthCookie(t, resp)
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

func TestGlobalPromptOverridesAndReset(t *testing.T) {
	env := newAPITestEnv(t)
	admin := bootstrapAdmin(t, env, "admin@example.com")
	user := registerUser(t, env, "writer@example.com", "secret123", "Writer")

	// Non-admin cannot manage overrides.
	forbidden := doJSONRequest(t, env.handler, http.MethodPut, "/api/v1/admin/prompt-overrides/translation", user.Token, map[string]any{
		"prompt": map[string]any{"systemPrompt": "ADMIN SYS", "userPrompt": ""},
	})
	assertStatus(t, forbidden, http.StatusForbidden)

	// Unknown key -> 400.
	unknown := doJSONRequest(t, env.handler, http.MethodPut, "/api/v1/admin/prompt-overrides/bogus", admin.Token, map[string]any{
		"prompt": map[string]any{"systemPrompt": "X"},
	})
	assertStatus(t, unknown, http.StatusBadRequest)

	// Set a global override.
	put := doJSONRequest(t, env.handler, http.MethodPut, "/api/v1/admin/prompt-overrides/translation", admin.Token, map[string]any{
		"prompt": map[string]any{"systemPrompt": "ADMIN TRANSLATION SYS", "userPrompt": "ADMIN TRANSLATION USER"},
	})
	assertStatus(t, put, http.StatusOK)

	// The user now sees the global override.
	prompts := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/prompts", user.Token, nil)
	assertStatus(t, prompts, http.StatusOK)
	type promptEntry struct {
		Key    string `json:"key"`
		Prompt struct {
			SystemPrompt string `json:"systemPrompt"`
			UserPrompt   string `json:"userPrompt"`
		} `json:"prompt"`
	}
	var list []promptEntry
	decodeResponse(t, prompts, &list)
	findPrompt := func(key string) *promptEntry {
		for i := range list {
			if list[i].Key == key {
				return &list[i]
			}
		}
		return nil
	}
	entry := findPrompt("translation")
	if entry == nil || entry.Prompt.SystemPrompt != "ADMIN TRANSLATION SYS" {
		t.Fatalf("expected global override to apply, got %+v", entry)
	}

	// The user's own setting beats the global.
	upsert := doJSONRequest(t, env.handler, http.MethodPut, "/api/v1/prompts/translation", user.Token, map[string]any{
		"label":  "Traducción",
		"prompt": map[string]any{"systemPrompt": "USER SYS", "userPrompt": "USER USER"},
		"active": true,
	})
	assertStatus(t, upsert, http.StatusOK)
	prompts = doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/prompts", user.Token, nil)
	decodeResponse(t, prompts, &list)
	entry = findPrompt("translation")
	if entry == nil || entry.Prompt.SystemPrompt != "USER SYS" {
		t.Fatalf("expected user override to win, got %+v", entry.Prompt)
	}

	// Runtime resolution: effective prompts honor user > global > embedded.
	novel := createNovel(t, env.handler, user.Token, "Novela", "es", "en")
	effective, err := env.store.GetEffectivePrompts(user.User.ID, &store.Novel{ID: novel.ID, OwnerID: user.User.ID})
	if err != nil {
		t.Fatalf("effective prompts: %v", err)
	}
	for _, p := range effective {
		if p.Key == "translation" && p.SystemPrompt != "USER SYS" {
			t.Fatalf("expected USER SYS effective, got %q", p.SystemPrompt)
		}
	}

	// Reset deletes only the user's row: the global applies again.
	reset := doJSONRequest(t, env.handler, http.MethodDelete, "/api/v1/prompts/translation", user.Token, nil)
	assertStatus(t, reset, http.StatusOK)
	prompts = doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/prompts", user.Token, nil)
	decodeResponse(t, prompts, &list)
	entry = findPrompt("translation")
	if entry == nil || entry.Prompt.SystemPrompt != "ADMIN TRANSLATION SYS" {
		t.Fatalf("expected global after reset, got %+v", entry.Prompt)
	}

	// Per-novel prompts still win over everything.
	effective, err = env.store.GetEffectivePrompts(user.User.ID, &store.Novel{ID: novel.ID, OwnerID: user.User.ID, TranslationSystemPrompt: "NOVEL SYS"})
	if err != nil {
		t.Fatalf("effective prompts: %v", err)
	}
	for _, p := range effective {
		if p.Key == "translation" && p.SystemPrompt != "NOVEL SYS" {
			t.Fatalf("expected novel prompt to win, got %q", p.SystemPrompt)
		}
	}

	// Admin deletes the override: embedded default applies again.
	del := doJSONRequest(t, env.handler, http.MethodDelete, "/api/v1/admin/prompt-overrides/translation", admin.Token, nil)
	assertStatus(t, del, http.StatusNoContent)
	prompts = doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/prompts", user.Token, nil)
	decodeResponse(t, prompts, &list)
	entry = findPrompt("translation")
	if entry == nil || entry.Prompt.SystemPrompt != store.DefaultTranslationSystemPrompt {
		t.Fatalf("expected embedded default after admin delete, got %q", entry.Prompt.SystemPrompt)
	}
}

func TestSecurityHeaders(t *testing.T) {
	env := newAPITestEnv(t)

	resp := doJSONRequest(t, env.handler, http.MethodGet, "/healthz", "", nil)
	csp := resp.Header().Get("Content-Security-Policy")
	if resp.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatalf("expected X-Frame-Options DENY, got %q", resp.Header().Get("X-Frame-Options"))
	}
	if resp.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("expected Referrer-Policy no-referrer, got %q", resp.Header().Get("Referrer-Policy"))
	}
	if resp.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("expected nosniff, got %q", resp.Header().Get("X-Content-Type-Options"))
	}
	if csp == "" || !strings.Contains(csp, "default-src 'self'") {
		t.Fatalf("expected CSP with default-src 'self', got %q", csp)
	}
	if resp.Header().Get("Strict-Transport-Security") != "" {
		t.Fatal("HSTS must not be sent over plain HTTP requests")
	}

	// Behind a proxy that terminates TLS, HSTS is enabled.
	hstsReq := doRequestWithHeader(t, env.handler, http.MethodGet, "/healthz", "", "X-Forwarded-Proto", "https", nil)
	if hstsReq.Header().Get("Strict-Transport-Security") == "" {
		t.Fatal("expected HSTS when X-Forwarded-Proto is https")
	}
}

func TestLoginRateLimited(t *testing.T) {
	env := newAPITestEnv(t)
	registerUser(t, env, "victim@example.com", "secret123", "Victim")

	// Burn through the per-IP login budget (capacity 5).
	for i := 0; i < 5; i++ {
		resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/auth/login", "", map[string]any{
			"email":    "victim@example.com",
			"password": "wrong-password",
		})
		if resp.Code == http.StatusTooManyRequests {
			t.Fatalf("unexpected 429 on attempt %d", i+1)
		}
	}

	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/auth/login", "", map[string]any{
		"email":    "victim@example.com",
		"password": "wrong-password",
	})
	assertStatus(t, resp, http.StatusTooManyRequests)
	if resp.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header on 429")
	}
}

func TestAuthBodySizeCapped(t *testing.T) {
	env := newAPITestEnv(t)

	big := strings.Repeat("a", 20_000)
	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/auth/login", "", map[string]any{
		"email":    "x@example.com",
		"password": big,
	})
	if resp.Code >= 500 {
		t.Fatalf("expected client error for oversized body, got %d", resp.Code)
	}
}

func doRequestWithHeader(t *testing.T, handler http.Handler, method, path, token, headerKey, headerValue string, body any) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if headerKey != "" {
		req.Header.Set(headerKey, headerValue)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	return resp
}

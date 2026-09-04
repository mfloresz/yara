package api

import (
	"net/http"
	"strings"
	"testing"
)
// Logout must clear the session cookie with the same Secure attribute it was
// set with: behind HTTPS (tunnel / reverse proxy) a non-Secure clear leaves
// the Secure session cookie alive in the browser.
func TestLogoutClearsSecureCookieOnHTTPS(t *testing.T) {
	env := newAPITestEnv(t)
	user := registerUser(t, env, "logout-secure@example.com", "secret123", "Logout")

	resp := doRequestWithHeader(t, env.handler, http.MethodPost, "/api/v1/auth/logout", user.Token, "X-Forwarded-Proto", "https", nil)
	assertStatus(t, resp, http.StatusNoContent)

	found := false
	for _, h := range resp.Result().Header.Values("Set-Cookie") {
		if strings.HasPrefix(h, authCookieName+"=") {
			found = true
			if !strings.Contains(strings.ToLower(h), "secure") {
				t.Fatalf("expected Secure on cleared cookie behind https, got %q", h)
			}
		}
	}
	if !found {
		t.Fatalf("expected %s Set-Cookie in logout response", authCookieName)
	}
}

// The admin-managed collections are guarded by PocketBase rules referencing
// the hidden users.role field. PocketBase list rules act as filters, so the
// property to lock is fail-closed: a regular user must see zero rows and no
// key material through the native REST surface (the Go layer under
// /api/v1/admin is the only functional path).
func TestNativeAdminCollectionsForbiddenForUsers(t *testing.T) {
	env := newAPITestEnv(t)
	admin := bootstrapAdmin(t, env, "root-native@example.com")
	user := registerUser(t, env, "plain-native@example.com", "secret123", "Plain")
	invitation, _ := createInvitation(t, env, admin, "someone@example.com", "user")

	for _, path := range []string{
		"/api/collections/invitations/records",
		"/api/collections/shared_provider_keys/records",
		"/api/collections/prompt_overrides/records",
	} {
		resp := doJSONRequest(t, env.handler, http.MethodGet, path, user.Token, nil)
		body := resp.Body.String()
		if strings.Contains(body, "token_hash") || strings.Contains(body, "api_key_encrypted") {
			t.Fatalf("GET %s: key material leaked to non-admin: %s", path, body)
		}
		if !strings.Contains(body, `"totalItems":0`) {
			t.Fatalf("GET %s: expected zero rows for non-admin, got: %s", path, body)
		}
	}

	// Single-record views must 404 (rule-filtered), never expose the row.
	view := doJSONRequest(t, env.handler, http.MethodGet, "/api/collections/invitations/records/"+invitation.ID, user.Token, nil)
	assertStatus(t, view, http.StatusNotFound)
}

// Marking the invitation used happens before user creation, so a second
// redemption of the same token must always fail.
func TestInvitationDoubleRedeemRejected(t *testing.T) {
	env := newAPITestEnv(t)
	admin := bootstrapAdmin(t, env, "admin-double@example.com")
	_, rawToken := createInvitation(t, env, admin, "twice@example.com", "user")

	if _, err := env.store.RedeemInvitation(rawToken, "secret123"); err != nil {
		t.Fatalf("first redeem: %v", err)
	}
	if _, err := env.store.RedeemInvitation(rawToken, "secret456"); err == nil {
		t.Fatal("expected second redeem of the same token to fail")
	}
}

// Reset is idempotent: without an own override the effective prompt already
// applies, so repeating DELETE returns 200 with the same content.
func TestResetPromptIdempotent(t *testing.T) {
	env := newAPITestEnv(t)
	bootstrapAdmin(t, env, "admin-reset@example.com")
	user := registerUser(t, env, "reset-idem@example.com", "secret123", "Reset")

	first := doJSONRequest(t, env.handler, http.MethodDelete, "/api/v1/prompts/translation", user.Token, nil)
	assertStatus(t, first, http.StatusOK)
	second := doJSONRequest(t, env.handler, http.MethodDelete, "/api/v1/prompts/translation", user.Token, nil)
	assertStatus(t, second, http.StatusOK)
	if first.Body.String() != second.Body.String() {
		t.Fatalf("expected identical effective prompt on repeated reset, got %q vs %q", first.Body.String(), second.Body.String())
	}
}

// When PUBLIC_URL is configured, invitation links use it instead of the
// client-controlled request Host.
func TestInvitationURLUsesPublicBaseURL(t *testing.T) {
	env := newAPITestEnv(t)
	admin := bootstrapAdmin(t, env, "admin-publicurl@example.com")
	env.server.Cfg.PublicBaseURL = "https://novels.example.com"

	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/admin/invitations", admin.Token, map[string]any{
		"email": "publicurl@example.com",
		"role":  "user",
	})
	assertStatus(t, resp, http.StatusCreated)
	var out struct {
		InvitationURL string `json:"invitationUrl"`
	}
	decodeResponse(t, resp, &out)
	if !strings.HasPrefix(out.InvitationURL, "https://novels.example.com/invite/") {
		t.Fatalf("expected public origin in invitation URL, got %q", out.InvitationURL)
	}
}

// Upgrade attempts share one limiter bucket per client IP: a peer looping
// reconnects must hit 429 instead of squatting every unauthenticated slot.
// Plain GETs fail the WS handshake (400) but still consume limiter tokens,
// which is exactly what the guard counts.
func TestBrowserWorkerWSRateLimitedPerIP(t *testing.T) {
	env := newAPITestEnv(t)

	limited := false
	for range 20 {
		resp := doJSONRequest(t, env.handler, http.MethodGet, "/ws/browser-worker", "", nil)
		if resp.Code == http.StatusTooManyRequests {
			limited = true
			if resp.Header().Get("Retry-After") == "" {
				t.Fatal("expected Retry-After on rate-limited WS upgrade")
			}
			break
		}
	}
	if !limited {
		t.Fatal("expected per-IP rate limiting on /ws/browser-worker after 16 attempts")
	}
}

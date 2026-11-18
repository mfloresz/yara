package api

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func TestWorkerAuthClientStateValidation(t *testing.T) {
	env := newAPITestEnv(t)
	for _, state := range []string{"", strings.Repeat("a", 63), strings.Repeat("A", 64), strings.Repeat("g", 64), strings.Repeat("a", 65)} {
		response := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/worker-auth/authorize?extension_id=test-extension&client_state="+url.QueryEscape(state), "", nil)
		assertStatus(t, response, http.StatusBadRequest)
	}
	state := strings.Repeat("a", 64)
	response := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/worker-auth/authorize?extension_id=test-extension&client_state="+state, "", nil)
	assertStatus(t, response, http.StatusOK)
	href := regexp.MustCompile(`href="([^"]+)"`).FindStringSubmatch(response.Body.String())
	if len(href) != 2 {
		t.Fatal("login redirect missing")
	}
	login, err := url.Parse(html.UnescapeString(href[1]))
	if err != nil {
		t.Fatal(err)
	}
	target, err := url.Parse(login.Query().Get("redirect"))
	if err != nil {
		t.Fatal(err)
	}
	if target.Path != "/api/v1/worker-auth/authorize" || target.Query().Get("client_state") != state || target.Query().Get("extension_id") != "test-extension" {
		t.Fatal("login redirect dropped authorization state")
	}
}

func TestWorkerAuthClientStateRoundTrip(t *testing.T) {
	env := newAPITestEnv(t)
	user := registerUser(t, env, "worker-state@example.com", "secret123", "User")
	other := registerUser(t, env, "worker-state-other@example.com", "secret123", "Other")
	clientState := strings.Repeat("0123456789abcdef", 4)
	authorize := func() string {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/worker-auth/authorize?extension_id=test-extension&client_state="+clientState, nil)
		req.AddCookie(&http.Cookie{Name: authCookieName, Value: user.Token})
		response := httptest.NewRecorder()
		env.handler.ServeHTTP(response, req)
		assertStatus(t, response, http.StatusOK)
		state := regexp.MustCompile(`name="state" value="([^"]+)"`).FindStringSubmatch(response.Body.String())
		if len(state) != 2 {
			t.Fatal("consent state missing")
		}
		t.Cleanup(func() { pendingAuthsMu.Lock(); delete(pendingAuths, state[1]); pendingAuthsMu.Unlock() })
		return state[1]
	}
	approve := func(state, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, workerAuthApprovePath, strings.NewReader(url.Values{"state": {state}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: authCookieName, Value: token})
		response := httptest.NewRecorder()
		env.handler.ServeHTTP(response, req)
		return response
	}
	response := approve(authorize(), user.Token)
	assertStatus(t, response, http.StatusOK)
	callback := regexp.MustCompile(`http-equiv="refresh" content="1;url=([^"]+)"`).FindStringSubmatch(response.Body.String())
	if len(callback) != 2 {
		t.Fatal("callback redirect missing")
	}
	target, err := url.Parse(html.UnescapeString(callback[1]))
	if err != nil {
		t.Fatal(err)
	}
	if target.Path != "/api/v1/worker-auth/callback" || target.Query().Get("client_state") != clientState || target.Query().Get("user") != user.User.ID || target.Query().Get("token") == "" {
		t.Fatal("callback dropped authorization state")
	}
	assertStatus(t, doJSONRequest(t, env.handler, http.MethodGet, target.String(), "", nil), http.StatusOK)
	assertStatus(t, approve(authorize(), other.Token), http.StatusBadRequest)
	targetQuery := target.Query()
	targetQuery.Del("client_state")
	target.RawQuery = targetQuery.Encode()
	assertStatus(t, doJSONRequest(t, env.handler, http.MethodGet, target.String(), "", nil), http.StatusBadRequest)
}

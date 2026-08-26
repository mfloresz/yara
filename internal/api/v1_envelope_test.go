package api

import (
	"net/http"
	"testing"

	"translator-server/internal/store"
)

// TestV1NovelListEnvelope verifies the v1 list response shape:
//   {data: [...], meta: {total, page, per_page, has_more}, links: {self, next}}
// vs. the legacy {items: [...], hasMore: bool} shape.
func TestV1NovelListEnvelope(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-v1-envelope@example.com", "secret123", "Alice")
	createNovel(t, env.handler, alice.Token, "Envolvente", "es", "en")

	// Legacy shape.
	legacyResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/db/novels", alice.Token, nil)
	assertStatus(t, legacyResp, http.StatusOK)
	var legacy struct {
		Items   []map[string]any `json:"items"`
		HasMore bool             `json:"hasMore"`
	}
	decodeResponse(t, legacyResp, &legacy)
	if len(legacy.Items) != 1 {
		t.Fatalf("legacy list expected 1 item, got %d", len(legacy.Items))
	}

	// v1 shape.
	v1Resp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels", alice.Token, nil)
	assertStatus(t, v1Resp, http.StatusOK)
	var v1 struct {
		Data  []map[string]any `json:"data"`
		Meta  *v1Meta          `json:"meta"`
		Links *v1Links         `json:"links"`
	}
	decodeResponse(t, v1Resp, &v1)
	if v1.Data == nil || len(v1.Data) != 1 {
		t.Fatalf("v1 list expected data[1], got %+v", v1.Data)
	}
	if v1.Meta == nil {
		t.Fatal("v1 list response missing meta")
	}
	if v1.Meta.Page != 1 || v1.Meta.PerPage != 50 {
		t.Fatalf("v1 meta.page=%d per_page=%d, want 1/50", v1.Meta.Page, v1.Meta.PerPage)
	}
	if v1.Links == nil || v1.Links.Self == "" {
		t.Fatalf("v1 list response missing links.self")
	}
}

// TestV1NovelCreateReturns201AndLocation verifies that POST /api/v1/novels
// returns 201 Created plus a Location header pointing at the new resource.
func TestV1NovelCreateReturns201AndLocation(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-v1-create@example.com", "secret123", "Alice")

	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels", alice.Token, map[string]any{
		"sourceTitle":    "V1",
		"sourceLanguage": "en",
		"targetLanguage": "es",
	})
	assertStatus(t, resp, http.StatusCreated)
	loc := resp.Header().Get("Location")
	if loc == "" {
		t.Fatal("expected Location header on v1 novel create")
	}
	if len(loc) < len("/api/v1/novels/") || loc[:len("/api/v1/novels/")] != "/api/v1/novels/" {
		t.Fatalf("Location header does not point to /api/v1/novels/: %q", loc)
	}
}

// TestV1NovelDeleteReturns204 verifies v1 DELETE returns 204 with no body,
// while legacy DELETE keeps the {ok:true} 200 response for backward compat.
func TestV1NovelDeleteReturns204(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-v1-delete@example.com", "secret123", "Alice")
	novel := createNovel(t, env.handler, alice.Token, "Para borrar", "es", "en")

	resp := doJSONRequest(t, env.handler, http.MethodDelete, "/api/v1/novels/"+novel.ID, alice.Token, nil)
	assertStatus(t, resp, http.StatusNoContent)
	if body := readBody(t, resp); body != "" {
		t.Fatalf("v1 DELETE should have empty body, got %q", body)
	}
}

// TestV1NovelFieldsSparseFieldsets verifies ?fields= filters down the
// response payload (sparse fieldsets for lightweight lists).
func TestV1NovelFieldsSparseFieldsets(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-v1-fields@example.com", "secret123", "Alice")
	createNovel(t, env.handler, alice.Token, "Ligero", "en", "es")

	resp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels?fields=id,sourceTitle,status", alice.Token, nil)
	assertStatus(t, resp, http.StatusOK)
	var v1 struct {
		Data []map[string]any `json:"data"`
	}
	decodeResponse(t, resp, &v1)
	if len(v1.Data) != 1 {
		t.Fatalf("expected 1 novel, got %d", len(v1.Data))
	}
	row := v1.Data[0]
	if _, ok := row["id"]; !ok {
		t.Fatal("fields= response missing id")
	}
	if _, ok := row["sourceTitle"]; !ok {
		t.Fatal("fields= response missing sourceTitle")
	}
	// Heavy fields must be absent.
	if _, ok := row["glossary"]; ok {
		t.Fatal("fields= response should not include glossary")
	}
	if _, ok := row["aiOptions"]; ok {
		t.Fatal("fields= response should not include aiOptions")
	}
}

// TestV1ChapterSummariesPagination verifies that ?page&per_page emits meta
// and links.
func TestV1ChapterSummariesPagination(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-v1-chapters@example.com", "secret123", "Alice")
	novel := createNovel(t, env.handler, alice.Token, "Paginada", "es", "en")
	for i := 1; i <= 3; i++ {
		createChapter(t, env.handler, alice.Token, novel.ID, i)
	}

	resp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels/"+novel.ID+"/chapter-summaries?page=1&per_page=2", alice.Token, nil)
	assertStatus(t, resp, http.StatusOK)
	var v1 struct {
		Data  []map[string]any `json:"data"`
		Meta  *v1Meta          `json:"meta"`
		Links *v1Links         `json:"links"`
	}
	decodeResponse(t, resp, &v1)
	if len(v1.Data) != 2 {
		t.Fatalf("expected 2 chapters in page, got %d", len(v1.Data))
	}
	if v1.Meta == nil || v1.Meta.Total != 3 {
		t.Fatalf("expected meta.total=3, got %+v", v1.Meta)
	}
	if !v1.Meta.HasMore {
		t.Fatalf("expected meta.has_more=true, got %v", v1.Meta.HasMore)
	}
	if v1.Links == nil || v1.Links.Next == "" {
		t.Fatalf("expected links.next, got %+v", v1.Links)
	}
}

// TestV1DeprecationHeaders verifies the legacy /api/db/* responses carry the
// Deprecation / Sunset / Link trio so clients can migrate at their own pace.
func TestV1DeprecationHeaders(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-deprecation@example.com", "secret123", "Alice")

	resp := doJSONRequest(t, env.handler, http.MethodGet, "/api/db/novels", alice.Token, nil)
	assertStatus(t, resp, http.StatusOK)
	if got := resp.Header().Get("Deprecation"); got != "true" {
		t.Fatalf("expected Deprecation=true on legacy route, got %q", got)
	}
	if got := resp.Header().Get("Sunset"); got == "" {
		t.Fatal("expected Sunset header on legacy route")
	}
	if got := resp.Header().Get("Link"); got == "" {
		t.Fatal("expected Link successor-version on legacy route")
	}

	v1Resp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels", alice.Token, nil)
	assertStatus(t, v1Resp, http.StatusOK)
	if got := v1Resp.Header().Get("X-API-Version"); got != "v1" {
		t.Fatalf("expected X-API-Version=v1 on v1 route, got %q", got)
	}
	if got := v1Resp.Header().Get("Deprecation"); got != "" {
		t.Fatalf("v1 route should not carry Deprecation header, got %q", got)
	}
}

// TestV1JobCancelAndRetryEndpoints verifies the new POST /jobs/{id}/cancel
// and POST /jobs/{id}/retry endpoints work and return the v1 envelope.
func TestV1JobCancelAndRetryEndpoints(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-v1-jobs@example.com", "secret123", "Alice")
	novel := createNovel(t, env.handler, alice.Token, "Trabajos", "es", "en")

	// Create a job directly in failed state (the in-process worker would
	// otherwise race a retry).
	job := &store.Job{NovelID: novel.ID, Status: "failed", Operation: "translate"}
	if err := env.store.CreateJob(alice.User.ID, job); err != nil {
		t.Fatalf("create direct job: %v", err)
	}

	cancelResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/jobs/"+job.ID+"/cancel", alice.Token, nil)
	assertStatus(t, cancelResp, http.StatusOK)
	var cancelled struct {
		Data map[string]any `json:"data"`
	}
	decodeResponse(t, cancelResp, &cancelled)
	if cancelled.Data["status"] != "cancelled" {
		t.Fatalf("expected job status=cancelled, got %v", cancelled.Data["status"])
	}

	// retry: failed -> pending and re-queued.
	retryResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/jobs/"+job.ID+"/retry", alice.Token, nil)
	assertStatus(t, retryResp, http.StatusOK)
	var retried struct {
		Data map[string]any `json:"data"`
	}
	decodeResponse(t, retryResp, &retried)
	if retried.Data["status"] != "pending" {
		t.Fatalf("expected job status=pending after retry, got %v", retried.Data["status"])
	}

	// retry on already-active job -> 409 conflict.
	if status := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/jobs/"+job.ID+"/retry", alice.Token, nil).Code; status != http.StatusConflict {
		t.Fatalf("expected 409 Conflict on retry of pending job, got %d", status)
	}
}

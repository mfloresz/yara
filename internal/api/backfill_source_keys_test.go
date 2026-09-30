package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"translator-server/internal/store"
)

func TestBackfillSourceKeyPlan(t *testing.T) {
	threeChapters := []sourceChapter{
		{Title: "Prologue", URL: "https://s/1", Key: "https://s/1", Order: 1},
		{Title: "Growth", URL: "https://s/2", Key: "https://s/2", Order: 2},
		{Title: "The Gate Opens", URL: "https://s/3", Key: "https://s/3", Order: 3},
	}

	t.Run("happy path fills every legacy row", func(t *testing.T) {
		stored := []store.ChapterSyncMeta{
			{ID: "c1", ChapterOrder: 1, Title: "Prologue"},
			{ID: "c2", ChapterOrder: 2, Title: "Growth"},
			{ID: "c3", ChapterOrder: 3, Title: "The Gate Opens"},
		}
		plan, ok := backfillSourceKeyPlan(threeChapters, stored)
		if !ok {
			t.Fatal("expected ok=true")
		}
		want := map[string]string{
			"c1": "https://s/1",
			"c2": "https://s/2",
			"c3": "https://s/3",
		}
		if !reflect.DeepEqual(plan, want) {
			t.Errorf("plan = %v, want %v", plan, want)
		}
	})

	t.Run("title match trims and ignores case", func(t *testing.T) {
		stored := []store.ChapterSyncMeta{
			{ID: "c1", ChapterOrder: 1, Title: "  prologue "},
		}
		plan, ok := backfillSourceKeyPlan(threeChapters, stored)
		if !ok || plan["c1"] != "https://s/1" {
			t.Errorf("plan = %v ok = %v, want c1 keyed with its URL", plan, ok)
		}
	})

	t.Run("duplicate titles resolve by position", func(t *testing.T) {
		// The Growth case: the site reuses titles (episodes 2 and 5 are both
		// "Growth" with different keys). Stored legacy 1-4 plus an already
		// keyed chapter at order 6, with order 5 not downloaded yet: the gap
		// must not abort the plan and the duplicate title must not matter.
		chapters := []sourceChapter{
			{Title: "One", Key: "k1", Order: 1},
			{Title: "Growth", Key: "k2", Order: 2},
			{Title: "Three", Key: "k3", Order: 3},
			{Title: "Four", Key: "k4", Order: 4},
			{Title: "Growth", Key: "k5", Order: 5},
			{Title: "Six", Key: "k6", Order: 6},
		}
		stored := []store.ChapterSyncMeta{
			{ID: "c1", ChapterOrder: 1, Title: "One"},
			{ID: "c2", ChapterOrder: 2, Title: "Growth"},
			{ID: "c3", ChapterOrder: 3, Title: "Three"},
			{ID: "c4", ChapterOrder: 4, Title: "Four"},
			{ID: "c6", ChapterOrder: 6, Title: "Six", SourceKey: "k6"},
		}
		plan, ok := backfillSourceKeyPlan(chapters, stored)
		if !ok {
			t.Fatal("expected ok=true")
		}
		want := map[string]string{"c1": "k1", "c2": "k2", "c3": "k3", "c4": "k4"}
		if !reflect.DeepEqual(plan, want) {
			t.Errorf("plan = %v, want %v", plan, want)
		}
	})

	t.Run("key falls back to URL when the script exports none", func(t *testing.T) {
		chapters := []sourceChapter{{Title: "Alpha", URL: "https://s/1", Order: 1}}
		stored := []store.ChapterSyncMeta{{ID: "c1", ChapterOrder: 1, Title: "Alpha"}}
		plan, ok := backfillSourceKeyPlan(chapters, stored)
		if !ok || plan["c1"] != "https://s/1" {
			t.Errorf("plan = %v ok = %v, want c1 keyed with the URL", plan, ok)
		}
	})

	t.Run("title drift aborts the whole plan", func(t *testing.T) {
		stored := []store.ChapterSyncMeta{
			{ID: "c1", ChapterOrder: 1, Title: "Prologue"},
			{ID: "c2", ChapterOrder: 2, Title: "Renamed On Site"},
		}
		plan, ok := backfillSourceKeyPlan(threeChapters, stored)
		if ok || plan != nil {
			t.Errorf("plan = %v ok = %v, want nil/false", plan, ok)
		}
	})

	t.Run("stored order beyond the snapshot aborts", func(t *testing.T) {
		stored := []store.ChapterSyncMeta{
			{ID: "c1", ChapterOrder: 1, Title: "Prologue"},
			{ID: "c4", ChapterOrder: 4, Title: "Extra"},
		}
		plan, ok := backfillSourceKeyPlan(threeChapters, stored)
		if ok || plan != nil {
			t.Errorf("plan = %v ok = %v, want nil/false", plan, ok)
		}
	})

	t.Run("empty snapshot title aborts", func(t *testing.T) {
		chapters := []sourceChapter{{Title: "", Key: "k1", Order: 1}}
		stored := []store.ChapterSyncMeta{{ID: "c1", ChapterOrder: 1, Title: "Prologue"}}
		plan, ok := backfillSourceKeyPlan(chapters, stored)
		if ok || plan != nil {
			t.Errorf("plan = %v ok = %v, want nil/false", plan, ok)
		}
	})

	t.Run("empty stored title aborts", func(t *testing.T) {
		stored := []store.ChapterSyncMeta{{ID: "c1", ChapterOrder: 1, Title: ""}}
		plan, ok := backfillSourceKeyPlan(threeChapters, stored)
		if ok || plan != nil {
			t.Errorf("plan = %v ok = %v, want nil/false", plan, ok)
		}
	})

	t.Run("all keyed stored is a vacuous no-op", func(t *testing.T) {
		stored := []store.ChapterSyncMeta{
			{ID: "c1", ChapterOrder: 1, Title: "Prologue", SourceKey: "https://s/1"},
		}
		plan, ok := backfillSourceKeyPlan(threeChapters, stored)
		if !ok || len(plan) != 0 {
			t.Errorf("plan = %v ok = %v, want empty/true", plan, ok)
		}
	})

	t.Run("no stored chapters is a vacuous no-op", func(t *testing.T) {
		plan, ok := backfillSourceKeyPlan(threeChapters, nil)
		if !ok || len(plan) != 0 {
			t.Errorf("plan = %v ok = %v, want empty/true", plan, ok)
		}
	})
}

// The test-livewire-catalog parser (the one the test env loads for
// skydemonorder.com URLs) prefixes every catalog title with its episode
// number, so a duplicated site title is produced by repeating the episode:
// two entries with episode 4 emit the identical title "4. Growth" with
// different slugs, mirroring the real-world case (episodes 18 and 32 of a
// production novel are both called "Growth"). A legacy novel — every chapter
// stored before source_key existed — ran the title-first heuristic, so the
// second duplicate-title episode was skipped as "already downloaded" and the
// update reported the novel up to date. The sync now backfills source keys
// from the TOC snapshot before diffing, which flips the novel to keyed
// identity and makes the duplicate-title episode detectable.
func TestSyncBackfillsLegacySourceKeysBeforeDiff(t *testing.T) {
	catalog := []map[string]any{
		{"episode": 1, "title": "Alpha", "slug": "1-alpha"},
		{"episode": 2, "title": "Beta", "slug": "2-beta"},
		{"episode": 3, "title": "Gamma", "slug": "3-gamma"},
		// Same episode and title as the real duplicate-title case; the slugs
		// differ, so the parser emits two chapters with identical titles.
		{"episode": 4, "title": "Growth", "slug": "4-growth"},
		{"episode": 4, "title": "Growth", "slug": "4-growth-bis"},
	}
	catalogJSON, err := json.Marshal(catalog)
	if err != nil {
		t.Fatalf("marshal catalog: %v", err)
	}
	// The real page stores the catalog as a JSON.parse() string with quotes
	// escaped as \u0022; mirror that so the parser exercises the same path.
	escaped := strings.ReplaceAll(string(catalogJSON), `"`, `\u0022`)
	projectHTML := `<!doctype html><html><head><meta name="csrf-token" content="x"></head><body>` +
		`<h1 class="font-title">Sky Demon Test Novel</h1>` +
		`<div wire:id="abc" wire:name="project.chapter-list" x-data="{ activeTab: 'free', freeChapters: JSON.parse('` + escaped + `') }"></div>` +
		`</body></html>`

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, projectHTML)
	}))
	defer mock.Close()

	rewrites := map[string]string{"skydemonorder.com": mock.URL}
	env := newAPITestEnv(t)
	// Park download jobs in the dispatch queue without executing them, as in
	// the other update-from-url tests.
	env.server.dispatchDisabled = true
	useRewritingClient(env, rewrites)

	alice := registerUser(t, env, "alice-dup-titles@example.com", "secret123", "Alice")

	novel := createNovel(t, env.handler, alice.Token, "Test", "en", "es")
	patchResp := doJSONRequest(t, env.handler, http.MethodPatch, "/api/v1/novels/"+novel.ID, alice.Token, map[string]any{
		"url": "https://skydemonorder.com/projects/12345-sky-demon-test-novel",
	})
	assertStatus(t, patchResp, http.StatusOK)

	// Legacy chapters 1-4: no source_key, so the novel starts in legacy mode.
	// Titles carry the parser's "<episode>. <title>" prefix so the positional
	// pairing in the backfill plan matches the TOC snapshot byte for byte.
	createChapterWithTitle(t, env.handler, alice.Token, novel.ID, 1, "1. Alpha")
	createChapterWithTitle(t, env.handler, alice.Token, novel.ID, 2, "2. Beta")
	createChapterWithTitle(t, env.handler, alice.Token, novel.ID, 3, "3. Gamma")
	createChapterWithTitle(t, env.handler, alice.Token, novel.ID, 4, "4. Growth")

	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/"+novel.ID+"/check-preview", alice.Token, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	var preview struct {
		TotalChapters   int `json:"totalChapters"`
		NewChapters     int `json:"newChapters"`
		FirstNewChapter int `json:"firstNewChapter"`
		LastNewChapter  int `json:"lastNewChapter"`
	}
	decodeData(t, resp, &preview)
	if preview.TotalChapters != 5 {
		t.Errorf("totalChapters: got %d, want 5", preview.TotalChapters)
	}
	// The second "4. Growth" shares its title with stored chapter 4; only
	// keyed identity can tell them apart, so the backfill must have run
	// before the diff. Without it the legacy title-first heuristic reports
	// the novel as up to date.
	if preview.NewChapters != 1 {
		t.Errorf("newChapters: got %d, want 1 (second duplicate-title episode)", preview.NewChapters)
	}
	if preview.FirstNewChapter != 5 || preview.LastNewChapter != 5 {
		t.Errorf("first/last new chapter: got %d/%d, want 5/5", preview.FirstNewChapter, preview.LastNewChapter)
	}

	// The legacy chapters are planned against the snapshot in memory, but the
	// check itself is read-only: no source keys are persisted by a preview.
	metas, err := env.store.ListChapterSyncMeta(alice.User.ID, novel.ID)
	if err != nil {
		t.Fatalf("list chapter sync meta: %v", err)
	}
	keyed := 0
	for _, meta := range metas {
		if meta.SourceKey != "" {
			keyed++
		}
	}
	if keyed != 0 {
		t.Errorf("chapters with source_key after check-preview: got %d, want 0 (previews never persist the backfill plan)", keyed)
	}

	// The download path sees the same keyed novel through the preview cache.
	updateResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/"+novel.ID+"/update-from-url", alice.Token, map[string]any{})
	if updateResp.Code != http.StatusAccepted {
		t.Fatalf("update: expected 202, got %d: %s", updateResp.Code, updateResp.Body.String())
	}
	var update struct {
		PendingChapters int `json:"pendingChapters"`
	}
	decodeResponse(t, updateResp, &update)
	if update.PendingChapters != 1 {
		t.Errorf("pendingChapters: got %d, want 1", update.PendingChapters)
	}

	// The update flow — the one that actually enqueues downloads — persists
	// the backfill plan computed from the same snapshot.
	metas, err = env.store.ListChapterSyncMeta(alice.User.ID, novel.ID)
	if err != nil {
		t.Fatalf("list chapter sync meta: %v", err)
	}
	keyed = 0
	for _, meta := range metas {
		if meta.SourceKey != "" {
			keyed++
		}
	}
	if keyed != 4 {
		t.Errorf("chapters with source_key after update-from-url: got %d, want 4 (backfill persisted by the update flow)", keyed)
	}
}

package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
	"translator-server/internal/noveldownloader"
)

const testNovelfireHTML = `<!doctype html><html><head>
<meta property="og:image" content="https://novelfire.net/cover.jpg">
<meta itemprop="description" content="A short test novel used to exercise the URL import endpoint end-to-end.">
</head><body>
<div class="main-head"><h1>Mock Test Novel</h1></div>
<span itemprop="author">Tester</span>
<ul class="chapter-list">
  <li><a href="chapter-1"><span class="chapter-title">Chapter 1: First Steps</span></a></li>
  <li><a href="chapter-2"><span class="chapter-title">Chapter 2: The Journey</span></a></li>
</ul>
</body></html>`

const testNovelfireChapterHTML = `<!doctype html><html><head></head><body>
<span class="chapter-title">Chapter 1: First Steps</span>
<div class="chapter-content"><p>It was a dark and stormy night.</p><p>The end.</p></div>
</body></html>`

type hostRewritingTransport struct {
	rewrites map[string]string
	inner    http.RoundTripper
}

func (t *hostRewritingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	inner := t.inner
	if inner == nil {
		inner = http.DefaultTransport
	}
	rewritten := false
	if target, ok := t.rewrites[req.URL.Host]; ok {
		req2 := req.Clone(req.Context())
		u, err := url.Parse(target)
		if err != nil {
			return nil, err
		}
		req2.URL.Scheme = u.Scheme
		req2.URL.Host = u.Host
		req.Host = u.Host
		req = req2
		rewritten = true
	}
	resp, err := inner.RoundTrip(req)
	if rewritten && resp != nil {
		resp.Request = req
	}
	return resp, err
}

func TestImportUrlNovelAttachesCoverAndCreatesNovel(t *testing.T) {
	var chapterHits int
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/cover.jpg"):
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("\xff\xd8\xff\xe0fake-jpeg-bytes"))
		case strings.HasSuffix(r.URL.Path, "/chapters"):
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(w, testNovelfireHTML)
		case strings.Contains(r.URL.Path, "/chapter-"):
			chapterHits++
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(w, testNovelfireChapterHTML)
		default:
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(w, testNovelfireHTML)
		}
	}))
	defer mock.Close()

	rewrites := map[string]string{
		"novelfire.net": mock.URL,
	}
	transport := &hostRewritingTransport{rewrites: rewrites}
	client := noveldownloader.NewHTTPClientWithTransport(transport)

	env := newAPITestEnv(t)
	env.server.DownloaderFactory = func(string) *noveldownloader.Downloader {
		return noveldownloader.NewDownloaderWithClient(client)
	}

	alice := registerUser(t, env, "alice-import-url@example.com", "secret123", "Alice")

	novelURL := "https://novelfire.net/book/test-novel"
	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/import-from-url", alice.Token, map[string]any{
		"url":            novelURL,
		"sourceLanguage": "en",
		"targetLanguage": "es",
	})
	if resp.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", resp.Code, resp.Body.String())
	}

	var importResp struct {
		Novel struct {
			ID                string `json:"id"`
			SourceTitle       string `json:"sourceTitle"`
			SourceAuthor      string `json:"sourceAuthor"`
			SourceDescription string `json:"sourceDescription"`
			URL               string `json:"url"`
			CoverPath         string `json:"coverPath"`
		} `json:"novel"`
		ChaptersImported int `json:"chaptersImported"`
	}
	decodeData(t, resp, &importResp)
	if importResp.Novel.ID == "" {
		t.Fatal("expected non-empty novel id")
	}
	if importResp.Novel.SourceTitle != "Mock Test Novel" {
		t.Errorf("unexpected title: %q", importResp.Novel.SourceTitle)
	}
	if importResp.Novel.SourceAuthor != "Tester" {
		t.Errorf("unexpected author: %q", importResp.Novel.SourceAuthor)
	}
	if importResp.Novel.SourceDescription == "" {
		t.Errorf("expected non-empty description")
	}
	if importResp.Novel.URL != novelURL {
		t.Errorf("expected url %q, got %q", novelURL, importResp.Novel.URL)
	}
	if importResp.Novel.CoverPath == "" {
		t.Fatalf("expected coverPath, got empty: %s", resp.Body.String())
	}
	if importResp.ChaptersImported != 1 {
		t.Errorf("expected 1 chapter imported, got %d", importResp.ChaptersImported)
	}
	if chapterHits != 1 {
		t.Errorf("expected chapter endpoint hit 1 time, got %d", chapterHits)
	}

	listResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels", alice.Token, nil)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list failed: %d: %s", listResp.Code, listResp.Body.String())
	}
	var list []map[string]any
	decodeData(t, listResp, &list)
	found := false
	for _, n := range list {
		if id, _ := n["id"].(string); id == importResp.Novel.ID {
			found = true
			if cp, _ := n["coverPath"].(string); cp == "" {
				t.Errorf("novel in list has empty coverPath")
			}
			break
		}
	}
	if !found {
		t.Errorf("imported novel not present in list")
	}

	// CoverPath now points at the authenticated cover route (the file fields
	// are protected), so the request must carry the owner's token.
	coverReq := httptest.NewRequest(http.MethodGet, importResp.Novel.CoverPath, nil)
	coverReq.Header.Set("Authorization", "Bearer "+alice.Token)
	coverRec := httptest.NewRecorder()
	env.handler.ServeHTTP(coverRec, coverReq)
	if coverRec.Code != http.StatusOK {
		t.Fatalf("cover fetch returned %d: %s", coverRec.Code, coverRec.Body.String())
	}
	if coverRec.Body.Len() == 0 {
		t.Errorf("expected non-empty cover body")
	}
}

func originFromRequest(r *http.Request) string {
	if r.TLS != nil {
		return "https://" + r.Host
	}
	return "http://" + r.Host
}

func TestPreviewUrlNovelReturnsMetadata(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/cover.jpg"):
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("fake-jpeg"))
		case strings.HasSuffix(r.URL.Path, "/chapters"):
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(w, testNovelfireHTML)
		case strings.Contains(r.URL.Path, "/chapter-"):
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(w, testNovelfireChapterHTML)
		default:
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(w, testNovelfireHTML)
		}
	}))
	defer mock.Close()

	rewrites := map[string]string{
		"novelfire.net": mock.URL,
	}
	transport := &hostRewritingTransport{rewrites: rewrites}
	client := noveldownloader.NewHTTPClientWithTransport(transport)

	env := newAPITestEnv(t)
	env.server.DownloaderFactory = func(string) *noveldownloader.Downloader {
		return noveldownloader.NewDownloaderWithClient(client)
	}

	alice := registerUser(t, env, "alice-preview-url@example.com", "secret123", "Alice")

	novelURL := "https://novelfire.net/book/test-novel"
	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/preview-from-url", alice.Token, map[string]any{
		"url": novelURL,
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	var preview struct {
		Title         string `json:"title"`
		Author        string `json:"author"`
		Description   string `json:"description"`
		CoverURL      string `json:"coverURL"`
		TotalChapters int    `json:"totalChapters"`
		SourceURL     string `json:"sourceURL"`
	}
	decodeData(t, resp, &preview)
	if preview.Title != "Mock Test Novel" {
		t.Errorf("title: %q", preview.Title)
	}
	if preview.Author != "Tester" {
		t.Errorf("author: %q", preview.Author)
	}
	if preview.Description == "" {
		t.Errorf("description empty")
	}
	if preview.CoverURL == "" {
		t.Errorf("coverURL empty")
	}
	if preview.TotalChapters != 2 {
		t.Errorf("totalChapters: got %d, want 2", preview.TotalChapters)
	}
	if preview.SourceURL != novelURL {
		t.Errorf("sourceURL: got %q, want %q", preview.SourceURL, novelURL)
	}
}

func TestPreviewUrlNovelRejectsUnsupportedHost(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "alice-preview-bad@example.com", "secret123", "Alice")

	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/preview-from-url", alice.Token, map[string]any{
		"url": "https://example.com/novel/foo",
	})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestPreviewUrlNovelRejectsEmptyURL(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "alice-preview-empty@example.com", "secret123", "Alice")

	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/preview-from-url", alice.Token, map[string]any{
		"url": "",
	})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestUpdateUrlPreviewReturnsComparison(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/cover.jpg"):
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("fake-jpeg"))
		case strings.HasSuffix(r.URL.Path, "/chapters"):
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(w, testNovelfireHTML)
		case strings.Contains(r.URL.Path, "/chapter-"):
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(w, testNovelfireChapterHTML)
		default:
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(w, testNovelfireHTML)
		}
	}))
	defer mock.Close()

	rewrites := map[string]string{"novelfire.net": mock.URL}
	transport := &hostRewritingTransport{rewrites: rewrites}
	client := noveldownloader.NewHTTPClientWithTransport(transport)

	env := newAPITestEnv(t)
	env.server.DownloaderFactory = func(string) *noveldownloader.Downloader {
		return noveldownloader.NewDownloaderWithClient(client)
	}

	alice := registerUser(t, env, "alice-update-preview@example.com", "secret123", "Alice")

	novel := createNovel(t, env.handler, alice.Token, "Test", "en", "es")
	patchResp := doJSONRequest(t, env.handler, http.MethodPatch, "/api/v1/novels/"+novel.ID, alice.Token, map[string]any{
		"url": "https://novelfire.net/book/test-novel",
	})
	assertStatus(t, patchResp, http.StatusOK)

	createChapter(t, env.handler, alice.Token, novel.ID, 1)
	createChapterWithTitle(t, env.handler, alice.Token, novel.ID, 2, "Chapter 2: The Journey")

	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/"+novel.ID+"/check-preview", alice.Token, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	var preview struct {
		Title           string `json:"title"`
		Author          string `json:"author"`
		CoverURL        string `json:"coverURL"`
		SourceURL       string `json:"sourceURL"`
		CurrentChapters int    `json:"currentChapters"`
		TotalChapters   int    `json:"totalChapters"`
		NewChapters     int    `json:"newChapters"`
		FirstNewChapter int    `json:"firstNewChapter"`
		LastNewChapter  int    `json:"lastNewChapter"`
	}
	decodeData(t, resp, &preview)
	if preview.Title != "Mock Test Novel" {
		t.Errorf("title: got %q, want %q", preview.Title, "Mock Test Novel")
	}
	if preview.CoverURL == "" {
		t.Errorf("coverURL should be set")
	}
	if preview.CurrentChapters != 2 {
		t.Errorf("currentChapters: got %d, want 2", preview.CurrentChapters)
	}
	if preview.TotalChapters != 2 {
		t.Errorf("totalChapters: got %d, want 2", preview.TotalChapters)
	}
	if preview.NewChapters != 0 {
		t.Errorf("newChapters: got %d, want 0", preview.NewChapters)
	}
	if preview.FirstNewChapter != 0 {
		t.Errorf("firstNewChapter: got %d, want 0", preview.FirstNewChapter)
	}
	if preview.LastNewChapter != 0 {
		t.Errorf("lastNewChapter: got %d, want 0", preview.LastNewChapter)
	}
}

func TestUpdateUrlPreviewReportsNoneWhenUpToDate(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/cover.jpg"):
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("fake-jpeg"))
		case strings.HasSuffix(r.URL.Path, "/chapters"):
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(w, testNovelfireHTML)
		case strings.Contains(r.URL.Path, "/chapter-"):
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(w, testNovelfireChapterHTML)
		default:
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(w, testNovelfireHTML)
		}
	}))
	defer mock.Close()

	rewrites := map[string]string{"novelfire.net": mock.URL}
	transport := &hostRewritingTransport{rewrites: rewrites}
	client := noveldownloader.NewHTTPClientWithTransport(transport)

	env := newAPITestEnv(t)
	env.server.DownloaderFactory = func(string) *noveldownloader.Downloader {
		return noveldownloader.NewDownloaderWithClient(client)
	}

	alice := registerUser(t, env, "alice-update-ok@example.com", "secret123", "Alice")

	novel := createNovel(t, env.handler, alice.Token, "Test", "en", "es")
	patchResp := doJSONRequest(t, env.handler, http.MethodPatch, "/api/v1/novels/"+novel.ID, alice.Token, map[string]any{
		"url": "https://novelfire.net/book/test-novel",
	})
	assertStatus(t, patchResp, http.StatusOK)

	createChapterWithTitle(t, env.handler, alice.Token, novel.ID, 1, "Chapter 1: First Steps")
	createChapterWithTitle(t, env.handler, alice.Token, novel.ID, 2, "Chapter 2: The Journey")

	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/"+novel.ID+"/check-preview", alice.Token, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	var preview struct {
		CurrentChapters int `json:"currentChapters"`
		TotalChapters   int `json:"totalChapters"`
		NewChapters     int `json:"newChapters"`
		FirstNewChapter int `json:"firstNewChapter"`
		LastNewChapter  int `json:"lastNewChapter"`
	}
	decodeData(t, resp, &preview)
	if preview.NewChapters != 0 {
		t.Errorf("newChapters: got %d, want 0", preview.NewChapters)
	}
	if preview.FirstNewChapter != 0 || preview.LastNewChapter != 0 {
		t.Errorf("first/last new chapter should be 0 when up to date, got %d/%d", preview.FirstNewChapter, preview.LastNewChapter)
	}
}

// SkyDemonOrder catalog titles never carry the episode number; multi-part
// arcs only have "(1)", "(2)", … suffixes. The old title-based order
// extraction turned episode 1121's "Some Arc (1)" into order 1, colliding
// with the orders of already-downloaded chapters and hiding every new
// multi-part chapter from update checks (preview reported "up to date").
// The parser-provided Order (episode number) must win over title parsing.
func TestUpdateUrlPreviewDetectsEpisodesHiddenByPartNumberTitles(t *testing.T) {
	catalog := []map[string]any{
		{"episode": 1, "title": "Prologue", "slug": "1-prologue"},
		{"episode": 2, "title": "A Long Journey", "slug": "2-a-long-journey"},
		{"episode": 3, "title": "The Gate Opens", "slug": "3-the-gate-opens"},
		{"episode": 4, "title": "The Demon War (1)", "slug": "4-the-demon-war-1"},
		{"episode": 5, "title": "The Demon War (2)", "slug": "5-the-demon-war-2"},
		{"episode": 6, "title": "The Demon War (3)", "slug": "6-the-demon-war-3"},
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
	transport := &hostRewritingTransport{rewrites: rewrites}
	client := noveldownloader.NewHTTPClientWithTransport(transport)

	env := newAPITestEnv(t)
	oldQueue := env.server.downloadQueue
	env.server.downloadQueue = make(chan string, 1000)
	close(oldQueue)
	env.server.DownloaderFactory = func(string) *noveldownloader.Downloader {
		return noveldownloader.NewDownloaderWithClient(client)
	}

	alice := registerUser(t, env, "alice-part-titles@example.com", "secret123", "Alice")

	novel := createNovel(t, env.handler, alice.Token, "Test", "en", "es")
	patchResp := doJSONRequest(t, env.handler, http.MethodPatch, "/api/v1/novels/"+novel.ID, alice.Token, map[string]any{
		"url": "https://skydemonorder.com/projects/12345-sky-demon-test-novel",
	})
	assertStatus(t, patchResp, http.StatusOK)

	// Episodes 1-3 are already downloaded; their stored orders are 1, 2, 3 —
	// exactly the numbers the old heuristic extracted from "The Demon War (N)".
	createChapterWithTitle(t, env.handler, alice.Token, novel.ID, 1, "Prologue")
	createChapterWithTitle(t, env.handler, alice.Token, novel.ID, 2, "A Long Journey")
	createChapterWithTitle(t, env.handler, alice.Token, novel.ID, 3, "The Gate Opens")

	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/"+novel.ID+"/check-preview", alice.Token, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	var preview struct {
		CurrentChapters int `json:"currentChapters"`
		TotalChapters   int `json:"totalChapters"`
		NewChapters     int `json:"newChapters"`
		FirstNewChapter int `json:"firstNewChapter"`
		LastNewChapter  int `json:"lastNewChapter"`
	}
	decodeData(t, resp, &preview)
	if preview.TotalChapters != 6 {
		t.Errorf("totalChapters: got %d, want 6", preview.TotalChapters)
	}
	if preview.NewChapters != 3 {
		t.Errorf("newChapters: got %d, want 3 (episodes 4-6 hidden behind part-number titles)", preview.NewChapters)
	}
	if preview.FirstNewChapter != 4 || preview.LastNewChapter != 6 {
		t.Errorf("first/last new chapter: got %d/%d, want 4/6", preview.FirstNewChapter, preview.LastNewChapter)
	}

	// The download path applies the same filter: with the preview cache hot it
	// must schedule exactly episodes 4-6 instead of reporting "up to date".
	updateResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/"+novel.ID+"/update-from-url", alice.Token, map[string]any{})
	if updateResp.Code != http.StatusAccepted {
		t.Fatalf("update: expected 202, got %d: %s", updateResp.Code, updateResp.Body.String())
	}
	var update struct {
		PendingChapters int `json:"pendingChapters"`
	}
	decodeResponse(t, updateResp, &update)
	if update.PendingChapters != 3 {
		t.Errorf("pendingChapters: got %d, want 3", update.PendingChapters)
	}
}

// Novelfire chapter URLs are numbered sequentially (chapter-93, chapter-94)
// while their titles keep the novel's own numbering ("Chapter 92.1",
// "Chapter 92.2"), with a prologue ("c-1") shifting every title off its URL
// number. The old title-based order extraction collapsed both decimals to
// order 92: update checks reported "up to date" because order 92 already
// existed, and when both decimals were queued the unique (novel,
// chapter_order) index rejected the second insert, silently dropping one
// chapter. The parser-provided Order (URL number) must keep them distinct.
func TestUpdateFromUrlKeepsDecimalNumberedChapters(t *testing.T) {
	var items []string
	items = append(items, `<li><a href="chapter-1"><span class="chapter-title">c-1: Prologue</span></a></li>`)
	for n := 2; n <= 92; n++ {
		items = append(items, fmt.Sprintf(`<li><a href="chapter-%d"><span class="chapter-title">Chapter %d: Filler</span></a></li>`, n, n-1))
	}
	items = append(items,
		`<li><a href="chapter-93"><span class="chapter-title">Chapter 92.1: Collapse of Xyrus</span></a></li>`,
		`<li><a href="chapter-94"><span class="chapter-title">Chapter 92.2: Bird's Cage</span></a></li>`,
	)
	chapterHTML := `<!doctype html><html><head>
<meta property="og:image" content="https://novelfire.net/cover.jpg">
<meta itemprop="description" content="A novel with decimal-numbered chapters.">
</head><body>
<div class="main-head"><h1>Decimal Test Novel</h1></div>
<span itemprop="author">Tester</span>
<ul class="chapter-list">` + strings.Join(items, "") + `</ul>
</body></html>`

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/cover.jpg"):
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("fake-jpeg"))
		case strings.Contains(r.URL.Path, "/chapter-"):
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(w, testNovelfireChapterHTML)
		default:
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(w, chapterHTML)
		}
	}))
	defer mock.Close()

	rewrites := map[string]string{"novelfire.net": mock.URL}
	transport := &hostRewritingTransport{rewrites: rewrites}
	client := noveldownloader.NewHTTPClientWithTransport(transport)

	env := newAPITestEnv(t)
	oldQueue := env.server.downloadQueue
	env.server.downloadQueue = make(chan string, 1000)
	close(oldQueue)
	env.server.DownloaderFactory = func(string) *noveldownloader.Downloader {
		return noveldownloader.NewDownloaderWithClient(client)
	}

	alice := registerUser(t, env, "alice-decimal-chapters@example.com", "secret123", "Alice")

	novel := createNovel(t, env.handler, alice.Token, "Test", "en", "es")
	patchResp := doJSONRequest(t, env.handler, http.MethodPatch, "/api/v1/novels/"+novel.ID, alice.Token, map[string]any{
		"url": "https://novelfire.net/book/decimal-test-novel",
	})
	assertStatus(t, patchResp, http.StatusOK)

	// Chapters 1-92 were downloaded by a previous import: stored orders are
	// the sequential URL positions, which is what import-from-url assigns.
	createChapterWithTitle(t, env.handler, alice.Token, novel.ID, 1, "c-1: Prologue")
	for n := 2; n <= 92; n++ {
		createChapterWithTitle(t, env.handler, alice.Token, novel.ID, n, fmt.Sprintf("Chapter %d: Filler", n-1))
	}

	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/"+novel.ID+"/check-preview", alice.Token, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("preview: expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	var preview struct {
		TotalChapters   int `json:"totalChapters"`
		NewChapters     int `json:"newChapters"`
		FirstNewChapter int `json:"firstNewChapter"`
		LastNewChapter  int `json:"lastNewChapter"`
	}
	decodeResponse(t, resp, &preview)
	if preview.TotalChapters != 94 {
		t.Errorf("totalChapters: got %d, want 94", preview.TotalChapters)
	}
	if preview.NewChapters != 2 {
		t.Errorf("newChapters: got %d, want 2 (the two decimal chapters)", preview.NewChapters)
	}
	if preview.FirstNewChapter != 93 || preview.LastNewChapter != 94 {
		t.Errorf("first/last new chapter: got %d/%d, want 93/94", preview.FirstNewChapter, preview.LastNewChapter)
	}

	updateResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/"+novel.ID+"/update-from-url", alice.Token, map[string]any{})
	if updateResp.Code != http.StatusAccepted {
		t.Fatalf("update: expected 202, got %d: %s", updateResp.Code, updateResp.Body.String())
	}
	var update struct {
		PendingChapters int `json:"pendingChapters"`
	}
	decodeResponse(t, updateResp, &update)
	if update.PendingChapters != 2 {
		t.Fatalf("pendingChapters: got %d, want 2", update.PendingChapters)
	}

	// The queued job must carry the sequential URL orders (93, 94), not the
	// colliding title-derived 92 — otherwise the unique (novel,
	// chapter_order) index drops one of the two decimal chapters.
	jobs, err := env.store.ListJobs(alice.User.ID, novel.ID, false)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected one job, got %d", len(jobs))
	}
	var opts struct {
		Chapters []struct {
			Order int `json:"order"`
		} `json:"chapters"`
	}
	if err := json.Unmarshal([]byte(jobs[0].OptionsJSON), &opts); err != nil {
		t.Fatalf("decode job options: %v", err)
	}
	orders := map[int]bool{}
	for _, ch := range opts.Chapters {
		if orders[ch.Order] {
			t.Errorf("duplicate order %d in download job", ch.Order)
		}
		orders[ch.Order] = true
	}
	if !orders[93] || !orders[94] || len(orders) != 2 {
		t.Errorf("expected job orders {93, 94}, got %v", orders)
	}
}

func TestUpdateFromUrlRangeIncludesEndChapter(t *testing.T) {
	var chapterItems []string
	for n := 1; n <= 13; n++ {
		chapterItems = append(chapterItems, fmt.Sprintf(`<li><a href="chapter-%d"><span class="chapter-title">Chapter %d</span></a></li>`, n, n))
	}
	chapterHTML := `<!doctype html><html><head>
<meta property="og:image" content="https://novelfire.net/cover.jpg">
<meta itemprop="description" content="A short test novel used to exercise the URL import endpoint end-to-end.">
</head><body>
<div class="main-head"><h1>Mock Test Novel</h1></div>
<span itemprop="author">Tester</span>
<ul class="chapter-list">` + strings.Join(chapterItems, "") + `</ul>
</body></html>`

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/cover.jpg"):
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("fake-jpeg"))
		case strings.HasSuffix(r.URL.Path, "/chapters"):
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(w, chapterHTML)
		case strings.Contains(r.URL.Path, "/chapter-"):
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(w, testNovelfireChapterHTML)
		default:
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(w, chapterHTML)
		}
	}))
	defer mock.Close()

	rewrites := map[string]string{"novelfire.net": mock.URL}
	transport := &hostRewritingTransport{rewrites: rewrites}
	client := noveldownloader.NewHTTPClientWithTransport(transport)

	env := newAPITestEnv(t)
	oldQueue := env.server.downloadQueue
	env.server.downloadQueue = make(chan string, 1000)
	close(oldQueue)
	env.server.DownloaderFactory = func(string) *noveldownloader.Downloader {
		return noveldownloader.NewDownloaderWithClient(client)
	}

	alice := registerUser(t, env, "alice-update-range@example.com", "secret123", "Alice")

	novel := createNovel(t, env.handler, alice.Token, "Test", "en", "es")
	patchResp := doJSONRequest(t, env.handler, http.MethodPatch, "/api/v1/novels/"+novel.ID, alice.Token, map[string]any{
		"url": "https://novelfire.net/book/test-novel",
	})
	assertStatus(t, patchResp, http.StatusOK)

	for n := 1; n <= 9; n++ {
		createChapterWithTitle(t, env.handler, alice.Token, novel.ID, n, fmt.Sprintf("Chapter %d", n))
	}

	for _, tc := range []struct {
		name  string
		input map[string]any
		want  int
	}{
		{"all", map[string]any{}, 4},
		{"range 10-13", map[string]any{"startChapter": 10, "endChapter": 13}, 4},
		{"range 10-12", map[string]any{"startChapter": 10, "endChapter": 12}, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/"+novel.ID+"/update-from-url", alice.Token, tc.input)
			if resp.Code != http.StatusAccepted {
				t.Fatalf("expected 202, got %d: %s", resp.Code, resp.Body.String())
			}
			t.Logf("%s response: %s", tc.name, resp.Body.String())
			var result struct {
				PendingChapters int `json:"pendingChapters"`
			}
			decodeResponse(t, resp, &result)
			if result.PendingChapters != tc.want {
				t.Errorf("pendingChapters: got %d, want %d", result.PendingChapters, tc.want)
			}
		})
	}
}

func TestUpdateFromUrlQueueRejectionReturns503(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, redownloadIndexHTML(redownloadChapterTitles))
	}))
	defer mock.Close()

	rewrites := map[string]string{"novelfire.net": mock.URL}
	transport := &hostRewritingTransport{rewrites: rewrites}
	client := noveldownloader.NewHTTPClientWithTransport(transport)

	env := newAPITestEnv(t)
	oldQueue := env.server.downloadQueue
	env.server.downloadQueue = make(chan string)
	close(oldQueue)
	env.server.DownloaderFactory = func(string) *noveldownloader.Downloader {
		return noveldownloader.NewDownloaderWithClient(client)
	}

	alice := registerUser(t, env, "alice-update-queue@example.com", "secret123", "Alice")

	novel := createNovel(t, env.handler, alice.Token, "Test", "en", "es")
	patchResp := doJSONRequest(t, env.handler, http.MethodPatch, "/api/v1/novels/"+novel.ID, alice.Token, map[string]any{
		"url": "https://novelfire.net/book/test-novel",
	})
	assertStatus(t, patchResp, http.StatusOK)

	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/"+novel.ID+"/update-from-url", alice.Token, map[string]any{})
	assertStatus(t, resp, http.StatusServiceUnavailable)

	jobs, err := env.store.ListJobs(alice.User.ID, novel.ID, false)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected exactly one job, got %d", len(jobs))
	}
	if jobs[0].Status != "failed" {
		t.Errorf("expected rejected job to be failed, got %q", jobs[0].Status)
	}
	if jobs[0].ErrorMessage != jobQueueFullMessage {
		t.Errorf("expected queue-full error message, got %q", jobs[0].ErrorMessage)
	}
}

func TestUpdateUrlPreviewRejectsNovelWithoutURL(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "alice-update-nourl@example.com", "secret123", "Alice")
	novel := createNovel(t, env.handler, alice.Token, "Test", "en", "es")

	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/"+novel.ID+"/check-preview", alice.Token, nil)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestUpdateFromUrlUsesCacheFromPreview(t *testing.T) {
	var novelInfoRequests int
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/cover.jpg"):
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("fake-jpeg"))
		case strings.HasSuffix(r.URL.Path, "/chapters"):
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(w, testNovelfireHTML)
		case strings.Contains(r.URL.Path, "/chapter-"):
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(w, testNovelfireChapterHTML)
		default:
			novelInfoRequests++
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(w, testNovelfireHTML)
		}
	}))
	defer mock.Close()

	rewrites := map[string]string{"novelfire.net": mock.URL}
	transport := &hostRewritingTransport{rewrites: rewrites}
	client := noveldownloader.NewHTTPClientWithTransport(transport)

	env := newAPITestEnv(t)
	oldQueue := env.server.downloadQueue
	env.server.downloadQueue = make(chan string, 1000)
	close(oldQueue)
	env.server.DownloaderFactory = func(string) *noveldownloader.Downloader {
		return noveldownloader.NewDownloaderWithClient(client)
	}

	alice := registerUser(t, env, "alice-cache-test@example.com", "secret123", "Alice")

	novel := createNovel(t, env.handler, alice.Token, "Test", "en", "es")
	patchResp := doJSONRequest(t, env.handler, http.MethodPatch, "/api/v1/novels/"+novel.ID, alice.Token, map[string]any{
		"url": "https://novelfire.net/book/test-novel",
	})
	assertStatus(t, patchResp, http.StatusOK)

	createChapter(t, env.handler, alice.Token, novel.ID, 1)

	novelInfoRequests = 0

	previewResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/"+novel.ID+"/check-preview", alice.Token, nil)
	if previewResp.Code != http.StatusOK {
		t.Fatalf("preview: expected 200, got %d: %s", previewResp.Code, previewResp.Body.String())
	}
	if novelInfoRequests != 1 {
		t.Fatalf("after preview: expected 1 novel info request, got %d", novelInfoRequests)
	}

	updateResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/"+novel.ID+"/update-from-url", alice.Token, map[string]any{})
	if updateResp.Code != http.StatusAccepted {
		t.Fatalf("update: expected 202, got %d: %s", updateResp.Code, updateResp.Body.String())
	}
	if novelInfoRequests != 1 {
		t.Errorf("after update: expected still 1 novel info request (cache hit), got %d", novelInfoRequests)
	}

	var result struct {
		PendingChapters int `json:"pendingChapters"`
	}
	decodeResponse(t, updateResp, &result)
	if result.PendingChapters != 1 {
		t.Errorf("pendingChapters: got %d, want 1", result.PendingChapters)
	}
}

func TestUpdateFromUrlFallsBackWithoutPreview(t *testing.T) {
	var novelInfoRequests int
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/cover.jpg"):
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("fake-jpeg"))
		case strings.HasSuffix(r.URL.Path, "/chapters"):
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(w, testNovelfireHTML)
		case strings.Contains(r.URL.Path, "/chapter-"):
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(w, testNovelfireChapterHTML)
		default:
			novelInfoRequests++
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(w, testNovelfireHTML)
		}
	}))
	defer mock.Close()

	rewrites := map[string]string{"novelfire.net": mock.URL}
	transport := &hostRewritingTransport{rewrites: rewrites}
	client := noveldownloader.NewHTTPClientWithTransport(transport)

	env := newAPITestEnv(t)
	oldQueue := env.server.downloadQueue
	env.server.downloadQueue = make(chan string, 1000)
	close(oldQueue)
	env.server.DownloaderFactory = func(string) *noveldownloader.Downloader {
		return noveldownloader.NewDownloaderWithClient(client)
	}

	alice := registerUser(t, env, "alice-fallback-test@example.com", "secret123", "Alice")

	novel := createNovel(t, env.handler, alice.Token, "Test", "en", "es")
	patchResp := doJSONRequest(t, env.handler, http.MethodPatch, "/api/v1/novels/"+novel.ID, alice.Token, map[string]any{
		"url": "https://novelfire.net/book/test-novel",
	})
	assertStatus(t, patchResp, http.StatusOK)

	createChapter(t, env.handler, alice.Token, novel.ID, 1)

	novelInfoRequests = 0

	updateResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/"+novel.ID+"/update-from-url", alice.Token, map[string]any{})
	if updateResp.Code != http.StatusAccepted {
		t.Fatalf("update: expected 202, got %d: %s", updateResp.Code, updateResp.Body.String())
	}
	if novelInfoRequests != 1 {
		t.Errorf("without preview: expected 1 novel info request (fallback scrape), got %d", novelInfoRequests)
	}
}

func createChapterWithTitle(t *testing.T, handler http.Handler, token, novelID string, order int, title string) {
	t.Helper()
	resp := doJSONRequest(t, handler, http.MethodPost, "/api/v1/novels/"+novelID+"/chapters", token, map[string]any{
		"chapterOrder":    order,
		"title":           title,
		"originalContent": "Texto original",
	})
	assertStatus(t, resp, http.StatusCreated)
}

// TestDownloadJobCancelAfterSavedChapterKeepsNovelStatsConsistent is a
// regression test for canceling a download job while the worker is sleeping
// between chapters. Cancellation during that wait used to make
// processDownloadJob return early with ctx.Err(), skipping the final
// RecalculateNovelStats and leaving the persisted novel stats stale (the
// chapter saved right before the sleep was missing from chapterCount,
// maxChapterOrder and the char counts).
func TestDownloadJobCancelAfterSavedChapterKeepsNovelStatsConsistent(t *testing.T) {
	const indexHTML = `<!doctype html><html><head>
<meta itemprop="description" content="A test novel used to exercise download-job cancellation.">
</head><body>
<div class="main-head"><h1>Cancel Test Novel</h1></div>
<span itemprop="author">Tester</span>
<ul class="chapter-list">
  <li><a href="chapter-1"><span class="chapter-title">Chapter 1</span></a></li>
  <li><a href="chapter-2"><span class="chapter-title">Chapter 2</span></a></li>
  <li><a href="chapter-3"><span class="chapter-title">Chapter 3</span></a></li>
</ul>
</body></html>`

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if strings.Contains(r.URL.Path, "/chapter-") {
			_, _ = fmt.Fprint(w, testNovelfireChapterHTML)
			return
		}
		_, _ = fmt.Fprint(w, indexHTML)
	}))
	defer mock.Close()

	transport := &hostRewritingTransport{rewrites: map[string]string{"novelfire.net": mock.URL}}
	client := noveldownloader.NewHTTPClientWithTransport(transport)

	env := newAPITestEnv(t)
	env.server.DownloaderFactory = func(string) *noveldownloader.Downloader {
		dl := noveldownloader.NewDownloaderWithClient(client)
		// A fixed 5s inter-chapter delay guarantees SleepBetweenChapters
		// blocks long enough for the test to cancel while the worker waits.
		dl.MinChapterDelay = 5 * time.Second
		dl.MaxChapterDelay = 5 * time.Second
		return dl
	}

	alice := registerUser(t, env, "alice-dl-cancel@example.com", "secret123", "Alice")

	// 3 chapters: chapter 1 is imported synchronously; chapters 2-3 become a
	// background download job (chapter 2 saved first, then a 5s sleep before 3).
	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/import-from-url", alice.Token, map[string]any{
		"url":            "https://novelfire.net/book/cancel-test",
		"sourceLanguage": "en",
		"targetLanguage": "es",
		"startChapter":   1,
		"endChapter":     3,
	})
	assertStatus(t, resp, http.StatusCreated)

	var importResp struct {
		Novel struct {
			ID string `json:"id"`
		} `json:"novel"`
		DownloadJob struct {
			ID string `json:"id"`
		} `json:"downloadJob"`
	}
	decodeData(t, resp, &importResp)
	if importResp.Novel.ID == "" || importResp.DownloadJob.ID == "" {
		t.Fatalf("expected novel and download job ids, got %s", resp.Body.String())
	}

	// The worker saves the first queued chapter (order 2) via
	// UpsertChapterWithoutStats and then sleeps before the next one. Once it
	// reports completedChapters=1 the sleep is imminent, so cancel now: the
	// cancellation must hit SleepBetweenChapters while it is blocking.
	// cancelJob mirrors what the PATCH cancel endpoint does (minus its
	// best-effort stats recalc), so the worker's own recalc is what is tested.
	waitForCondition(t, 15*time.Second, "worker to save the first queued chapter", func() bool {
		job, err := env.store.GetJob(importResp.DownloadJob.ID)
		return err == nil && job.CompletedChapters == 1
	})
	env.server.cancelJob(importResp.DownloadJob.ID)

	// processDownloadJob must leave the loop through the common path on
	// cancellation (not return early), so RecalculateNovelStats still runs and
	// the novel reflects the two saved chapters (imported ch1 + downloaded ch2).
	waitForCondition(t, 5*time.Second, "novel stats to reflect the saved chapters after cancel", func() bool {
		novel, err := env.store.GetNovelAccessible(alice.User.ID, importResp.Novel.ID)
		return err == nil && novel.ChapterCount == 2
	})

	novel, err := env.store.GetNovelAccessible(alice.User.ID, importResp.Novel.ID)
	if err != nil {
		t.Fatalf("get novel: %v", err)
	}
	if novel.ChapterCount != 2 {
		t.Errorf("expected chapterCount=2 after canceled download, got %d", novel.ChapterCount)
	}
	if novel.MaxChapterOrder != 2 {
		t.Errorf("expected maxChapterOrder=2 after canceled download, got %d", novel.MaxChapterOrder)
	}
	if novel.OriginalCharCount <= 0 {
		t.Errorf("expected non-zero originalCharCount after canceled download, got %d", novel.OriginalCharCount)
	}
	if novel.TranslatedCount != 0 || novel.CompletedCount != 0 {
		t.Errorf("expected no translated/completed chapters after download only, got translated=%d completed=%d",
			novel.TranslatedCount, novel.CompletedCount)
	}
}

func waitForCondition(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

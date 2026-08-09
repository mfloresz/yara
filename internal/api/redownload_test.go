package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"translator-server/internal/noveldownloader"
	"translator-server/internal/store"
)

var redownloadChapterTitles = []string{
	"Chapter 1: First Steps",
	"Chapter 2: The Journey",
	"Chapter 3: The Return",
}

// redownloadIndexHTML builds the mock novel index page from the given chapter
// titles so tests can simulate the source site renumbering its chapters.
func redownloadIndexHTML(titles []string) string {
	var b strings.Builder
	b.WriteString(`<!doctype html><html><head>
<meta property="og:image" content="https://novelfire.net/cover.jpg">
<meta itemprop="description" content="A short test novel used to exercise the re-download endpoint end-to-end.">
</head><body>
<div class="main-head"><h1>Mock Test Novel</h1></div>
<span itemprop="author">Tester</span>
<ul class="chapter-list">`)
	for i, t := range titles {
		fmt.Fprintf(&b, `
  <li><a href="chapter-%d"><span class="chapter-title">%s</span></a></li>`, i+1, t)
	}
	b.WriteString(`
</ul>
</body></html>`)
	return b.String()
}

// redownloadFixture boots a mock novelfire server whose chapter body can be
// swapped mid-test via setBody to simulate the source site fixing its content,
// and whose index titles can be swapped via setTitles to simulate the site
// renumbering its chapters.
type redownloadFixture struct {
	env        *apiTestEnv
	alice      authPayload
	novelID    string
	chapterIDs []string
	setBody    func(string)
	setTitles  func([]string)
}

func setupRedownloadFixture(t *testing.T, withChapters bool) *redownloadFixture {
	t.Helper()

	var mu sync.Mutex
	body := "It was a dark and stormy night."
	titles := append([]string(nil), redownloadChapterTitles...)
	setBody := func(newBody string) {
		mu.Lock()
		defer mu.Unlock()
		body = newBody
	}
	setTitles := func(ts []string) {
		mu.Lock()
		defer mu.Unlock()
		titles = ts
	}
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		current := body
		currentTitles := titles
		mu.Unlock()
		switch {
		case strings.Contains(r.URL.Path, "/chapter-"):
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprintf(w, `<html><head></head><body><span class="chapter-title">Chapter</span><div class="chapter-content"><p>%s</p></div></body></html>`, current)
		default:
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(w, redownloadIndexHTML(currentTitles))
		}
	}))
	t.Cleanup(mock.Close)

	rewrites := map[string]string{"novelfire.net": mock.URL}
	client := noveldownloader.NewHTTPClientWithTransport(&hostRewritingTransport{rewrites: rewrites})

	env := newAPITestEnv(t)
	env.server.DownloaderFactory = func(string) *noveldownloader.Downloader {
		return noveldownloader.NewDownloaderWithClient(client)
	}

	alice := registerUser(t, env.handler, "alice-redownload@example.com", "secret123", "Alice")

	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/db/novels", alice.Token, map[string]any{
		"sourceTitle":    "Mock Test Novel",
		"sourceLanguage": "en",
		"targetLanguage": "es",
		"url":            "https://novelfire.net/book/test-novel",
	})
	assertStatus(t, resp, http.StatusCreated)
	var novel struct {
		ID string `json:"id"`
	}
	decodeResponse(t, resp, &novel)

	fx := &redownloadFixture{env: env, alice: alice, novelID: novel.ID, setBody: setBody, setTitles: setTitles}
	if !withChapters {
		return fx
	}
	fx.chapterIDs = make([]string, 0, len(redownloadChapterTitles))
	for i, title := range redownloadChapterTitles {
		ch, err := env.store.UpsertChapterWithoutStats(alice.User.ID, novel.ID, &store.Chapter{
			ChapterOrder:    i + 1,
			Title:           title,
			OriginalContent: "It was a dark and stormy night.",
			Status:          "pending",
		})
		if err != nil {
			t.Fatalf("upsert chapter %d: %v", i+1, err)
		}
		fx.chapterIDs = append(fx.chapterIDs, ch.ID)
	}
	return fx
}

type testChapter struct {
	ID                string `json:"id"`
	ChapterOrder      int    `json:"chapterOrder"`
	Title             string `json:"title"`
	OriginalContent   string `json:"originalContent"`
	TranslatedContent string `json:"translatedContent"`
	RefinedContent    string `json:"refinedContent"`
	TranslatedChars   int    `json:"translatedChars"`
	RefinedChars      int    `json:"refinedChars"`
	Status            string `json:"status"`
}

func listFullChapters(t *testing.T, env *apiTestEnv, alice authPayload, novelID string) []testChapter {
	t.Helper()
	resp := doJSONRequest(t, env.handler, http.MethodGet, "/api/db/novels/"+novelID+"/chapters/full", alice.Token, nil)
	assertStatus(t, resp, http.StatusOK)
	var out []testChapter
	decodeResponse(t, resp, &out)
	return out
}

func listChapterSummaries(t *testing.T, env *apiTestEnv, alice authPayload, novelID string) []testChapter {
	t.Helper()
	resp := doJSONRequest(t, env.handler, http.MethodGet, "/api/db/novels/"+novelID+"/chapters", alice.Token, nil)
	assertStatus(t, resp, http.StatusOK)
	var out []testChapter
	decodeResponse(t, resp, &out)
	return out
}

func waitForJobDone(t *testing.T, env *apiTestEnv, jobID string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		job, err := env.store.GetJob(jobID)
		if err != nil {
			t.Fatalf("load job: %v", err)
		}
		if job.Status == "done" {
			return
		}
		if job.Status == "failed" {
			t.Fatalf("job %s failed: %s", jobID, job.ErrorMessage)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("job %s did not finish within deadline", jobID)
}

func TestRedownloadFromUrlPreservesTranslations(t *testing.T) {
	fx := setupRedownloadFixture(t, true)

	// Chapters 2 and 3 were already translated and refined; chapter 1 is not.
	for i, chID := range fx.chapterIDs[1:] {
		if _, err := fx.env.store.UpsertChapterWithoutStats(fx.alice.User.ID, fx.novelID, &store.Chapter{
			ID:                chID,
			ChapterOrder:      i + 2,
			Status:            "refined",
			TranslatedContent: fmt.Sprintf("Traducción preservada %d", i+2),
			RefinedContent:    fmt.Sprintf("Refinamiento preservado %d", i+2),
		}); err != nil {
			t.Fatalf("set translation on chapter: %v", err)
		}
	}

	// The source site fixes its chapter content.
	fx.setBody("It was a bright sunny morning.")

	resp := doJSONRequest(t, fx.env.handler, http.MethodPost, "/api/db/novels/"+fx.novelID+"/redownload-from-url", fx.alice.Token, map[string]any{})
	assertStatus(t, resp, http.StatusOK)
	var out struct {
		PendingChapters int    `json:"pendingChapters"`
		DownloadJobID   string `json:"downloadJobId"`
	}
	decodeResponse(t, resp, &out)
	if out.PendingChapters != 3 {
		t.Fatalf("expected 3 pending chapters, got %d", out.PendingChapters)
	}
	if out.DownloadJobID == "" {
		t.Fatal("expected a download job id")
	}
	waitForJobDone(t, fx.env, out.DownloadJobID)

	chapters := listFullChapters(t, fx.env, fx.alice, fx.novelID)
	if len(chapters) != 3 {
		t.Fatalf("expected 3 chapters, got %d", len(chapters))
	}
	for _, ch := range chapters {
		if !strings.Contains(ch.OriginalContent, "sunny morning") {
			t.Errorf("chapter %d original content not refreshed: %q", ch.ChapterOrder, ch.OriginalContent)
		}
		if strings.Contains(ch.OriginalContent, "stormy") {
			t.Errorf("chapter %d still has old content: %q", ch.ChapterOrder, ch.OriginalContent)
		}
	}
	// Chapters 2 and 3 keep their translations, refinements and status.
	for i, ch := range chapters[1:] {
		wantTr := fmt.Sprintf("Traducción preservada %d", i+2)
		if ch.TranslatedContent != wantTr {
			t.Errorf("chapter %d translation lost: got %q want %q", ch.ChapterOrder, ch.TranslatedContent, wantTr)
		}
		wantRef := fmt.Sprintf("Refinamiento preservado %d", i+2)
		if ch.RefinedContent != wantRef {
			t.Errorf("chapter %d refinement lost: got %q want %q", ch.ChapterOrder, ch.RefinedContent, wantRef)
		}
		if ch.Status != "refined" {
			t.Errorf("chapter %d status clobbered: got %q", ch.ChapterOrder, ch.Status)
		}
		if ch.Title != redownloadChapterTitles[i+1] {
			t.Errorf("chapter %d title clobbered: got %q", ch.ChapterOrder, ch.Title)
		}
	}
	// Chapter 1 stays pending (it had no translation).
	if chapters[0].Status != "pending" {
		t.Errorf("chapter 1 status changed: got %q", chapters[0].Status)
	}

	summaries := listChapterSummaries(t, fx.env, fx.alice, fx.novelID)
	if len(summaries) != 3 {
		t.Fatalf("expected 3 chapter summaries, got %d", len(summaries))
	}
	for _, ch := range summaries[1:] {
		wantTranslated := len(fmt.Sprintf("Traducción preservada %d", ch.ChapterOrder))
		wantRefined := len(fmt.Sprintf("Refinamiento preservado %d", ch.ChapterOrder))
		if ch.TranslatedChars != wantTranslated {
			t.Errorf("chapter %d translated character count incorrect: got %d want %d", ch.ChapterOrder, ch.TranslatedChars, wantTranslated)
		}
		if ch.RefinedChars != wantRefined {
			t.Errorf("chapter %d refined character count incorrect: got %d want %d", ch.ChapterOrder, ch.RefinedChars, wantRefined)
		}
	}

	statsResp := doJSONRequest(t, fx.env.handler, http.MethodGet, "/api/db/novels/"+fx.novelID+"/chapter-stats", fx.alice.Token, nil)
	assertStatus(t, statsResp, http.StatusOK)
	var stats struct {
		TranslatedCharacters int `json:"translatedCharacters"`
		RefinedCharacters    int `json:"refinedCharacters"`
	}
	decodeResponse(t, statsResp, &stats)
	wantTranslated, wantRefined := 0, 0
	for _, ch := range summaries {
		wantTranslated += ch.TranslatedChars
		wantRefined += ch.RefinedChars
	}
	if stats.TranslatedCharacters != wantTranslated {
		t.Errorf("novel translated character count incorrect: got %d want %d", stats.TranslatedCharacters, wantTranslated)
	}
	if stats.RefinedCharacters != wantRefined {
		t.Errorf("novel refined character count incorrect: got %d want %d", stats.RefinedCharacters, wantRefined)
	}
}

func TestRedownloadFromUrlRangeFilter(t *testing.T) {
	fx := setupRedownloadFixture(t, true)
	fx.setBody("It was a bright sunny morning.")

	resp := doJSONRequest(t, fx.env.handler, http.MethodPost, "/api/db/novels/"+fx.novelID+"/redownload-from-url", fx.alice.Token, map[string]any{
		"startChapter": 2,
		"endChapter":   2,
	})
	assertStatus(t, resp, http.StatusOK)
	var out struct {
		PendingChapters int    `json:"pendingChapters"`
		DownloadJobID   string `json:"downloadJobId"`
	}
	decodeResponse(t, resp, &out)
	if out.PendingChapters != 1 {
		t.Fatalf("expected 1 pending chapter, got %d", out.PendingChapters)
	}
	waitForJobDone(t, fx.env, out.DownloadJobID)

	for _, ch := range listFullChapters(t, fx.env, fx.alice, fx.novelID) {
		if ch.ChapterOrder == 2 {
			if !strings.Contains(ch.OriginalContent, "sunny morning") {
				t.Errorf("chapter 2 not refreshed: %q", ch.OriginalContent)
			}
		} else if !strings.Contains(ch.OriginalContent, "stormy night") {
			t.Errorf("chapter %d should not have been refreshed: %q", ch.ChapterOrder, ch.OriginalContent)
		}
	}
}

func TestRedownloadFromUrlRequiresSourceURL(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-redownload-no-url@example.com", "secret123", "Alice")
	novel := createNovel(t, env.handler, alice.Token, "Sin URL", "es", "en")

	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/db/novels/"+novel.ID+"/redownload-from-url", alice.Token, map[string]any{})
	assertStatus(t, resp, http.StatusBadRequest)
}

func TestRedownloadFromUrlNoMatchingChapters(t *testing.T) {
	fx := setupRedownloadFixture(t, false)

	resp := doJSONRequest(t, fx.env.handler, http.MethodPost, "/api/db/novels/"+fx.novelID+"/redownload-from-url", fx.alice.Token, map[string]any{})
	assertStatus(t, resp, http.StatusOK)
	var out map[string]any
	decodeResponse(t, resp, &out)
	if pending, _ := out["pendingChapters"].(float64); pending != 0 {
		t.Errorf("expected pendingChapters 0, got %v", out["pendingChapters"])
	}
	if _, has := out["downloadJobId"]; has {
		t.Errorf("expected no download job for zero matches, got %v", out["downloadJobId"])
	}
}

func TestRedownloadFromUrlTitleMismatchRequiresConfirmation(t *testing.T) {
	fx := setupRedownloadFixture(t, true)

	// The source site renumbers its chapters: the title at order 2 changed.
	fx.setTitles([]string{
		"Chapter 1: First Steps",
		"Chapter 2: The Journey (Revised)",
		"Chapter 3: The Return",
	})

	resp := doJSONRequest(t, fx.env.handler, http.MethodPost, "/api/db/novels/"+fx.novelID+"/redownload-from-url", fx.alice.Token, map[string]any{})
	assertStatus(t, resp, http.StatusOK)
	var preview struct {
		PendingChapters   int    `json:"pendingChapters"`
		TitleMismatches   int    `json:"titleMismatches"`
		NeedsConfirmation bool   `json:"needsConfirmation"`
		DownloadJobID     string `json:"downloadJobId"`
		Chapters          []struct {
			Order       int    `json:"order"`
			SourceTitle string `json:"sourceTitle"`
			StoredTitle string `json:"storedTitle"`
		} `json:"chapters"`
	}
	decodeResponse(t, resp, &preview)
	if !preview.NeedsConfirmation {
		t.Fatal("expected needsConfirmation=true when titles mismatch")
	}
	if preview.TitleMismatches != 1 {
		t.Fatalf("expected 1 title mismatch, got %d", preview.TitleMismatches)
	}
	if preview.PendingChapters != 3 {
		t.Fatalf("expected 3 pending chapters, got %d", preview.PendingChapters)
	}
	if preview.DownloadJobID != "" {
		t.Fatalf("preview must not create a job, got %s", preview.DownloadJobID)
	}
	if len(preview.Chapters) != 1 {
		t.Fatalf("expected 1 mismatched chapter, got %d", len(preview.Chapters))
	}
	m := preview.Chapters[0]
	if m.Order != 2 || m.SourceTitle != "Chapter 2: The Journey (Revised)" || m.StoredTitle != "Chapter 2: The Journey" {
		t.Errorf("unexpected mismatch detail: %+v", m)
	}

	// Nothing was enqueued yet.
	jobs, err := fx.env.store.ListJobs(fx.alice.User.ID, fx.novelID, false)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("preview created %d jobs, want 0", len(jobs))
	}

	// The user confirms; the job is created and the chapters are refreshed.
	fx.setBody("It was a bright sunny morning.")
	resp = doJSONRequest(t, fx.env.handler, http.MethodPost, "/api/db/novels/"+fx.novelID+"/redownload-from-url", fx.alice.Token, map[string]any{"confirm": true})
	assertStatus(t, resp, http.StatusOK)
	var out struct {
		PendingChapters int    `json:"pendingChapters"`
		DownloadJobID   string `json:"downloadJobId"`
	}
	decodeResponse(t, resp, &out)
	if out.PendingChapters != 3 || out.DownloadJobID == "" {
		t.Fatalf("unexpected confirm response: %+v", out)
	}
	waitForJobDone(t, fx.env, out.DownloadJobID)
	for _, ch := range listFullChapters(t, fx.env, fx.alice, fx.novelID) {
		if !strings.Contains(ch.OriginalContent, "sunny morning") {
			t.Errorf("chapter %d not refreshed: %q", ch.ChapterOrder, ch.OriginalContent)
		}
	}
}

func TestRedownloadFromUrlRejectsInvertedRange(t *testing.T) {
	fx := setupRedownloadFixture(t, true)

	resp := doJSONRequest(t, fx.env.handler, http.MethodPost, "/api/db/novels/"+fx.novelID+"/redownload-from-url", fx.alice.Token, map[string]any{
		"startChapter": 3,
		"endChapter":   1,
	})
	assertStatus(t, resp, http.StatusBadRequest)

	jobs, err := fx.env.store.ListJobs(fx.alice.User.ID, fx.novelID, false)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("expected no job for inverted range, got %+v", jobs)
	}
}

func TestRedownloadFromUrlRejectsActiveDownloadJob(t *testing.T) {
	fx := setupRedownloadFixture(t, true)

	job := &store.Job{
		NovelID:       fx.novelID,
		Status:        "running",
		Operation:     "download",
		ChapterIDs:    "[]",
		TotalChapters: 1,
	}
	if err := fx.env.store.CreateJob(fx.alice.User.ID, job); err != nil {
		t.Fatalf("create download job: %v", err)
	}

	resp := doJSONRequest(t, fx.env.handler, http.MethodPost, "/api/db/novels/"+fx.novelID+"/redownload-from-url", fx.alice.Token, map[string]any{})
	assertStatus(t, resp, http.StatusConflict)
}

func TestRedownloadFromUrlConcurrentRequestsConflict(t *testing.T) {
	fx := setupRedownloadFixture(t, true)

	// Park the created job in the queue so the download worker cannot consume
	// it between the two concurrent requests.
	oldQueue := fx.env.server.downloadQueue
	fx.env.server.downloadQueue = make(chan string, 10)
	close(oldQueue)

	var wg sync.WaitGroup
	codes := make([]int, 2)
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp := doJSONRequest(t, fx.env.handler, http.MethodPost, "/api/db/novels/"+fx.novelID+"/redownload-from-url", fx.alice.Token, map[string]any{"confirm": true})
			codes[i] = resp.Code
		}(i)
	}
	wg.Wait()

	sort.Ints(codes)
	if codes[0] != http.StatusOK || codes[1] != http.StatusConflict {
		t.Fatalf("expected one 200 and one 409, got %v", codes)
	}
}

func TestRedownloadFromUrlRejectsActiveTranslationJob(t *testing.T) {
	fx := setupRedownloadFixture(t, true)

	// A translation job for chapter 2 is pending.
	chapterIDsJSON, _ := json.Marshal([]string{fx.chapterIDs[1]})
	job := &store.Job{
		NovelID:       fx.novelID,
		Status:        "pending",
		Operation:     "translate",
		ChapterIDs:    string(chapterIDsJSON),
		TotalChapters: 1,
	}
	if err := fx.env.store.CreateJob(fx.alice.User.ID, job); err != nil {
		t.Fatalf("create translate job: %v", err)
	}

	resp := doJSONRequest(t, fx.env.handler, http.MethodPost, "/api/db/novels/"+fx.novelID+"/redownload-from-url", fx.alice.Token, map[string]any{})
	assertStatus(t, resp, http.StatusConflict)

	// No download job was created, only the translate one remains.
	jobs, err := fx.env.store.ListJobs(fx.alice.User.ID, fx.novelID, false)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].Operation != "translate" {
		t.Fatalf("expected only the translate job, got %+v", jobs)
	}

	// The confirmed request is refused the same way.
	resp = doJSONRequest(t, fx.env.handler, http.MethodPost, "/api/db/novels/"+fx.novelID+"/redownload-from-url", fx.alice.Token, map[string]any{"confirm": true})
	assertStatus(t, resp, http.StatusConflict)
}

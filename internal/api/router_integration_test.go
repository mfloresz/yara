package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase"
	"translator-server/internal/config"
	"translator-server/internal/secure"
	"translator-server/internal/store"
)

type apiTestEnv struct {
	handler http.Handler
	store   *store.Store
	server  *Server
}

type authPayload struct {
	Token string     `json:"token"`
	User  store.User `json:"user"`
}

type novelPayload struct {
	ID          string `json:"id"`
	OwnerID     string `json:"ownerId"`
	SourceTitle string `json:"sourceTitle"`
	IsPublic    bool   `json:"isPublic"`
}

type chapterPayload struct {
	ID                string `json:"id"`
	NovelID           string `json:"novelId"`
	ChapterOrder      int    `json:"chapterOrder"`
	Title             string `json:"title"`
	TranslatedTitle   string `json:"translatedTitle"`
	OriginalContent   string `json:"originalContent"`
	TranslatedContent string `json:"translatedContent"`
	RefinedContent    string `json:"refinedContent"`
	Status            string `json:"status"`
	ErrorMessage      string `json:"errorMessage"`
}

type jobPayload struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type activeJobStatusPayload struct {
	HasActive bool `json:"hasActive"`
}

type providersPayload struct {
	Providers []store.ProviderSetting `json:"providers"`
}

func TestAuthRegisterAndFetchMe(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice@example.com", "secret123", "Alice")

	resp := doJSONRequest(t, env.handler, http.MethodGet, "/api/auth/me", alice.Token, nil)
	assertStatus(t, resp, http.StatusOK)

	var me authPayload
	decodeResponse(t, resp, &me)
	if me.User.ID == "" {
		t.Fatalf("expected user id in /me response")
	}
	if me.User.Email != "alice@example.com" {
		t.Fatalf("expected email alice@example.com, got %q", me.User.Email)
	}
	if me.User.Theme != "system" {
		t.Fatalf("expected default theme system, got %q", me.User.Theme)
	}
}

func TestNovelResponseIncludesOwnerIDAndChapterStatusRequiresOwnership(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice@example.com", "secret123", "Alice")
	bob := registerUser(t, env.handler, "bob@example.com", "secret123", "Bob")

	novelResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/db/novels", alice.Token, map[string]any{
		"sourceTitle":    "Mi novela",
		"sourceLanguage": "es",
		"targetLanguage": "en",
	})
	assertStatus(t, novelResp, http.StatusCreated)

	var novel novelPayload
	decodeResponse(t, novelResp, &novel)
	if novel.OwnerID != alice.User.ID {
		t.Fatalf("expected ownerId %q, got %q", alice.User.ID, novel.OwnerID)
	}

	chapterResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/db/novels/"+novel.ID+"/chapters", alice.Token, map[string]any{
		"chapterOrder":    1,
		"title":           "Capítulo 1",
		"originalContent": "Hola mundo",
	})
	assertStatus(t, chapterResp, http.StatusCreated)

	var chapter chapterPayload
	decodeResponse(t, chapterResp, &chapter)

	forbiddenResp := doJSONRequest(t, env.handler, http.MethodPatch, "/api/db/novels/"+novel.ID+"/chapters/"+chapter.ID+"/status", bob.Token, map[string]any{
		"status":       "failed",
		"errorMessage": "intrusion",
	})
	assertStatus(t, forbiddenResp, http.StatusForbidden)

	okResp := doJSONRequest(t, env.handler, http.MethodPatch, "/api/db/novels/"+novel.ID+"/chapters/"+chapter.ID+"/status", alice.Token, map[string]any{
		"status":       "processing",
		"errorMessage": "",
	})
	assertStatus(t, okResp, http.StatusOK)

	decodeResponse(t, okResp, &chapter)
	if chapter.Status != "processing" {
		t.Fatalf("expected chapter status processing, got %q", chapter.Status)
	}
}

func TestChapterUpsertPreservesStatusWhenOmitted(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-preserve-status@example.com", "secret123", "Alice")

	novel := createNovel(t, env.handler, alice.Token, "Estado", "es", "en")
	chapter := createChapter(t, env.handler, alice.Token, novel.ID, 1)

	statusResp := doJSONRequest(t, env.handler, http.MethodPatch, "/api/db/novels/"+novel.ID+"/chapters/"+chapter.ID+"/status", alice.Token, map[string]any{
		"status":       "translated",
		"errorMessage": "",
	})
	assertStatus(t, statusResp, http.StatusOK)

	updateResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/db/novels/"+novel.ID+"/chapters", alice.Token, map[string]any{
		"id":                chapter.ID,
		"chapterOrder":      1,
		"title":             "Capítulo 1 editado",
		"originalContent":   "Texto original",
		"translatedContent": "Texto traducido manual",
	})
	assertStatus(t, updateResp, http.StatusCreated)

	var updatedChapter chapterPayload
	decodeResponse(t, updateResp, &updatedChapter)
	if updatedChapter.Status != "translated" {
		t.Fatalf("expected chapter status translated after manual save, got %q", updatedChapter.Status)
	}
}

func TestImportEpubPersistsCoverFile(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-cover@example.com", "secret123", "Alice")

	blob, err := os.ReadFile(filepath.Join("..", "..", "test", "epub.epub"))
	if err != nil {
		t.Skip("test epub not found:", err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	fileWriter, err := writer.CreateFormFile("file", "test.epub")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fileWriter.Write(blob); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := writer.WriteField("sourceLanguage", "en"); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := writer.WriteField("targetLanguage", "es"); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/db/novels/import-epub", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+alice.Token)

	rec := httptest.NewRecorder()
	env.handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var importResp struct {
		Novel map[string]any `json:"novel"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &importResp); err != nil {
		t.Fatalf("decode import response: %v", err)
	}
	coverPath, _ := importResp.Novel["coverPath"].(string)
	if coverPath == "" {
		t.Fatalf("expected coverPath in novel response, got %v", importResp.Novel["coverPath"])
	}

	coverReq := httptest.NewRequest(http.MethodGet, coverPath, nil)
	coverRec := httptest.NewRecorder()
	env.handler.ServeHTTP(coverRec, coverReq)
	if coverRec.Code != http.StatusOK {
		t.Fatalf("expected cover response 200, got %d: %s", coverRec.Code, coverRec.Body.String())
	}
	coverBody, err := io.ReadAll(coverRec.Body)
	if err != nil {
		t.Fatalf("read cover body: %v", err)
	}
	if len(coverBody) == 0 {
		t.Fatal("expected non-empty cover body")
	}
}

func TestImportZipIgnoresEmptyTranslatedFiles(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-zip-empty@example.com", "secret123", "Alice")

	var zipBuf bytes.Buffer
	zipWriter := zip.NewWriter(&zipBuf)
	addZipEntry := func(name, content string) {
		w, err := zipWriter.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}
	addZipEntry("metadata.json", `{"sourceTitle":"Zipped Novel","sourceLanguage":"en","targetLanguage":"es"}`)
	addZipEntry("originals/chapter-002.md", "Chapter 2: Situation\nOriginal body text.")
	// 0-byte placeholder: must not produce a translated title or content.
	addZipEntry("translated/chapter-002.md", "")
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	fileWriter, err := writer.CreateFormFile("file", "novel.zip")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fileWriter.Write(zipBuf.Bytes()); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/db/novels/import-from-zip", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+alice.Token)

	rec := httptest.NewRecorder()
	env.handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var importResp struct {
		Novel            map[string]any `json:"novel"`
		ChaptersImported int            `json:"chaptersImported"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &importResp); err != nil {
		t.Fatalf("decode import response: %v", err)
	}
	if importResp.ChaptersImported != 1 {
		t.Fatalf("expected 1 chapter imported, got %d", importResp.ChaptersImported)
	}

	chaptersResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/db/novels/"+importResp.Novel["id"].(string)+"/chapters", alice.Token, nil)
	assertStatus(t, chaptersResp, http.StatusOK)

	var chaptersFromAPI []chapterPayload
	decodeResponse(t, chaptersResp, &chaptersFromAPI)
	if len(chaptersFromAPI) != 1 {
		t.Fatalf("expected 1 chapter, got %d", len(chaptersFromAPI))
	}
	chapter := chaptersFromAPI[0]
	if chapter.Title != "Chapter 2: Situation" {
		t.Fatalf("expected original title %q, got %q", "Chapter 2: Situation", chapter.Title)
	}
	if chapter.TranslatedTitle != "" {
		t.Fatalf("expected empty translated title for empty translated file, got %q", chapter.TranslatedTitle)
	}
	if chapter.TranslatedContent != "" {
		t.Fatalf("expected empty translated content, got %q", chapter.TranslatedContent)
	}
	if chapter.Status != "pending" {
		t.Fatalf("expected chapter status pending, got %q", chapter.Status)
	}
}

func TestListNovelsSortByCreatedSucceeds(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-sort@example.com", "secret123", "Alice")
	createNovel(t, env.handler, alice.Token, "Ordenable", "en", "es")

	resp := doJSONRequest(t, env.handler, http.MethodGet, "/api/db/novels", alice.Token, nil)
	assertStatus(t, resp, http.StatusOK)

	var listResp struct {
		Items []map[string]any `json:"items"`
	}
	decodeResponse(t, resp, &listResp)
	if len(listResp.Items) != 1 {
		t.Fatalf("expected 1 novel in list, got %d", len(listResp.Items))
	}
}

func TestNovelCanUpdateFlag(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-canupdate@example.com", "secret123", "Alice")

	// Novel without a source URL is never updatable.
	noURL := createNovel(t, env.handler, alice.Token, "Sin URL", "en", "es")

	// Novel with a parser-supported URL is updatable.
	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/db/novels", alice.Token, map[string]any{
		"sourceTitle":    "Desde URL",
		"sourceLanguage": "en",
		"targetLanguage": "es",
		"url":            "https://www.novelfire.net/novel/123",
	})
	assertStatus(t, resp, http.StatusCreated)
	var withURL novelPayload
	decodeResponse(t, resp, &withURL)

	// Novel with a URL from an unsupported domain is not updatable.
	resp = doJSONRequest(t, env.handler, http.MethodPost, "/api/db/novels", alice.Token, map[string]any{
		"sourceTitle":    "Sitio desconocido",
		"sourceLanguage": "en",
		"targetLanguage": "es",
		"url":            "https://example.com/novel/123",
	})
	assertStatus(t, resp, http.StatusCreated)
	var unknownURL novelPayload
	decodeResponse(t, resp, &unknownURL)

	getCanUpdate := func(id string) bool {
		t.Helper()
		resp := doJSONRequest(t, env.handler, http.MethodGet, "/api/db/novels/"+id, alice.Token, nil)
		assertStatus(t, resp, http.StatusOK)
		var n struct {
			CanUpdate bool `json:"canUpdate"`
		}
		decodeResponse(t, resp, &n)
		return n.CanUpdate
	}

	if getCanUpdate(noURL.ID) {
		t.Fatalf("expected canUpdate=false for novel without URL")
	}
	if !getCanUpdate(withURL.ID) {
		t.Fatalf("expected canUpdate=true for novelfire.net novel")
	}
	if getCanUpdate(unknownURL.ID) {
		t.Fatalf("expected canUpdate=false for unsupported domain")
	}
}

func TestListNovelsSorting(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-sorting@example.com", "secret123", "Alice")

	createNovelFull := func(sourceTitle, targetTitle string) novelPayload {
		t.Helper()
		body := map[string]any{
			"sourceTitle":    sourceTitle,
			"sourceLanguage": "en",
			"targetLanguage": "es",
		}
		if targetTitle != "" {
			body["targetTitle"] = targetTitle
		}
		resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/db/novels", alice.Token, body)
		assertStatus(t, resp, http.StatusCreated)
		var n novelPayload
		decodeResponse(t, resp, &n)
		return n
	}

	bravo := createNovelFull("Bravo", "")
	time.Sleep(20 * time.Millisecond)
	alpha := createNovelFull("Alpha", "")
	time.Sleep(20 * time.Millisecond)
	// Zulu displays as "Charlie" (target title preferred), so title-sorted order is
	// Alpha, Bravo, Charlie while creation order is Bravo, Alpha, Zulu.
	zulu := createNovelFull("Zulu", "Charlie")

	listSorted := func(query string) (items []map[string]any, hasMore bool) {
		t.Helper()
		path := "/api/db/novels"
		if query != "" {
			path += "?" + query
		}
		resp := doJSONRequest(t, env.handler, http.MethodGet, path, alice.Token, nil)
		assertStatus(t, resp, http.StatusOK)
		var listResp struct {
			Items   []map[string]any `json:"items"`
			HasMore bool             `json:"hasMore"`
		}
		decodeResponse(t, resp, &listResp)
		return listResp.Items, listResp.HasMore
	}

	ids := func(items []map[string]any) []string {
		out := make([]string, 0, len(items))
		for _, it := range items {
			out = append(out, it["id"].(string))
		}
		return out
	}

	assertIDs := func(name string, got, want []string) {
		t.Helper()
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s = %v, want %v", name, got, want)
		}
	}

	// Default sort is title ascending, preferring target titles.
	def, _ := listSorted("")
	assertIDs("default sort (title asc)", ids(def), []string{alpha.ID, bravo.ID, zulu.ID})

	// Explicit title sort both directions.
	asc, _ := listSorted("sort=title&order=asc")
	assertIDs("title asc", ids(asc), []string{alpha.ID, bravo.ID, zulu.ID})
	desc, _ := listSorted("sort=title&order=desc")
	assertIDs("title desc", ids(desc), []string{zulu.ID, bravo.ID, alpha.ID})

	// created follows the UI convention: asc = most-recent first (Zulu created
	// last), desc = oldest first (Bravo created first).
	createdAsc, _ := listSorted("sort=created&order=asc")
	assertIDs("created asc", ids(createdAsc), []string{zulu.ID, alpha.ID, bravo.ID})
	createdDesc, _ := listSorted("sort=created&order=desc")
	assertIDs("created desc", ids(createdDesc), []string{bravo.ID, alpha.ID, zulu.ID})

	// Invalid sort/order values fall back to the defaults (title asc).
	fallback, _ := listSorted("sort=bogus&order=sideways")
	assertIDs("invalid sort/order fallback", ids(fallback), []string{alpha.ID, bravo.ID, zulu.ID})

	// lastRead requires reading progress: bravo read first, then zulu; alpha unread.
	// Like created, asc = most-recently-read first; unread (alpha) stays last.
	chBravo := createChapter(t, env.handler, alice.Token, bravo.ID, 1)
	time.Sleep(20 * time.Millisecond)
	chZulu := createChapter(t, env.handler, alice.Token, zulu.ID, 1)

	progressResp := doJSONRequest(t, env.handler, http.MethodPut, "/api/user/novels/"+bravo.ID+"/reading-progress", alice.Token, map[string]any{
		"chapterId": chBravo.ID, "scrollPercent": 0,
	})
	assertStatus(t, progressResp, http.StatusOK)
	time.Sleep(20 * time.Millisecond)
	progressResp = doJSONRequest(t, env.handler, http.MethodPut, "/api/user/novels/"+zulu.ID+"/reading-progress", alice.Token, map[string]any{
		"chapterId": chZulu.ID, "scrollPercent": 0,
	})
	assertStatus(t, progressResp, http.StatusOK)

	lastReadAsc, _ := listSorted("sort=lastRead&order=asc")
	assertIDs("lastRead asc", ids(lastReadAsc), []string{zulu.ID, bravo.ID, alpha.ID})
	lastReadDesc, _ := listSorted("sort=lastRead&order=desc")
	assertIDs("lastRead desc", ids(lastReadDesc), []string{bravo.ID, zulu.ID, alpha.ID})

	// Pagination stays globally consistent with the requested sort: page 1 + page 2
	// reconstructs the full title-asc order with no duplicates.
	page1, p1More := listSorted("sort=title&order=asc&limit=2&offset=0")
	if len(page1) != 2 || !p1More {
		t.Fatalf("expected 2 items with hasMore on page 1, got %d items hasMore=%v", len(page1), p1More)
	}
	page2, p2More := listSorted("sort=title&order=asc&limit=2&offset=2")
	if len(page2) != 1 || p2More {
		t.Fatalf("expected 1 item without hasMore on page 2, got %d items hasMore=%v", len(page2), p2More)
	}
	assertIDs("paged title asc", append(ids(page1), ids(page2)...), []string{alpha.ID, bravo.ID, zulu.ID})

	// Search applies the same sort/order (query "a" matches Alpha, Bravo, Charlie).
	searchDesc, _ := listSorted("q=a&sort=title&order=desc")
	assertIDs("search title desc", ids(searchDesc), []string{zulu.ID, bravo.ID, alpha.ID})
	searchFallback, _ := listSorted("q=a&sort=created&order=asc")
	assertIDs("search created asc", ids(searchFallback), []string{zulu.ID, alpha.ID, bravo.ID})
}

func TestImportedCoverIsPubliclyFetchable(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-public-cover@example.com", "secret123", "Alice")
	bob := registerUser(t, env.handler, "bob-public-cover@example.com", "secret123", "Bob")

	blob, err := os.ReadFile(filepath.Join("..", "..", "test", "epub.epub"))
	if err != nil {
		t.Skip("test epub not found:", err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	fileWriter, err := writer.CreateFormFile("file", "test.epub")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fileWriter.Write(blob); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := writer.WriteField("sourceLanguage", "en"); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := writer.WriteField("targetLanguage", "es"); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/db/novels/import-epub", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+alice.Token)

	rec := httptest.NewRecorder()
	env.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var importResp struct {
		Novel struct {
			CoverPath string `json:"coverPath"`
			ID        string `json:"id"`
		} `json:"novel"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &importResp); err != nil {
		t.Fatalf("decode import response: %v", err)
	}
	if importResp.Novel.CoverPath == "" {
		t.Fatalf("expected coverPath in novel response, got %v", rec.Body.String())
	}

	ownerReq := httptest.NewRequest(http.MethodGet, importResp.Novel.CoverPath, nil)
	ownerRec := httptest.NewRecorder()
	env.handler.ServeHTTP(ownerRec, ownerReq)
	if ownerRec.Code != http.StatusOK {
		t.Fatalf("expected cover response 200 for owner, got %d: %s", ownerRec.Code, ownerRec.Body.String())
	}

	_ = bob
}

func TestImportEpubWithLongDescriptionSucceeds(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-longdesc@example.com", "secret123", "Alice")

	matches, err := filepath.Glob(filepath.Join("..", "..", "test", "*.epub"))
	if err != nil {
		t.Fatalf("glob epubs: %v", err)
	}
	if len(matches) == 0 {
		t.Skip("no epubs in test/ directory")
	}

	var (
		blob       []byte
		uploadName string
	)
	for _, m := range matches {
		b, err := os.ReadFile(m)
		if err != nil {
			continue
		}
		blob = b
		uploadName = filepath.Base(m)
		break
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	fileWriter, err := writer.CreateFormFile("file", uploadName)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fileWriter.Write(blob); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := writer.WriteField("sourceLanguage", "en"); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := writer.WriteField("targetLanguage", "es"); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/db/novels/import-epub", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+alice.Token)

	rec := httptest.NewRecorder()
	env.handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var importResp struct {
		Novel map[string]any `json:"novel"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &importResp); err != nil {
		t.Fatalf("decode import response: %v", err)
	}
	for _, key := range []string{"sourceTitle", "sourceAuthor", "sourceDescription"} {
		if _, ok := importResp.Novel[key]; !ok {
			t.Fatalf("expected %q in novel response, got %v", key, importResp.Novel)
		}
	}
}

func TestActiveJobStatusAndCreatedJobMarksChapterProcessing(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-active-job@example.com", "secret123", "Alice")

	novel := createNovel(t, env.handler, alice.Token, "Trabajo activo", "es", "en")
	chapter := createChapter(t, env.handler, alice.Token, novel.ID, 1)

	chapterIDsJSON, err := json.Marshal([]string{chapter.ID})
	if err != nil {
		t.Fatalf("marshal chapter ids: %v", err)
	}
	activeJob := &store.Job{
		NovelID:       novel.ID,
		Status:        "pending",
		Operation:     "translate",
		ChapterIDs:    string(chapterIDsJSON),
		TotalChapters: 1,
	}
	if err := env.store.CreateJob(alice.User.ID, activeJob); err != nil {
		t.Fatalf("create active job: %v", err)
	}

	statusResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/db/translation-jobs/active/status", alice.Token, nil)
	assertStatus(t, statusResp, http.StatusOK)

	var activeStatus activeJobStatusPayload
	decodeResponse(t, statusResp, &activeStatus)
	if !activeStatus.HasActive {
		t.Fatal("expected hasActive=true when user has a pending job")
	}

	jobResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/db/novels/"+novel.ID+"/translation-jobs", alice.Token, map[string]any{
		"chapterIds": []string{chapter.ID},
		"operation":  "translate",
		"options": map[string]any{
			"provider": "venice",
			"model":    "deepseek-v4-flash",
		},
	})
	assertStatus(t, jobResp, http.StatusCreated)

	chapterResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/db/novels/"+novel.ID+"/chapters/"+chapter.ID, alice.Token, nil)
	assertStatus(t, chapterResp, http.StatusOK)

	var updatedChapter chapterPayload
	decodeResponse(t, chapterResp, &updatedChapter)
	if updatedChapter.Status != "processing" {
		t.Fatalf("expected job creation to mark chapter processing, got %q", updatedChapter.Status)
	}
}

func TestTranslationJobQueueRejectionResetsProcessingChapter(t *testing.T) {
	env := newAPITestEnv(t)
	oldQueue := env.server.translateQueue
	env.server.translateQueue = make(chan string)
	close(oldQueue)
	alice := registerUser(t, env.handler, "alice-queue@example.com", "secret123", "Alice")
	novel := createNovel(t, env.handler, alice.Token, "Trabajo", "es", "en")
	chapter := createChapter(t, env.handler, alice.Token, novel.ID, 1)

	jobResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/db/novels/"+novel.ID+"/translation-jobs", alice.Token, map[string]any{
		"chapterIds": []string{chapter.ID},
		"operation":  "translate",
		"options":    map[string]any{},
	})
	assertStatus(t, jobResp, http.StatusServiceUnavailable)
	jobs, err := env.store.ListJobs(alice.User.ID, novel.ID, false)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected exactly one job, got %d", len(jobs))
	}
	if jobs[0].Status != "failed" {
		t.Fatalf("expected rejected job to be failed, got %q", jobs[0].Status)
	}
	if jobs[0].ErrorMessage != jobQueueFullMessage {
		t.Fatalf("expected queue-full error message, got %q", jobs[0].ErrorMessage)
	}

	chapterResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/db/novels/"+novel.ID+"/chapters/"+chapter.ID, alice.Token, nil)
	assertStatus(t, chapterResp, http.StatusOK)
	var updatedChapter chapterPayload
	decodeResponse(t, chapterResp, &updatedChapter)
	if updatedChapter.Status != "pending" {
		t.Fatalf("expected rejected job chapter to reset to pending, got %q", updatedChapter.Status)
	}
}

func TestTranslationJobQueueRejectionWithWholeNovelResetsChapters(t *testing.T) {
	env := newAPITestEnv(t)
	oldQueue := env.server.translateQueue
	env.server.translateQueue = make(chan string)
	close(oldQueue)
	alice := registerUser(t, env.handler, "alice-novel-queue@example.com", "secret123", "Alice")
	novel := createNovel(t, env.handler, alice.Token, "Novela", "es", "en")
	first := createChapter(t, env.handler, alice.Token, novel.ID, 1)
	second := createChapter(t, env.handler, alice.Token, novel.ID, 2)

	// No chapterIds: the job covers the whole novel, so the handler marks every
	// chapter processing before the queue rejects it.
	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/db/novels/"+novel.ID+"/translation-jobs", alice.Token, map[string]any{
		"operation": "translate",
		"options":   map[string]any{},
	})
	assertStatus(t, resp, http.StatusServiceUnavailable)

	jobs, err := env.store.ListJobs(alice.User.ID, novel.ID, false)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].Status != "failed" {
		t.Fatalf("expected one failed job, got %+v", jobs)
	}

	for _, id := range []string{first.ID, second.ID} {
		chapterResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/db/novels/"+novel.ID+"/chapters/"+id, alice.Token, nil)
		assertStatus(t, chapterResp, http.StatusOK)
		var updated chapterPayload
		decodeResponse(t, chapterResp, &updated)
		if updated.Status != "pending" {
			t.Fatalf("expected chapter %s to reset to pending, got %q", id, updated.Status)
		}
	}
}

func TestJobPatchStatusTransitions(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-transitions@example.com", "secret123", "Alice")
	novel := createNovel(t, env.handler, alice.Token, "Transiciones", "es", "en")
	chapter := createChapter(t, env.handler, alice.Token, novel.ID, 1)
	ids, _ := json.Marshal([]string{chapter.ID})

	newJob := func(status string) string {
		t.Helper()
		job := &store.Job{
			NovelID:       novel.ID,
			Status:        status,
			Operation:     "translate",
			ChapterIDs:    string(ids),
			TotalChapters: 1,
		}
		if err := env.store.CreateJob(alice.User.ID, job); err != nil {
			t.Fatalf("create job: %v", err)
		}
		return job.ID
	}
	patchStatus := func(jobID string, status string) *httptest.ResponseRecorder {
		t.Helper()
		return doJSONRequest(t, env.handler, http.MethodPatch, "/api/db/translation-jobs/"+jobID, alice.Token, map[string]any{
			"status": status,
		})
	}

	pendingID := newJob("pending")
	assertStatus(t, patchStatus(pendingID, "pending"), http.StatusConflict)

	runningID := newJob("running")
	assertStatus(t, patchStatus(runningID, "pending"), http.StatusConflict)

	failedID := newJob("failed")
	assertStatus(t, patchStatus(failedID, "bogus"), http.StatusBadRequest)
	assertStatus(t, patchStatus(failedID, "pending"), http.StatusOK)

	assertStatus(t, patchStatus(runningID, "cancelled"), http.StatusOK)
}

func TestBatchTranslateQueueRejectionMarkedInResponse(t *testing.T) {
	env := newAPITestEnv(t)
	oldQueue := env.server.translateQueue
	env.server.translateQueue = make(chan string)
	close(oldQueue)
	alice := registerUser(t, env.handler, "alice-batch-queue@example.com", "secret123", "Alice")
	novel := createNovel(t, env.handler, alice.Token, "Lote", "es", "en")
	chapter := createChapter(t, env.handler, alice.Token, novel.ID, 1)

	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/db/novels/batch-translate", alice.Token, map[string]any{
		"selections": []map[string]any{
			{"novelId": novel.ID, "chapterIds": []string{chapter.ID}},
		},
	})
	assertStatus(t, resp, http.StatusOK)

	var result struct {
		Jobs []struct {
			NovelID       string `json:"novelId"`
			JobID         string `json:"jobId"`
			EnqueueFailed bool   `json:"enqueueFailed"`
		} `json:"jobs"`
		TotalPending int `json:"totalPending"`
	}
	decodeResponse(t, resp, &result)
	if len(result.Jobs) != 1 {
		t.Fatalf("expected one job entry, got %+v", result.Jobs)
	}
	if !result.Jobs[0].EnqueueFailed {
		t.Fatalf("expected enqueueFailed on rejected job entry: %+v", result.Jobs[0])
	}
	if result.TotalPending != 0 {
		t.Fatalf("expected totalPending 0 for rejected job, got %d", result.TotalPending)
	}

	storedJob, err := env.store.GetJob(result.Jobs[0].JobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if storedJob.Status != "failed" {
		t.Fatalf("expected rejected job to be failed, got %q", storedJob.Status)
	}

	chapterResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/db/novels/"+novel.ID+"/chapters/"+chapter.ID, alice.Token, nil)
	assertStatus(t, chapterResp, http.StatusOK)
	var updated chapterPayload
	decodeResponse(t, chapterResp, &updated)
	if updated.Status != "pending" {
		t.Fatalf("expected rejected batch chapter to reset to pending, got %q", updated.Status)
	}
}

func TestJobPatchRequiresOwner(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice@example.com", "secret123", "Alice")
	bob := registerUser(t, env.handler, "bob@example.com", "secret123", "Bob")

	novel := createNovel(t, env.handler, alice.Token, "Trabajo", "es", "en")
	chapter := createChapter(t, env.handler, alice.Token, novel.ID, 1)

	// Create the job directly: a job created through the HTTP endpoint would be
	// picked up by the live translation worker and race the PATCH assertions.
	ids, _ := json.Marshal([]string{chapter.ID})
	job := &store.Job{
		NovelID:       novel.ID,
		Status:        "pending",
		Operation:     "translate",
		ChapterIDs:    string(ids),
		TotalChapters: 1,
	}
	if err := env.store.CreateJob(alice.User.ID, job); err != nil {
		t.Fatalf("create job: %v", err)
	}

	forbiddenResp := doJSONRequest(t, env.handler, http.MethodPatch, "/api/db/translation-jobs/"+job.ID, bob.Token, map[string]any{
		"status": "cancelled",
	})
	assertStatus(t, forbiddenResp, http.StatusForbidden)

	processingResp := doJSONRequest(t, env.handler, http.MethodPatch, "/api/db/novels/"+novel.ID+"/chapters/"+chapter.ID+"/status", alice.Token, map[string]any{
		"status":       "processing",
		"errorMessage": "",
	})
	assertStatus(t, processingResp, http.StatusOK)

	okResp := doJSONRequest(t, env.handler, http.MethodPatch, "/api/db/translation-jobs/"+job.ID, alice.Token, map[string]any{
		"status": "cancelled",
	})
	assertStatus(t, okResp, http.StatusOK)

	var patched jobPayload
	decodeResponse(t, okResp, &patched)
	if patched.Status != "cancelled" {
		t.Fatalf("expected job status cancelled, got %q", patched.Status)
	}

	chapterResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/db/novels/"+novel.ID+"/chapters/"+chapter.ID, alice.Token, nil)
	assertStatus(t, chapterResp, http.StatusOK)

	var updatedChapter chapterPayload
	decodeResponse(t, chapterResp, &updatedChapter)
	if updatedChapter.Status != "pending" {
		t.Fatalf("expected cancelled job chapter to reset to pending, got %q", updatedChapter.Status)
	}
}

func TestDeleteChapterRemovesJobReferencesAndUpdatesStats(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-delete-chapter@example.com", "secret123", "Alice")

	novel := createNovel(t, env.handler, alice.Token, "Novela", "es", "en")
	chapter1 := createChapter(t, env.handler, alice.Token, novel.ID, 1)
	chapter2 := createChapter(t, env.handler, alice.Token, novel.ID, 2)

	chapterIDsJSON, err := json.Marshal([]string{chapter1.ID, chapter2.ID})
	if err != nil {
		t.Fatalf("marshal chapter ids: %v", err)
	}
	job := &store.Job{
		NovelID:                 novel.ID,
		Status:                  "pending",
		Operation:               "translate",
		ChapterIDs:              string(chapterIDsJSON),
		TotalChapters:           2,
		AutoSegmentChapterID:    chapter1.ID,
		AutoSegmentChapterTitle: "Capítulo",
	}
	if err := env.store.CreateJob(alice.User.ID, job); err != nil {
		t.Fatalf("create job: %v", err)
	}

	deleteResp := doJSONRequest(t, env.handler, http.MethodDelete, "/api/db/novels/"+novel.ID+"/chapters/"+chapter1.ID, alice.Token, nil)
	assertStatus(t, deleteResp, http.StatusOK)

	deletedChapterResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/db/novels/"+novel.ID+"/chapters/"+chapter1.ID, alice.Token, nil)
	assertStatus(t, deletedChapterResp, http.StatusNotFound)

	updatedJob, err := env.store.GetOwnedJob(alice.User.ID, job.ID)
	if err != nil {
		t.Fatalf("get updated job: %v", err)
	}
	var updatedIDs []string
	if err := json.Unmarshal([]byte(updatedJob.ChapterIDs), &updatedIDs); err != nil {
		t.Fatalf("decode updated job chapter ids: %v", err)
	}
	if len(updatedIDs) != 1 || updatedIDs[0] != chapter2.ID {
		t.Fatalf("expected job to keep only surviving chapter id %q, got %#v", chapter2.ID, updatedIDs)
	}
	if updatedJob.AutoSegmentChapterID != "" || updatedJob.AutoSegmentChapterTitle != "" {
		t.Fatalf("expected auto segment refs cleared after chapter delete, got id=%q title=%q", updatedJob.AutoSegmentChapterID, updatedJob.AutoSegmentChapterTitle)
	}
	if updatedJob.TotalChapters != 1 {
		t.Fatalf("expected pending job total chapters to shrink to 1, got %d", updatedJob.TotalChapters)
	}

	stats, err := env.store.GetChapterStatsAccessible(alice.User.ID, novel.ID)
	if err != nil {
		t.Fatalf("get chapter stats: %v", err)
	}
	if stats.TotalChapters != 1 {
		t.Fatalf("expected chapter_count=1 after delete, got %d", stats.TotalChapters)
	}
}

func TestDeleteNovelCascadesRelatedRecords(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-delete-novel@example.com", "secret123", "Alice")

	imported, err := env.store.ImportEpubNovel(&store.ImportEpubNovelInput{
		OwnerID:        alice.User.ID,
		FileName:       "novela.epub",
		FileBlob:       []byte("fake-epub"),
		MimeType:       "application/epub+zip",
		SourceTitle:    "Novela completa",
		SourceLanguage: "es",
		TargetLanguage: "en",
		CoverMime:      "image/png",
		CoverBlob:      []byte("fake-cover"),
		Chapters: []store.ImportedEpubChapter{
			{Title: "Capítulo 1", Content: "Texto 1"},
			{Title: "Capítulo 2", Content: "Texto 2"},
		},
	})
	if err != nil {
		t.Fatalf("import novel: %v", err)
	}
	chapters, err := env.store.ListChaptersAccessible(alice.User.ID, imported.Novel.ID)
	if err != nil {
		t.Fatalf("list chapters: %v", err)
	}
	if len(chapters) != 2 {
		t.Fatalf("expected 2 chapters after import, got %d", len(chapters))
	}
	chapterIDs := []string{chapters[0].ID, chapters[1].ID}
	chapterIDsJSON, err := json.Marshal(chapterIDs)
	if err != nil {
		t.Fatalf("marshal chapter ids: %v", err)
	}
	job := &store.Job{NovelID: imported.Novel.ID, Status: "pending", Operation: "translate", ChapterIDs: string(chapterIDsJSON), TotalChapters: len(chapterIDs)}
	if err := env.store.CreateJob(alice.User.ID, job); err != nil {
		t.Fatalf("create job: %v", err)
	}

	deleteResp := doJSONRequest(t, env.handler, http.MethodDelete, "/api/db/novels/"+imported.Novel.ID, alice.Token, nil)
	assertStatus(t, deleteResp, http.StatusOK)

	novelResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/db/novels/"+imported.Novel.ID, alice.Token, nil)
	assertStatus(t, novelResp, http.StatusNotFound)

	chapterRecords, err := env.store.App.FindRecordsByFilter(store.ChaptersCollection, "novel = {:novel}", "", 10, 0, map[string]any{"novel": imported.Novel.ID})
	if err != nil {
		t.Fatalf("find chapter records: %v", err)
	}
	if len(chapterRecords) != 0 {
		t.Fatalf("expected no chapter records after novel delete, got %d", len(chapterRecords))
	}

	jobRecords, err := env.store.App.FindRecordsByFilter(store.JobsCollection, "novel = {:novel}", "", 10, 0, map[string]any{"novel": imported.Novel.ID})
	if err != nil {
		t.Fatalf("find job records: %v", err)
	}
	if len(jobRecords) != 0 {
		t.Fatalf("expected no job records after novel delete, got %d", len(jobRecords))
	}

	epubRecords, err := env.store.App.FindRecordsByFilter(store.EpubsCollection, "novel = {:novel}", "", 10, 0, map[string]any{"novel": imported.Novel.ID})
	if err != nil {
		t.Fatalf("find epub records: %v", err)
	}
	if len(epubRecords) != 0 {
		t.Fatalf("expected no epub records after novel delete, got %d", len(epubRecords))
	}

	if imported.Novel.CoverPath != "" {
		coverResp := doJSONRequest(t, env.handler, http.MethodGet, imported.Novel.CoverPath, "", nil)
		assertStatus(t, coverResp, http.StatusNotFound)
	}
}

func TestProviderAPIKeysAreWriteOnlyAndRevocable(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice@example.com", "secret123", "Alice")
	secret := "super-secret-api-key"

	replaceResp := doJSONRequest(t, env.handler, http.MethodPut, "/api/user/providers/venice/key", alice.Token, map[string]any{
		"apiKey": secret,
	})
	assertStatus(t, replaceResp, http.StatusOK)
	body := readBody(t, replaceResp)
	if strings.Contains(body, secret) {
		t.Fatalf("provider key leaked in replace response: %s", body)
	}

	listResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/user/providers", alice.Token, nil)
	assertStatus(t, listResp, http.StatusOK)
	listBody := readBody(t, listResp)
	if strings.Contains(listBody, secret) {
		t.Fatalf("provider key leaked in list response: %s", listBody)
	}

	var providers providersPayload
	decodeStringResponse(t, listBody, &providers)
	venice := findProvider(t, providers.Providers, "venice")
	if !venice.APIKeyConfigured {
		t.Fatalf("expected venice api key to be marked configured")
	}
	if venice.APIKeyUpdatedAt == "" {
		t.Fatalf("expected venice api key updated timestamp")
	}

	deleteResp := doJSONRequest(t, env.handler, http.MethodDelete, "/api/user/providers/venice/key", alice.Token, nil)
	assertStatus(t, deleteResp, http.StatusNoContent)

	resolved, err := env.store.ResolveProviderAISettings(alice.User.ID, "venice")
	if err != nil {
		t.Fatalf("resolve provider settings: %v", err)
	}
	if resolved.APIKey != "" {
		t.Fatalf("expected resolved api key to be empty after delete, got %q", resolved.APIKey)
	}

	finalResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/user/providers", alice.Token, nil)
	assertStatus(t, finalResp, http.StatusOK)
	var finalProviders providersPayload
	decodeResponse(t, finalResp, &finalProviders)
	venice = findProvider(t, finalProviders.Providers, "venice")
	if venice.APIKeyConfigured {
		t.Fatalf("expected venice api key to be unconfigured after delete")
	}
}

func TestProviderConfiguredTimeoutMsIsRespected(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice@example.com", "secret123", "Alice")

	updateResp := doJSONRequest(t, env.handler, http.MethodPut, "/api/user/providers/venice", alice.Token, map[string]any{
		"model":     "deepseek-v4-flash",
		"baseUrl":   "https://api.venice.ai/api/v1",
		"timeoutMs": 600000,
	})
	assertStatus(t, updateResp, http.StatusOK)

	resolved, err := env.store.ResolveProviderAISettings(alice.User.ID, "venice")
	if err != nil {
		t.Fatalf("resolve provider settings: %v", err)
	}
	if resolved.TimeoutMs != 600000 {
		t.Fatalf("expected resolved TimeoutMs=600000 (user-configured), got %d", resolved.TimeoutMs)
	}

	clearResp := doJSONRequest(t, env.handler, http.MethodPut, "/api/user/providers/venice", alice.Token, map[string]any{
		"model":     "deepseek-v4-flash",
		"baseUrl":   "https://api.venice.ai/api/v1",
		"timeoutMs": 0,
	})
	assertStatus(t, clearResp, http.StatusOK)

	defaultResolved, err := env.store.ResolveProviderAISettings(alice.User.ID, "venice")
	if err != nil {
		t.Fatalf("resolve provider settings: %v", err)
	}
	if defaultResolved.TimeoutMs != store.DefaultAISettings.TimeoutMs {
		t.Fatalf("expected resolved TimeoutMs to fall back to default %d when unset, got %d", store.DefaultAISettings.TimeoutMs, defaultResolved.TimeoutMs)
	}
}

func newAPITestEnv(t *testing.T) *apiTestEnv {
	t.Helper()

	dataDir := t.TempDir()
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: dataDir})
	if err := app.Bootstrap(); err != nil {
		t.Fatalf("bootstrap pocketbase: %v", err)
	}

	encryptor, err := secure.NewEncryptorFromConfig("", filepath.Join(dataDir, "app.key"))
	if err != nil {
		t.Fatalf("create encryptor: %v", err)
	}

	st := store.New(app, encryptor)
	if err := st.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	server := New(st, &config.Config{DataDir: dataDir})
	return &apiTestEnv{handler: Router(server), store: st, server: server}
}

func registerUser(t *testing.T, handler http.Handler, email, password, name string) authPayload {
	t.Helper()
	resp := doJSONRequest(t, handler, http.MethodPost, "/api/auth/register", "", map[string]any{
		"email":    email,
		"password": password,
		"name":     name,
	})
	assertStatus(t, resp, http.StatusCreated)
	var out authPayload
	decodeResponse(t, resp, &out)
	if out.Token == "" {
		t.Fatalf("expected auth token for %s", email)
	}
	return out
}

func createNovel(t *testing.T, handler http.Handler, token, title, sourceLanguage, targetLanguage string) novelPayload {
	t.Helper()
	resp := doJSONRequest(t, handler, http.MethodPost, "/api/db/novels", token, map[string]any{
		"sourceTitle":    title,
		"sourceLanguage": sourceLanguage,
		"targetLanguage": targetLanguage,
	})
	assertStatus(t, resp, http.StatusCreated)
	var novel novelPayload
	decodeResponse(t, resp, &novel)
	return novel
}

func TestCleanOnlyOriginalsPreservesOtherFields(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-clean@example.com", "secret123", "Alice")

	novel := createNovel(t, env.handler, alice.Token, "Limpieza", "es", "en")
	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/db/novels/"+novel.ID+"/chapters", alice.Token, map[string]any{
		"chapterOrder":      1,
		"title":             "Título Original",
		"translatedTitle":   "Translated Title",
		"originalContent":   "línea uno\nlínea dos\nBORRAR DESPUÉS\nlínea tres",
		"translatedContent": "translated one\ntranslated two\ntranslated three",
		"refinedContent":    "refined one\nrefined two\nrefined three",
	})
	assertStatus(t, resp, http.StatusCreated)
	var chapter chapterPayload
	decodeResponse(t, resp, &chapter)

	cleanResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/db/novels/"+novel.ID+"/chapters/clean", alice.Token, map[string]any{
		"chapterIds": []string{chapter.ID},
		"mode":       "remove_after",
		"searchText": "BORRAR",
		"applyTo":    "original",
	})
	assertStatus(t, cleanResp, http.StatusOK)

	fetchResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/db/novels/"+novel.ID+"/chapters/"+chapter.ID, alice.Token, nil)
	assertStatus(t, fetchResp, http.StatusOK)
	var updated chapterPayload
	decodeResponse(t, fetchResp, &updated)

	if updated.OriginalContent != "línea uno\nlínea dos" {
		t.Fatalf("original content not cleaned as expected, got %q", updated.OriginalContent)
	}
	if updated.TranslatedContent != "translated one\ntranslated two\ntranslated three" {
		t.Fatalf("translated content was overwritten, got %q", updated.TranslatedContent)
	}
	if updated.RefinedContent != "refined one\nrefined two\nrefined three" {
		t.Fatalf("refined content was overwritten, got %q", updated.RefinedContent)
	}
	if updated.Title != "Título Original" {
		t.Fatalf("title was overwritten, got %q", updated.Title)
	}
	if updated.TranslatedTitle != "Translated Title" {
		t.Fatalf("translated title was overwritten, got %q", updated.TranslatedTitle)
	}
}

func TestCleanPreviewBulkReturnsOnlyChangedChapters(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-preview@example.com", "secret123", "Alice")

	novel := createNovel(t, env.handler, alice.Token, "Vista Previa", "es", "en")

	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/db/novels/"+novel.ID+"/chapters", alice.Token, map[string]any{
		"chapterOrder":    1,
		"title":           "Capítulo Uno",
		"originalContent": "línea uno\nlínea dos\nBORRAR\nlínea tres",
	})
	assertStatus(t, resp, http.StatusCreated)
	var ch1 chapterPayload
	decodeResponse(t, resp, &ch1)

	resp = doJSONRequest(t, env.handler, http.MethodPost, "/api/db/novels/"+novel.ID+"/chapters", alice.Token, map[string]any{
		"chapterOrder":    2,
		"title":           "Capítulo Dos",
		"originalContent": "sin coincidencia alguna",
	})
	assertStatus(t, resp, http.StatusCreated)
	var ch2 chapterPayload
	decodeResponse(t, resp, &ch2)

	previewResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/db/novels/"+novel.ID+"/chapters/clean-preview-bulk", alice.Token, map[string]any{
		"chapterIds": []string{ch1.ID, ch2.ID},
		"mode":       "remove_after",
		"searchText": "BORRAR",
		"applyTo":    "original",
	})
	assertStatus(t, previewResp, http.StatusOK)

	var body struct {
		Items   []CleanPreviewBulkItem `json:"items"`
		Total   int                    `json:"total"`
		Changed int                    `json:"changed"`
	}
	decodeResponse(t, previewResp, &body)

	if body.Total != 2 {
		t.Fatalf("expected total=2, got %d", body.Total)
	}
	if body.Changed != 1 {
		t.Fatalf("expected changed=1, got %d", body.Changed)
	}
	if len(body.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(body.Items))
	}
	item := body.Items[0]
	if item.ChapterID != ch1.ID {
		t.Fatalf("expected chapter %q, got %q", ch1.ID, item.ChapterID)
	}
	if item.ChapterTitle != "Capítulo Uno" {
		t.Fatalf("expected chapter title %q, got %q", "Capítulo Uno", item.ChapterTitle)
	}
	if item.Cleaned != "línea uno\nlínea dos" {
		t.Fatalf("expected cleaned %q, got %q", "línea uno\nlínea dos", item.Cleaned)
	}
	if !item.Changed {
		t.Fatal("expected changed=true on the affected chapter")
	}
	if len(item.Changes) == 0 {
		t.Fatal("expected changes to be present on the affected chapter")
	}
	foundRemoval := false
	for _, hunk := range item.Changes {
		if len(hunk.Before) > 0 && len(hunk.After) == 0 {
			foundRemoval = true
		}
	}
	if !foundRemoval {
		t.Fatalf("expected at least one pure-removal hunk, got %+v", item.Changes)
	}

	// Invalid mode is rejected.
	badResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/db/novels/"+novel.ID+"/chapters/clean-preview-bulk", alice.Token, map[string]any{
		"chapterIds": []string{ch1.ID},
		"mode":       "nope",
		"applyTo":    "original",
	})
	assertStatus(t, badResp, http.StatusBadRequest)
}

func createChapter(t *testing.T, handler http.Handler, token, novelID string, order int) chapterPayload {
	t.Helper()
	resp := doJSONRequest(t, handler, http.MethodPost, "/api/db/novels/"+novelID+"/chapters", token, map[string]any{
		"chapterOrder":    order,
		"title":           "Capítulo",
		"originalContent": "Texto original",
	})
	assertStatus(t, resp, http.StatusCreated)
	var chapter chapterPayload
	decodeResponse(t, resp, &chapter)
	return chapter
}

func doJSONRequest(t *testing.T, handler http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
	}

	req := httptest.NewRequest(method, path, bytes.NewReader(payload))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	return resp
}

func assertStatus(t *testing.T, resp *httptest.ResponseRecorder, want int) {
	t.Helper()
	if resp.Code != want {
		t.Fatalf("expected status %d, got %d: %s", want, resp.Code, resp.Body.String())
	}
}

func decodeResponse(t *testing.T, resp *httptest.ResponseRecorder, out any) {
	t.Helper()
	decodeStringResponse(t, resp.Body.String(), out)
}

func decodeStringResponse(t *testing.T, body string, out any) {
	t.Helper()
	if err := json.Unmarshal([]byte(body), out); err != nil {
		t.Fatalf("decode response: %v\nbody: %s", err, body)
	}
}

func readBody(t *testing.T, resp *httptest.ResponseRecorder) string {
	t.Helper()
	return resp.Body.String()
}

func findProvider(t *testing.T, providers []store.ProviderSetting, key string) store.ProviderSetting {
	t.Helper()
	for _, provider := range providers {
		if provider.Provider == key {
			return provider
		}
	}
	t.Fatalf("provider %q not found in response", key)
	return store.ProviderSetting{}
}

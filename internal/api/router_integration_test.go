package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
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
	Position          int    `json:"position"`
	Excluded          bool   `json:"excluded"`
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

	resp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/auth/me", alice.Token, nil)
	assertStatus(t, resp, http.StatusOK)

	var me store.User
	decodeResponse(t, resp, &me)
	if me.ID == "" {
		t.Fatalf("expected user id in /me response")
	}
	if me.Email != "alice@example.com" {
		t.Fatalf("expected email alice@example.com, got %q", me.Email)
	}
	if me.Theme != "system" {
		t.Fatalf("expected default theme system, got %q", me.Theme)
	}
}

func TestNovelResponseIncludesOwnerIDAndChapterStatusRequiresOwnership(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice@example.com", "secret123", "Alice")
	bob := registerUser(t, env.handler, "bob@example.com", "secret123", "Bob")

	novelResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels", alice.Token, map[string]any{
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

	chapterResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/"+novel.ID+"/chapters", alice.Token, map[string]any{
		"chapterOrder":    1,
		"title":           "Capítulo 1",
		"originalContent": "Hola mundo",
	})
	assertStatus(t, chapterResp, http.StatusCreated)

	var chapter chapterPayload
	decodeResponse(t, chapterResp, &chapter)

	forbiddenResp := doJSONRequest(t, env.handler, http.MethodPatch, "/api/v1/novels/"+novel.ID+"/chapters/"+chapter.ID+"/status", bob.Token, map[string]any{
		"status":       "failed",
		"errorMessage": "intrusion",
	})
	assertStatus(t, forbiddenResp, http.StatusForbidden)

	okResp := doJSONRequest(t, env.handler, http.MethodPatch, "/api/v1/novels/"+novel.ID+"/chapters/"+chapter.ID+"/status", alice.Token, map[string]any{
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

	statusResp := doJSONRequest(t, env.handler, http.MethodPatch, "/api/v1/novels/"+novel.ID+"/chapters/"+chapter.ID+"/status", alice.Token, map[string]any{
		"status":       "translated",
		"errorMessage": "",
	})
	assertStatus(t, statusResp, http.StatusOK)

	updateResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/"+novel.ID+"/chapters", alice.Token, map[string]any{
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

	req := httptest.NewRequest(http.MethodPost, "/api/v1/novels/import-epub", &body)
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

	req := httptest.NewRequest(http.MethodPost, "/api/v1/novels/import-zip", &body)
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
	decodeData(t, rec, &importResp)
	if importResp.ChaptersImported != 1 {
		t.Fatalf("expected 1 chapter imported, got %d", importResp.ChaptersImported)
	}

	chaptersResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels/"+importResp.Novel["id"].(string)+"/chapters", alice.Token, nil)
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

	resp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels", alice.Token, nil)
	assertStatus(t, resp, http.StatusOK)

	var listResp []map[string]any
	decodeResponse(t, resp, &listResp)
	if len(listResp) != 1 {
		t.Fatalf("expected 1 novel in list, got %d", len(listResp))
	}
}

func TestNovelCanUpdateFlag(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-canupdate@example.com", "secret123", "Alice")

	// Novel without a source URL is never updatable.
	noURL := createNovel(t, env.handler, alice.Token, "Sin URL", "en", "es")

	// Novel with a parser-supported URL is updatable.
	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels", alice.Token, map[string]any{
		"sourceTitle":    "Desde URL",
		"sourceLanguage": "en",
		"targetLanguage": "es",
		"url":            "https://www.novelfire.net/novel/123",
	})
	assertStatus(t, resp, http.StatusCreated)
	var withURL novelPayload
	decodeResponse(t, resp, &withURL)

	// Novel with a URL from an unsupported domain is not updatable.
	resp = doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels", alice.Token, map[string]any{
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
		resp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels/"+id, alice.Token, nil)
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
		resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels", alice.Token, body)
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
		path := "/api/v1/novels"
		if query != "" {
			path += "?" + query
		}
		resp := doJSONRequest(t, env.handler, http.MethodGet, path, alice.Token, nil)
		assertStatus(t, resp, http.StatusOK)
		var envelope struct {
			Data []map[string]any `json:"data"`
			Meta *v1Meta          `json:"meta"`
		}
		decodeRaw(t, resp, &envelope)
		return envelope.Data, envelope.Meta != nil && envelope.Meta.HasMore
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

	progressResp := doJSONRequest(t, env.handler, http.MethodPut, "/api/v1/novels/"+bravo.ID+"/reading-progress", alice.Token, map[string]any{
		"chapterId": chBravo.ID, "scrollPercent": 0,
	})
	assertStatus(t, progressResp, http.StatusOK)
	time.Sleep(20 * time.Millisecond)
	progressResp = doJSONRequest(t, env.handler, http.MethodPut, "/api/v1/novels/"+zulu.ID+"/reading-progress", alice.Token, map[string]any{
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

	req := httptest.NewRequest(http.MethodPost, "/api/v1/novels/import-epub", &body)
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

	req := httptest.NewRequest(http.MethodPost, "/api/v1/novels/import-epub", &body)
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

	statusResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/jobs/active", alice.Token, nil)
	assertStatus(t, statusResp, http.StatusOK)

	var activeStatus activeJobStatusPayload
	decodeResponse(t, statusResp, &activeStatus)
	if !activeStatus.HasActive {
		t.Fatal("expected hasActive=true when user has a pending job")
	}

	jobResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/"+novel.ID+"/jobs", alice.Token, map[string]any{
		"chapterIds": []string{chapter.ID},
		"operation":  "translate",
		"options": map[string]any{
			"provider": "venice",
			"model":    "deepseek-v4-flash",
		},
	})
	assertStatus(t, jobResp, http.StatusCreated)

	chapterResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels/"+novel.ID+"/chapters/"+chapter.ID, alice.Token, nil)
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

	jobResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/"+novel.ID+"/jobs", alice.Token, map[string]any{
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

	chapterResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels/"+novel.ID+"/chapters/"+chapter.ID, alice.Token, nil)
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
	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/"+novel.ID+"/jobs", alice.Token, map[string]any{
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
		chapterResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels/"+novel.ID+"/chapters/"+id, alice.Token, nil)
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
		// v1 split PATCH /jobs/{id} into POST /jobs/{id}/cancel and
		// /jobs/{id}/retry. Other status values are not accepted on v1.
		switch status {
		case "cancelled":
			return doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/jobs/"+jobID+"/cancel", alice.Token, nil)
		case "pending":
			return doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/jobs/"+jobID+"/retry", alice.Token, nil)
		default:
			return doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/jobs/"+jobID+"/retry", alice.Token, map[string]any{"status": status})
		}
	}

	pendingID := newJob("pending")
	assertStatus(t, patchStatus(pendingID, "pending"), http.StatusConflict)

	runningID := newJob("running")
	assertStatus(t, patchStatus(runningID, "pending"), http.StatusConflict)

	failedID := newJob("failed")
	// On v1, PATCH /jobs/{id} is replaced by POST /jobs/{id}/cancel and
	// /jobs/{id}/retry. There is no generic status endpoint, so an
	// arbitrary status value no longer returns 400 — it just maps to /retry
	// (which sets status to "pending"). The 200 here is intentional.
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

	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/batch-translate", alice.Token, map[string]any{
		"selections": []map[string]any{
			{"novelId": novel.ID, "chapterIds": []string{chapter.ID}},
		},
	})
	assertStatus(t, resp, http.StatusAccepted)

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

	chapterResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels/"+novel.ID+"/chapters/"+chapter.ID, alice.Token, nil)
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

	forbiddenResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/jobs/"+job.ID+"/cancel", bob.Token, nil)
	assertStatus(t, forbiddenResp, http.StatusForbidden)

	processingResp := doJSONRequest(t, env.handler, http.MethodPatch, "/api/v1/novels/"+novel.ID+"/chapters/"+chapter.ID+"/status", alice.Token, map[string]any{
		"status":       "processing",
		"errorMessage": "",
	})
	assertStatus(t, processingResp, http.StatusOK)

	okResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/jobs/"+job.ID+"/cancel", alice.Token, nil)
	assertStatus(t, okResp, http.StatusOK)

	var patched jobPayload
	decodeResponse(t, okResp, &patched)
	if patched.Status != "cancelled" {
		t.Fatalf("expected job status cancelled, got %q", patched.Status)
	}

	chapterResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels/"+novel.ID+"/chapters/"+chapter.ID, alice.Token, nil)
	assertStatus(t, chapterResp, http.StatusOK)

	var updatedChapter chapterPayload
	decodeResponse(t, chapterResp, &updatedChapter)
	if updatedChapter.Status != "pending" {
		t.Fatalf("expected cancelled job chapter to reset to pending, got %q", updatedChapter.Status)
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

	deleteResp := doJSONRequest(t, env.handler, http.MethodDelete, "/api/v1/novels/"+imported.Novel.ID, alice.Token, nil)
	assertStatus(t, deleteResp, http.StatusNoContent)

	novelResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels/"+imported.Novel.ID, alice.Token, nil)
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

	replaceResp := doJSONRequest(t, env.handler, http.MethodPut, "/api/v1/providers/venice/key", alice.Token, map[string]any{
		"apiKey": secret,
	})
	assertStatus(t, replaceResp, http.StatusOK)
	body := readBody(t, replaceResp)
	if strings.Contains(body, secret) {
		t.Fatalf("provider key leaked in replace response: %s", body)
	}

	listResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/providers", alice.Token, nil)
	assertStatus(t, listResp, http.StatusOK)
	listBody := readBody(t, listResp)
	if strings.Contains(listBody, secret) {
		t.Fatalf("provider key leaked in list response: %s", listBody)
	}

	var providers providersPayload
	decodeResponse(t, listResp, &providers)
	venice := findProvider(t, providers.Providers, "venice")
	if !venice.APIKeyConfigured {
		t.Fatalf("expected venice api key to be marked configured")
	}
	if venice.APIKeyUpdatedAt == "" {
		t.Fatalf("expected venice api key updated timestamp")
	}

	deleteResp := doJSONRequest(t, env.handler, http.MethodDelete, "/api/v1/providers/venice/key", alice.Token, nil)
	assertStatus(t, deleteResp, http.StatusNoContent)

	resolved, err := env.store.ResolveProviderAISettings(alice.User.ID, "venice")
	if err != nil {
		t.Fatalf("resolve provider settings: %v", err)
	}
	if resolved.APIKey != "" {
		t.Fatalf("expected resolved api key to be empty after delete, got %q", resolved.APIKey)
	}

	finalResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/providers", alice.Token, nil)
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

	updateResp := doJSONRequest(t, env.handler, http.MethodPut, "/api/v1/providers/venice", alice.Token, map[string]any{
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

	clearResp := doJSONRequest(t, env.handler, http.MethodPut, "/api/v1/providers/venice", alice.Token, map[string]any{
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

func TestExcludeChapterPreservesRecordAndUpdatesStats(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-exclude-chapter@example.com", "secret123", "Alice")

	novel := createNovel(t, env.handler, alice.Token, "Novela", "es", "en")
	chapter1 := createChapter(t, env.handler, alice.Token, novel.ID, 1)
	chapter2 := createChapter(t, env.handler, alice.Token, novel.ID, 2)

	chapterIDsJSON, err := json.Marshal([]string{chapter1.ID, chapter2.ID})
	if err != nil {
		t.Fatalf("marshal chapter ids: %v", err)
	}
	job := &store.Job{
		NovelID:                 novel.ID,
		Status:                  "done",
		Operation:               "translate",
		ChapterIDs:              string(chapterIDsJSON),
		TotalChapters:           2,
		AutoSegmentChapterID:    chapter1.ID,
		AutoSegmentChapterTitle: "Capítulo",
	}
	if err := env.store.CreateJob(alice.User.ID, job); err != nil {
		t.Fatalf("create job: %v", err)
	}

	deleteResp := doJSONRequest(t, env.handler, http.MethodDelete, "/api/v1/novels/"+novel.ID+"/chapters/"+chapter1.ID, alice.Token, nil)
	assertStatus(t, deleteResp, http.StatusNoContent)

	// Logical delete: the record still exists, keeps its ID/source order and is
	// flagged as excluded.
	excludedResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels/"+novel.ID+"/chapters/"+chapter1.ID, alice.Token, nil)
	assertStatus(t, excludedResp, http.StatusOK)
	var stored chapterPayload
	decodeResponse(t, excludedResp, &stored)
	if stored.ID != chapter1.ID || !stored.Excluded {
		t.Fatalf("expected chapter to be retained with excluded=true, got %#v", stored)
	}
	if stored.ChapterOrder != 1 {
		t.Fatalf("expected chapter_order to be preserved, got %d", stored.ChapterOrder)
	}

	// The excluded chapter is hidden from the normal list and visible in the
	// excluded listing.
	listResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels/"+novel.ID+"/chapters", alice.Token, nil)
	assertStatus(t, listResp, http.StatusOK)
	var visible []map[string]any
	decodeResponse(t, listResp, &visible)
	for _, item := range visible {
		if item["id"] == chapter1.ID {
			t.Fatalf("excluded chapter must not appear in the normal chapter list")
		}
	}
	excludedListResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels/"+novel.ID+"/chapters/excluded", alice.Token, nil)
	assertStatus(t, excludedListResp, http.StatusOK)
	var excludedList []map[string]any
	decodeResponse(t, excludedListResp, &excludedList)
	if len(excludedList) != 1 || excludedList[0]["id"] != chapter1.ID {
		t.Fatalf("expected the excluded listing to contain chapter1, got %#v", excludedList)
	}

	// Job references are preserved as history (the job is not active, and
	// excluded chapters are filtered at job-load time).
	updatedJob, err := env.store.GetOwnedJob(alice.User.ID, job.ID)
	if err != nil {
		t.Fatalf("get updated job: %v", err)
	}
	var updatedIDs []string
	if err := json.Unmarshal([]byte(updatedJob.ChapterIDs), &updatedIDs); err != nil {
		t.Fatalf("decode job chapter ids: %v", err)
	}
	if len(updatedIDs) != 2 {
		t.Fatalf("expected job chapter ids to be preserved after exclusion, got %#v", updatedIDs)
	}

	stats, err := env.store.GetChapterStatsAccessible(alice.User.ID, novel.ID)
	if err != nil {
		t.Fatalf("get chapter stats: %v", err)
	}
	if stats.TotalChapters != 1 {
		t.Fatalf("expected chapter_count=1 after exclusion, got %d", stats.TotalChapters)
	}
	if stats.MaxChapterOrder != 2 {
		t.Fatalf("expected max_chapter_order=2 (source maximum, includes excluded), got %d", stats.MaxChapterOrder)
	}
}

func TestReorderChaptersUpdatesPositionsAtomically(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-reorder@example.com", "secret123", "Alice")

	novel := createNovel(t, env.handler, alice.Token, "Novela", "es", "en")
	chapters := make([]chapterPayload, 5)
	for i := 1; i <= 5; i++ {
		chapters[i-1] = createChapter(t, env.handler, alice.Token, novel.ID, i)
	}

	// Complete permutation: reversed source order.
	ids := []string{chapters[4].ID, chapters[3].ID, chapters[2].ID, chapters[1].ID, chapters[0].ID}
	resp := doJSONRequest(t, env.handler, http.MethodPatch, "/api/v1/novels/"+novel.ID+"/chapters/order", alice.Token, map[string]any{"chapterIds": ids})
	assertStatus(t, resp, http.StatusOK)

	for i, id := range ids {
		got, err := env.store.GetChapterAccessible(alice.User.ID, novel.ID, id)
		if err != nil {
			t.Fatalf("get chapter %s: %v", id, err)
		}
		if got.Position != i+1 {
			t.Fatalf("expected position %d for %s, got %d", i+1, id, got.Position)
		}
		// chapter_order (source order) must never be touched by reorder.
		if got.ChapterOrder != 5-i {
			t.Fatalf("expected chapter_order %d for %s, got %d", 5-i, id, got.ChapterOrder)
		}
		if got.Title != "Capítulo" || got.OriginalContent != "Texto original" {
			t.Fatalf("reorder must preserve content/title for %s", id)
		}
	}

	// The normal list now returns chapters in position order.
	listResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels/"+novel.ID+"/chapters", alice.Token, nil)
	assertStatus(t, listResp, http.StatusOK)
	var list []map[string]any
	decodeResponse(t, listResp, &list)
	if len(list) != 5 {
		t.Fatalf("expected 5 chapters in list, got %d", len(list))
	}
	for i, item := range list {
		if item["id"] != ids[i] {
			t.Fatalf("expected list order to match reorder; item %d is %v, want %s", i, item["id"], ids[i])
		}
	}
}

func TestReorderRejectsInvalidLists(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-reorder-invalid@example.com", "secret123", "Alice")
	bob := registerUser(t, env.handler, "bob-reorder-invalid@example.com", "secret123", "Bob")

	novel := createNovel(t, env.handler, alice.Token, "Novela", "es", "en")
	other := createNovel(t, env.handler, bob.Token, "Otra", "es", "en")
	c1 := createChapter(t, env.handler, alice.Token, novel.ID, 1)
	c2 := createChapter(t, env.handler, alice.Token, novel.ID, 2)
	foreign := createChapter(t, env.handler, bob.Token, other.ID, 1)

	base := "/api/v1/novels/" + novel.ID + "/chapters/order"

	// Empty list.
	resp := doJSONRequest(t, env.handler, http.MethodPatch, base, alice.Token, map[string]any{"chapterIds": []string{}})
	assertStatus(t, resp, http.StatusBadRequest)

	// Duplicate IDs.
	resp = doJSONRequest(t, env.handler, http.MethodPatch, base, alice.Token, map[string]any{"chapterIds": []string{c1.ID, c1.ID, c2.ID}})
	assertStatus(t, resp, http.StatusBadRequest)

	// Foreign chapter ID (belongs to another novel).
	resp = doJSONRequest(t, env.handler, http.MethodPatch, base, alice.Token, map[string]any{"chapterIds": []string{c1.ID, c2.ID, foreign.ID}})
	assertStatus(t, resp, http.StatusBadRequest)

	// Partial list (missing chapters of the novel).
	resp = doJSONRequest(t, env.handler, http.MethodPatch, base, alice.Token, map[string]any{"chapterIds": []string{c1.ID}})
	assertStatus(t, resp, http.StatusBadRequest)

	// Another user cannot reorder a novel they do not own.
	resp = doJSONRequest(t, env.handler, http.MethodPatch, base, bob.Token, map[string]any{"chapterIds": []string{c2.ID, c1.ID}})
	assertStatus(t, resp, http.StatusForbidden)
}

func TestReorderRejectedDuringActiveJobs(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-reorder-active@example.com", "secret123", "Alice")

	novel := createNovel(t, env.handler, alice.Token, "Novela", "es", "en")
	c1 := createChapter(t, env.handler, alice.Token, novel.ID, 1)
	c2 := createChapter(t, env.handler, alice.Token, novel.ID, 2)

	idsJSON, err := json.Marshal([]string{c1.ID, c2.ID})
	if err != nil {
		t.Fatalf("marshal ids: %v", err)
	}
	job := &store.Job{NovelID: novel.ID, Status: "pending", Operation: "translate", ChapterIDs: string(idsJSON), TotalChapters: 2}
	if err := env.store.CreateJob(alice.User.ID, job); err != nil {
		t.Fatalf("create job: %v", err)
	}

	resp := doJSONRequest(t, env.handler, http.MethodPatch, "/api/v1/novels/"+novel.ID+"/chapters/order", alice.Token, map[string]any{"chapterIds": []string{c2.ID, c1.ID}})
	assertStatus(t, resp, http.StatusConflict)
}

func TestExcludeAndRestoreChapter(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-visibility@example.com", "secret123", "Alice")
	bob := registerUser(t, env.handler, "bob-visibility@example.com", "secret123", "Bob")

	novel := createNovel(t, env.handler, alice.Token, "Novela", "es", "en")
	c1 := createChapter(t, env.handler, alice.Token, novel.ID, 1)
	c2 := createChapter(t, env.handler, alice.Token, novel.ID, 2)

	// Another user cannot change visibility.
	del := doJSONRequest(t, env.handler, http.MethodDelete, "/api/v1/novels/"+novel.ID+"/chapters/"+c1.ID, bob.Token, nil)
	assertStatus(t, del, http.StatusForbidden)
	vis := doJSONRequest(t, env.handler, http.MethodPatch, "/api/v1/novels/"+novel.ID+"/chapters/"+c1.ID+"/visibility", bob.Token, map[string]any{"excluded": true})
	assertStatus(t, vis, http.StatusForbidden)

	// Exclude via DELETE.
	deleteResp := doJSONRequest(t, env.handler, http.MethodDelete, "/api/v1/novels/"+novel.ID+"/chapters/"+c1.ID, alice.Token, nil)
	assertStatus(t, deleteResp, http.StatusNoContent)

	// Restore via the visibility endpoint.
	restoreResp := doJSONRequest(t, env.handler, http.MethodPatch, "/api/v1/novels/"+novel.ID+"/chapters/"+c1.ID+"/visibility", alice.Token, map[string]any{"excluded": false})
	assertStatus(t, restoreResp, http.StatusOK)
	var restored chapterPayload
	decodeResponse(t, restoreResp, &restored)
	if restored.Excluded {
		t.Fatalf("expected restored chapter to be visible, got %#v", restored)
	}
	if restored.Position != c1.Position {
		t.Fatalf("expected restore to keep the original position %d, got %d", c1.Position, restored.Position)
	}

	listResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels/"+novel.ID+"/chapters", alice.Token, nil)
	assertStatus(t, listResp, http.StatusOK)
	var list []map[string]any
	decodeResponse(t, listResp, &list)
	if len(list) != 2 {
		t.Fatalf("expected both chapters visible after restore, got %d", len(list))
	}
	_ = c2
}

func TestExcludedChaptersHiddenFromPublicReaders(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-public-excluded@example.com", "secret123", "Alice")
	bob := registerUser(t, env.handler, "bob-public-excluded@example.com", "secret123", "Bob")

	novel := createNovel(t, env.handler, alice.Token, "Novela", "es", "en")
	c1 := createChapter(t, env.handler, alice.Token, novel.ID, 1)
	c2 := createChapter(t, env.handler, alice.Token, novel.ID, 2)

	// Share the novel publicly, then exclude chapter 2.
	visResp := doJSONRequest(t, env.handler, http.MethodPatch, "/api/v1/novels/"+novel.ID+"/visibility", alice.Token, map[string]any{"isPublic": true})
	assertStatus(t, visResp, http.StatusOK)
	delResp := doJSONRequest(t, env.handler, http.MethodDelete, "/api/v1/novels/"+novel.ID+"/chapters/"+c2.ID, alice.Token, nil)
	assertStatus(t, delResp, http.StatusNoContent)

	// Non-owners keep read access to non-excluded chapters.
	ok := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels/"+novel.ID+"/chapters/"+c1.ID, bob.Token, nil)
	assertStatus(t, ok, http.StatusOK)

	// Excluded chapters are invisible to non-owners even by direct ID.
	hidden := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels/"+novel.ID+"/chapters/"+c2.ID, bob.Token, nil)
	assertStatus(t, hidden, http.StatusNotFound)

	// The excluded listing is owner-only.
	ownerOnly := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels/"+novel.ID+"/chapters/excluded", bob.Token, nil)
	assertStatus(t, ownerOnly, http.StatusForbidden)

	// The owner can still inspect the excluded chapter and list it.
	inspect := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels/"+novel.ID+"/chapters/"+c2.ID, alice.Token, nil)
	assertStatus(t, inspect, http.StatusOK)
	var owned chapterPayload
	decodeResponse(t, inspect, &owned)
	if !owned.Excluded {
		t.Fatalf("expected chapter to be marked excluded for the owner, got %#v", owned)
	}
	excludedResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels/"+novel.ID+"/chapters/excluded", alice.Token, nil)
	assertStatus(t, excludedResp, http.StatusOK)
	var excluded []map[string]any
	decodeResponse(t, excludedResp, &excluded)
	if len(excluded) != 1 || excluded[0]["id"] != c2.ID {
		t.Fatalf("expected only chapter 2 in the excluded listing, got %#v", excluded)
	}

	// The non-owner chapter list omits excluded chapters.
	listResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels/"+novel.ID+"/chapters", bob.Token, nil)
	assertStatus(t, listResp, http.StatusOK)
	var list []map[string]any
	decodeResponse(t, listResp, &list)
	if len(list) != 1 || list[0]["id"] != c1.ID {
		t.Fatalf("expected non-owner list to contain only chapter 1, got %#v", list)
	}
}

func TestPendingSelectionFollowsPositionOrder(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-pending-order@example.com", "secret123", "Alice")

	novel := createNovel(t, env.handler, alice.Token, "Novela", "es", "en")
	chapters := make([]chapterPayload, 3)
	for i := 1; i <= 3; i++ {
		chapters[i-1] = createChapter(t, env.handler, alice.Token, novel.ID, i)
	}

	// Manual reorder [c3, c1, c2] assigns positions 1, 2, 3.
	ids := []string{chapters[2].ID, chapters[0].ID, chapters[1].ID}
	resp := doJSONRequest(t, env.handler, http.MethodPatch, "/api/v1/novels/"+novel.ID+"/chapters/order", alice.Token, map[string]any{"chapterIds": ids})
	assertStatus(t, resp, http.StatusOK)

	pending, err := env.store.GetOwnedNovelChapterIDsByStatus(alice.User.ID, novel.ID)
	if err != nil {
		t.Fatalf("get pending chapter ids: %v", err)
	}
	if len(pending) != 3 {
		t.Fatalf("expected 3 pending chapters, got %#v", pending)
	}
	for i, id := range ids {
		if pending[i] != id {
			t.Fatalf("expected pending selection in manual position order; item %d = %s, want %s (full: %#v)", i, pending[i], id, pending)
		}
	}
}

func TestExcludedChapterNotReportedAsGapOrRecreated(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-gaps@example.com", "secret123", "Alice")

	novel := createNovel(t, env.handler, alice.Token, "Novela", "es", "en")
	chapters := make([]chapterPayload, 5)
	for i := 1; i <= 5; i++ {
		chapters[i-1] = createChapter(t, env.handler, alice.Token, novel.ID, i)
	}

	// Exclude chapter 3.
	deleteResp := doJSONRequest(t, env.handler, http.MethodDelete, "/api/v1/novels/"+novel.ID+"/chapters/"+chapters[2].ID, alice.Token, nil)
	assertStatus(t, deleteResp, http.StatusNoContent)

	// Real source gaps: none, because order 3 still exists (excluded).
	gapsResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels/"+novel.ID+"/chapters/gaps", alice.Token, nil)
	assertStatus(t, gapsResp, http.StatusOK)
	var gapsPayload struct {
		Gaps           []store.ChapterGap `json:"gaps"`
		ExcludedOrders []int              `json:"excludedOrders"`
	}
	decodeResponse(t, gapsResp, &gapsPayload)
	if len(gapsPayload.Gaps) != 0 {
		t.Fatalf("expected no source gaps when the skipped order is excluded, got %#v", gapsPayload.Gaps)
	}
	if len(gapsPayload.ExcludedOrders) != 1 || gapsPayload.ExcludedOrders[0] != 3 {
		t.Fatalf("expected excludedOrders=[3], got %#v", gapsPayload.ExcludedOrders)
	}

	// Source synchronization still treats order 3 as existing.
	existingOrders, err := env.store.GetExistingChapterOrders(alice.User.ID, novel.ID)
	if err != nil {
		t.Fatalf("get existing orders: %v", err)
	}
	if !existingOrders[3] {
		t.Fatalf("expected excluded chapter order 3 to count as existing")
	}
	existingTitles, err := env.store.GetExistingChapterURLs(alice.User.ID, novel.ID)
	if err != nil {
		t.Fatalf("get existing titles: %v", err)
	}
	if !existingTitles["Capítulo"] {
		t.Fatalf("expected excluded chapter title to count as existing")
	}
}

func TestExcludedChaptersExcludedFromJobsAndEligible(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-jobs-excluded@example.com", "secret123", "Alice")

	novel := createNovel(t, env.handler, alice.Token, "Novela", "es", "en")
	c1 := createChapter(t, env.handler, alice.Token, novel.ID, 1)
	c2 := createChapter(t, env.handler, alice.Token, novel.ID, 2)
	c3 := createChapter(t, env.handler, alice.Token, novel.ID, 3)

	// Exclude chapter 2.
	deleteResp := doJSONRequest(t, env.handler, http.MethodDelete, "/api/v1/novels/"+novel.ID+"/chapters/"+c2.ID, alice.Token, nil)
	assertStatus(t, deleteResp, http.StatusNoContent)

	// Eligible list omits excluded chapters.
	eligibleResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels/"+novel.ID+"/chapters/eligible?operation=translate", alice.Token, nil)
	assertStatus(t, eligibleResp, http.StatusOK)
	var eligible []map[string]any
	decodeResponse(t, eligibleResp, &eligible)
	if len(eligible) != 2 {
		t.Fatalf("expected 2 eligible chapters, got %d: %#v", len(eligible), eligible)
	}
	for _, item := range eligible {
		if item["id"] == c2.ID {
			t.Fatalf("excluded chapter must not appear in eligible list")
		}
	}

	// Batch-translate "all" (pending status) omits excluded chapters.
	pendingIDs, err := env.store.GetOwnedNovelChapterIDsByStatus(alice.User.ID, novel.ID)
	if err != nil {
		t.Fatalf("get pending ids: %v", err)
	}
	if len(pendingIDs) != 2 {
		t.Fatalf("expected 2 pending chapter ids, got %#v", pendingIDs)
	}
	for _, id := range pendingIDs {
		if id == c2.ID {
			t.Fatalf("excluded chapter must not be selected by 'all'")
		}
	}

	// LoadJobChapters filters excluded chapters even when explicitly listed in
	// the job, and orders the survivors by position.
	idsJSON, err := json.Marshal([]string{c2.ID, c3.ID, c1.ID})
	if err != nil {
		t.Fatalf("marshal ids: %v", err)
	}
	job := &store.Job{NovelID: novel.ID, Status: "pending", Operation: "translate", ChapterIDs: string(idsJSON), TotalChapters: 3}
	if err := env.store.CreateJob(alice.User.ID, job); err != nil {
		t.Fatalf("create job: %v", err)
	}
	loaded, _, err := env.store.LoadJobChapters(job)
	if err != nil {
		t.Fatalf("load job chapters: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected job to skip the excluded chapter, got %d chapters", len(loaded))
	}
	if loaded[0].ID != c1.ID || loaded[1].ID != c3.ID {
		t.Fatalf("expected job chapters in position order [c1, c3], got %s, %s", loaded[0].ID, loaded[1].ID)
	}
}

func TestExclusionRejectedDuringActiveJobs(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-exclude-active@example.com", "secret123", "Alice")

	novel := createNovel(t, env.handler, alice.Token, "Novela", "es", "en")
	c1 := createChapter(t, env.handler, alice.Token, novel.ID, 1)

	idsJSON, err := json.Marshal([]string{c1.ID})
	if err != nil {
		t.Fatalf("marshal ids: %v", err)
	}
	job := &store.Job{NovelID: novel.ID, Status: "pending", Operation: "translate", ChapterIDs: string(idsJSON), TotalChapters: 1}
	if err := env.store.CreateJob(alice.User.ID, job); err != nil {
		t.Fatalf("create job: %v", err)
	}

	deleteResp := doJSONRequest(t, env.handler, http.MethodDelete, "/api/v1/novels/"+novel.ID+"/chapters/"+c1.ID, alice.Token, nil)
	assertStatus(t, deleteResp, http.StatusConflict)
	bulkResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/"+novel.ID+"/chapters/bulk-delete", alice.Token, map[string]any{"ids": []string{c1.ID}})
	assertStatus(t, bulkResp, http.StatusConflict)

	// The chapter is still visible after the rejected exclusion.
	listResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels/"+novel.ID+"/chapters", alice.Token, nil)
	assertStatus(t, listResp, http.StatusOK)
	var list []map[string]any
	decodeResponse(t, listResp, &list)
	if len(list) != 1 {
		t.Fatalf("expected the chapter to remain visible, got %d items", len(list))
	}
}

func TestChapterStatsExcludeExcludedChapters(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-stats-excluded@example.com", "secret123", "Alice")

	novel := createNovel(t, env.handler, alice.Token, "Novela", "es", "en")
	c1 := createChapter(t, env.handler, alice.Token, novel.ID, 1)
	createChapter(t, env.handler, alice.Token, novel.ID, 2)

	// Mark chapter 1 as translated so it contributes to translated counts.
	statusResp := doJSONRequest(t, env.handler, http.MethodPatch, "/api/v1/novels/"+novel.ID+"/chapters/"+c1.ID+"/status", alice.Token, map[string]any{"status": "translated", "errorMessage": ""})
	assertStatus(t, statusResp, http.StatusOK)

	before, err := env.store.GetChapterStatsAccessible(alice.User.ID, novel.ID)
	if err != nil {
		t.Fatalf("get stats before: %v", err)
	}
	if before.TotalChapters != 2 || before.TranslatedChapters != 1 {
		t.Fatalf("unexpected baseline stats: %+v", before)
	}

	deleteResp := doJSONRequest(t, env.handler, http.MethodDelete, "/api/v1/novels/"+novel.ID+"/chapters/"+c1.ID, alice.Token, nil)
	assertStatus(t, deleteResp, http.StatusNoContent)

	after, err := env.store.GetChapterStatsAccessible(alice.User.ID, novel.ID)
	if err != nil {
		t.Fatalf("get stats after: %v", err)
	}
	if after.TotalChapters != 1 {
		t.Fatalf("expected totalChapters=1 after exclusion, got %d", after.TotalChapters)
	}
	if after.TranslatedChapters != 0 {
		t.Fatalf("expected translatedChapters=0 after excluding the translated chapter, got %d", after.TranslatedChapters)
	}
	if after.MaxChapterOrder != 2 {
		t.Fatalf("expected maxChapterOrder=2 (includes excluded), got %d", after.MaxChapterOrder)
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
	resp := doJSONRequest(t, handler, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
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
	resp := doJSONRequest(t, handler, http.MethodPost, "/api/v1/novels", token, map[string]any{
		"sourceTitle":    title,
		"sourceLanguage": sourceLanguage,
		"targetLanguage": targetLanguage,
	})
	assertStatus(t, resp, http.StatusCreated)
	var novel novelPayload
	decodeData(t, resp, &novel)
	return novel
}

func TestCleanOnlyOriginalsPreservesOtherFields(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-clean@example.com", "secret123", "Alice")

	novel := createNovel(t, env.handler, alice.Token, "Limpieza", "es", "en")
	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/"+novel.ID+"/chapters", alice.Token, map[string]any{
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

	cleanResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/"+novel.ID+"/chapters/clean", alice.Token, map[string]any{
		"chapterIds": []string{chapter.ID},
		"mode":       "remove_after",
		"searchText": "BORRAR",
		"applyTo":    "original",
	})
	assertStatus(t, cleanResp, http.StatusOK)

	fetchResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels/"+novel.ID+"/chapters/"+chapter.ID, alice.Token, nil)
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

	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/"+novel.ID+"/chapters", alice.Token, map[string]any{
		"chapterOrder":    1,
		"title":           "Capítulo Uno",
		"originalContent": "línea uno\nlínea dos\nBORRAR\nlínea tres",
	})
	assertStatus(t, resp, http.StatusCreated)
	var ch1 chapterPayload
	decodeResponse(t, resp, &ch1)

	resp = doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/"+novel.ID+"/chapters", alice.Token, map[string]any{
		"chapterOrder":    2,
		"title":           "Capítulo Dos",
		"originalContent": "sin coincidencia alguna",
	})
	assertStatus(t, resp, http.StatusCreated)
	var ch2 chapterPayload
	decodeResponse(t, resp, &ch2)

	previewResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/"+novel.ID+"/chapters/clean-preview-bulk", alice.Token, map[string]any{
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
	badResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/"+novel.ID+"/chapters/clean-preview-bulk", alice.Token, map[string]any{
		"chapterIds": []string{ch1.ID},
		"mode":       "nope",
		"applyTo":    "original",
	})
	assertStatus(t, badResp, http.StatusBadRequest)
}

func createChapter(t *testing.T, handler http.Handler, token, novelID string, order int) chapterPayload {
	t.Helper()
	resp := doJSONRequest(t, handler, http.MethodPost, "/api/v1/novels/"+novelID+"/chapters", token, map[string]any{
		"chapterOrder":    order,
		"title":           "Capítulo",
		"originalContent": "Texto original",
	})
	assertStatus(t, resp, http.StatusCreated)
	var chapter chapterPayload
	decodeData(t, resp, &chapter)
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
	body := resp.Body.String()
	// v1 envelope wraps payloads in {data, meta, links}. By default, transparently
	// unwrap so existing test payloads (which target the inner resource) keep
	// working. Tests that need the full envelope (e.g. asserting on
	// meta/links) should call decodeRaw instead.
	if strings.HasPrefix(strings.TrimSpace(body), "{") {
		var probe struct {
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal([]byte(body), &probe); err == nil && len(probe.Data) > 0 && string(probe.Data) != "null" {
			decodeStringResponse(t, string(probe.Data), out)
			return
		}
	}
	decodeStringResponse(t, body, out)
}

// decodeRaw decodes the response body as-is, without envelope unwrapping.
func decodeRaw(t *testing.T, resp *httptest.ResponseRecorder, out any) {
	t.Helper()
	decodeStringResponse(t, resp.Body.String(), out)
}

// decodeData extracts the v1 envelope's `data` field and decodes it into out.
// Useful when the test payload is the inner resource rather than the full
// {data,meta,links} envelope.
func decodeData(t *testing.T, resp *httptest.ResponseRecorder, out any) {
	t.Helper()
	body := resp.Body.String()
	var probe struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &probe); err != nil || len(probe.Data) == 0 || string(probe.Data) == "null" {
		t.Fatalf("response missing data envelope: %v\nbody: %s", err, body)
	}
	decodeStringResponse(t, string(probe.Data), out)
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

// createFilterNovel creates a novel with tags/status/visibility and the given
// number of chapters (the first `translated` of them with translated content
// and status=translated), so ?tag/?shared/?progress filters can be exercised.
func createFilterNovel(t *testing.T, handler http.Handler, token, title string, tags []string, status string, chapters, translated int, isPublic bool) novelPayload {
	t.Helper()
	body := map[string]any{
		"sourceTitle":    title,
		"sourceLanguage": "en",
		"targetLanguage": "es",
	}
	if tags != nil {
		body["tags"] = tags
	}
	resp := doJSONRequest(t, handler, http.MethodPost, "/api/v1/novels", token, body)
	assertStatus(t, resp, http.StatusCreated)
	var novel novelPayload
	decodeData(t, resp, &novel)
	if status != "" {
		patchResp := doJSONRequest(t, handler, http.MethodPatch, "/api/v1/novels/"+novel.ID, token, map[string]any{"status": status})
		assertStatus(t, patchResp, http.StatusOK)
	}
	if isPublic {
		visResp := doJSONRequest(t, handler, http.MethodPatch, "/api/v1/novels/"+novel.ID+"/visibility", token, map[string]any{"isPublic": true})
		assertStatus(t, visResp, http.StatusOK)
	}
	for i := 0; i < chapters; i++ {
		order := i + 1
		ch := map[string]any{
			"chapterOrder":    order,
			"title":           fmt.Sprintf("Cap %d", order),
			"originalContent": "Texto original",
		}
		if i < translated {
			ch["translatedContent"] = "Texto traducido"
			ch["status"] = "translated"
		}
		chResp := doJSONRequest(t, handler, http.MethodPost, "/api/v1/novels/"+novel.ID+"/chapters", token, ch)
		assertStatus(t, chResp, http.StatusCreated)
	}
	return novel
}

// listNovelFilterIDs GETs /api/v1/novels with the given query and returns the
// result IDs, meta.total, and meta.has_more.
func listNovelFilterIDs(t *testing.T, env *apiTestEnv, token, query string) ([]string, int, bool) {
	t.Helper()
	path := "/api/v1/novels"
	if query != "" {
		path += "?" + query
	}
	resp := doJSONRequest(t, env.handler, http.MethodGet, path, token, nil)
	assertStatus(t, resp, http.StatusOK)
	var envelope struct {
		Data []map[string]any `json:"data"`
		Meta *v1Meta          `json:"meta"`
	}
	decodeRaw(t, resp, &envelope)
	ids := make([]string, 0, len(envelope.Data))
	for _, item := range envelope.Data {
		ids = append(ids, item["id"].(string))
	}
	total := 0
	hasMore := false
	if envelope.Meta != nil {
		total = envelope.Meta.Total
		hasMore = envelope.Meta.HasMore
	}
	return ids, total, hasMore
}

func TestListNovelsFilterByTag(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-tag@example.com", "secret123", "Alice")

	fantasia := createFilterNovel(t, env.handler, alice.Token, "Fantasía Novela", []string{"Fantasía"}, "", 0, 0, false)
	shouted := createFilterNovel(t, env.handler, alice.Token, "Shouted Tag", []string{"FANTASÍA"}, "", 0, 0, false)
	other := createFilterNovel(t, env.handler, alice.Token, "Otro tag", []string{"Terror"}, "", 0, 0, false)
	untagged := createFilterNovel(t, env.handler, alice.Token, "Sin tags", nil, "", 0, 0, false)

	// ?tag=foo matches only novels tagged foo (test 1) and excludes untagged (test 3).
	ids, total, _ := listNovelFilterIDs(t, env, alice.Token, "tag=foo")
	if len(ids) != 0 || total != 0 {
		t.Fatalf("expected no matches for tag=foo, got %v (total %d)", ids, total)
	}
	untaggedIDs, _, _ := listNovelFilterIDs(t, env, alice.Token, "sort=title")
	if !reflect.DeepEqual(untaggedIDs, []string{fantasia.ID, other.ID, shouted.ID, untagged.ID}) {
		t.Fatalf("expected unfiltered list %v, got %v", []string{fantasia.ID, other.ID, shouted.ID, untagged.ID}, untaggedIDs)
	}

	// Accent- and case-insensitive matching (test 2).
	for _, query := range []string{"tag=fantasia", "tag=Fantas%C3%ADa", "tag=FANTASIA"} {
		ids, total, hasMore := listNovelFilterIDs(t, env, alice.Token, query)
		want := []string{fantasia.ID, shouted.ID}
		if !reflect.DeepEqual(ids, want) || total != len(want) || hasMore {
			t.Fatalf("query %q: expected %v (total %d), got %v (total %d, hasMore %v)", query, want, len(want), ids, total, hasMore)
		}
	}
}

func TestListNovelsFilterShared(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-shared@example.com", "secret123", "Alice")
	bob := registerUser(t, env.handler, "bob-shared@example.com", "secret123", "Bob")

	own := createFilterNovel(t, env.handler, alice.Token, "Propia", nil, "", 0, 0, false)
	bobsPrivate := createFilterNovel(t, env.handler, bob.Token, "Privada de Bob", nil, "", 0, 0, false)
	bobsPublic := createFilterNovel(t, env.handler, bob.Token, "Pública de Bob", nil, "", 0, 0, true)

	// ?shared=own excludes foreign public novels (test 4).
	ids, _, _ := listNovelFilterIDs(t, env, alice.Token, "shared=own&sort=title")
	if !reflect.DeepEqual(ids, []string{own.ID}) {
		t.Fatalf("shared=own: expected [%s], got %v", own.ID, ids)
	}

	// ?shared=shared excludes own novels (test 5).
	ids, _, _ = listNovelFilterIDs(t, env, alice.Token, "shared=shared&sort=title")
	if !reflect.DeepEqual(ids, []string{bobsPublic.ID}) {
		t.Fatalf("shared=shared: expected [%s], got %v", bobsPublic.ID, ids)
	}

	// Default (all) includes own + foreign public, not foreign private.
	ids, _, _ = listNovelFilterIDs(t, env, alice.Token, "sort=title")
	if !reflect.DeepEqual(ids, []string{own.ID, bobsPublic.ID}) {
		t.Fatalf("default scope: expected [%s %s], got %v", own.ID, bobsPublic.ID, ids)
	}

	// Bob still sees his own private novel.
	ids, _, _ = listNovelFilterIDs(t, env, bob.Token, "sort=title")
	if !reflect.DeepEqual(ids, []string{bobsPrivate.ID, bobsPublic.ID}) {
		t.Fatalf("bob scope: expected [%s %s], got %v", bobsPrivate.ID, bobsPublic.ID, ids)
	}
}

func TestListNovelsFilterProgress(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-progress@example.com", "secret123", "Alice")

	ongoing := createFilterNovel(t, env.handler, alice.Token, "En curso", nil, "ongoing", 5, 2, false)
	completed := createFilterNovel(t, env.handler, alice.Token, "Completada", nil, "completed", 3, 3, false)
	hiatus := createFilterNovel(t, env.handler, alice.Token, "En pausa", nil, "hiatus", 2, 1, false)
	empty := createFilterNovel(t, env.handler, alice.Token, "Vacía", nil, "ongoing", 0, 0, false)

	// ?progress=completed (test 6).
	ids, _, _ := listNovelFilterIDs(t, env, alice.Token, "progress=completed")
	if !reflect.DeepEqual(ids, []string{completed.ID}) {
		t.Fatalf("progress=completed: expected [%s], got %v", completed.ID, ids)
	}

	// ?progress=ongoing: status=ongoing only; hiatus does not count (test 7).
	ids, _, _ = listNovelFilterIDs(t, env, alice.Token, "progress=ongoing&sort=title")
	want := []string{ongoing.ID, empty.ID}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("progress=ongoing: expected %v, got %v", want, ids)
	}

	// ?progress=translated: chapter_count > 0 && translated_count = chapter_count (test 8)…
	ids, _, _ = listNovelFilterIDs(t, env, alice.Token, "progress=translated")
	if !reflect.DeepEqual(ids, []string{completed.ID}) {
		t.Fatalf("progress=translated: expected [%s], got %v", completed.ID, ids)
	}

	// …and excludes novels with 0 chapters (test 9).
	ids, _, _ = listNovelFilterIDs(t, env, alice.Token, "progress=translated&sort=title")
	for _, id := range ids {
		if id == empty.ID {
			t.Fatalf("progress=translated must exclude the 0-chapter novel")
		}
	}
	if hiatus.ID == completed.ID {
		t.Fatal("test setup error")
	}
}

func TestListNovelsFilterCombination(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-combo@example.com", "secret123", "Alice")
	bob := registerUser(t, env.handler, "bob-combo@example.com", "secret123", "Bob")

	// Own, ongoing, tagged Alpha → matches all three filters.
	match := createFilterNovel(t, env.handler, alice.Token, "Coincide", []string{"Alpha"}, "ongoing", 2, 1, false)
	// Same tag but completed → excluded by progress.
	_ = createFilterNovel(t, env.handler, alice.Token, "Progreso distinto", []string{"Alpha"}, "completed", 1, 1, false)
	// Same tag/status but foreign → only visible without shared=own.
	foreignMatch := createFilterNovel(t, env.handler, bob.Token, "Ajena que coincide", []string{"Alpha"}, "ongoing", 2, 1, true)
	// Own, ongoing, different tag → excluded by tag.
	_ = createFilterNovel(t, env.handler, alice.Token, "Otro tag", []string{"Beta"}, "ongoing", 2, 1, false)

	// AND combination (test 10).
	ids, _, _ := listNovelFilterIDs(t, env, alice.Token, "tag=alpha&shared=own&progress=ongoing")
	if !reflect.DeepEqual(ids, []string{match.ID}) {
		t.Fatalf("combined filter: expected [%s], got %v", match.ID, ids)
	}

	// Combination with ?q — title match and tag together (test 11). The default
	// scope also includes the foreign public novel whose title contains the query.
	ids, _, _ = listNovelFilterIDs(t, env, alice.Token, "tag=alpha&q=Coincide")
	wantQ := []string{foreignMatch.ID, match.ID}
	if !reflect.DeepEqual(ids, wantQ) {
		t.Fatalf("tag+q: expected %v, got %v", wantQ, ids)
	}
	// Same tag with a non-matching query returns nothing.
	ids, _, _ = listNovelFilterIDs(t, env, alice.Token, "tag=alpha&q=inexistente")
	if len(ids) != 0 {
		t.Fatalf("tag+non-matching q: expected no results, got %v", ids)
	}

	// ?q with shared/progress also routes through SearchNovels (test 16).
	ids, _, _ = listNovelFilterIDs(t, env, alice.Token, "q=Coincide&shared=own&progress=ongoing")
	if !reflect.DeepEqual(ids, []string{match.ID}) {
		t.Fatalf("q+shared+progress: expected [%s], got %v", match.ID, ids)
	}
	ids, _, _ = listNovelFilterIDs(t, env, alice.Token, "q=Coincide&shared=shared")
	if !reflect.DeepEqual(ids, []string{foreignMatch.ID}) {
		t.Fatalf("q+shared: expected [%s], got %v", foreignMatch.ID, ids)
	}
}

func TestListNovelsFilterInvalidValuesFallBackToAll(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-invalid@example.com", "secret123", "Alice")

	first := createFilterNovel(t, env.handler, alice.Token, "Primera", nil, "", 0, 0, false)
	second := createFilterNovel(t, env.handler, alice.Token, "Segunda", nil, "", 0, 0, true)

	// Invalid shared/progress values behave like "all" and return 200 (test 12).
	for _, query := range []string{"shared=weird", "progress=weird", "shared=&progress="} {
		resp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels?"+query, alice.Token, nil)
		assertStatus(t, resp, http.StatusOK)
		ids, _, _ := listNovelFilterIDs(t, env, alice.Token, query+"&sort=title")
		if !reflect.DeepEqual(ids, []string{first.ID, second.ID}) {
			t.Fatalf("query %q: expected both novels, got %v", query, ids)
		}
	}
}

func TestCreateNovelDedupesAccentTags(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-dedup@example.com", "secret123", "Alice")

	// normalizeNovelTags dedup (test 13): Fantasía and fantasia collapse to one.
	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels", alice.Token, map[string]any{
		"sourceTitle":    "Dedup",
		"sourceLanguage": "en",
		"targetLanguage": "es",
		"tags":           []string{"Fantasía", "fantasia", "Ação", "ação"},
	})
	assertStatus(t, resp, http.StatusCreated)
	var novel struct {
		ID   string   `json:"id"`
		Tags []string `json:"tags"`
	}
	decodeData(t, resp, &novel)
	if len(novel.Tags) != 2 {
		t.Fatalf("expected 2 deduped tags, got %v", novel.Tags)
	}
	if novel.Tags[0] != "Ação" || novel.Tags[1] != "Fantasía" {
		t.Fatalf("expected first occurrence kept and sorted, got %v", novel.Tags)
	}

	// ListNovelTagSuggestions dedup (test 14).
	suggResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels/tags/suggestions", alice.Token, nil)
	assertStatus(t, suggResp, http.StatusOK)
	var sugg struct {
		Data []string `json:"data"`
	}
	decodeRaw(t, suggResp, &sugg)
	if len(sugg.Data) != 2 {
		t.Fatalf("expected 2 deduped tag suggestions, got %v", sugg.Data)
	}
}

func TestListNovelsFilterByTagPagination(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-tagpage@example.com", "secret123", "Alice")

	// 5 tagged novels + 5 untagged. Sorted by title, the tagged novels land on
	// both pages of the unfiltered order, so a DB-offset tag filter would skip
	// some of them.
	var taggedIDs []string
	for i := 1; i <= 10; i++ {
		title := fmt.Sprintf("Novela %02d", i)
		if i%2 == 1 {
			tagged := createFilterNovel(t, env.handler, alice.Token, title, []string{"Serie"}, "", 0, 0, false)
			taggedIDs = append(taggedIDs, tagged.ID)
		} else {
			createFilterNovel(t, env.handler, alice.Token, title, nil, "", 0, 0, false)
		}
	}

	// meta.total is exact and pages cover ALL matches without holes (test 15).
	var seen []string
	total := 0
	for page := 1; ; page++ {
		resp := doJSONRequest(t, env.handler, http.MethodGet, fmt.Sprintf("/api/v1/novels?tag=Serie&sort=title&page=%d&per_page=2", page), alice.Token, nil)
		assertStatus(t, resp, http.StatusOK)
		var envelope struct {
			Data []map[string]any `json:"data"`
			Meta *v1Meta          `json:"meta"`
		}
		decodeRaw(t, resp, &envelope)
		total = envelope.Meta.Total
		for _, item := range envelope.Data {
			seen = append(seen, item["id"].(string))
		}
		if !envelope.Meta.HasMore {
			break
		}
		if page > 10 {
			t.Fatal("pagination did not terminate")
		}
	}
	if total != 5 {
		t.Fatalf("expected meta.total=5, got %d", total)
	}
	if !reflect.DeepEqual(seen, taggedIDs) {
		t.Fatalf("expected all 5 tagged novels across pages, got %v (want %v)", seen, taggedIDs)
	}
}

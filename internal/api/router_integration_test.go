package api

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

func TestJobPatchRequiresOwner(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice@example.com", "secret123", "Alice")
	bob := registerUser(t, env.handler, "bob@example.com", "secret123", "Bob")

	novel := createNovel(t, env.handler, alice.Token, "Trabajo", "es", "en")
	chapter := createChapter(t, env.handler, alice.Token, novel.ID, 1)

	jobResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/db/novels/"+novel.ID+"/translation-jobs", alice.Token, map[string]any{
		"chapterIds": []string{chapter.ID},
		"operation":  "translate",
		"options": map[string]any{
			"provider": "venice",
			"model":    "deepseek-v4-flash",
		},
	})
	assertStatus(t, jobResp, http.StatusCreated)

	var job jobPayload
	decodeResponse(t, jobResp, &job)

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

	decodeResponse(t, okResp, &job)
	if job.Status != "cancelled" {
		t.Fatalf("expected job status cancelled, got %q", job.Status)
	}

	chapterResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/db/novels/"+novel.ID+"/chapters/"+chapter.ID, alice.Token, nil)
	assertStatus(t, chapterResp, http.StatusOK)

	var updatedChapter chapterPayload
	decodeResponse(t, chapterResp, &updatedChapter)
	if updatedChapter.Status != "pending" {
		t.Fatalf("expected cancelled job chapter to reset to pending, got %q", updatedChapter.Status)
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

	deleteResp := doJSONRequest(t, env.handler, http.MethodDelete, "/api/db/novels/"+novel.ID+"/chapters/"+chapter1.ID, alice.Token, nil)
	assertStatus(t, deleteResp, http.StatusOK)

	// Logical delete: the record still exists, keeps its ID/source order and is
	// flagged as excluded.
	excludedResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/db/novels/"+novel.ID+"/chapters/"+chapter1.ID, alice.Token, nil)
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
	listResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/db/novels/"+novel.ID+"/chapters", alice.Token, nil)
	assertStatus(t, listResp, http.StatusOK)
	var visible []map[string]any
	decodeResponse(t, listResp, &visible)
	for _, item := range visible {
		if item["id"] == chapter1.ID {
			t.Fatalf("excluded chapter must not appear in the normal chapter list")
		}
	}
	excludedListResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/db/novels/"+novel.ID+"/chapters/excluded", alice.Token, nil)
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
	resp := doJSONRequest(t, env.handler, http.MethodPatch, "/api/db/novels/"+novel.ID+"/chapters/order", alice.Token, map[string]any{"chapterIds": ids})
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
	listResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/db/novels/"+novel.ID+"/chapters", alice.Token, nil)
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

	base := "/api/db/novels/" + novel.ID + "/chapters/order"

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

	resp := doJSONRequest(t, env.handler, http.MethodPatch, "/api/db/novels/"+novel.ID+"/chapters/order", alice.Token, map[string]any{"chapterIds": []string{c2.ID, c1.ID}})
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
	del := doJSONRequest(t, env.handler, http.MethodDelete, "/api/db/novels/"+novel.ID+"/chapters/"+c1.ID, bob.Token, nil)
	assertStatus(t, del, http.StatusForbidden)
	vis := doJSONRequest(t, env.handler, http.MethodPatch, "/api/db/novels/"+novel.ID+"/chapters/"+c1.ID+"/visibility", bob.Token, map[string]any{"excluded": true})
	assertStatus(t, vis, http.StatusForbidden)

	// Exclude via DELETE.
	deleteResp := doJSONRequest(t, env.handler, http.MethodDelete, "/api/db/novels/"+novel.ID+"/chapters/"+c1.ID, alice.Token, nil)
	assertStatus(t, deleteResp, http.StatusOK)

	// Restore via the visibility endpoint.
	restoreResp := doJSONRequest(t, env.handler, http.MethodPatch, "/api/db/novels/"+novel.ID+"/chapters/"+c1.ID+"/visibility", alice.Token, map[string]any{"excluded": false})
	assertStatus(t, restoreResp, http.StatusOK)
	var restored chapterPayload
	decodeResponse(t, restoreResp, &restored)
	if restored.Excluded {
		t.Fatalf("expected restored chapter to be visible, got %#v", restored)
	}
	if restored.Position != c1.Position {
		t.Fatalf("expected restore to keep the original position %d, got %d", c1.Position, restored.Position)
	}

	listResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/db/novels/"+novel.ID+"/chapters", alice.Token, nil)
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
	visResp := doJSONRequest(t, env.handler, http.MethodPatch, "/api/db/novels/"+novel.ID+"/visibility", alice.Token, map[string]any{"isPublic": true})
	assertStatus(t, visResp, http.StatusOK)
	delResp := doJSONRequest(t, env.handler, http.MethodDelete, "/api/db/novels/"+novel.ID+"/chapters/"+c2.ID, alice.Token, nil)
	assertStatus(t, delResp, http.StatusOK)

	// Non-owners keep read access to non-excluded chapters.
	ok := doJSONRequest(t, env.handler, http.MethodGet, "/api/db/novels/"+novel.ID+"/chapters/"+c1.ID, bob.Token, nil)
	assertStatus(t, ok, http.StatusOK)

	// Excluded chapters are invisible to non-owners even by direct ID.
	hidden := doJSONRequest(t, env.handler, http.MethodGet, "/api/db/novels/"+novel.ID+"/chapters/"+c2.ID, bob.Token, nil)
	assertStatus(t, hidden, http.StatusNotFound)

	// The excluded listing is owner-only.
	ownerOnly := doJSONRequest(t, env.handler, http.MethodGet, "/api/db/novels/"+novel.ID+"/chapters/excluded", bob.Token, nil)
	assertStatus(t, ownerOnly, http.StatusForbidden)

	// The owner can still inspect the excluded chapter and list it.
	inspect := doJSONRequest(t, env.handler, http.MethodGet, "/api/db/novels/"+novel.ID+"/chapters/"+c2.ID, alice.Token, nil)
	assertStatus(t, inspect, http.StatusOK)
	var owned chapterPayload
	decodeResponse(t, inspect, &owned)
	if !owned.Excluded {
		t.Fatalf("expected chapter to be marked excluded for the owner, got %#v", owned)
	}
	excludedResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/db/novels/"+novel.ID+"/chapters/excluded", alice.Token, nil)
	assertStatus(t, excludedResp, http.StatusOK)
	var excluded []map[string]any
	decodeResponse(t, excludedResp, &excluded)
	if len(excluded) != 1 || excluded[0]["id"] != c2.ID {
		t.Fatalf("expected only chapter 2 in the excluded listing, got %#v", excluded)
	}

	// The non-owner chapter list omits excluded chapters.
	listResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/db/novels/"+novel.ID+"/chapters", bob.Token, nil)
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
	resp := doJSONRequest(t, env.handler, http.MethodPatch, "/api/db/novels/"+novel.ID+"/chapters/order", alice.Token, map[string]any{"chapterIds": ids})
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
	deleteResp := doJSONRequest(t, env.handler, http.MethodDelete, "/api/db/novels/"+novel.ID+"/chapters/"+chapters[2].ID, alice.Token, nil)
	assertStatus(t, deleteResp, http.StatusOK)

	// Real source gaps: none, because order 3 still exists (excluded).
	gapsResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/db/novels/"+novel.ID+"/chapters/gaps", alice.Token, nil)
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
	deleteResp := doJSONRequest(t, env.handler, http.MethodDelete, "/api/db/novels/"+novel.ID+"/chapters/"+c2.ID, alice.Token, nil)
	assertStatus(t, deleteResp, http.StatusOK)

	// Eligible list omits excluded chapters.
	eligibleResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/db/novels/"+novel.ID+"/chapters/eligible?operation=translate", alice.Token, nil)
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

	deleteResp := doJSONRequest(t, env.handler, http.MethodDelete, "/api/db/novels/"+novel.ID+"/chapters/"+c1.ID, alice.Token, nil)
	assertStatus(t, deleteResp, http.StatusConflict)
	bulkResp := doJSONRequest(t, env.handler, http.MethodPost, "/api/db/novels/"+novel.ID+"/chapters/bulk-delete", alice.Token, map[string]any{"ids": []string{c1.ID}})
	assertStatus(t, bulkResp, http.StatusConflict)

	// The chapter is still visible after the rejected exclusion.
	listResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/db/novels/"+novel.ID+"/chapters", alice.Token, nil)
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
	statusResp := doJSONRequest(t, env.handler, http.MethodPatch, "/api/db/novels/"+novel.ID+"/chapters/"+c1.ID+"/status", alice.Token, map[string]any{"status": "translated", "errorMessage": ""})
	assertStatus(t, statusResp, http.StatusOK)

	before, err := env.store.GetChapterStatsAccessible(alice.User.ID, novel.ID)
	if err != nil {
		t.Fatalf("get stats before: %v", err)
	}
	if before.TotalChapters != 2 || before.TranslatedChapters != 1 {
		t.Fatalf("unexpected baseline stats: %+v", before)
	}

	deleteResp := doJSONRequest(t, env.handler, http.MethodDelete, "/api/db/novels/"+novel.ID+"/chapters/"+c1.ID, alice.Token, nil)
	assertStatus(t, deleteResp, http.StatusOK)

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

package api

import (
	"archive/zip"
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"translator-server/internal/store"
)

type comicPayload struct {
	ID              string `json:"id"`
	OwnerID         string `json:"ownerId"`
	SourceTitle     string `json:"sourceTitle"`
	SourceAuthor    string `json:"sourceAuthor"`
	TargetTitle     string `json:"targetTitle"`
	URL             string `json:"url"`
	Status          string `json:"status"`
	ChapterCount    int    `json:"chapterCount"`
	DownloadedCount int    `json:"downloadedCount"`
	CoverPath       string `json:"coverPath"`
	CanUpdate       bool   `json:"canUpdate"`
	RequiresBrowser bool   `json:"requiresBrowser"`
}

type comicChapterPayload struct {
	ID           string `json:"id"`
	ComicID      string `json:"comicId"`
	ChapterOrder int    `json:"chapterOrder"`
	Position     int    `json:"position"`
	Title        string `json:"title"`
	Status       string `json:"status"`
	PageCount    int    `json:"pageCount"`
	HasPages     bool   `json:"hasPages"`
}

type comicPagePayload struct {
	ID        string `json:"id"`
	PageOrder int    `json:"pageOrder"`
	URL       string `json:"url"`
}

func createComicFor(t *testing.T, env *apiTestEnv, token, title string) comicPayload {
	t.Helper()
	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/comics", token, map[string]any{
		"sourceLanguage": "en",
		"targetLanguage": "es",
		"sourceTitle":    title,
		"sourceAuthor":   "Author",
	})
	assertStatus(t, resp, http.StatusCreated)
	if resp.Header().Get("Location") == "" {
		t.Fatalf("expected Location header on comic create")
	}
	var comic comicPayload
	decodeResponse(t, resp, &comic)
	return comic
}

func TestComicCRUDLifecycle(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "alice@example.com", "secret123", "Alice")

	comic := createComicFor(t, env, alice.Token, "Solo Leveling")
	if comic.SourceTitle != "Solo Leveling" || comic.ID == "" {
		t.Fatalf("unexpected create payload: %+v", comic)
	}

	resp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/comics/"+comic.ID, alice.Token, nil)
	assertStatus(t, resp, http.StatusOK)

	resp = doJSONRequest(t, env.handler, http.MethodPatch, "/api/v1/comics/"+comic.ID, alice.Token, map[string]any{"targetTitle": "Solo Leveling (ES)"})
	assertStatus(t, resp, http.StatusOK)

	resp = doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/comics", alice.Token, nil)
	assertStatus(t, resp, http.StatusOK)
	var listEnvelope struct {
		Data []comicPayload `json:"data"`
		Meta struct {
			Total int `json:"total"`
		} `json:"meta"`
	}
	decodeRaw(t, resp, &listEnvelope)
	if len(listEnvelope.Data) != 1 || listEnvelope.Data[0].ID != comic.ID {
		t.Fatalf("expected the created comic in the list, got %+v", listEnvelope.Data)
	}

	resp = doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/comics?fields=id,sourceTitle", alice.Token, nil)
	assertStatus(t, resp, http.StatusOK)

	resp = doJSONRequest(t, env.handler, http.MethodPatch, "/api/v1/comics/"+comic.ID+"/visibility", alice.Token, map[string]any{"isPublic": true})
	assertStatus(t, resp, http.StatusOK)

	bob := registerUser(t, env, "bob@example.com", "secret123", "Bob")
	resp = doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/comics/"+comic.ID, bob.Token, nil)
	assertStatus(t, resp, http.StatusOK)
	resp = doJSONRequest(t, env.handler, http.MethodDelete, "/api/v1/comics/"+comic.ID, bob.Token, nil)
	assertStatus(t, resp, http.StatusForbidden)

	resp = doJSONRequest(t, env.handler, http.MethodDelete, "/api/v1/comics/"+comic.ID, alice.Token, nil)
	assertStatus(t, resp, http.StatusNoContent)
}

// tinyPNG builds a valid 1x1 PNG for the page pipeline.
func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func TestComicChaptersPagesAndStats(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "alice@example.com", "secret123", "Alice")
	comic := createComicFor(t, env, alice.Token, "Berserk")

	resp := doJSONRequest(t, env.handler, http.MethodPost, fmt.Sprintf("/api/v1/comics/%s/chapters", comic.ID), alice.Token, map[string]any{"title": "Capítulo 1", "chapterOrder": 1})
	assertStatus(t, resp, http.StatusCreated)
	var chapter comicChapterPayload
	decodeResponse(t, resp, &chapter)
	if chapter.Title != "Capítulo 1" || chapter.Status != store.ComicChapterPending {
		t.Fatalf("unexpected chapter payload: %+v", chapter)
	}

	// Store two pages directly (the HTTP surface for pages is download-driven;
	// the store call is the same one the download job makes).
	png := tinyPNG(t)
	if err := env.store.ReplaceComicChapterPages(chapter.ID, []store.ComicPageUpload{
		{Data: png, MimeType: "image/png", Order: 1, SourceURL: "https://img.example/1.png"},
		{Data: png, MimeType: "image/png", Order: 2, SourceURL: "https://img.example/2.png"},
	}); err != nil {
		t.Fatalf("replace pages: %v", err)
	}

	resp = doJSONRequest(t, env.handler, http.MethodGet, fmt.Sprintf("/api/v1/comics/%s/chapters", comic.ID), alice.Token, nil)
	assertStatus(t, resp, http.StatusOK)
	var chapters []comicChapterPayload
	decodeResponse(t, resp, &chapters)
	if len(chapters) != 1 {
		t.Fatalf("expected 1 chapter, got %d", len(chapters))
	}
	got := chapters[0]
	if got.Status != store.ComicChapterDone || got.PageCount != 2 || !got.HasPages {
		t.Fatalf("chapter not done with 2 pages: %+v", got)
	}

	resp = doJSONRequest(t, env.handler, http.MethodGet, fmt.Sprintf("/api/v1/comics/%s/chapters/%s/pages", comic.ID, chapter.ID), alice.Token, nil)
	assertStatus(t, resp, http.StatusOK)
	var pages []comicPagePayload
	decodeResponse(t, resp, &pages)
	if len(pages) != 2 || pages[0].URL == "" {
		t.Fatalf("unexpected pages: %+v", pages)
	}

	resp = doJSONRequest(t, env.handler, http.MethodGet, pages[0].URL, alice.Token, nil)
	assertStatus(t, resp, http.StatusOK)
	if resp.Header().Get("Cache-Control") == "" {
		t.Fatalf("expected Cache-Control on page image")
	}

	resp = doJSONRequest(t, env.handler, http.MethodGet, fmt.Sprintf("/api/v1/comics/%s/chapter-stats", comic.ID), alice.Token, nil)
	assertStatus(t, resp, http.StatusOK)

	resp = doJSONRequest(t, env.handler, http.MethodGet, fmt.Sprintf("/api/v1/comics/%s/chapters/%s/neighbors", comic.ID, chapter.ID), alice.Token, nil)
	assertStatus(t, resp, http.StatusOK)
}

func TestComicReadingProgress(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "alice@example.com", "secret123", "Alice")
	comic := createComicFor(t, env, alice.Token, "Vagabond")

	resp := doJSONRequest(t, env.handler, http.MethodPut, fmt.Sprintf("/api/v1/comics/%s/reading-progress", comic.ID), alice.Token, map[string]any{"chapterId": "ch-1", "scrollPercent": 42.5})
	assertStatus(t, resp, http.StatusOK)

	resp = doJSONRequest(t, env.handler, http.MethodGet, fmt.Sprintf("/api/v1/comics/%s/reading-progress", comic.ID), alice.Token, nil)
	assertStatus(t, resp, http.StatusOK)
	var progress struct {
		ChapterID     string  `json:"chapterId"`
		ScrollPercent float64 `json:"scrollPercent"`
	}
	decodeResponse(t, resp, &progress)
	if progress.ChapterID != "ch-1" || progress.ScrollPercent != 42.5 {
		t.Fatalf("unexpected progress: %+v", progress)
	}
}

func multipartRequest(t *testing.T, handler http.Handler, method, path, token, field, fileName string, blob []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	fileWriter, err := writer.CreateFormFile(field, fileName)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fileWriter.Write(blob); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	req := httptest.NewRequest(method, path, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	return resp
}

func TestComicCoverUploadAndServe(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "alice@example.com", "secret123", "Alice")
	comic := createComicFor(t, env, alice.Token, "One Piece")

	resp := multipartRequest(t, env.handler, http.MethodPost, fmt.Sprintf("/api/v1/comics/%s/cover", comic.ID), alice.Token, "cover", "cover.png", tinyPNG(t))
	assertStatus(t, resp, http.StatusOK)

	resp = doJSONRequest(t, env.handler, http.MethodGet, fmt.Sprintf("/api/v1/comics/%s/cover", comic.ID), alice.Token, nil)
	assertStatus(t, resp, http.StatusOK)
	if resp.Header().Get("Cache-Control") == "" {
		t.Fatalf("expected Cache-Control on comic cover")
	}
}

// buildComicZip builds an in-memory archive: name → contents.
func buildComicZip(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range entries {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func TestComicImportZip(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "alice@example.com", "secret123", "Alice")

	zipBytes := buildComicZip(t, map[string]string{
		"metadata.json":        `{"sourceLanguage":"ja","targetLanguage":"es","sourceTitle":"Zip Comic","sourceAuthor":"Z"}`,
		"Chapter 001/001.jpg":  "fake-page-1",
		"Chapter 001/002.jpg":  "fake-page-2",
		"Chapter 002/001.jpg":  "fake-page-3",
	})

	resp := multipartRequest(t, env.handler, http.MethodPost, "/api/v1/comics/import-zip", alice.Token, "file", "comic.zip", zipBytes)
	assertStatus(t, resp, http.StatusCreated)

	var imported struct {
		Comic            comicPayload `json:"comic"`
		ChaptersImported int          `json:"chaptersImported"`
	}
	decodeResponse(t, resp, &imported)
	if imported.ChaptersImported != 2 {
		t.Fatalf("expected 2 imported chapters, got %d", imported.ChaptersImported)
	}
	if imported.Comic.ID == "" || imported.Comic.SourceTitle != "Zip Comic" {
		t.Fatalf("unexpected comic: %+v", imported.Comic)
	}

	resp = doJSONRequest(t, env.handler, http.MethodGet, fmt.Sprintf("/api/v1/comics/%s/chapters", imported.Comic.ID), alice.Token, nil)
	assertStatus(t, resp, http.StatusOK)
	var chapters []comicChapterPayload
	decodeResponse(t, resp, &chapters)
	if len(chapters) != 2 {
		t.Fatalf("expected 2 chapters, got %d", len(chapters))
	}
	if chapters[0].PageCount != 2 || chapters[1].PageCount != 1 {
		t.Fatalf("unexpected page counts: %+v", chapters)
	}
	if chapters[0].Status != store.ComicChapterDone {
		t.Fatalf("imported chapters should be done: %+v", chapters[0])
	}
}

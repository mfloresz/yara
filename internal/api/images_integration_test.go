package api

import (
	"archive/zip"
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// buildImageEpub assembles a one-chapter EPUB whose body embeds the given
// <img> tag(s), plus one image entry img1.jpg, in memory.
func buildImageEpub(t *testing.T, bodyHTML string) []byte {
	t.Helper()
	files := map[string]string{
		"META-INF/container.xml": `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`,
		"OEBPS/content.opf": `<?xml version="1.0" encoding="UTF-8"?>
<package version="3.0" xmlns="http://www.idpf.org/2007/opf" unique-identifier="book-id">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>Image Novel</dc:title>
    <dc:creator>Jane Author</dc:creator>
    <dc:language>en</dc:language>
  </metadata>
  <manifest>
    <item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
    <item id="img1" href="img1.jpg" media-type="image/jpeg"/>
    <item id="ch1" href="chap1.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine toc="ncx">
    <itemref idref="ch1"/>
  </spine>
</package>`,
		"OEBPS/img1.jpg":  "\xFF\xD8\xFFfake-jpeg-bytes",
		"OEBPS/chap1.xhtml": `<?xml version="1.0" encoding="utf-8"?>
<html xmlns="http://www.w3.org/1999/xhtml"><head><title>Chapter 1</title></head>
<body><h1>Chapter 1</h1>` + bodyHTML + `</body></html>`,
	}

	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)
	mh := &zip.FileHeader{Name: "mimetype", Method: zip.Store}
	f, err := w.CreateHeader(mh)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("application/epub+zip")); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func postMultipart(t *testing.T, handler http.Handler, path, token, field, filename string, fileBytes []byte, fields ...[2]string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, kv := range fields {
		if err := writer.WriteField(kv[0], kv[1]); err != nil {
			t.Fatalf("write field %s: %v", kv[0], err)
		}
	}
	fileWriter, err := writer.CreateFormFile(field, filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fileWriter.Write(fileBytes); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestImportEpubWithImages(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "alice-img-epub@example.com", "secret123", "Alice")

	longBody := "This is a sufficiently long paragraph of chapter text so the importer does not discard it."
	epub := buildImageEpub(t, `<p>`+longBody+`</p><p><img src="img1.jpg" alt="Mapa del imperio"/></p><p>`+longBody+`</p>`)

	rec := postMultipart(t, env.handler, "/api/v1/novels/import-epub", alice.Token, "file", "book.epub", epub,
		[2]string{"sourceLanguage", "en"}, [2]string{"targetLanguage", "es"})
	assertStatus(t, rec, http.StatusCreated)

	var importResp struct {
		Novel map[string]any `json:"novel"`
	}
	decodeData(t, rec, &importResp)
	novelID := importResp.Novel["id"].(string)

	// Full chapter list carries the images array next to the tokens.
	resp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels/"+novelID+"/chapters?includeContent=true", alice.Token, nil)
	assertStatus(t, resp, http.StatusOK)
	var chapters []struct {
		ID              string           `json:"id"`
		OriginalContent string           `json:"originalContent"`
		Images          []map[string]any `json:"images"`
	}
	decodeData(t, resp, &chapters)
	if len(chapters) != 1 {
		t.Fatalf("expected 1 chapter, got %d", len(chapters))
	}
	chapter := chapters[0]
	if strings.Count(chapter.OriginalContent, "[[IMG-1]]") != 1 {
		t.Fatalf("content should carry the token once, got:\n%s", chapter.OriginalContent)
	}
	if len(chapter.Images) != 1 {
		t.Fatalf("expected 1 image in response, got %v", chapter.Images)
	}
	img := chapter.Images[0]
	if img["token"] != "[[IMG-1]]" || img["alt"] != "Mapa del imperio" || img["mime"] != "image/jpeg" {
		t.Fatalf("unexpected image payload: %v", img)
	}
	imageURL, _ := img["url"].(string)
	if imageURL == "" {
		t.Fatalf("image url missing: %v", img)
	}

	// Owner can fetch the bytes; a stranger cannot.
	ownerResp := doJSONRequest(t, env.handler, http.MethodGet, imageURL, alice.Token, nil)
	assertStatus(t, ownerResp, http.StatusOK)
	ownerBody, _ := io.ReadAll(ownerResp.Body)
	if !strings.Contains(string(ownerBody), "fake-jpeg-bytes") {
		t.Errorf("served image should be the stored blob, got %q", string(ownerBody))
	}

	bob := registerUser(t, env, "bob-img-epub@example.com", "secret123", "Bob")
	// A stranger gets 404, not 403: the endpoint must not leak that the
	// novel/image exists.
	strangerResp := doJSONRequest(t, env.handler, http.MethodGet, imageURL, bob.Token, nil)
	assertStatus(t, strangerResp, http.StatusNotFound)

	missingResp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels/"+novelID+"/images/nonexistent", alice.Token, nil)
	assertStatus(t, missingResp, http.StatusNotFound)
}

func TestImportZipWithImageMarkers(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "alice-img-zip@example.com", "secret123", "Alice")

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
	addZipEntry("metadata.json", `{"sourceTitle":"Zipped Images","sourceLanguage":"en","targetLanguage":"es"}`)
	addZipEntry("originals/0001.txt", "Chapter 1\nBody with map.\n\n[[IMG:mapa.jpg]]\n\nEnd.")
	addZipEntry("translated/0001.txt", "Capítulo 1\nCuerpo con mapa.\n\n[[IMG:mapa.jpg]]\n\n[[IMG:inexistente.jpg]]\n\nFin.")
	addZipEntry("images/mapa.jpg", "\xFF\xD8\xFFfake-jpeg-bytes")
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}

	rec := postMultipart(t, env.handler, "/api/v1/novels/import-zip", alice.Token, "file", "novel.zip", zipBuf.Bytes())
	assertStatus(t, rec, http.StatusCreated)

	var importResp struct {
		Novel map[string]any `json:"novel"`
	}
	decodeData(t, rec, &importResp)
	novelID := importResp.Novel["id"].(string)

	resp := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels/"+novelID+"/chapters?includeContent=true", alice.Token, nil)
	assertStatus(t, resp, http.StatusOK)
	var chapters []struct {
		OriginalContent   string           `json:"originalContent"`
		TranslatedContent string           `json:"translatedContent"`
		Images            []map[string]any `json:"images"`
	}
	decodeData(t, resp, &chapters)
	if len(chapters) != 1 {
		t.Fatalf("expected 1 chapter, got %d", len(chapters))
	}
	chapter := chapters[0]
	if !strings.Contains(chapter.OriginalContent, "[[IMG-1]]") {
		t.Fatalf("original content should carry the token, got:\n%s", chapter.OriginalContent)
	}
	if strings.Contains(chapter.OriginalContent, "[[IMG:") {
		t.Fatalf("original content must not keep authoring markers, got:\n%s", chapter.OriginalContent)
	}
	// Translated tree maps the known marker to the same token and drops the
	// one the original does not reference.
	if !strings.Contains(chapter.TranslatedContent, "[[IMG-1]]") || strings.Contains(chapter.TranslatedContent, "[[IMG:") {
		t.Fatalf("translated content should keep only the mapped token, got:\n%s", chapter.TranslatedContent)
	}
	if len(chapter.Images) != 1 || chapter.Images[0]["token"] != "[[IMG-1]]" {
		t.Fatalf("expected one stored image, got %v", chapter.Images)
	}
	imageURL := chapter.Images[0]["url"].(string)
	imgResp := doJSONRequest(t, env.handler, http.MethodGet, imageURL, alice.Token, nil)
	assertStatus(t, imgResp, http.StatusOK)
}

func TestImportZipMissingImageFails(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "alice-img-missing@example.com", "secret123", "Alice")

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
	addZipEntry("metadata.json", `{"sourceTitle":"Broken Images","sourceLanguage":"en","targetLanguage":"es"}`)
	addZipEntry("originals/0001.txt", "Chapter 1\nBody.\n\n[[IMG:mapa.jpg]]")
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}

	rec := postMultipart(t, env.handler, "/api/v1/novels/import-zip", alice.Token, "file", "novel.zip", zipBuf.Bytes())
	assertStatus(t, rec, http.StatusBadRequest)
	if !strings.Contains(rec.Body.String(), "mapa.jpg") {
		t.Errorf("error should name the missing image, got: %s", rec.Body.String())
	}
}

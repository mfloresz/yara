package api

import (
	"archive/zip"
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The backup archive streams the entire data dir (every user's data plus the
// app encryption key), so it must only be reachable by the admin role.
func TestBackupExportAdminOnly(t *testing.T) {
	env := newAPITestEnv(t)
	admin := promoteToAdmin(t, env, registerUser(t, env, "admin-backup@example.com", "secret123", "Admin"))
	user := registerUser(t, env, "user-backup@example.com", "secret123", "User")

	// The legacy authenticated-user path is gone.
	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/backups/export", user.Token, map[string]any{})
	assertStatus(t, resp, http.StatusNotFound)

	// A regular user is rejected on the admin path.
	forbidden := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/admin/backups/export", user.Token, map[string]any{})
	assertStatus(t, forbidden, http.StatusForbidden)

	// An admin gets the streamed zip.
	allowed := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/admin/backups/export", admin.Token, map[string]any{})
	assertStatus(t, allowed, http.StatusOK)
	if ct := allowed.Header().Get("Content-Type"); ct != "application/zip" {
		t.Fatalf("expected application/zip, got %q", ct)
	}
	body := allowed.Body.Bytes()
	if len(body) < 4 || !bytes.Equal(body[:2], []byte("PK")) {
		t.Fatalf("response is not a zip archive (first bytes: %x)", body[:min(4, len(body))])
	}
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("parse backup zip: %v", err)
	}
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	if !names["app.key"] {
		t.Fatalf("expected app.key in the backup (documented behavior), got entries: %v", names)
	}
}

// Cover/thumbnail are protected file fields: PocketBase's native /api/files
// route must not serve them, and the authenticated /api/v1/novels/{id}/cover
// endpoint must enforce ownership (owner or is_public).
func TestCoverRouteEnforcesOwnership(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "alice-cover@example.com", "secret123", "Alice")
	bob := registerUser(t, env, "bob-cover@example.com", "secret123", "Bob")

	novel := createNovel(t, env.handler, alice.Token, "Portada", "es", "en")

	png := append([]byte{0x89, 'P', 'N', 'G'}, bytes.Repeat([]byte{0}, 64)...)
	if _, err := env.store.UpdateNovelCover(alice.User.ID, novel.ID, png, "image/png"); err != nil {
		t.Fatalf("attach cover: %v", err)
	}
	updated, err := env.store.GetOwnedNovel(alice.User.ID, novel.ID)
	if err != nil {
		t.Fatalf("reload novel: %v", err)
	}
	if updated.CoverFile == "" {
		t.Fatal("expected a stored cover file")
	}
	if !strings.HasPrefix(updated.CoverPath, "/api/v1/novels/"+novel.ID+"/cover") {
		t.Fatalf("coverPath should point at the authenticated route, got %q", updated.CoverPath)
	}

	// PocketBase's native file route must reject anonymous access (protected).
	pbFile := doJSONRequest(t, env.handler, http.MethodGet, "/api/files/novels/"+novel.ID+"/"+updated.CoverFile, "", nil)
	assertStatus(t, pbFile, http.StatusNotFound)

	// Anonymous access to the app route is rejected by RequireAuth.
	anon := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels/"+novel.ID+"/cover", "", nil)
	assertStatus(t, anon, http.StatusUnauthorized)

	// Another user cannot read a private novel's cover.
	cross := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels/"+novel.ID+"/cover", bob.Token, nil)
	assertStatus(t, cross, http.StatusForbidden)

	// The owner can.
	own := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels/"+novel.ID+"/cover", alice.Token, nil)
	assertStatus(t, own, http.StatusOK)

	// Making the novel public opens the cover to other authenticated users.
	vis := doJSONRequest(t, env.handler, http.MethodPatch, "/api/v1/novels/"+novel.ID+"/visibility", alice.Token, map[string]any{"isPublic": true})
	assertStatus(t, vis, http.StatusOK)
	public := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/novels/"+novel.ID+"/cover", bob.Token, nil)
	assertStatus(t, public, http.StatusOK)
}

// logout-all rotates the server-side token key: every previously issued
// token for the user must stop working immediately.
func TestLogoutAllInvalidatesTokens(t *testing.T) {
	env := newAPITestEnv(t)
	user := registerUser(t, env, "carol-sessions@example.com", "secret123", "Carol")

	me := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/auth/me", user.Token, nil)
	assertStatus(t, me, http.StatusOK)

	// A second session minted before logout-all must die with the first one.
	second, err := env.store.AuthenticateUser(user.User.Email, "secret123")
	if err != nil {
		t.Fatalf("mint second session: %v", err)
	}

	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/auth/logout-all", user.Token, nil)
	assertStatus(t, resp, http.StatusNoContent)

	for name, token := range map[string]string{"first-session": user.Token, "second-session": second.Token} {
		rejected := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/auth/me", token, nil)
		if rejected.Code != http.StatusUnauthorized {
			t.Fatalf("%s: expected 401 after logout-all, got %d", name, rejected.Code)
		}
	}

	// A fresh login works again with the rotated key.
	fresh, err := env.store.AuthenticateUser(user.User.Email, "secret123")
	if err != nil {
		t.Fatalf("login after logout-all: %v", err)
	}
	ok := doJSONRequest(t, env.handler, http.MethodGet, "/api/v1/auth/me", fresh.Token, nil)
	assertStatus(t, ok, http.StatusOK)
}

// The epub import must cap total decompressed content so a zip bomb cannot
// exhaust server memory.
func TestEpubImportRejectsZipBomb(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "alice-bomb@example.com", "secret123", "Alice")

	// One tiny zip whose single chapter decompresses far past the 25MB cap.
	// The compressed archive stays a few KB.
	chapter := "<html><body><h1>Bomb</h1><p>" + strings.Repeat("a", 30<<20) + "</p></body></html>"
	blob := buildBombEPUB(t, chapter)

	body := &bytes.Buffer{}
	mw := multipartBody(t, body, "file", "bomb.epub", blob)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/novels/import-epub", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+alice.Token)
	resp := httptest.NewRecorder()
	env.handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for zip bomb, got %d: %s", resp.Code, resp.Body.String())
	}
}

func buildBombEPUB(t *testing.T, chapter string) []byte {
	t.Helper()
	container := `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`
	opf := `<?xml version="1.0" encoding="UTF-8"?>
<package version="3.0" xmlns="http://www.idpf.org/2007/opf" unique-identifier="book-id">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>Bomb</dc:title>
    <dc:creator>A</dc:creator>
    <dc:language>en</dc:language>
  </metadata>
  <manifest>
    <item id="ch1" href="chap1.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine>
    <itemref idref="ch1"/>
  </spine>
</package>`
	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)
	for name, content := range map[string]string{
		"META-INF/container.xml": container,
		"OEBPS/content.opf":      opf,
		"OEBPS/chap1.xhtml":      chapter,
	} {
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

func multipartBody(t *testing.T, buf *bytes.Buffer, field, filename string, blob []byte) *multipart.Writer {
	t.Helper()
	mw := multipart.NewWriter(buf)
	fw, err := mw.CreateFormFile(field, filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(blob); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return mw
}

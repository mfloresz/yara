package store

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"translator-server/internal/secure"
)

func TestSaveRefinedContentIfUnchanged(t *testing.T) {
	dataDir := t.TempDir()
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: dataDir})
	if err := app.Bootstrap(); err != nil {
		t.Fatalf("bootstrap pocketbase: %v", err)
	}

	encryptor, err := secure.NewEncryptorFromConfig("", filepath.Join(dataDir, "app.key"))
	if err != nil {
		t.Fatalf("create encryptor: %v", err)
	}

	st := New(app, encryptor)
	if err := st.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	users, err := app.FindCollectionByNameOrId(UsersCollection)
	if err != nil {
		t.Fatalf("find users collection: %v", err)
	}
	owner := core.NewRecord(users)
	owner.Set("email", "occ-test@example.com")
	owner.Set("password", "secret123")
	owner.Set("passwordConfirm", "secret123")
	if err := app.Save(owner); err != nil {
		t.Fatalf("save owner user: %v", err)
	}

	novels, err := app.FindCollectionByNameOrId(NovelsCollection)
	if err != nil {
		t.Fatalf("find novels collection: %v", err)
	}
	novel := core.NewRecord(novels)
	novel.Set("owner", owner.Id)
	novel.Set("source_language", "en")
	novel.Set("target_language", "es")
	novel.Set("source_title", "Test Novel")
	novel.Set("source_author", "Author")
	novel.Set("source_description", "")
	if err := app.Save(novel); err != nil {
		t.Fatalf("save novel: %v", err)
	}

	chapters, err := app.FindCollectionByNameOrId(ChaptersCollection)
	if err != nil {
		t.Fatalf("find chapters collection: %v", err)
	}
	chapter := core.NewRecord(chapters)
	chapter.Set("novel", novel.Id)
	chapter.Set("chapter_order", 1)
	chapter.Set("title", "Ch 1")
	chapter.Set("original_content", "Original text")
	chapter.Set("translated_content", "original translation")
	chapter.Set("status", "translated")
	if err := app.Save(chapter); err != nil {
		t.Fatalf("save chapter: %v", err)
	}

	applied, err := st.SaveRefinedContentIfUnchanged(chapter.Id, "original translation", "refined text", "refined")
	if err != nil {
		t.Fatalf("first save: %v", err)
	}
	if !applied {
		t.Fatal("expected first save to apply")
	}

	saved, err := app.FindRecordById(ChaptersCollection, chapter.Id)
	if err != nil {
		t.Fatalf("re-fetch chapter: %v", err)
	}
	if got := saved.GetString("refined_content"); got != "refined text" {
		t.Fatalf("refined_content = %q, want %q", got, "refined text")
	}
	if got := saved.GetString("status"); got != "refined" {
		t.Fatalf("status = %q, want %q", got, "refined")
	}

	saved.Set("translated_content", "edited by user")
	if err := app.Save(saved); err != nil {
		t.Fatalf("simulate user edit: %v", err)
	}

	applied, err = st.SaveRefinedContentIfUnchanged(chapter.Id, "original translation", "should not be saved", "refined")
	if err != nil {
		t.Fatalf("second save: %v", err)
	}
	if applied {
		t.Fatal("expected second save to NOT apply (stale baseline)")
	}

	final, err := app.FindRecordById(ChaptersCollection, chapter.Id)
	if err != nil {
		t.Fatalf("re-fetch chapter after stale save: %v", err)
	}
	if got := final.GetString("refined_content"); got != "refined text" {
		t.Fatalf("refined_content = %q after stale save, want %q", got, "refined text")
	}
}

func TestCopyNovelPreservesCoverAndChapterContent(t *testing.T) {
	dataDir := t.TempDir()
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: dataDir})
	if err := app.Bootstrap(); err != nil {
		t.Fatalf("bootstrap pocketbase: %v", err)
	}

	encryptor, err := secure.NewEncryptorFromConfig("", filepath.Join(dataDir, "app.key"))
	if err != nil {
		t.Fatalf("create encryptor: %v", err)
	}

	st := New(app, encryptor)
	if err := st.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	users, err := app.FindCollectionByNameOrId(UsersCollection)
	if err != nil {
		t.Fatalf("find users collection: %v", err)
	}
	owner := core.NewRecord(users)
	owner.Set("email", "copy-test@example.com")
	owner.Set("password", "secret123")
	owner.Set("passwordConfirm", "secret123")
	if err := app.Save(owner); err != nil {
		t.Fatalf("save owner user: %v", err)
	}

	novels, err := app.FindCollectionByNameOrId(NovelsCollection)
	if err != nil {
		t.Fatalf("find novels collection: %v", err)
	}
	novel := core.NewRecord(novels)
	novel.Set("owner", owner.Id)
	novel.Set("source_language", "en")
	novel.Set("target_language", "es")
	novel.Set("source_title", "Test Novel")
	novel.Set("source_author", "Author")
	novel.Set("source_description", "")
	novel.Set("status", "ongoing")
	if err := app.Save(novel); err != nil {
		t.Fatalf("save novel: %v", err)
	}

	// Attach a cover blob to the source novel.
	coverBlob := []byte("fake-cover-bytes")
	if _, err := st.UpdateNovelCover(owner.Id, novel.Id, coverBlob, "image/png"); err != nil {
		t.Fatalf("attach cover: %v", err)
	}

	// Add a chapter with all content fields populated.
	chapters, err := app.FindCollectionByNameOrId(ChaptersCollection)
	if err != nil {
		t.Fatalf("find chapters collection: %v", err)
	}
	chapter := core.NewRecord(chapters)
	chapter.Set("novel", novel.Id)
	chapter.Set("chapter_order", 1)
	chapter.Set("title", "Capítulo 1")
	chapter.Set("translated_title", "Chapter 1")
	chapter.Set("original_content", "Contenido original del capítulo.")
	chapter.Set("translated_content", "Contenido traducido del capítulo.")
	chapter.Set("refined_content", "Contenido refinado del capítulo.")
	chapter.Set("status", "translated")
	if err := app.Save(chapter); err != nil {
		t.Fatalf("save chapter: %v", err)
	}

	// Copy the novel.
	copied, err := st.CopyNovel(owner.Id, novel.Id)
	if err != nil {
		t.Fatalf("copy novel: %v", err)
	}
	if copied == nil {
		t.Fatal("expected copied novel, got nil")
	}
	if copied.ID == novel.Id {
		t.Fatal("copied novel should have a different ID")
	}
	if copied.OwnerID != owner.Id {
		t.Fatalf("copied novel owner = %q, want %q", copied.OwnerID, owner.Id)
	}
	if copied.CoverFile == "" {
		t.Fatal("expected copied novel to have a cover file")
	}


	// Verify the cover blob is accessible on the clone.
	copiedRecord, err := app.FindRecordById(NovelsCollection, copied.ID)
	if err != nil {
		t.Fatalf("find copied novel record: %v", err)
	}
	copiedCoverFiles := copiedRecord.GetStringSlice("cover")
	if len(copiedCoverFiles) == 0 {
		t.Fatal("expected copied novel record to have a cover file")
	}
	// Verify the cover bytes were actually replicated (not just the URL).
	copiedCoverBlob, _, err := st.readNovelFileBlob(copied.ID, copiedCoverFiles[0])
	if err != nil {
		t.Fatalf("read copied cover blob: %v", err)
	}
	if string(copiedCoverBlob) != string(coverBlob) {
		t.Fatalf("copied cover blob mismatch: got %q, want %q", copiedCoverBlob, coverBlob)
	}

	// Verify chapters are faithfully cloned with all content fields.
	copiedChapters, err := st.ListChaptersAccessible(owner.Id, copied.ID)
	if err != nil {
		t.Fatalf("list copied chapters: %v", err)
	}
	if len(copiedChapters) != 1 {
		t.Fatalf("expected 1 copied chapter, got %d", len(copiedChapters))
	}
	cc := copiedChapters[0]
	if cc.Title != "Capítulo 1" {
		t.Errorf("copied chapter title = %q, want %q", cc.Title, "Capítulo 1")
	}
	if cc.TranslatedTitle != "Chapter 1" {
		t.Errorf("copied chapter translated_title = %q, want %q", cc.TranslatedTitle, "Chapter 1")
	}
	if cc.OriginalContent != "Contenido original del capítulo." {
		t.Errorf("copied chapter original_content = %q, want %q", cc.OriginalContent, "Contenido original del capítulo.")
	}
	if cc.TranslatedContent != "Contenido traducido del capítulo." {
		t.Errorf("copied chapter translated_content = %q, want %q", cc.TranslatedContent, "Contenido traducido del capítulo.")
	}
	if cc.RefinedContent != "Contenido refinado del capítulo." {
		t.Errorf("copied chapter refined_content = %q, want %q", cc.RefinedContent, "Contenido refinado del capítulo.")
	}
	if cc.Status != "translated" {
		t.Errorf("copied chapter status = %q, want %q", cc.Status, "translated")
	}

	// Verify the source novel's chapters are untouched.
	srcChapters, err := st.ListChaptersAccessible(owner.Id, novel.Id)
	if err != nil {
		t.Fatalf("list source chapters: %v", err)
	}
	if len(srcChapters) != 1 {
		t.Fatalf("expected 1 source chapter, got %d", len(srcChapters))
	}
	sc := srcChapters[0]
	if sc.TranslatedContent != "Contenido traducido del capítulo." {
		t.Errorf("source chapter translated_content was modified: %q", sc.TranslatedContent)
	}
	if sc.RefinedContent != "Contenido refinado del capítulo." {
		t.Errorf("source chapter refined_content was modified: %q", sc.RefinedContent)
	}
}

func TestClampTextTruncatesAndStrips(t *testing.T) {
	got := clampText("  hello world  ", 5)
	if got != "hello" {
		t.Fatalf("expected %q, got %q", "hello", got)
	}
	if clampText("short", 100) != "short" {
		t.Fatalf("short string should be preserved")
	}
	if clampText(strings.Repeat("a", 5000), 5000) != strings.Repeat("a", 5000) {
		t.Fatalf("string at boundary should be preserved")
	}
}

func TestEnsureSchemaMigratesUsersCollectionWithActiveProvider(t *testing.T) {
	dataDir := t.TempDir()
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: dataDir})
	if err := app.Bootstrap(); err != nil {
		t.Fatalf("bootstrap pocketbase: %v", err)
	}

	legacyUsers, err := app.FindCollectionByNameOrId(UsersCollection)
	if err != nil {
		t.Fatalf("find bootstrap users collection: %v", err)
	}
	legacyUsers.Fields.RemoveByName("theme")
	legacyUsers.Fields.RemoveByName("active_provider")
	if legacyUsers.Fields.GetByName("name") == nil {
		legacyUsers.Fields.Add(&core.TextField{Name: "name", Max: 120})
	}
	if err := app.Save(legacyUsers); err != nil {
		t.Fatalf("save legacy users collection: %v", err)
	}

	encryptor, err := secure.NewEncryptorFromConfig("", filepath.Join(dataDir, "app.key"))
	if err != nil {
		t.Fatalf("create encryptor: %v", err)
	}

	st := New(app, encryptor)
	if err := st.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	users, err := app.FindCollectionByNameOrId(UsersCollection)
	if err != nil {
		t.Fatalf("find users collection: %v", err)
	}
	if users.Fields.GetByName("active_provider") == nil {
		t.Fatal("expected active_provider field to be added to existing users collection")
	}
	if users.Fields.GetByName("theme") == nil {
		t.Fatal("expected theme field to be added to existing users collection")
	}
}

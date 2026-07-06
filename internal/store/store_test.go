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

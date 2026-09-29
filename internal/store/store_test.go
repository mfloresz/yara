package store

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
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

func TestClearNovelPendingNewChaptersResetsCountAndKeepsCheckedAt(t *testing.T) {
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
	owner.Set("email", "clear-pending@example.com")
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
	novel.Set("source_title", "Clear Pending Novel")
	novel.Set("status", "ongoing")
	if err := app.Save(novel); err != nil {
		t.Fatalf("save novel: %v", err)
	}

	if err := st.UpdateNovelCheckResult(novel.Id, "2026-01-02T03:04:05Z", 5); err != nil {
		t.Fatalf("record check result: %v", err)
	}
	if err := st.ClearNovelPendingNewChapters(novel.Id); err != nil {
		t.Fatalf("clear pending new chapters: %v", err)
	}

	after, err := st.GetNovelAccessible(owner.Id, novel.Id)
	if err != nil {
		t.Fatalf("reload novel: %v", err)
	}
	if after.LastCheckNewChapters != 0 {
		t.Fatalf("expected pending new chapters to be 0, got %d", after.LastCheckNewChapters)
	}
	if after.LastCheckedAt != "2026-01-02T03:04:05Z" {
		t.Fatalf("expected last_checked_at to be preserved, got %q", after.LastCheckedAt)
	}

	if err := st.ClearNovelPendingNewChapters("missing-novel-id"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound for unknown novel, got %v", err)
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

func TestNormalizeLanguageCodeLowercasesAndTrims(t *testing.T) {
	cases := map[string]string{
		"en":     "en",
		"EN":     "en",
		"  Es  ": "es",
		"":       "",
		"pt-BR":  "pt-br",
	}
	for input, want := range cases {
		if got := normalizeLanguageCode(input); got != want {
			t.Fatalf("normalizeLanguageCode(%q) = %q, want %q", input, got, want)
		}
	}
}

// Languages are stored lowercased so the ?progress=translated filter's
// `source_language = target_language` comparison (case-sensitive in SQL) matches
// regardless of how the client cased the value.
func TestCreateAndUpdateNovelNormalizeLanguages(t *testing.T) {
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
	owner.Set("email", "lang-test@example.com")
	owner.Set("password", "secret123")
	owner.Set("passwordConfirm", "secret123")
	if err := app.Save(owner); err != nil {
		t.Fatalf("save owner user: %v", err)
	}

	novel := &Novel{
		SourceTitle:    "Novela",
		SourceLanguage: "ES",
		TargetLanguage: " es ",
	}
	if err := st.CreateNovel(owner.Id, novel); err != nil {
		t.Fatalf("create novel: %v", err)
	}
	if novel.SourceLanguage != "es" || novel.TargetLanguage != "es" {
		t.Fatalf("create should normalize languages, got %q -> %q", novel.SourceLanguage, novel.TargetLanguage)
	}

	updated, err := st.UpdateNovel(owner.Id, novel.ID, map[string]any{
		"sourceLanguage": "EN",
		"targetLanguage": "Es",
	})
	if err != nil {
		t.Fatalf("update novel: %v", err)
	}
	if updated.SourceLanguage != "en" || updated.TargetLanguage != "es" {
		t.Fatalf("update should normalize languages, got %q -> %q", updated.SourceLanguage, updated.TargetLanguage)
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

func TestRunChapterPositionsMigrationBackfillsAndIsIdempotent(t *testing.T) {
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

	// Simulate a pre-migration collection: the position/excluded fields exist
	// (added idempotently by EnsureSchema) but no positions are assigned and
	// the unique index is absent — exactly the state of an upgraded database
	// before running --migrate-chapter-positions.
	chaptersCollection, err := app.FindCollectionByNameOrId(ChaptersCollection)
	if err != nil {
		t.Fatalf("find chapters collection: %v", err)
	}
	chaptersCollection.RemoveIndex("idx_chapters_novel_position_unique")
	if err := app.Save(chaptersCollection); err != nil {
		t.Fatalf("drop position index to simulate legacy schema: %v", err)
	}

	users, err := app.FindCollectionByNameOrId(UsersCollection)
	if err != nil {
		t.Fatalf("find users collection: %v", err)
	}
	owner := core.NewRecord(users)
	owner.Set("email", "migration-test@example.com")
	owner.Set("password", "secret123")
	owner.Set("passwordConfirm", "secret123")
	if err := app.Save(owner); err != nil {
		t.Fatalf("save owner user: %v", err)
	}

	novels, err := app.FindCollectionByNameOrId(NovelsCollection)
	if err != nil {
		t.Fatalf("find novels collection: %v", err)
	}
	newNovel := func(title string) *core.Record {
		n := core.NewRecord(novels)
		n.Set("owner", owner.Id)
		n.Set("source_language", "en")
		n.Set("target_language", "es")
		n.Set("source_title", title)
		n.Set("source_author", "Author")
		if err := app.Save(n); err != nil {
			t.Fatalf("save novel %s: %v", title, err)
		}
		return n
	}
	novelA := newNovel("Novel A")
	novelB := newNovel("Novel B")

	// Legacy inserts: chapter_order only, position left unset (0).
	addChapter := func(novelID string, order int) string {
		rec := core.NewRecord(chaptersCollection)
		rec.Set("novel", novelID)
		rec.Set("chapter_order", order)
		rec.Set("title", "Legacy Ch")
		rec.Set("original_content", "legacy content")
		rec.Set("status", "pending")
		if err := app.Save(rec); err != nil {
			t.Fatalf("save legacy chapter: %v", err)
		}
		return rec.Id
	}
	a3 := addChapter(novelA.Id, 3)
	a1 := addChapter(novelA.Id, 1)
	a2 := addChapter(novelA.Id, 2)
	b10 := addChapter(novelB.Id, 10)
	b5 := addChapter(novelB.Id, 5)

	if err := st.RunChapterPositionsMigration(); err != nil {
		t.Fatalf("run chapter positions migration: %v", err)
	}

	positionOf := func(id string) int {
		rec, err := app.FindRecordById(ChaptersCollection, id)
		if err != nil {
			t.Fatalf("find chapter %s: %v", id, err)
		}
		return asInt(rec.GetFloat("position"), 0)
	}
	// Novel A: positions assigned in ascending chapter_order (1, 2, 3).
	if got := positionOf(a1); got != 1 {
		t.Fatalf("expected a1 position 1, got %d", got)
	}
	if got := positionOf(a2); got != 2 {
		t.Fatalf("expected a2 position 2, got %d", got)
	}
	if got := positionOf(a3); got != 3 {
		t.Fatalf("expected a3 position 3, got %d", got)
	}
	// Novel B: independent positions per novel.
	if got := positionOf(b5); got != 1 {
		t.Fatalf("expected b5 position 1, got %d", got)
	}
	if got := positionOf(b10); got != 2 {
		t.Fatalf("expected b10 position 2, got %d", got)
	}

	// Idempotent: re-running changes nothing.
	snapshot := map[string]int{a1: positionOf(a1), a2: positionOf(a2), a3: positionOf(a3), b5: positionOf(b5), b10: positionOf(b10)}
	if err := st.RunChapterPositionsMigration(); err != nil {
		t.Fatalf("re-run migration: %v", err)
	}
	for id, want := range snapshot {
		if got := positionOf(id); got != want {
			t.Fatalf("migration re-run changed position of %s: got %d, want %d", id, got, want)
		}
	}

	// A user reorder is preserved: only unset positions are backfilled.
	moved := a1
	rec, err := app.FindRecordById(ChaptersCollection, moved)
	if err != nil {
		t.Fatalf("find chapter to move: %v", err)
	}
	rec.Set("position", 99)
	if err := app.Save(rec); err != nil {
		t.Fatalf("simulate reorder: %v", err)
	}
	if err := st.RunChapterPositionsMigration(); err != nil {
		t.Fatalf("re-run migration after reorder: %v", err)
	}
	if got := positionOf(moved); got != 99 {
		t.Fatalf("migration clobbered a user reorder: position %d, want 99", got)
	}

	// The unique (novel, position) index is created by the migration.
	fresh, err := app.FindCollectionByNameOrId(ChaptersCollection)
	if err != nil {
		t.Fatalf("reload chapters collection: %v", err)
	}
	if fresh.GetIndex("idx_chapters_novel_position_unique") == "" {
		t.Fatal("expected migration to create the unique chapter position index")
	}
}

// setupChaptersTestEnv boots a real PocketBase into a temp dir and creates an
// owner + novel, returning the store, app, owner, novel and chapters collection.
func setupChaptersTestEnv(t *testing.T, email, title string) (*Store, *pocketbase.PocketBase, *core.Record, *core.Record, *core.Collection) {
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
	st := New(app, encryptor)
	if err := st.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	users, err := app.FindCollectionByNameOrId(UsersCollection)
	if err != nil {
		t.Fatalf("find users collection: %v", err)
	}
	owner := core.NewRecord(users)
	owner.Set("email", email)
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
	novel.Set("source_title", title)
	if err := app.Save(novel); err != nil {
		t.Fatalf("save novel: %v", err)
	}
	chapters, err := app.FindCollectionByNameOrId(ChaptersCollection)
	if err != nil {
		t.Fatalf("find chapters collection: %v", err)
	}
	return st, app, owner, novel, chapters
}

// Guards the aggregate SQL behind RecalculateNovelStats: excluded chapters
// must count toward max_chapter_order only, and the visible counters/char
// sums must skip them — the exact semantics of the record loop it replaced.
func TestRecalculateNovelStatsSkipsExcludedChapters(t *testing.T) {
	st, app, _, novel, chapters := setupChaptersTestEnv(t, "stats-test@example.com", "Stats Novel")

	addChapter := func(order int, position int, status string, excluded bool, orig, trans, refined int) {
		rec := core.NewRecord(chapters)
		rec.Set("novel", novel.Id)
		rec.Set("chapter_order", order)
		rec.Set("position", position)
		rec.Set("excluded", excluded)
		rec.Set("status", status)
		rec.Set("original_content", strings.Repeat("a", orig))
		rec.Set("translated_content", strings.Repeat("b", trans))
		rec.Set("refined_content", strings.Repeat("c", refined))
		rec.Set("original_char_count", orig)
		rec.Set("translated_char_count", trans)
		rec.Set("refined_char_count", refined)
		if err := app.Save(rec); err != nil {
			t.Fatalf("save chapter %d: %v", order, err)
		}
	}
	addChapter(1, 1, "translated", false, 100, 80, 0)
	addChapter(2, 2, "refined", false, 200, 150, 140)
	addChapter(3, 3, "pending", true, 500, 0, 0)

	if err := st.RecalculateNovelStats(novel.Id); err != nil {
		t.Fatalf("recalculate: %v", err)
	}
	updated, err := app.FindRecordById(NovelsCollection, novel.Id)
	if err != nil {
		t.Fatalf("reload novel: %v", err)
	}
	assertField := func(name string, want float64) {
		t.Helper()
		if got := updated.GetFloat(name); got != want {
			t.Fatalf("%s = %v, want %v", name, got, want)
		}
	}
	assertField("chapter_count", 2)
	assertField("translated_count", 2)
	assertField("completed_count", 1)
	assertField("original_char_count", 300)
	assertField("translated_char_count", 230)
	assertField("refined_char_count", 140)
	assertField("total_char_count", 670)
	assertField("max_chapter_order", 3)
}

// Guards the projected summary queries: flags/counts/neighbors must match
// what the full-record hydration produced before, without selecting content.
func TestChapterSummariesProjection(t *testing.T) {
	st, app, owner, novel, chapters := setupChaptersTestEnv(t, "summary-test@example.com", "Summary Novel")

	addChapter := func(order int, title string, orig, trans, refined string, excluded bool) *core.Record {
		rec := core.NewRecord(chapters)
		rec.Set("novel", novel.Id)
		rec.Set("chapter_order", order)
		rec.Set("position", order)
		rec.Set("excluded", excluded)
		rec.Set("title", title)
		rec.Set("original_content", orig)
		rec.Set("translated_content", trans)
		rec.Set("refined_content", refined)
		rec.Set("original_char_count", len(orig))
		rec.Set("translated_char_count", len(trans))
		rec.Set("refined_char_count", len(refined))
		if err := app.Save(rec); err != nil {
			t.Fatalf("save chapter %d: %v", order, err)
		}
		return rec
	}
	one := addChapter(1, "One", "abc", "defg", "", false)
	addChapter(2, "Two", "", "", "", false)
	addChapter(3, "Three", "xyz", "", "", true)

	visible, err := st.ListAllChapterSummariesAccessible(owner.Id, novel.Id)
	if err != nil {
		t.Fatalf("list summaries: %v", err)
	}
	if len(visible) != 2 {
		t.Fatalf("expected 2 visible summaries (excluded hidden), got %d", len(visible))
	}
	first, second := visible[0], visible[1]
	if first.Title != "One" || second.Title != "Two" {
		t.Fatalf("unexpected order: %q then %q", first.Title, second.Title)
	}
	if !first.HasOriginalContent || !first.HasTranslatedContent || first.HasRefinedContent {
		t.Fatalf("chapter One flags wrong: %+v", first)
	}
	if first.OriginalChars != 3 || first.TranslatedChars != 4 || first.RefinedChars != 0 {
		t.Fatalf("chapter One char counts wrong: %+v", first)
	}
	if first.Position != 1 || second.Position != 2 {
		t.Fatalf("positions wrong: %d, %d", first.Position, second.Position)
	}
	if _, err := types.ParseDateTime(first.CreatedAt); err != nil {
		t.Fatalf("CreatedAt %q does not parse as PB datetime: %v", first.CreatedAt, err)
	}
	if second.HasOriginalContent || second.HasTranslatedContent || second.HasRefinedContent {
		t.Fatalf("chapter Two should have no content flags: %+v", second)
	}
	if second.Status != "pending" {
		t.Fatalf("empty status should default to pending, got %q", second.Status)
	}

	excluded, err := st.ListExcludedChapterSummariesAccessible(owner.Id, novel.Id)
	if err != nil {
		t.Fatalf("list excluded summaries: %v", err)
	}
	if len(excluded) != 1 || excluded[0].Title != "Three" || !excluded[0].HasOriginalContent {
		t.Fatalf("excluded summaries wrong: %+v", excluded)
	}

	prev, next, err := st.GetChapterNeighborsAccessible(owner.Id, novel.Id, one.Id)
	if err != nil {
		t.Fatalf("neighbors: %v", err)
	}
	if prev != nil || next == nil || next.Title != "Two" {
		t.Fatalf("neighbors of One wrong: prev=%v next=%v", prev, next)
	}
	prev, next, err = st.GetChapterNeighborsAccessible(owner.Id, novel.Id, next.ID)
	if err != nil {
		t.Fatalf("neighbors of Two: %v", err)
	}
	if prev == nil || prev.Title != "One" || next != nil {
		t.Fatalf("neighbors of Two wrong: prev=%v next=%v", prev, next)
	}
}

// Guards the narrow job-progress UPDATE: a running job gets its progress
// columns persisted without a full row rewrite, and a cancelled job ignores
// mid-flight writes (mirroring UpdateJob's allowlist — only the final
// bookkeeping may touch a cancelled job).
func TestUpdateJobProgressFastCancelledGuard(t *testing.T) {
	st, app, owner, novel, _ := setupChaptersTestEnv(t, "jobprogress-test@example.com", "Progress Novel")

	jobs, err := app.FindCollectionByNameOrId(JobsCollection)
	if err != nil {
		t.Fatalf("find jobs collection: %v", err)
	}
	createJob := func(status string) *core.Record {
		rec := core.NewRecord(jobs)
		rec.Set("owner", owner.Id)
		rec.Set("novel", novel.Id)
		rec.Set("status", status)
		rec.Set("operation", "translate")
		rec.Set("total_chapters", 3)
		if err := app.Save(rec); err != nil {
			t.Fatalf("save job %s: %v", status, err)
		}
		return rec
	}
	running := createJob("running")
	cancelled := createJob("cancelled")

	if err := st.UpdateJobProgressFast(running.Id, map[string]any{
		"completedChapters":       2,
		"failedChapters":          1,
		"autoSegmentChapterTitle": "Capítulo 2",
	}); err != nil {
		t.Fatalf("update running job progress: %v", err)
	}
	fresh, err := app.FindRecordById(JobsCollection, running.Id)
	if err != nil {
		t.Fatalf("reload running job: %v", err)
	}
	if got := fresh.GetFloat("completed_chapters"); got != 2 {
		t.Fatalf("running completed_chapters = %v, want 2", got)
	}
	if got := fresh.GetFloat("failed_chapters"); got != 1 {
		t.Fatalf("running failed_chapters = %v, want 1", got)
	}
	if got := fresh.GetString("auto_segment_chapter_title"); got != "Capítulo 2" {
		t.Fatalf("running auto_segment_chapter_title = %q, want %q", got, "Capítulo 2")
	}

	if err := st.UpdateJobProgressFast(cancelled.Id, map[string]any{
		"completedChapters":       99,
		"autoSegmentChapterTitle": "late write",
	}); err != nil {
		t.Fatalf("update cancelled job progress: %v", err)
	}
	freshCancelled, err := app.FindRecordById(JobsCollection, cancelled.Id)
	if err != nil {
		t.Fatalf("reload cancelled job: %v", err)
	}
	if got := freshCancelled.GetFloat("completed_chapters"); got != 0 {
		t.Fatalf("cancelled completed_chapters = %v, want 0 (write must be skipped)", got)
	}
	if got := freshCancelled.GetString("auto_segment_chapter_title"); got != "" {
		t.Fatalf("cancelled auto_segment_chapter_title = %q, want empty (write must be skipped)", got)
	}
}

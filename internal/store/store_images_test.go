package store

import (
	"path/filepath"
	"testing"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"translator-server/internal/secure"
)

// newImagesTestStore boots a throwaway PocketBase with a schema, one user and
// one novel, returning the store, owner ID and novel ID.
func newImagesTestStore(t *testing.T) (*Store, string, string) {
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
	owner.Set("email", "images-test@example.com")
	owner.Set("password", "secret123")
	owner.Set("passwordConfirm", "secret123")
	if err := app.Save(owner); err != nil {
		t.Fatalf("save owner: %v", err)
	}
	novel := &Novel{
		SourceTitle:    "Image Book",
		SourceLanguage: "en",
		TargetLanguage: "es",
		Status:         "completed",
	}
	if err := st.CreateNovel(owner.Id, novel); err != nil {
		t.Fatalf("create novel: %v", err)
	}
	return st, owner.Id, novel.ID
}

func TestImportNovelImagesLifecycle(t *testing.T) {
	st, ownerID, _ := newImagesTestStore(t)

	result, err := st.ImportEpubNovel(&ImportEpubNovelInput{
		OwnerID:        ownerID,
		FileName:       "book.epub",
		FileBlob:       []byte("PK\x03\x04fake-epub-bytes"),
		MimeType:       "application/epub+zip",
		SourceTitle:    "Image Book",
		SourceLanguage: "en",
		TargetLanguage: "es",
		Chapters: []ImportedEpubChapter{
			{
				Title:   "Chapter 1",
				Content: "Body one.\n\n[[IMG-1]]\n\n[[IMG-2]]\n\nEnd.",
				Images: []ImportedNovelImage{
					{Alt: "First", MimeType: "image/jpeg", Blob: []byte{0xFF, 0xD8, 0xFF, 0xE0}},
					{Alt: "Second", MimeType: "image/png", Blob: []byte{0x89, 0x50, 0x4E, 0x47}},
				},
			},
			{Title: "Chapter 2", Content: "No images."},
		},
	})
	if err != nil {
		t.Fatalf("ImportEpubNovel: %v", err)
	}

	images, err := st.ListNovelImages(result.Novel.ID)
	if err != nil {
		t.Fatalf("ListNovelImages: %v", err)
	}
	if len(images) != 2 {
		t.Fatalf("expected 2 stored images, got %d", len(images))
	}
	if images[0].ChapterID == "" || images[0].Num != 1 || images[0].Alt != "First" || images[0].FileName == "" {
		t.Errorf("unexpected image[0]: %+v", images[0])
	}
	if images[1].Num != 2 || images[1].MimeType != "image/png" {
		t.Errorf("unexpected image[1]: %+v", images[1])
	}

	chapterID := images[0].ChapterID
	ownedNovelID := result.Novel.ID
	record, fileName, err := st.GetNovelImageFile(ownedNovelID, images[0].ID)
	if err != nil {
		t.Fatalf("GetNovelImageFile: %v", err)
	}
	if record.Id != images[0].ID || fileName == "" {
		t.Errorf("unexpected image file resolution: %s / %q", record.Id, fileName)
	}
	if _, _, err := st.GetNovelImageFile(ownedNovelID+"-other", images[0].ID); err != ErrNotFound {
		t.Errorf("expected ErrNotFound for foreign novel, got %v", err)
	}
	if _, _, err := st.GetNovelImageFile(ownedNovelID, "doesnotexist"); err != ErrNotFound {
		t.Errorf("expected ErrNotFound for unknown image, got %v", err)
	}

	// Deleting the owning chapter must cascade-delete its images.
	chapters, err := st.ListChaptersAccessible(ownerID, ownedNovelID)
	if err != nil {
		t.Fatalf("ListChaptersAccessible: %v", err)
	}
	for _, ch := range chapters {
		if ch.ID != chapterID {
			continue
		}
		chaptersCollection, cErr := st.App.FindCollectionByNameOrId(ChaptersCollection)
		if cErr != nil {
			t.Fatalf("find chapters collection: %v", cErr)
		}
		chRecord, rErr := st.App.FindRecordById(chaptersCollection, ch.ID)
		if rErr != nil {
			t.Fatalf("find chapter record: %v", rErr)
		}
		if err := st.App.Delete(chRecord); err != nil {
			t.Fatalf("delete chapter record: %v", err)
		}
	}
	remaining, err := st.ListChapterImages(chapterID)
	if err != nil {
		t.Fatalf("ListChapterImages after delete: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("expected cascade delete to remove chapter images, got %d", len(remaining))
	}
}

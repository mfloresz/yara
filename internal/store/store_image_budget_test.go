package store

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImageWriteBudgetCountsAllOccurrences(t *testing.T) {
	budget := &imageWriteBudget{remaining: 4}
	for i := 0; i < 2; i++ {
		if err := budget.add(2); err != nil {
			t.Fatal(err)
		}
	}
	if err := budget.add(1); err == nil {
		t.Fatal("budget accepted a byte over the limit")
	}
	if err := newImageWriteBudget().add(20<<20 + 1); err == nil {
		t.Fatal("budget accepted an oversized image")
	}
}

func TestImageWriteBudgetRejectsBeforeStorageIncludingS3(t *testing.T) {
	st, owner, _ := newImagesTestStore(t)
	t.Cleanup(func() { st.App.ResetBootstrapState() })
	// Only one MiB is allocated in memory, but 65 occurrences would cause
	// 65 MiB of uploads when no local hardlink optimization is available.
	blob := bytes.Repeat([]byte{1}, 1<<20)
	epubChapters := make([]ImportedEpubChapter, 65)
	zipChapters := make([]ImportedZipChapter, 65)
	for i := range epubChapters {
		images := []ImportedNovelImage{{MimeType: "image/png", Blob: blob}}
		epubChapters[i] = ImportedEpubChapter{Title: "Chapter", Content: "[[IMG-1]]", Images: images}
		zipChapters[i] = ImportedZipChapter{Title: "Chapter", OriginalContent: "[[IMG-1]]", Images: images}
	}
	for _, s3 := range []bool{false, true} {
		st.App.Settings().S3.Enabled = s3
		for _, run := range []func() error{
			func() error {
				_, err := st.ImportEpubNovel(&ImportEpubNovelInput{OwnerID: owner, SourceTitle: "Rejected", Chapters: epubChapters})
				return err
			},
			func() error {
				_, err := st.ImportZipNovel(&ImportZipNovelInput{OwnerID: owner, MetadataJSON: `{"sourceTitle":"Rejected","sourceLanguage":"en","targetLanguage":"es"}`, Chapters: zipChapters})
				return err
			},
		} {
			if err := run(); err == nil || !strings.Contains(err.Error(), "all chapter references count") {
				t.Fatalf("S3=%v unexpected error: %v", s3, err)
			}
			for collection, want := range map[string]int64{NovelsCollection: 1, ChaptersCollection: 0, NovelImagesCollection: 0, EpubsCollection: 0} {
				count, err := st.App.CountRecords(collection)
				if err != nil || count != want {
					t.Fatalf("S3=%v partial state in %s: count=%d err=%v", s3, collection, count, err)
				}
			}
		}
	}
	st.App.Settings().S3.Enabled = false
	storage := filepath.Join(st.App.DataDir(), "storage")
	err := filepath.WalkDir(storage, func(path string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			t.Errorf("rejected import wrote storage file %s", path)
		}
		return err
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestCopyImageWriteBudgetRejectsBeforeCreatingClone(t *testing.T) {
	st, owner, _ := newImagesTestStore(t)
	t.Cleanup(func() { st.App.ResetBootstrapState() })
	_, chapters := sharedImageFixture(t)
	chapters = append(chapters, chapters[0])
	result, err := st.ImportEpubNovel(&ImportEpubNovelInput{OwnerID: owner, FileName: "legacy.epub", FileBlob: []byte("fixture"),
		MimeType: "application/epub+zip", SourceTitle: "Legacy", SourceLanguage: "en", TargetLanguage: "es", Chapters: chapters})
	if err != nil {
		t.Fatal(err)
	}
	_, paths := imagePaths(t, st, result.Novel.ID)
	// Simulate pre-fix data with four references to a 17 MiB image. The
	// existing paths share an inode, so truncate creates one sparse fixture.
	if err := os.Truncate(paths[0], 17<<20); err != nil {
		t.Fatal(err)
	}
	before, err := st.App.CountRecords(NovelsCollection)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CopyNovel(owner, result.Novel.ID); err == nil || !strings.Contains(err.Error(), "all chapter references count") {
		t.Fatalf("copy error: %v", err)
	}
	after, err := st.App.CountRecords(NovelsCollection)
	if err != nil || before != after {
		t.Fatalf("copy left a partial clone: before=%d after=%d err=%v", before, after, err)
	}
}

func TestImageBulkFailureRollsBackRecordsAndFiles(t *testing.T) {
	st, _, novelID := newImagesTestStore(t)
	t.Cleanup(func() { st.App.ResetBootstrapState() })
	chapterIDs, err := st.insertChaptersBulk(novelID, []Chapter{{Title: "Fixture", ChapterOrder: 1, Position: 1, OriginalContent: "[[IMG-1]]"}})
	if err != nil {
		t.Fatal(err)
	}
	images := []NovelImageBlob{
		{ChapterID: chapterIDs[0], Num: 1, MimeType: "image/png", Blob: []byte("fixture")},
		{ChapterID: chapterIDs[0], Num: 1, MimeType: "image/png", Blob: []byte("fixture")},
	}
	if err := st.insertNovelImageBlobsBulk(novelID, images); err == nil {
		t.Fatal("duplicate chapter/token should reject the bulk operation")
	}
	if count, err := st.App.CountRecords(NovelImagesCollection); err != nil || count != 0 {
		t.Fatalf("image transaction left records: count=%d err=%v", count, err)
	}
	collection, err := st.App.FindCollectionByNameOrId(NovelImagesCollection)
	if err != nil {
		t.Fatal(err)
	}
	err = filepath.WalkDir(filepath.Join(st.App.DataDir(), "storage", collection.Id), func(path string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			t.Errorf("rolled back image transaction left file %s", path)
		}
		return err
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestUnavailableHardlinksLeaveIndependentFilesIntact(t *testing.T) {
	dir := t.TempDir()
	source, target := filepath.Join(dir, "source.png"), filepath.Join(dir, "target.png")
	blob := []byte("synthetic image bytes")
	for _, path := range []string{source, target} {
		if err := os.WriteFile(path, blob, 0600); err != nil {
			t.Fatal(err)
		}
	}
	unsupported := errors.New("hardlinks unsupported")
	if err := replaceImageWithHardlink(source, target, func(_, _ string) error { return unsupported }); !errors.Is(err, unsupported) {
		t.Fatalf("hardlink error: %v", err)
	}
	for _, path := range []string{source, target} {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, blob) {
			t.Fatalf("fallback changed file: %v", err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 {
		t.Fatalf("temporary link leaked: entries=%d err=%v", len(entries), err)
	}
	sourceInfo, _ := os.Stat(source)
	targetInfo, _ := os.Stat(target)
	if os.SameFile(sourceInfo, targetInfo) {
		t.Fatal("failed hardlink unexpectedly linked files")
	}
}

package store

import (
	"archive/zip"
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"translator-server/internal/epubexport"
	"translator-server/internal/epubimport"
)

func sharedImageFixture(t *testing.T) ([]byte, []ImportedEpubChapter) {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, 2, 2))
	im.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, im); err != nil {
		t.Fatal(err)
	}
	chapters := make([]ImportedEpubChapter, 3)
	for i := range chapters {
		chapters[i] = ImportedEpubChapter{
			Title:   fmt.Sprintf("Chapter %d", i+1),
			Content: strings.Repeat("Synthetic fixture text, no user data. ", 10) + "\n\n[[IMG-1]]",
			Images:  []ImportedNovelImage{{Alt: fmt.Sprintf("Alt %d", i+1), MimeType: "image/png", Blob: buf.Bytes()}},
		}
	}
	return buf.Bytes(), chapters
}

func imagePaths(t *testing.T, st *Store, novelID string) ([]NovelImage, []string) {
	t.Helper()
	images, err := st.ListNovelImages(novelID)
	if err != nil {
		t.Fatal(err)
	}
	paths := make([]string, len(images))
	for i, img := range images {
		record, name, err := st.GetNovelImageFile(novelID, img.ID)
		if err != nil {
			t.Fatal(err)
		}
		paths[i] = filepath.Join(st.App.DataDir(), "storage", record.BaseFilesPath(), name)
	}
	return images, paths
}

func assertSharedImages(t *testing.T, st *Store, novelID string, want int, expected []byte) []string {
	t.Helper()
	images, paths := imagePaths(t, st, novelID)
	if len(images) != want {
		t.Fatalf("images=%d want=%d", len(images), want)
	}
	var first os.FileInfo
	for i, name := range paths {
		info, err := os.Stat(name)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = info
		} else if !os.SameFile(first, info) {
			t.Fatal("identical image paths do not share physical storage")
		}
		blob, err := os.ReadFile(name)
		if err != nil || !bytes.Equal(blob, expected) {
			t.Fatalf("image bytes mismatch: %v", err)
		}
	}
	blobs, err := st.GetNovelImageBlobs(novelID)
	if err != nil || len(blobs) != want {
		t.Fatalf("image blobs=%d err=%v", len(blobs), err)
	}
	for i := 1; i < len(blobs); i++ {
		if &blobs[0].Blob[0] != &blobs[i].Blob[0] {
			t.Fatal("export retains a separate allocation for repeated bytes")
		}
	}
	return paths
}

func TestSharedImportedImageStorageCopyDeleteAndExport(t *testing.T) {
	st, owner, _ := newImagesTestStore(t)
	t.Cleanup(func() { st.App.ResetBootstrapState() })
	blob, chapters := sharedImageFixture(t)
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	files := map[string]string{
		"mimetype":               "application/epub+zip",
		"META-INF/container.xml": `<container><rootfiles><rootfile full-path="OEBPS/content.opf"/></rootfiles></container>`,
	}
	manifest, spine := `<item id="img" href="shared.png" media-type="image/png"/>`, ""
	for i, ch := range chapters {
		manifest += fmt.Sprintf(`<item id="c%d" href="chapter%d.xhtml" media-type="application/xhtml+xml"/>`, i+1, i+1)
		spine += fmt.Sprintf(`<itemref idref="c%d"/>`, i+1)
		files[fmt.Sprintf("OEBPS/chapter%d.xhtml", i+1)] = fmt.Sprintf(`<html><head><title>%s</title></head><body><h1>%s</h1><p>%s</p><img src="shared.png" alt="Alt %d"/></body></html>`, ch.Title, ch.Title, strings.ReplaceAll(ch.Content, "[[IMG-1]]", ""), i+1)
	}
	files["OEBPS/content.opf"] = `<package><metadata><dc:title>Shared images</dc:title><dc:language>en</dc:language></metadata><manifest>` + manifest + `</manifest><spine>` + spine + `</spine></package>`
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, content); err != nil {
			t.Fatal(err)
		}
	}
	w, err := zw.Create("OEBPS/shared.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(blob); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	parsedInput, err := epubimport.Parse(archive.Bytes(), "shared.epub")
	if err != nil || len(parsedInput.Chapters) != 3 {
		t.Fatalf("parse fixture: %v", err)
	}
	for i, ch := range parsedInput.Chapters {
		if len(ch.Images) != 1 {
			t.Fatalf("fixture chapter images=%d", len(ch.Images))
		}
		chapters[i] = ImportedEpubChapter{Title: ch.Title, Content: ch.Content,
			Images: []ImportedNovelImage{{Alt: ch.Images[0].Alt, MimeType: ch.Images[0].MimeType, Blob: ch.Images[0].Blob}}}
	}
	input := &ImportEpubNovelInput{OwnerID: owner, FileName: "shared.epub", FileBlob: archive.Bytes(),
		MimeType: "application/epub+zip", SourceTitle: "Shared images", SourceLanguage: "en", TargetLanguage: "es", Chapters: chapters}
	result, err := st.ImportEpubNovel(input)
	if err != nil {
		t.Fatal(err)
	}
	sourcePaths := assertSharedImages(t, st, result.Novel.ID, 3, blob)
	clone, err := st.CopyNovel(owner, result.Novel.ID)
	if err != nil {
		t.Fatal(err)
	}
	clonePaths := assertSharedImages(t, st, clone.ID, 3, blob)
	sourceInfo, _ := os.Stat(sourcePaths[0])
	cloneInfo, _ := os.Stat(clonePaths[0])
	if os.SameFile(sourceInfo, cloneInfo) {
		t.Fatal("deduplication must be scoped to the novel, not across novels")
	}
	images, _ := imagePaths(t, st, result.Novel.ID)
	chapter, err := st.App.FindRecordById(ChaptersCollection, images[0].ChapterID)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.App.Delete(chapter); err != nil {
		t.Fatal(err)
	}
	assertSharedImages(t, st, result.Novel.ID, 2, blob)
	if _, err := os.Stat(sourcePaths[0]); !os.IsNotExist(err) {
		t.Fatalf("deleted chapter path still exists: %v", err)
	}
	novel, err := st.App.FindRecordById(NovelsCollection, result.Novel.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.App.Delete(novel); err != nil {
		t.Fatal(err)
	}
	assertSharedImages(t, st, clone.ID, 3, blob)

	// Export the copy after deleting the source; every token and alt must
	// still resolve to the correct bytes, including a parse round-trip.
	copiedChapters, err := st.ListChaptersAccessible(owner, clone.ID)
	if err != nil {
		t.Fatal(err)
	}
	blobs, err := st.GetNovelImageBlobs(clone.ID)
	if err != nil {
		t.Fatal(err)
	}
	exportChapters := make([]epubexport.ChapterData, len(copiedChapters))
	for i, ch := range copiedChapters {
		exportChapters[i] = epubexport.ChapterData{Title: ch.Title, Content: ch.OriginalContent}
		for _, img := range blobs {
			if img.ChapterID == ch.ID {
				exportChapters[i].Images = append(exportChapters[i].Images,
					epubexport.ImageFile{Alt: img.Alt, MimeType: img.MimeType, Blob: img.Blob})
			}
		}
	}
	exported, err := epubexport.GenerateEpubFile(epubexport.EpubMetadata{Title: clone.SourceTitle, Language: "en"}, exportChapters, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := epubimport.Parse(exported, "copy.epub")
	if err != nil || len(parsed.Chapters) != 3 {
		t.Fatalf("parse export chapters=%v err=%v", parsed, err)
	}
	for i, ch := range parsed.Chapters {
		if len(ch.Images) != 1 || !bytes.Equal(ch.Images[0].Blob, blob) || ch.Images[0].Alt != chapters[i].Images[0].Alt {
			t.Fatalf("round-trip image mismatch in chapter %d: title=%q images=%+v", i, ch.Title, ch.Images)
		}
	}
	zr, err := zip.NewReader(bytes.NewReader(exported), int64(len(exported)))
	if err != nil {
		t.Fatal(err)
	}
	imageCount := 0
	for _, f := range zr.File {
		if !strings.HasPrefix(f.Name, "OEBPS/images/") {
			continue
		}
		reader, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(reader)
		reader.Close()
		if err != nil || !bytes.Equal(got, blob) {
			t.Fatalf("export image mismatch: %v", err)
		}
		imageCount++
	}
	if imageCount != 3 {
		t.Fatalf("export images=%d", imageCount)
	}
}

func TestImageExportSharesLegacyCopiesWithoutChangingFiles(t *testing.T) {
	st, owner, _ := newImagesTestStore(t)
	t.Cleanup(func() { st.App.ResetBootstrapState() })
	blob, chapters := sharedImageFixture(t)
	result, err := st.ImportEpubNovel(&ImportEpubNovelInput{OwnerID: owner, FileName: "legacy.epub", FileBlob: []byte("fixture"), MimeType: "application/epub+zip", SourceTitle: "Legacy", SourceLanguage: "en", TargetLanguage: "es", Chapters: chapters})
	if err != nil {
		t.Fatal(err)
	}
	_, paths := imagePaths(t, st, result.Novel.ID)
	// Simulate an old deployment: independent files at the same record paths.
	for _, name := range paths {
		if err := os.Remove(name); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, blob, 0600); err != nil {
			t.Fatal(err)
		}
	}
	blobs, err := st.GetNovelImageBlobs(result.Novel.ID)
	if err != nil || len(blobs) != 3 {
		t.Fatalf("blobs=%d err=%v", len(blobs), err)
	}
	for i := 1; i < 3; i++ {
		if &blobs[0].Blob[0] != &blobs[i].Blob[0] {
			t.Fatal("legacy duplicates retain separate allocations")
		}
		first, _ := os.Stat(paths[0])
		other, _ := os.Stat(paths[i])
		if os.SameFile(first, other) {
			t.Fatal("read-only export changed legacy storage")
		}
	}
	if err := os.Remove(paths[1]); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetNovelImageBlobs(result.Novel.ID); err == nil {
		t.Fatal("export must report a missing image, not silently drop it")
	}
}

func TestImportZipImagesShareStorage(t *testing.T) {
	st, owner, _ := newImagesTestStore(t)
	t.Cleanup(func() { st.App.ResetBootstrapState() })
	blob, chapters := sharedImageFixture(t)
	zipChapters := make([]ImportedZipChapter, len(chapters))
	for i, ch := range chapters {
		zipChapters[i] = ImportedZipChapter{Title: ch.Title, OriginalContent: ch.Content, Images: ch.Images}
	}
	result, err := st.ImportZipNovel(&ImportZipNovelInput{OwnerID: owner, MetadataJSON: `{"sourceTitle":"ZIP shared","sourceLanguage":"en","targetLanguage":"es"}`, Chapters: zipChapters})
	if err != nil {
		t.Fatal(err)
	}
	assertSharedImages(t, st, result.Novel.ID, 3, blob)
}

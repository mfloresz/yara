package store

import (
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
)

// importedEpubChapterImages pairs each imported EPUB chapter (and its
// extracted images) with the ID of the chapter record created for it.
func importedEpubChapterImages(chapters []ImportedEpubChapter, chapterIDs []string) []ImportedChapterImages {
	out := make([]ImportedChapterImages, 0, len(chapters))
	for i, ch := range chapters {
		if len(ch.Images) == 0 || i >= len(chapterIDs) {
			continue
		}
		out = append(out, ImportedChapterImages{ChapterID: chapterIDs[i], Images: ch.Images})
	}
	return out
}

// importedZipChapterImages is the import-zip counterpart of
// importedEpubChapterImages.
func importedZipChapterImages(chapters []ImportedZipChapter, chapterIDs []string) []ImportedChapterImages {
	out := make([]ImportedChapterImages, 0, len(chapters))
	for i, ch := range chapters {
		if len(ch.Images) == 0 || i >= len(chapterIDs) {
			continue
		}
		out = append(out, ImportedChapterImages{ChapterID: chapterIDs[i], Images: ch.Images})
	}
	return out
}

// insertNovelImagesBulk persists the inline images of freshly imported
// chapters inside one transaction. chapterIDs[i] is the ID of the chapter
// whose content carries the [[IMG-(i+1)]] tokens for chapters[i].Images.
func (s *Store) insertNovelImagesBulk(novelID string, chapters []ImportedChapterImages) error {
	hasImages := false
	for _, ch := range chapters {
		if len(ch.Images) > 0 {
			hasImages = true
			break
		}
	}
	if !hasImages {
		return nil
	}
	collection, err := s.App.FindCollectionByNameOrId(NovelImagesCollection)
	if err != nil {
		return err
	}
	return s.App.RunInTransaction(func(txApp core.App) error {
		for _, ch := range chapters {
			for i, img := range ch.Images {
				if len(img.Blob) == 0 {
					continue
				}
				record := core.NewRecord(collection)
				record.Set("novel", novelID)
				record.Set("chapter", ch.ChapterID)
				record.Set("num", i+1)
				record.Set("alt", clampText(img.Alt, 2000))
				record.Set("mime", clampText(img.MimeType, 64))
				file, fErr := filesystem.NewFileFromBytes(img.Blob, imageFileName(i+1, img.MimeType, img.FileName))
				if fErr != nil {
					return fErr
				}
				record.Set("file", []*filesystem.File{file})
				if err := txApp.Save(record); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func imageFileName(num int, mimeType, sourceName string) string {
	ext := strings.ToLower(path.Ext(sourceName))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".svg":
	default:
		ext = mimeExt(mimeType)
	}
	return fmt.Sprintf("img-%d%s", num, ext)
}

func mimeExt(mimeType string) string {
	if i := strings.IndexByte(mimeType, ';'); i >= 0 {
		mimeType = mimeType[:i]
	}
	switch strings.TrimSpace(strings.ToLower(mimeType)) {
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/svg+xml":
		return ".svg"
	default:
		return ".jpg"
	}
}

func novelImageFromRecord(record *core.Record) NovelImage {
	files := record.GetStringSlice("file")
	fileName := ""
	if len(files) > 0 {
		fileName = files[0]
	}
	return NovelImage{
		ID:        record.Id,
		NovelID:   record.GetString("novel"),
		ChapterID: record.GetString("chapter"),
		Num:       int(record.GetInt("num")),
		Alt:       record.GetString("alt"),
		MimeType:  record.GetString("mime"),
		FileName:  fileName,
		CreatedAt: record.GetString("created"),
	}
}

// ListNovelImages returns every stored image of a novel, grouped-ready for
// per-chapter consumers: sorted by chapter ID and then by token number.
func (s *Store) ListNovelImages(novelID string) ([]NovelImage, error) {
	records, err := s.App.FindRecordsByFilter(NovelImagesCollection, "novel = {:novel}", "", 0, 0, dbx.Params{"novel": novelID})
	if err != nil {
		return nil, err
	}
	out := make([]NovelImage, 0, len(records))
	for _, record := range records {
		out = append(out, novelImageFromRecord(record))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ChapterID != out[j].ChapterID {
			return out[i].ChapterID < out[j].ChapterID
		}
		return out[i].Num < out[j].Num
	})
	return out, nil
}

// ListChapterImages returns the stored images of one chapter in token order.
func (s *Store) ListChapterImages(chapterID string) ([]NovelImage, error) {
	records, err := s.App.FindRecordsByFilter(NovelImagesCollection, "chapter = {:chapter}", "num", 0, 0, dbx.Params{"chapter": chapterID})
	if err != nil {
		return nil, err
	}
	out := make([]NovelImage, 0, len(records))
	for _, record := range records {
		out = append(out, novelImageFromRecord(record))
	}
	return out, nil
}

// NovelImageBlob is a stored inline image with its bytes loaded, ready for
// EPUB embedding during export.
type NovelImageBlob struct {
	ChapterID string
	Num       int
	Alt       string
	MimeType  string
	Blob      []byte
}

// GetNovelImageBlobs loads every stored image of a novel with its bytes,
// ordered by chapter ID and token number. Failing to read a registered file
// is treated as corruption and aborts, so exports never silently drop
// images.
func (s *Store) GetNovelImageBlobs(novelID string) ([]NovelImageBlob, error) {
	records, err := s.App.FindRecordsByFilter(NovelImagesCollection, "novel = {:novel}", "", 0, 0, dbx.Params{"novel": novelID})
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, nil
	}
	fsys, err := s.App.NewFilesystem()
	if err != nil {
		return nil, err
	}
	defer fsys.Close()
	out := make([]NovelImageBlob, 0, len(records))
	for _, record := range records {
		files := record.GetStringSlice("file")
		if len(files) == 0 {
			continue
		}
		fileKey := record.BaseFilesPath() + "/" + files[0]
		reader, rErr := fsys.GetReader(fileKey)
		if rErr != nil {
			return nil, fmt.Errorf("get reader for %s: %w", fileKey, rErr)
		}
		blob, rErr := io.ReadAll(reader)
		reader.Close()
		if rErr != nil {
			return nil, fmt.Errorf("read file %s: %w", fileKey, rErr)
		}
		out = append(out, NovelImageBlob{
			ChapterID: record.GetString("chapter"),
			Num:       int(record.GetInt("num")),
			Alt:       record.GetString("alt"),
			MimeType:  record.GetString("mime"),
			Blob:      blob,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ChapterID != out[j].ChapterID {
			return out[i].ChapterID < out[j].ChapterID
		}
		return out[i].Num < out[j].Num
	})
	return out, nil
}

// GetNovelImageFile resolves an image record for serving. It fails with
// ErrNotFound unless the image belongs to the given novel.
func (s *Store) GetNovelImageFile(novelID, imageID string) (*core.Record, string, error) {
	record, err := s.App.FindRecordById(NovelImagesCollection, imageID)
	if err != nil {
		return nil, "", ErrNotFound
	}
	if record.GetString("novel") != novelID {
		return nil, "", ErrNotFound
	}
	files := record.GetStringSlice("file")
	if len(files) == 0 {
		return nil, "", ErrNotFound
	}
	return record, files[0], nil
}

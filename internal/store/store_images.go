package store

import (
	"crypto/sha256"
	"fmt"
	"io"
	"log/slog"
	"path"
	"sort"
	"strings"

	"translator-server/internal/epubimport"

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
	var images []NovelImageBlob
	for _, ch := range chapters {
		for i, img := range ch.Images {
			images = append(images, NovelImageBlob{ChapterID: ch.ChapterID, Num: i + 1,
				Alt: img.Alt, MimeType: img.MimeType, FileName: img.FileName, Blob: img.Blob})
		}
	}
	return s.insertNovelImageBlobsBulk(novelID, images)
}

func (s *Store) insertNovelImageBlobsBulk(novelID string, images []NovelImageBlob) error {
	budget := newImageWriteBudget()
	for _, img := range images {
		if err := budget.add(int64(len(img.Blob))); err != nil {
			return err
		}
	}
	if len(images) == 0 {
		return nil
	}
	collection, err := s.App.FindCollectionByNameOrId(NovelImagesCollection)
	if err != nil {
		return err
	}
	return s.App.RunInTransaction(func(txApp core.App) error {
		// Each chapter keeps its own record and URL; equal local files share
		// an inode so deleting either record only unlinks that chapter's name.
		shared := make(map[[sha256.Size]byte]*core.Record)
		for _, img := range images {
			if len(img.Blob) == 0 {
				continue
			}
			record := core.NewRecord(collection)
			record.Set("novel", novelID)
			record.Set("chapter", img.ChapterID)
			record.Set("num", img.Num)
			record.Set("alt", clampText(img.Alt, 2000))
			record.Set("mime", clampText(img.MimeType, 64))
			file, fErr := filesystem.NewFileFromBytes(img.Blob, imageFileName(img.Num, img.MimeType, img.FileName))
			if fErr != nil {
				return fErr
			}
			record.Set("file", []*filesystem.File{file})
			if err := txApp.Save(record); err != nil {
				return err
			}
			digest := sha256.Sum256(img.Blob)
			if first := shared[digest]; first != nil {
				if err := s.linkIdenticalImageFiles(first, record); err != nil {
					// Hardlinks are an optimization, not a requirement for
					// imports on remote or unsupported filesystems.
					slog.Warn("could not share imported image storage", "error", err)
				}
			} else {
				shared[digest] = record
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
	FileName  string
	Blob      []byte
}

// GetNovelImageBlobs loads every stored image of a novel with its bytes,
// ordered by chapter ID and token number. Failing to read a registered file
// is treated as corruption and aborts, so exports never silently drop
// images. Identical content shares one backing byte slice; callers must treat
// the returned blobs as immutable.
func (s *Store) GetNovelImageBlobs(novelID string) ([]NovelImageBlob, error) {
	return s.getNovelImageBlobs(novelID, nil)
}

func (s *Store) getNovelImageBlobs(novelID string, writeBudget *imageWriteBudget) ([]NovelImageBlob, error) {
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
	shared := make(map[[sha256.Size]byte][]byte)
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
		// Hash through a bounded copy buffer before allocating a blob. This
		// also handles pre-remediation copies and remote object storage.
		hash := sha256.New()
		var input io.Reader = reader
		if writeBudget != nil {
			input = io.LimitReader(reader, min(epubimport.MaxImageBytes, writeBudget.remaining)+1)
		}
		size, rErr := io.Copy(hash, input)
		reader.Close()
		if rErr != nil {
			return nil, fmt.Errorf("hash file %s: %w", fileKey, rErr)
		}
		if writeBudget != nil {
			if err := writeBudget.add(size); err != nil {
				return nil, err
			}
		}
		var digest [sha256.Size]byte
		copy(digest[:], hash.Sum(nil))
		blob, exists := shared[digest]
		if !exists {
			reader, rErr = fsys.GetReader(fileKey)
			if rErr != nil {
				return nil, fmt.Errorf("get reader for %s: %w", fileKey, rErr)
			}
			blob, rErr = io.ReadAll(io.LimitReader(reader, size+1))
			reader.Close()
			if rErr != nil {
				return nil, fmt.Errorf("read file %s: %w", fileKey, rErr)
			}
			if sha256.Sum256(blob) != digest {
				return nil, fmt.Errorf("image changed during export: %s", fileKey)
			}
			shared[digest] = blob
		}
		out = append(out, NovelImageBlob{
			ChapterID: record.GetString("chapter"),
			Num:       int(record.GetInt("num")),
			Alt:       record.GetString("alt"),
			MimeType:  record.GetString("mime"),
			FileName:  files[0],
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

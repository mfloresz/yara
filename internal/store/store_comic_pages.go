package store

import (
	"fmt"
	"io"
	"mime"
	"path/filepath"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
)

func pageImageExtension(mimeType string) string {
	if exts, err := mime.ExtensionsByType(mimeType); err == nil && len(exts) > 0 {
		return exts[0]
	}
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "image/avif":
		return ".avif"
	default:
		return ".jpg"
	}
}

func comicPageFileName(chapterID string, order int, mimeType string) string {
	ext := pageImageExtension(mimeType)
	if ext == ".jpe" {
		ext = ".jpg"
	}
	return fmt.Sprintf("%s-%04d%s", chapterID, order, ext)
}

// ReplaceComicChapterPages deletes every existing page of the chapter and
// stores uploads in order. It also updates the chapter's page_count and status
// (fresh/failed downloads complete on pages) and the comic's counters.
func (s *Store) ReplaceComicChapterPages(chapterID string, uploads []ComicPageUpload) error {
	chapter, err := s.App.FindRecordById(ComicChaptersCollection, chapterID)
	if err != nil {
		return ErrNotFound
	}
	comicID := chapter.GetString("comic")

	existing, err := s.App.FindRecordsByFilter(ComicPagesCollection, "chapter = {:chapter}", "page_order", 5000, 0, dbx.Params{"chapter": chapterID})
	if err != nil {
		return err
	}
	for _, rec := range existing {
		if err := s.App.Delete(rec); err != nil {
			return err
		}
	}

	collection, err := s.App.FindCollectionByNameOrId(ComicPagesCollection)
	if err != nil {
		return err
	}
	for i, up := range uploads {
		rec := core.NewRecord(collection)
		rec.Set("chapter", chapterID)
		rec.Set("comic", comicID)
		rec.Set("page_order", i+1)
		rec.Set("source_url", up.SourceURL)
		file, err := filesystem.NewFileFromBytes(up.Data, comicPageFileName(chapterID, i+1, up.MimeType))
		if err != nil {
			return err
		}
		rec.Set("image", []*filesystem.File{file})
		if err := s.App.Save(rec); err != nil {
			return err
		}
	}

	chapter.Set("page_count", len(uploads))
	// Fresh/failed downloads complete on pages; anything already carrying a
	// meaningful state keeps it so re-downloads never clobber it.
	if len(uploads) > 0 {
		switch chapter.GetString("status") {
		case "", ComicChapterPending, ComicChapterFailed, ComicChapterProcessing:
			chapter.Set("status", ComicChapterDone)
		}
		chapter.Set("error_message", "")
	} else {
		chapter.Set("status", ComicChapterFailed)
	}
	if err := s.App.Save(chapter); err != nil {
		return err
	}
	return s.RecalculateComicStats(comicID)
}

// ListComicChapterPagesAccessible returns the pages of a chapter in reading
// order. Ownership flows through the comic: the caller must own the comic or
// the comic must be public.
func (s *Store) ListComicChapterPagesAccessible(userID, comicID, chapterID string) ([]ComicPage, error) {
	if _, err := s.GetComicAccessible(userID, comicID); err != nil {
		return nil, err
	}
	chapter, err := s.App.FindRecordById(ComicChaptersCollection, chapterID)
	if err != nil || chapter.GetString("comic") != comicID {
		return nil, ErrNotFound
	}
	records, err := s.App.FindRecordsByFilter(ComicPagesCollection, "chapter = {:chapter}", "page_order", 5000, 0, dbx.Params{"chapter": chapterID})
	if err != nil {
		return nil, err
	}
	out := make([]ComicPage, 0, len(records))
	for _, rec := range records {
		out = append(out, comicPageFromRecord(rec))
	}
	return out, nil
}

func comicPageFromRecord(rec *core.Record) ComicPage {
	file := firstString(rec.GetStringSlice("image"))
	comicID := rec.GetString("comic")
	chapterID := rec.GetString("chapter")
	return ComicPage{
		ID:        rec.Id,
		ChapterID: chapterID,
		ComicID:   comicID,
		PageOrder: asInt(rec.GetFloat("page_order"), 0),
		SourceURL: rec.GetString("source_url"),
		FileName:  file,
		URL:       "/api/v1/comics/" + comicID + "/chapters/" + chapterID + "/pages/" + rec.Id + "/image",
		CreatedAt: rec.GetString("created"),
		UpdatedAt: rec.GetString("updated"),
	}
}

// ReadComicPageBlob returns the raw image bytes + detected extension for a
// single page, after verifying the caller can access the comic.
func (s *Store) ReadComicPageBlob(userID, comicID, chapterID, pageID string) ([]byte, string, error) {
	if _, err := s.GetComicAccessible(userID, comicID); err != nil {
		return nil, "", err
	}
	rec, err := s.App.FindRecordById(ComicPagesCollection, pageID)
	if err != nil || rec.GetString("chapter") != chapterID || rec.GetString("comic") != comicID {
		return nil, "", ErrNotFound
	}
	return s.readComicPageFile(rec)
}

func (s *Store) readComicPageFile(rec *core.Record) ([]byte, string, error) {
	fileName := firstString(rec.GetStringSlice("image"))
	if fileName == "" {
		return nil, "", ErrNotFound
	}
	fsys, err := s.App.NewFilesystem()
	if err != nil {
		return nil, "", err
	}
	defer fsys.Close()
	reader, err := fsys.GetReader(rec.BaseFilesPath() + "/" + fileName)
	if err != nil {
		return nil, "", err
	}
	defer reader.Close()
	blob, err := io.ReadAll(reader)
	if err != nil {
		return nil, "", err
	}
	return blob, strings.ToLower(strings.TrimPrefix(filepath.Ext(fileName), ".")), nil
}

// GetComicChapterPageCount returns how many page images a chapter has.
func (s *Store) GetComicChapterPageCount(chapterID string) (int, error) {
	total, err := s.App.CountRecords(ComicPagesCollection, dbx.HashExp{"chapter": chapterID})
	if err != nil {
		return 0, err
	}
	return int(total), nil
}

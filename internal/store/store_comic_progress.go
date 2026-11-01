package store

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Comic reading progress is a separate collection from the novel tree's
// reading_progress: both are (user, work) rows, but a shared table would need
// two nullable unique keys and per-media branches on every read. A parallel
// collection keeps each tree self-contained (same rationale as D1 in
// docs/ui-shell-and-comics-plan.md).

func (s *Store) GetComicReadingProgress(userID, comicID string) (*ComicReadingProgress, error) {
	record, err := s.App.FindFirstRecordByFilter(ComicReadingProgressCollection,
		"user = {:user} && comic = {:comic}",
		dbx.Params{"user": userID, "comic": comicID},
	)
	if err != nil {
		return nil, ErrNotFound
	}
	progress := comicReadingProgressFromRecord(record)
	return &progress, nil
}

// UpsertComicReadingProgress persists the full desired state of the user's
// per-comic progress slot. A position save (chapterID set) marks the comic as
// actively reading; a status-only update (chapterID empty) leaves the resume
// point untouched.
func (s *Store) UpsertComicReadingProgress(userID, comicID, chapterID string, scrollPercent float64, status string) (*ComicReadingProgress, error) {
	if _, err := s.GetComicAccessible(userID, comicID); err != nil {
		return nil, err
	}
	record, err := s.App.FindFirstRecordByFilter(ComicReadingProgressCollection,
		"user = {:user} && comic = {:comic}",
		dbx.Params{"user": userID, "comic": comicID},
	)
	if err != nil {
		collection, cErr := s.App.FindCollectionByNameOrId(ComicReadingProgressCollection)
		if cErr != nil {
			return nil, cErr
		}
		record = core.NewRecord(collection)
		record.Set("user", userID)
		record.Set("comic", comicID)
	}
	if chapterID != "" {
		record.Set("chapter_id", chapterID)
		record.Set("scroll_percent", scrollPercent)
		if status == "" {
			status = ReadingStatusReading
		}
	}
	if status != "" {
		record.Set("status", status)
	}
	if err := s.App.Save(record); err != nil {
		return nil, err
	}
	progress := comicReadingProgressFromRecord(record)
	return &progress, nil
}

// DeleteComicReadingProgress removes the user's per-comic progress slot
// entirely (resume point plus reading mark). Idempotent: no error when absent.
func (s *Store) DeleteComicReadingProgress(userID, comicID string) error {
	record, err := s.App.FindFirstRecordByFilter(ComicReadingProgressCollection,
		"user = {:user} && comic = {:comic}",
		dbx.Params{"user": userID, "comic": comicID},
	)
	if err != nil {
		return nil
	}
	return s.App.Delete(record)
}

func comicReadingProgressFromRecord(record *core.Record) ComicReadingProgress {
	return ComicReadingProgress{
		ID:            record.Id,
		UserID:        record.GetString("user"),
		ComicID:       record.GetString("comic"),
		ChapterID:     record.GetString("chapter_id"),
		ScrollPercent: record.GetFloat("scroll_percent"),
		Status:        record.GetString("status"),
		CreatedAt:     record.GetString("created"),
		UpdatedAt:     record.GetString("updated"),
	}
}

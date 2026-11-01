package store

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Reading status marks stored in the progress row's status field. Empty means
// implicit reading (rows created before the field existed behave as reading).
const (
	ReadingStatusReading  = "reading"
	ReadingStatusPaused   = "paused"
	ReadingStatusFinished = "finished"
)

func (s *Store) GetReadingProgress(userID, novelID string) (*ReadingProgress, error) {
	record, err := s.App.FindFirstRecordByFilter(
		ReadingProgressCollection,
		"user = {:user} && novel = {:novel}",
		dbx.Params{"user": userID, "novel": novelID},
	)
	if err != nil {
		return nil, err
	}
	rp := readingProgressFromRecord(record)
	return &rp, nil
}

// UpsertReadingProgress persists the full desired state of the user's
// per-novel progress slot. A position save (chapterID set) marks the novel as
// actively reading; a status-only update (chapterID empty) leaves the resume
// point untouched.
func (s *Store) UpsertReadingProgress(userID, novelID, chapterID string, scrollPercent float64, status string) (*ReadingProgress, error) {
	collection, err := s.App.FindCollectionByNameOrId(ReadingProgressCollection)
	if err != nil {
		return nil, err
	}

	record, err := s.App.FindFirstRecordByFilter(
		ReadingProgressCollection,
		"user = {:user} && novel = {:novel}",
		dbx.Params{"user": userID, "novel": novelID},
	)
	if err != nil {
		record = core.NewRecord(collection)
		record.Set("user", userID)
		record.Set("novel", novelID)
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

	rp := readingProgressFromRecord(record)
	return &rp, nil
}

// DeleteReadingProgress removes the user's per-novel progress slot entirely
// (resume point plus reading mark). Idempotent: no error when absent.
func (s *Store) DeleteReadingProgress(userID, novelID string) error {
	record, err := s.App.FindFirstRecordByFilter(
		ReadingProgressCollection,
		"user = {:user} && novel = {:novel}",
		dbx.Params{"user": userID, "novel": novelID},
	)
	if err != nil {
		return nil
	}
	return s.App.Delete(record)
}

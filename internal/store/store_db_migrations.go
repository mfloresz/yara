package store

import (
	"fmt"
	"log/slog"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func (s *Store) RunThumbnailMigration() error {
	collection, err := s.App.FindCollectionByNameOrId(NovelsCollection)
	if err != nil {
		return fmt.Errorf("find novels collection: %w", err)
	}
	if err := s.ensureField(collection, &core.FileField{Name: "thumbnail", MaxSelect: 1}); err != nil {
		return fmt.Errorf("ensure thumbnail field: %w", err)
	}

	records, err := s.App.FindRecordsByFilter(NovelsCollection, "cover != ''", "", 5000, 0)
	if err != nil {
		return fmt.Errorf("list novels with covers: %w", err)
	}

	slog.Info("starting thumbnail migration", "total", len(records))
	for _, record := range records {
		coverFile := firstString(record.GetStringSlice("cover"))
		if coverFile == "" {
			continue
		}
		blob, err := s.readCoverBlob(record, coverFile)
		if err != nil {
			slog.Warn("failed to read cover for thumbnail migration", "novelId", record.Id, "error", err)
			continue
		}
		s.attachCoverThumbnail(record.Id, blob)
	}
	slog.Info("thumbnail migration completed")
	return nil
}

// RunChapterStatsMigration recalculates chapter stats for every novel in the
// database. It is intended to be run once via the --migrate-chapter-stats flag
// to repair stats that were left stale after a cancelled download/translation job.
func (s *Store) RunChapterStatsMigration() error {
	records, err := s.App.FindRecordsByFilter(NovelsCollection, "", "", 5000, 0)
	if err != nil {
		return fmt.Errorf("list novels: %w", err)
	}

	slog.Info("starting chapter stats migration", "total", len(records))
	repaired := 0
	for _, record := range records {
		novelID := record.Id
		if err := s.RecalculateNovelStats(novelID); err != nil {
			slog.Warn("failed to recalculate novel stats", "novelId", novelID, "error", err)
			continue
		}
		repaired++
	}
	slog.Info("chapter stats migration completed", "repaired", repaired)
	return nil
}

// RunChapterPositionsMigration initializes the user-controlled chapter
// position for every existing record. It must be run once via the
// --migrate-chapter-positions flag before the reorder/exclusion feature is
// used with pre-existing data.
//
// Positions are assigned per novel in ascending chapter_order (tie-broken by
// creation time), starting after the current maximum position. Chapters that
// already have a position are left untouched, so re-running the migration is
// a no-op and never clobbers a user-initiated reorder. It also creates the
// unique (novel, position) index, which is safe only after the backfill
// guarantees unique positions.
func (s *Store) RunChapterPositionsMigration() error {
	collection, err := s.App.FindCollectionByNameOrId(ChaptersCollection)
	if err != nil {
		return fmt.Errorf("find chapters collection: %w", err)
	}
	if err := s.ensureField(collection, &core.NumberField{Name: "position"}); err != nil {
		return fmt.Errorf("ensure position field: %w", err)
	}
	if err := s.ensureField(collection, &core.BoolField{Name: "excluded"}); err != nil {
		return fmt.Errorf("ensure excluded field: %w", err)
	}

	novels, err := s.App.FindRecordsByFilter(NovelsCollection, "", "created", 5000, 0)
	if err != nil {
		return fmt.Errorf("list novels: %w", err)
	}

	slog.Info("starting chapter positions migration", "novels", len(novels))
	positioned := 0
	for _, novel := range novels {
		chapters, err := s.App.FindRecordsByFilter(ChaptersCollection, "novel = {:novel}", "chapter_order,created", 100000, 0, dbx.Params{"novel": novel.Id})
		if err != nil {
			slog.Warn("failed to list chapters for position migration", "novelId", novel.Id, "error", err)
			continue
		}
		maxPos := 0
		for _, chapter := range chapters {
			pos := asInt(chapter.GetFloat("position"), 0)
			if pos > maxPos {
				maxPos = pos
			}
		}
		for _, chapter := range chapters {
			if asInt(chapter.GetFloat("position"), 0) != 0 {
				continue
			}
			maxPos++
			chapter.Set("position", maxPos)
			if err := s.App.Save(chapter); err != nil {
				slog.Warn("failed to assign chapter position", "chapterId", chapter.Id, "novelId", novel.Id, "error", err)
				continue
			}
			positioned++
		}
	}

	// Create the unique index after the backfill. Idempotent: if the index
	// already exists the collection save is a no-op for it.
	fresh, err := s.App.FindCollectionByNameOrId(ChaptersCollection)
	if err != nil {
		return fmt.Errorf("reload chapters collection: %w", err)
	}
	if fresh.Fields.GetByName("position") != nil && fresh.GetIndex("idx_chapters_novel_position_unique") == "" {
		fresh.AddIndex("idx_chapters_novel_position_unique", true, "novel,position", "")
		if err := s.App.Save(fresh); err != nil {
			return fmt.Errorf("create unique chapter position index: %w", err)
		}
	}

	slog.Info("chapter positions migration completed", "positioned", positioned)
	return nil
}

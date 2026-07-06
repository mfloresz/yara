package store

import (
	"fmt"
	"log/slog"

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

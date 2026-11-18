package store

import (
	"fmt"

	"translator-server/internal/epubimport"
)

// Count every occurrence, not only unique blobs: uploads must remain bounded
// even when hardlinks are unavailable (including S3). Check before creating
// any records so a rejected import/copy leaves no partial novel.
type imageWriteBudget struct {
	remaining int64
}

func newImageWriteBudget() *imageWriteBudget {
	return &imageWriteBudget{remaining: epubimport.MaxImagesBytes}
}

func (b *imageWriteBudget) add(size int64) error {
	if size < 0 || size > epubimport.MaxImageBytes {
		return fmt.Errorf("inline image exceeds the %d byte per-image storage limit", epubimport.MaxImageBytes)
	}
	if size > b.remaining {
		return fmt.Errorf("inline image storage exceeds %d bytes per operation (all chapter references count, including repeated images)", epubimport.MaxImagesBytes)
	}
	b.remaining -= size
	return nil
}

func validateEpubImageWrites(chapters []ImportedEpubChapter) error {
	budget := newImageWriteBudget()
	for _, ch := range chapters {
		for _, img := range ch.Images {
			if err := budget.add(int64(len(img.Blob))); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateZipImageWrites(chapters []ImportedZipChapter) error {
	budget := newImageWriteBudget()
	for _, ch := range chapters {
		for _, img := range ch.Images {
			if err := budget.add(int64(len(img.Blob))); err != nil {
				return err
			}
		}
	}
	return nil
}

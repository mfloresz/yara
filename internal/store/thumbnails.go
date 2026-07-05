package store

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	stddraw "image/draw"
	"io"
	"log/slog"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"golang.org/x/image/draw"
)

const (
	thumbnailMaxHeight = 350
	thumbnailQuality   = 65
)

func generateThumbnailBlob(r io.Reader) ([]byte, error) {
	src, _, err := image.Decode(r)
	if err != nil {
		return nil, fmt.Errorf("decode image for thumbnail: %w", err)
	}

	bounds := src.Bounds()
	srcHeight := bounds.Dy()
	srcWidth := bounds.Dx()

	var newWidth, newHeight int
	if srcHeight > thumbnailMaxHeight {
		newWidth = int(float64(srcWidth) * float64(thumbnailMaxHeight) / float64(srcHeight))
		newHeight = thumbnailMaxHeight
	} else {
		newWidth = srcWidth
		newHeight = srcHeight
	}

	rgba := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))
	stddraw.Draw(rgba, rgba.Bounds(), &image.Uniform{C: color.White}, image.Point{}, stddraw.Src)

	// Escalamos sobre un fondo opaco para evitar que la transparencia del
	// original o de algunos decoders termine en thumbnails corruptos.
	draw.CatmullRom.Scale(rgba, rgba.Bounds(), src, src.Bounds(), draw.Over, nil)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, rgba, &jpeg.Options{Quality: thumbnailQuality}); err != nil {
		return nil, fmt.Errorf("encode jpeg thumbnail: %w", err)
	}
	return buf.Bytes(), nil
}

func thumbnailFileNameForBlob(thumbBlob []byte) string {
	sum := sha256.Sum256(thumbBlob)
	return fmt.Sprintf("thumb-%x.jpg", sum[:8])
}

func (s *Store) attachCoverThumbnail(recordID string, coverData []byte) {
	thumbBlob, err := generateThumbnailBlob(bytes.NewReader(coverData))
	if err != nil {
		slog.Warn("failed to generate cover thumbnail", "novelId", recordID, "error", err)
		return
	}
	collection, err := s.App.FindCollectionByNameOrId(NovelsCollection)
	if err != nil {
		slog.Warn("failed to find novels collection for thumbnail", "novelId", recordID, "error", err)
		return
	}
	record, err := s.App.FindRecordById(collection, recordID)
	if err != nil {
		slog.Warn("failed to find novel record for thumbnail", "novelId", recordID, "error", err)
		return
	}
	thumbFile, err := filesystem.NewFileFromBytes(thumbBlob, thumbnailFileNameForBlob(thumbBlob))
	if err != nil {
		slog.Warn("failed to create thumbnail file", "novelId", recordID, "error", err)
		return
	}
	record.Set("thumbnail", []*filesystem.File{thumbFile})
	if err := s.App.Save(record); err != nil {
		slog.Warn("failed to save thumbnail", "novelId", recordID, "error", err)
	}
}

func (s *Store) readCoverBlob(record *core.Record, coverFile string) ([]byte, error) {
	fsys, err := s.App.NewFilesystem()
	if err != nil {
		return nil, fmt.Errorf("new filesystem: %w", err)
	}
	defer fsys.Close()

	fileKey := record.BaseFilesPath() + "/" + coverFile
	reader, err := fsys.GetReader(fileKey)
	if err != nil {
		return nil, fmt.Errorf("get reader for %s: %w", fileKey, err)
	}
	defer reader.Close()

	blob, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read cover blob: %w", err)
	}
	return blob, nil
}

func (s *Store) regenerateThumbnailForNovel(recordID string) error {
	record, err := s.App.FindRecordById(NovelsCollection, recordID)
	if err != nil {
		return fmt.Errorf("find novel %s: %w", recordID, err)
	}

	coverFile := firstString(record.GetStringSlice("cover"))
	if coverFile == "" {
		return nil
	}

	fsys, err := s.App.NewFilesystem()
	if err != nil {
		return fmt.Errorf("new filesystem: %w", err)
	}
	defer fsys.Close()

	fileKey := record.BaseFilesPath() + "/" + coverFile
	reader, err := fsys.GetReader(fileKey)
	if err != nil {
		return fmt.Errorf("get reader for %s: %w", fileKey, err)
	}
	defer reader.Close()

	blob, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("read cover blob: %w", err)
	}
	s.attachCoverThumbnail(recordID, blob)
	return nil
}

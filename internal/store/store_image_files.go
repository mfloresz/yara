package store

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/pocketbase/pocketbase/core"
)

// linkIdenticalImageFiles leaves both PocketBase paths intact. Files are
// immutable after upload; PocketBase updates replace names rather than write
// in place. S3 and filesystems without hardlinks retain independent copies.
func (s *Store) linkIdenticalImageFiles(first, duplicate *core.Record) error {
	if s.App.Settings().S3.Enabled {
		return nil
	}
	filePath := func(record *core.Record) (string, error) {
		files := record.GetStringSlice("file")
		if len(files) != 1 || filepath.Base(files[0]) != files[0] {
			return "", fmt.Errorf("invalid image file for %s", record.Id)
		}
		return filepath.Join(s.App.DataDir(), "storage", record.BaseFilesPath(), files[0]), nil
	}
	source, err := filePath(first)
	if err != nil {
		return err
	}
	target, err := filePath(duplicate)
	if err != nil {
		return err
	}
	sourceInfo, err := os.Lstat(source)
	if err != nil {
		return err
	}
	targetInfo, err := os.Lstat(target)
	if err != nil {
		return err
	}
	if !sourceInfo.Mode().IsRegular() || !targetInfo.Mode().IsRegular() {
		return fmt.Errorf("image deduplication requires regular files")
	}
	if os.SameFile(sourceInfo, targetInfo) {
		return nil
	}
	if sourceInfo.Size() != targetInfo.Size() || sourceInfo.Mode() != targetInfo.Mode() {
		return fmt.Errorf("image deduplication requires identical size and permissions")
	}
	// Check the persisted bytes too, rather than relying solely on the input
	// digest: file hooks/storage failures must never replace different data.
	digest := func(name string) ([]byte, error) {
		file, err := os.Open(name)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		h := sha256.New()
		if _, err := io.Copy(h, file); err != nil {
			return nil, err
		}
		return h.Sum(nil), nil
	}
	sourceDigest, err := digest(source)
	if err != nil {
		return err
	}
	targetDigest, err := digest(target)
	if err != nil {
		return err
	}
	if !bytes.Equal(sourceDigest, targetDigest) {
		return fmt.Errorf("image deduplication requires identical contents")
	}
	return replaceImageWithHardlink(source, target, os.Link)
}

func replaceImageWithHardlink(source, target string, link func(string, string) error) error {
	placeholder, err := os.CreateTemp(filepath.Dir(target), ".image-link-*")
	if err != nil {
		return err
	}
	temporary := placeholder.Name()
	placeholder.Close()
	defer os.Remove(temporary)
	if err := os.Remove(temporary); err != nil {
		return err
	}
	if err := link(source, temporary); err != nil {
		return err
	}
	return os.Rename(temporary, target)
}

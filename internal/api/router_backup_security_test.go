package api

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestBackupSkipsSymlinks(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "sentinel")
	if err := os.WriteFile(outside, []byte("outside backup root"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "linked")); err != nil {
		t.Skip(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "regular"), []byte("expected"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	count, err := writeBackupZip(&out, dir)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 || len(zr.File) != 1 || zr.File[0].Name != "regular" {
		t.Fatalf("backup included non-regular entry: count=%d files=%v", count, zr.File)
	}
}

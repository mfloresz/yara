package epubimport

import (
	"archive/zip"
	"fmt"
	"io"
	"path"
)

// MaxDecompressedBytes caps the total decompressed content extracted from a
// single archive so a zip bomb (a few MB compressed expanding to GBs) cannot
// exhaust server memory during an import. 25MB sits comfortably above any
// real novel EPUB.
const MaxDecompressedBytes int64 = 25 << 20

// zipBudget reads zip entries while tracking the total decompressed bytes
// consumed so far, so the cap holds across every entry Parse touches.
type zipBudget struct {
	zr        *zip.Reader
	remaining int64
}

func newZipBudget(zr *zip.Reader) *zipBudget {
	return &zipBudget{zr: zr, remaining: MaxDecompressedBytes}
}

func (b *zipBudget) readFile(name string) ([]byte, error) {
	for _, f := range b.zr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		data, err := io.ReadAll(io.LimitReader(rc, b.remaining+1))
		if err != nil {
			return nil, err
		}
		if int64(len(data)) > b.remaining {
			return nil, fmt.Errorf("epub decompressed size exceeds %d bytes", MaxDecompressedBytes)
		}
		b.remaining -= int64(len(data))
		return data, nil
	}
	return nil, fmt.Errorf("file not found: %s", name)
}

func resolveZipPath(opfPath, href string) string {
	base := path.Dir(opfPath)
	if base == "." || base == "" {
		return path.Clean(href)
	}
	return path.Clean(path.Join(base, href))
}

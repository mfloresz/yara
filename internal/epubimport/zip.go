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

// Image reads run under their own budget: text and image bytes are capped
// independently so an illustration-heavy EPUB can still import while the
// zip-bomb guard on chapter HTML stays tight. MaxImagesBytes also sits under
// the 64MB multipart limit of the import endpoint.
const (
	MaxImagesBytes int64 = 64 << 20
	MaxImageBytes  int64 = 20 << 20
)

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

// imageBudget tracks decompressed image bytes separately from the text
// budget so illustration-heavy EPUBs are bounded without tightening the
// chapter-HTML guard.
type imageBudget struct {
	zr        *zip.Reader
	remaining int64
}

func newImageBudget(zr *zip.Reader) *imageBudget {
	return &imageBudget{zr: zr, remaining: MaxImagesBytes}
}

func (b *imageBudget) read(name string) ([]byte, error) {
	for _, f := range b.zr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		data, err := io.ReadAll(io.LimitReader(rc, MaxImageBytes+1))
		if err != nil {
			return nil, err
		}
		if int64(len(data)) > MaxImageBytes {
			return nil, fmt.Errorf("image %s exceeds the %d byte per-image limit", name, MaxImageBytes)
		}
		if int64(len(data)) > b.remaining {
			return nil, fmt.Errorf("epub images exceed %d bytes in total", MaxImagesBytes)
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

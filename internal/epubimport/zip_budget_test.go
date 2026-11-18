package epubimport

import (
	"archive/zip"
	"bytes"
	"errors"
	"strings"
	"testing"
)

type budgetCountingReader struct {
	*bytes.Reader
	reads int
}

func (r *budgetCountingReader) ReadAt(p []byte, off int64) (int, error) {
	r.reads++
	return r.Reader.ReadAt(p, off)
}

func budgetArchive(t *testing.T, files map[string]string) (*zip.Reader, *budgetCountingReader) {
	t.Helper()
	data := buildEPUB(t, files)
	r := &budgetCountingReader{Reader: bytes.NewReader(data)}
	zr, err := zip.NewReader(r, int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	return zr, r
}

type budgetTestReader struct {
	read      func(string) ([]byte, error)
	remaining *int64
}

var budgetTestFactories = []struct {
	name string
	new  func(*zip.Reader, int64) budgetTestReader
}{
	{"text", func(zr *zip.Reader, remaining int64) budgetTestReader {
		b := &zipBudget{zr: zr, remaining: remaining}
		return budgetTestReader{b.readFile, &b.remaining}
	}},
	{"images", func(zr *zip.Reader, remaining int64) budgetTestReader {
		b := &imageBudget{zr: zr, remaining: remaining}
		return budgetTestReader{b.read, &b.remaining}
	}},
}

func TestZipBudgetsRejectedReadsExhaustBudget(t *testing.T) {
	for _, factory := range budgetTestFactories {
		t.Run(factory.name, func(t *testing.T) {
			zr, source := budgetArchive(t, map[string]string{
				"oversized": strings.Repeat("x", 33),
				"small":     "ok",
			})
			b := factory.new(zr, 32)
			if data, err := b.read("oversized"); err == nil || data != nil {
				t.Fatalf("oversized read = %q, %v; want nil data and an error", data, err)
			}
			if *b.remaining != 0 {
				t.Fatalf("remaining = %d after rejection, want 0", *b.remaining)
			}
			reads := source.reads
			for _, name := range []string{"oversized", "oversized", "small"} {
				if data, err := b.read(name); err == nil || data != nil {
					t.Fatalf("exhausted read %q = %q, %v", name, data, err)
				}
				if source.reads != reads {
					t.Fatalf("exhausted read %q accessed the archive", name)
				}
			}
		})
	}
}

func TestZipBudgetsRemainingAcrossEntries(t *testing.T) {
	for _, factory := range budgetTestFactories {
		t.Run(factory.name, func(t *testing.T) {
			zr, _ := budgetArchive(t, map[string]string{"first": "12345", "second": "6789"})
			b := factory.new(zr, 8)
			if data, err := b.read("first"); err != nil || string(data) != "12345" {
				t.Fatalf("first read = %q, %v", data, err)
			}
			if *b.remaining != 3 {
				t.Fatalf("remaining = %d, want 3", *b.remaining)
			}
			if data, err := b.read("second"); err == nil || data != nil {
				t.Fatalf("second read = %q, %v; want rejection", data, err)
			}
			if *b.remaining != 0 {
				t.Fatalf("remaining = %d after rejection, want 0", *b.remaining)
			}
		})
	}
}

func TestZipBudgetsExactLimitExhaustsBudget(t *testing.T) {
	for _, factory := range budgetTestFactories {
		t.Run(factory.name, func(t *testing.T) {
			zr, source := budgetArchive(t, map[string]string{"exact": "1234", "empty": ""})
			b := factory.new(zr, 4)
			if data, err := b.read("exact"); err != nil || string(data) != "1234" {
				t.Fatalf("exact read = %q, %v", data, err)
			}
			reads := source.reads
			if _, err := b.read("empty"); err == nil {
				t.Fatal("expected an error after exactly consuming the budget")
			}
			if source.reads != reads {
				t.Fatal("exhausted budget accessed another entry")
			}
		})
	}
}

func TestZipBudgetsAccountForReadErrors(t *testing.T) {
	for _, factory := range budgetTestFactories {
		t.Run(factory.name, func(t *testing.T) {
			zr, _ := budgetArchive(t, map[string]string{"bad": "partial", "good": "ok"})
			for _, f := range zr.File {
				if f.Name == "bad" {
					// A checksum mismatch makes ReadAll return both bytes and an error.
					f.CRC32 ^= 1
				}
			}
			b := factory.new(zr, 10)
			if data, err := b.read("bad"); !errors.Is(err, zip.ErrChecksum) || data != nil {
				t.Fatalf("corrupt read = %q, %v; want checksum error", data, err)
			}
			if *b.remaining != 3 {
				t.Fatalf("remaining = %d after 7 bytes plus error, want 3", *b.remaining)
			}
			if data, err := b.read("good"); err != nil || string(data) != "ok" {
				t.Fatalf("read after partial failure = %q, %v", data, err)
			}
			if *b.remaining != 1 {
				t.Fatalf("remaining = %d, want 1", *b.remaining)
			}
		})
	}
}

func TestImageBudgetAccountsForPerImageRejection(t *testing.T) {
	zr, _ := budgetArchive(t, map[string]string{"oversized": strings.Repeat("x", int(MaxImageBytes)+1), "small": "ok"})
	b := newImageBudget(zr)
	if data, err := b.read("oversized"); err == nil || !strings.Contains(err.Error(), "per-image limit") || data != nil {
		t.Fatalf("oversized image read returned %d bytes, %v", len(data), err)
	}
	want := MaxImagesBytes - MaxImageBytes - 1
	if b.remaining != want {
		t.Fatalf("remaining = %d after per-image rejection, want %d", b.remaining, want)
	}
	if data, err := b.read("small"); err != nil || string(data) != "ok" {
		t.Fatalf("small image after per-image rejection = %q, %v", data, err)
	}
}

package api

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// toGBK encodes a UTF-8 string the way a GBK-serving site would send it.
func toGBK(t *testing.T, s string) []byte {
	t.Helper()
	out, err := io.ReadAll(transform.NewReader(strings.NewReader(s), simplifiedchinese.GBK.NewEncoder()))
	if err != nil {
		t.Fatalf("encode to GBK: %v", err)
	}
	return out
}

const chineseSample = "第一章 风起"

// A GBK page whose only charset signal is the <meta> declaration must decode,
// otherwise the parser sees U+FFFD for every CJK character.
func TestDecodeChineseCharsetFromMetaTag(t *testing.T) {
	raw := toGBK(t, `<html><head><meta charset="gbk"></head><body>`+chineseSample+`</body></html>`)
	if utf8.Valid(raw) {
		t.Fatal("test fixture is not actually GBK-encoded")
	}
	decoded := string(decodeChineseCharset(raw, ""))
	if !strings.Contains(decoded, chineseSample) {
		t.Errorf("meta-declared GBK was not decoded, got %q", decoded)
	}
}

// Some CDNs send the charset in the header and omit or mislabel the meta tag,
// so the header has to be honored on its own.
func TestDecodeChineseCharsetFromContentType(t *testing.T) {
	raw := toGBK(t, `<html><body>`+chineseSample+`</body></html>`)
	decoded := string(decodeChineseCharset(raw, "text/html; charset=GB2312"))
	if !strings.Contains(decoded, chineseSample) {
		t.Errorf("header-declared GB2312 was not decoded, got %q", decoded)
	}
}

// The browser worker decodes to UTF-8 before returning, while the relayed HTML
// may still declare charset=gbk. Re-decoding already-valid UTF-8 would corrupt
// it, so valid input must pass through untouched.
func TestDecodeChineseCharsetLeavesUTF8Alone(t *testing.T) {
	body := `<html><head><meta charset="gbk"></head><body>` + chineseSample + `</body></html>`
	decoded := decodeChineseCharset([]byte(body), "text/html; charset=gbk")
	if string(decoded) != body {
		t.Errorf("valid UTF-8 was re-decoded and corrupted:\n got %q\nwant %q", string(decoded), body)
	}
}

func TestDecodeChineseCharsetIgnoresUnknownCharset(t *testing.T) {
	// Latin-1 bytes with a charset we do not translate: returned unchanged
	// rather than risk mangling them.
	raw := []byte("<html><head><meta charset=\"windows-1252\"></head><body>caf\xe9</body></html>")
	if got := decodeChineseCharset(raw, ""); !bytes.Equal(got, raw) {
		t.Errorf("unknown charset should pass through unchanged, got %q", string(got))
	}
	// Undecodable bytes must not be replaced by something worse.
	broken := []byte{0xFF, 0xFE, 0x00, 0x01}
	if got := decodeChineseCharset(broken, ""); !bytes.Equal(got, broken) {
		t.Errorf("undecodable input should be returned unchanged, got %v", got)
	}
}

func TestApplySiteHeadersReferer(t *testing.T) {
	cases := []struct {
		url  string
		want string
	}{
		{"https://api.skynovels.net/api/novels/1/base", "https://www.skynovels.net/"},
		{"https://www.skynovels.net/novelas/1", "https://www.skynovels.net/"},
		{"https://skynovels.net/novelas/1", "https://www.skynovels.net/"},
		{"https://novelfire.net/book/1", ""},
		{"https://www.example.com/", ""},
	}
	for _, tc := range cases {
		req, err := http.NewRequest(http.MethodGet, tc.url, nil)
		if err != nil {
			t.Fatalf("build request for %s: %v", tc.url, err)
		}
		applySiteHeaders(req)
		if got := req.Header.Get("Referer"); got != tc.want {
			t.Errorf("%s: Referer = %q, want %q", tc.url, got, tc.want)
		}
	}
}

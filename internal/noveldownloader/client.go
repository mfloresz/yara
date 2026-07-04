package noveldownloader

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

type HTTPClient interface {
	Fetch(ctx context.Context, url string) ([]byte, error)
	FetchDocument(ctx context.Context, url string) (*goquery.Document, error)
	Do(req *http.Request) (*http.Response, error)
}

type httpClient struct {
	client *http.Client
}

func NewHTTPClient() HTTPClient {
	return &httpClient{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// NewHTTPClientWithTransport returns an HTTPClient backed by an http.Client
// using the given transport. Intended primarily for tests that need to
// rewrite hosts (e.g. map novelbin.com to a local httptest server).
func NewHTTPClientWithTransport(transport http.RoundTripper) HTTPClient {
	return &httpClient{
		client: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
	}
}

func (c *httpClient) Fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	// Auto-detect and decode Chinese encodings (GBK, GB2312, GB18030)
	body = decodeChineseCharset(body)

	return body, nil
}

// decodeChineseCharset detects GBK/GB2312/GB18030 encoding from HTML <meta>
// charset declarations and decodes to UTF-8. If no Chinese charset is found
// it returns the input unchanged.
func decodeChineseCharset(raw []byte) []byte {
	// If content is already valid UTF-8, skip charset detection entirely.
	// This handles content from browser proxy which decodes GBK to UTF-8
	// before returning it, but the HTML may still contain <meta charset="gbk">.
	if utf8.Valid(raw) {
		return raw
	}

	// Peek at the first 4096 bytes for <meta charset="..."> or <meta ... charset="...">
	peekLen := 4096
	if len(raw) < peekLen {
		peekLen = len(raw)
	}
	peek := string(raw[:peekLen])

	isGBK := false

	// Check <meta charset="gbk">, <meta charset="gb2312">, <meta charset="gb18030">
	lower := strings.ToLower(peek)
	for _, cs := range []string{"gbk", "gb2312", "gb18030"} {
		if strings.Contains(lower, `charset="`+cs+`"`) ||
			strings.Contains(lower, `content="text/html; charset=`+cs+`"`) ||
			strings.Contains(lower, `charset=`+cs) {
			isGBK = true
			break
		}
	}
	if !isGBK {
		return raw
	}

	// Try GBK first, fall back to GB18030
	for _, decoder := range []transform.Transformer{
		simplifiedchinese.GBK.NewDecoder(),
		simplifiedchinese.GB18030.NewDecoder(),
	} {
		decoded, err := io.ReadAll(transform.NewReader(bytes.NewReader(raw), decoder))
		if err == nil && isLikelyUTF8(decoded) {
			return decoded
		}
	}

	// If decoding failed, return original
	return raw
}

// isLikelyUTF8 does a quick check to see if the bytes look like valid UTF-8.
func isLikelyUTF8(b []byte) bool {
	// Check a sample: if we see common UTF-8 continuation bytes patterns it's likely OK
	// A simple heuristic: if more than 5% of non-ASCII bytes are valid UTF-8 sequences
	nonASCII := 0
	valid := 0
	i := 0
	sample := len(b)
	if sample > 5000 {
		sample = 5000
	}
	for i < sample {
		if b[i] < 0x80 {
			i++
			continue
		}
		nonASCII++
		// Check valid UTF-8 multi-byte sequences
		if b[i] >= 0xC0 && b[i] <= 0xDF && i+1 < sample && b[i+1]&0xC0 == 0x80 {
			valid++
			i += 2
		} else if b[i] >= 0xE0 && b[i] <= 0xEF && i+2 < sample && b[i+1]&0xC0 == 0x80 && b[i+2]&0xC0 == 0x80 {
			valid++
			i += 3
		} else if b[i] >= 0xF0 && b[i] <= 0xF4 && i+3 < sample && b[i+1]&0xC0 == 0x80 && b[i+2]&0xC0 == 0x80 && b[i+3]&0xC0 == 0x80 {
			valid++
			i += 4
		} else {
			i++
		}
	}
	if nonASCII == 0 {
		return true
	}
	return float64(valid)/float64(nonASCII) > 0.8
}

// DecodeHTMLBody decodes raw HTML bytes using charset detection and returns
// the decoded bytes. Useful for parsers that need to handle non-UTF-8 content
// when using a custom FetchDocument path.
func DecodeHTMLBody(raw []byte) []byte {
	return decodeChineseCharset(raw)
}

func (c *httpClient) FetchDocument(ctx context.Context, url string) (*goquery.Document, error) {
	body, err := c.Fetch(ctx, url)
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("parsing HTML: %w", err)
	}
	return doc, nil
}

func (c *httpClient) Do(req *http.Request) (*http.Response, error) {
	return c.client.Do(req)
}

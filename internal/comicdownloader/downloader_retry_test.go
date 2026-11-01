package comicdownloader

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// errReader fails every Read with a mid-body connection reset, mimicking an
// image CDN that drops the connection after sending the response headers.
type errReader struct{}

func (errReader) Read([]byte) (int, error) {
	return 0, fmt.Errorf("read tcp 192.168.68.103:35460->45.129.229.2:443: read: connection reset by peer")
}

func (errReader) Close() error { return nil }

// TestFetchImageRetriesTransientBodyReset ensures one mid-body connection
// reset does not fail the download: the next attempt succeeds.
func TestFetchImageRetriesTransientBodyReset(t *testing.T) {
	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	var calls atomic.Int32
	client := NewDownloaderWithClient(&httpClient{
		client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if calls.Add(1) == 1 {
				return &http.Response{
					StatusCode: 200,
					Header:     http.Header{"Content-Type": []string{"image/png"}},
					Body:       errReader{},
					Request:    req,
				}, nil
			}
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{"Content-Type": []string{"image/png"}},
				Body:       io.NopCloser(strings.NewReader(string(png))),
				Request:    req,
			}, nil
		})},
	})
	blob, _, err := client.DownloadImage(context.Background(), "https://img.example.com/p1.png", "https://site.example.com/ch/1")
	if err != nil {
		t.Fatalf("DownloadImage after transient reset: %v", err)
	}
	if string(blob) != string(png) {
		t.Fatalf("expected retried bytes, got %d bytes", len(blob))
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("expected 2 attempts, got %d", got)
	}
}

// TestFetchImageDoesNotRetryPermanentFailures ensures a 404 fails fast with
// a single attempt: replaying it can never heal.
func TestFetchImageDoesNotRetryPermanentFailures(t *testing.T) {
	var calls atomic.Int32
	client := NewDownloaderWithClient(&httpClient{
		client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls.Add(1)
			return &http.Response{
				StatusCode: 404,
				Header:     http.Header{"Content-Type": []string{"text/html"}},
				Body:       io.NopCloser(strings.NewReader("not found")),
				Request:    req,
			}, nil
		})},
	})
	if _, _, err := client.DownloadImage(context.Background(), "https://img.example.com/missing.png", ""); err == nil {
		t.Fatal("expected 404 error, got nil")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected 1 attempt for 404, got %d", got)
	}
}

// TestIsRetryableImageError locks the retry taxonomy: resets/timeouts/5xx
// retry, while 403 (referer rotation) and 404 do not.
func TestIsRetryableImageError(t *testing.T) {
	retryable := []string{
		"reading image body: read tcp 1.2.3.4:443: read: connection reset by peer",
		"fetching image: unexpected EOF",
		"HTTP 500 fetching image",
		"HTTP 503 fetching image",
		"HTTP 429 fetching image",
		"fetching image: timeout",
	}
	for _, msg := range retryable {
		if !isRetryableImageError(fmt.Errorf("%s", msg)) {
			t.Errorf("expected retryable: %q", msg)
		}
	}
	permanent := []string{
		"HTTP 403 fetching image",
		"HTTP 404 fetching image",
		`downloaded payload is not a valid image (content-type "text/html", 100 bytes)`,
		"image exceeds 26214400 bytes",
	}
	for _, msg := range permanent {
		if isRetryableImageError(fmt.Errorf("%s", msg)) {
			t.Errorf("expected permanent: %q", msg)
		}
	}
}

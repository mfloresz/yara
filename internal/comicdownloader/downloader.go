package comicdownloader

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

// Downloader owns binary image downloads (covers, chapter pages) and the
// inter-chapter throttle. Site logic (series metadata, chapter catalogs, page
// URL extraction) lives exclusively in the JavaScript parser scripts run by
// the comicparserhost engine (see parsers/); this package must not gain
// site-specific scraping code again.
type Downloader struct {
	client HTTPClient
	// MinChapterDelay is the lower bound (inclusive) of the random
	// sleep applied between two consecutive chapter fetches.
	MinChapterDelay time.Duration
	// MaxChapterDelay is the upper bound (inclusive) of the random
	// sleep applied between two consecutive chapter fetches.
	MaxChapterDelay time.Duration
}

const (
	// DefaultMinChapterDelay is the default minimum wait between two
	// consecutive chapter fetches.
	DefaultMinChapterDelay = 5 * time.Second
	// DefaultMaxChapterDelay is the default maximum wait between two
	// consecutive chapter fetches.
	DefaultMaxChapterDelay = 10 * time.Second
)

// imageFetchMaxAttempts bounds the per-(image, referer) retry loop in
// fetchImageWithReferer: one initial attempt plus three retries. Image CDNs
// routinely reset connections mid-body under load (read: connection reset by
// peer), and a single transient reset must not fail the whole chapter.
const imageFetchMaxAttempts = 4

// imageFetchRetryBaseDelay scales the backoff between image retries:
// attempt n waits n * baseDelay before retrying.
const imageFetchRetryBaseDelay = time.Second

func NewDownloader() *Downloader {
	return &Downloader{
		client:          NewHTTPClient(),
		MinChapterDelay: DefaultMinChapterDelay,
		MaxChapterDelay: DefaultMaxChapterDelay,
	}
}

// NewDownloaderWithClient returns a Downloader that uses the provided
// HTTPClient. Primarily intended for tests; the inter-chapter delay is
// disabled so test runs stay fast.
func NewDownloaderWithClient(client HTTPClient) *Downloader {
	return &Downloader{
		client: client,
	}
}

// SleepBetweenChapters waits a random duration within
// [MinChapterDelay, MaxChapterDelay] before fetching the next chapter.
func (d *Downloader) SleepBetweenChapters(ctx context.Context) error {
	min := d.MinChapterDelay
	max := d.MaxChapterDelay
	if min <= 0 && max <= 0 {
		return nil
	}
	if max < min {
		max = min
	}
	var delay time.Duration
	if max == min {
		delay = max
	} else {
		delay = min + time.Duration(rand.Int63n(int64(max-min)))
	}
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// DownloadCover fetches a remote image and returns its bytes together with
// the content type reported by the server. pageURL is the series/chapter
// page the cover was found on and seeds the Referer header (hotlink
// protection); pass "" when unknown.
func (d *Downloader) DownloadCover(ctx context.Context, coverURL, pageURL string) ([]byte, string, error) {
	return d.DownloadImage(ctx, coverURL, pageURL)
}

// DownloadImage fetches a remote image (cover or comic page) and returns its
// raw bytes plus content type. Callers persist bytes to disk (PocketBase file
// field or data-dir); images are never stored as base64 in the database.
// refererHint (usually the chapter/series page URL) seeds the Referer header:
// hotlink-protected hosts only serve images to the site's own origin, so the
// hint origin is tried first and the image origin second.
func (d *Downloader) DownloadImage(ctx context.Context, imageURL, refererHint string) ([]byte, string, error) {
	if strings.TrimSpace(imageURL) == "" {
		return nil, "", fmt.Errorf("empty image url")
	}
	// Page URL fragments carry transport-level directives (see
	// splitImageDirectives): #tira-enc=... decryption key material and
	// #tira-referer=... Referer overrides. Fragments are never sent to the
	// server; the payload is decrypted after download when encrypted.
	fetchURL, dir, err := splitImageDirectives(strings.TrimSpace(imageURL))
	if err != nil {
		return nil, "", err
	}
	enc := dir.enc
	candidates := []string{refererOrigin(refererHint), refererOrigin(imageURL)}
	// Preserve order, drop empties and duplicates.
	seen := map[string]bool{}
	referers := make([]string, 0, 2)
	if dir.hasReferer {
		// Explicit parser directive: use exactly this referer ("" sends
		// no Referer header) instead of the hint/image origins.
		referers = append(referers, dir.referer)
	} else {
		for _, r := range candidates {
			if r == "" || seen[r] {
				continue
			}
			seen[r] = true
			referers = append(referers, r)
		}
	}
	if len(referers) == 0 {
		referers = []string{""}
	}
	var lastErr error
	for _, referer := range referers {
		blob, mimeType, err := d.fetchImageWithReferer(ctx, fetchURL, referer, enc != nil)
		if err == nil {
			if enc != nil {
				blob, err = decryptAESCTR(blob, enc.key, enc.nonce)
				if err != nil {
					return nil, "", fmt.Errorf("decrypting image: %w", err)
				}
				// Encrypted payloads are served as opaque bytes (e.g.
				// application/octet-stream), so sniff the real type from
				// the decrypted magic bytes when possible.
				if sniffed := sniffImageMIME(blob); sniffed != "" {
					mimeType = sniffed
				}
			}
			if !isLikelyImage(blob) {
				return nil, "", fmt.Errorf("downloaded payload is not a valid image (content-type %q, %d bytes)", mimeType, len(blob))
			}
			return blob, mimeType, nil
		}
		lastErr = err
		// Only retry with the next referer on 403 (hotlink denial); other
		// errors (404, timeouts) won't heal by swapping Referer.
		if !strings.Contains(err.Error(), "HTTP 403") {
			return nil, "", err
		}
	}
	return nil, "", lastErr
}

// refererOrigin returns the "scheme://host/" origin of rawURL, or "" when
// rawURL is empty or unparseable.
func refererOrigin(rawURL string) string {
	if strings.TrimSpace(rawURL) == "" {
		return ""
	}
	idx := strings.Index(rawURL, "://")
	if idx <= 0 {
		return ""
	}
	rest := rawURL[idx+3:]
	if slash := strings.Index(rest, "/"); slash > 0 {
		return rawURL[:idx+3+slash+1]
	}
	return rawURL + "/"
}

func (d *Downloader) fetchImageWithReferer(ctx context.Context, imageURL, referer string, allowOpaque bool) ([]byte, string, error) {
	var lastErr error
	for attempt := 0; attempt < imageFetchMaxAttempts; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt) * imageFetchRetryBaseDelay
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, "", ctx.Err()
			case <-timer.C:
			}
		}
		blob, mimeType, err := d.fetchImageOnce(ctx, imageURL, referer, allowOpaque)
		if err == nil {
			return blob, mimeType, nil
		}
		// Permanent failures (404, invalid payload, oversized body, bad
		// request) never heal by retrying the same bytes.
		if !isRetryableImageError(err) {
			return nil, "", err
		}
		lastErr = err
		slog.Warn("retrying image fetch after transient failure", "url", imageURL, "attempt", attempt+1, "maxAttempts", imageFetchMaxAttempts, "error", err)
	}
	return nil, "", lastErr
}

// isRetryableImageError reports whether an image fetch failure is worth
// retrying: mid-body connection resets, timeouts and rate-limit/5xx
// statuses. HTTP 403 is excluded on purpose — the outer DownloadImage loop
// rotates the Referer instead, since a hotlink denial will not heal by
// replaying the identical request.
func isRetryableImageError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "http 403") {
		return false
	}
	if strings.Contains(msg, "http 408") || strings.Contains(msg, "http 429") {
		return true
	}
	// "HTTP %d fetching image" — any 5xx from the image host.
	if strings.Contains(msg, "http 5") {
		return true
	}
	for _, s := range []string{
		"connection reset",
		"broken pipe",
		"unexpected eof",
		"connection refused",
		"connection closed",
		"no connection",
		"timeout",
		"timed out",
		"temporary",
		"try again",
	} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

func (d *Downloader) fetchImageOnce(ctx context.Context, imageURL, referer string, allowOpaque bool) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("creating image request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "image/avif,image/webp,image/*,*/*;q=0.8")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("fetching image: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, "", fmt.Errorf("HTTP %d fetching image", resp.StatusCode)
	}
	// 25MB hard cap per image: the URL comes from scraped HTML/API, so its
	// size is untrusted — without the limit a malicious source could OOM us.
	const maxImageBytes int64 = 25 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("reading image body: %w", err)
	}
	if int64(len(body)) > maxImageBytes {
		return nil, "", fmt.Errorf("image exceeds %d bytes", maxImageBytes)
	}
	mimeType := resp.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "image/jpeg"
	}
	if !allowOpaque && !isLikelyImage(body) {
		return nil, "", fmt.Errorf("downloaded payload is not a valid image (content-type %q, %d bytes)", mimeType, len(body))
	}
	return body, mimeType, nil
}

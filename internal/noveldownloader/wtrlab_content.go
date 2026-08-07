package noveldownloader

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"
)

// wtrLabContentKey is the fixed AES-256-GCM key the site's client bundle uses
// to decrypt raw ("web") chapter text. It ships publicly in the page JS; this
// mirrors the browser-side decryption the reader performs before rendering.
var wtrLabContentKey = []byte("IJAFUUxjM25hyzL2AZrn0wl7cESED6Ru"[:32])

// wtrLabReaderResponse maps POST /api/reader/get responses. The chapter text
// lives in data.data.body: an array of paragraphs for the AI translation, or
// an encrypted blob for the raw "web" service (see wtrLabDecryptContent).
type wtrLabReaderResponse struct {
	Success          bool   `json:"success"`
	Code             string `json:"code"`
	Error            string `json:"error"`
	RequireTurnstile bool   `json:"requireTurnstile"`
	Chapter          *struct {
		Title  string `json:"title"`
		Locked bool   `json:"locked"`
	} `json:"chapter"`
	Data *struct {
		Data *struct {
			Body json.RawMessage `json:"body"`
			Title string         `json:"title"`
		} `json:"data"`
	} `json:"data"`
}

func (p *WTRLabParser) ParseChapter(ctx context.Context, client HTTPClient, chapterURL string) (*Chapter, error) {
	parts, ok := wtrLabParsePath(chapterURL)
	if !ok {
		return nil, fmt.Errorf("invalid wtr-lab chapter URL: %s", chapterURL)
	}
	if parts.Chapter <= 0 {
		return nil, fmt.Errorf("not a chapter URL: %s", chapterURL)
	}

	// The "web" reader service returns the raw source text (the Chinese
	// original for this novel) with no free-reading quota, unlike the paid AI
	// translation. That raw text is what the translation pipeline consumes.
	payload, err := json.Marshal(map[string]any{
		"translate":  "web",
		"language":   parts.Locale,
		"raw_id":     parts.RawID,
		"chapter_no": parts.Chapter,
	})
	if err != nil {
		return nil, fmt.Errorf("building reader request: %w", err)
	}

	apiURL := fmt.Sprintf("%s/api/reader/get", extractBaseURL(chapterURL))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("creating reader request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching chapter content: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading chapter response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d fetching chapter content: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var readerResp wtrLabReaderResponse
	if err := json.Unmarshal(respBody, &readerResp); err != nil {
		return nil, fmt.Errorf("parsing chapter content: %w", err)
	}

	if !readerResp.Success {
		if readerResp.RequireTurnstile {
			return nil, fmt.Errorf("chapter %d exceeds the free reading quota and requires a Cloudflare Turnstile challenge to continue", parts.Chapter)
		}
		if readerResp.Code == "1401" || readerResp.Code == "CHAPTER_LOCKED" || (readerResp.Chapter != nil && readerResp.Chapter.Locked) {
			return nil, fmt.Errorf("chapter %d is locked and requires a login to read", parts.Chapter)
		}
		msg := readerResp.Error
		if msg == "" {
			msg = readerResp.Code
		}
		if msg == "" {
			msg = "unknown error"
		}
		return nil, fmt.Errorf("reading chapter %d: %s", parts.Chapter, msg)
	}

	if readerResp.Data == nil || readerResp.Data.Data == nil {
		return nil, fmt.Errorf("chapter %d response has no content", parts.Chapter)
	}

	paras, err := wtrLabExtractParagraphs(readerResp.Data.Data.Body)
	if err != nil {
		return nil, fmt.Errorf("chapter %d: %w", parts.Chapter, err)
	}

	// Build <p> elements so the downloader's html->markdown conversion keeps
	// paragraph breaks, matching how the site renders the chapter body.
	var sb strings.Builder
	for _, para := range paras {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		sb.WriteString("<p>")
		sb.WriteString(html.EscapeString(para))
		sb.WriteString("</p>\n")
	}

	title := ""
	if readerResp.Chapter != nil {
		title = readerResp.Chapter.Title
	}
	if title == "" {
		title = readerResp.Data.Data.Title
	}

	return &Chapter{
		Title:     CleanTitle(title),
		Content:   strings.TrimSpace(sb.String()),
		SourceURL: chapterURL,
	}, nil
}

// wtrLabExtractParagraphs turns the reader response body into a list of
// paragraphs. The body is either a JSON array of strings (unencrypted) or an
// encrypted blob string ("arr:"/"str:" prefixes) produced by the "web" service.
func wtrLabExtractParagraphs(raw json.RawMessage) ([]string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, nil
	}
	switch trimmed[0] {
	case '[': // already a JSON array of paragraphs
		var paras []string
		if err := json.Unmarshal(trimmed, &paras); err != nil {
			return nil, fmt.Errorf("parsing chapter body: %w", err)
		}
		return paras, nil
	case '"': // encrypted blob
		var blob string
		if err := json.Unmarshal(trimmed, &blob); err != nil {
			return nil, fmt.Errorf("parsing chapter body string: %w", err)
		}
		return wtrLabDecryptContent(blob)
	default:
		return nil, fmt.Errorf("unexpected chapter body format")
	}
}

// wtrLabDecryptContent decrypts an encrypted chapter blob from the "web"
// reader service. Blob format:
//
//	arr:<iv_b64>:<authTag_b64>:<ciphertext_b64>  -> JSON array of paragraphs
//	str:<iv_b64>:<authTag_b64>:<ciphertext_b64>  -> plain text, newline-separated
//
// The plaintext is AES-256-GCM with the fixed public key; the IV length is
// non-standard (16 bytes), so a custom nonce size is required.
func wtrLabDecryptContent(blob string) ([]string, error) {
	jsonArray := false
	payload := blob
	switch {
	case strings.HasPrefix(blob, "arr:"):
		jsonArray = true
		payload = blob[len("arr:"):]
	case strings.HasPrefix(blob, "str:"):
		payload = blob[len("str:"):]
	default:
		return nil, fmt.Errorf("unsupported encrypted content prefix")
	}

	parts := strings.Split(payload, ":")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid encrypted content format")
	}
	iv, err := base64.StdEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("decoding iv: %w", err)
	}
	tag, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decoding auth tag: %w", err)
	}
	ct, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("decoding ciphertext: %w", err)
	}

	block, err := aes.NewCipher(wtrLabContentKey)
	if err != nil {
		return nil, fmt.Errorf("creating cipher: %w", err)
	}
	aead, err := cipher.NewGCMWithNonceSize(block, len(iv))
	if err != nil {
		return nil, fmt.Errorf("creating gcm: %w", err)
	}
	plain, err := aead.Open(nil, iv, append(ct, tag...), nil)
	if err != nil {
		return nil, fmt.Errorf("decrypting chapter content: %w", err)
	}

	if jsonArray {
		var paras []string
		if err := json.Unmarshal(plain, &paras); err != nil {
			return nil, fmt.Errorf("parsing decrypted paragraphs: %w", err)
		}
		return paras, nil
	}

	// str: payload — plain text; split into non-empty lines.
	var paras []string
	for _, line := range strings.Split(string(plain), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			paras = append(paras, line)
		}
	}
	return paras, nil
}

package comicdownloader

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
)

// Encrypted page URLs let parser scripts serve sites that ship AES-CTR
// encrypted image bytes (e.g. toonkotsu's /cdn/e/*.bin). The script appends
// the per-page key material as a URL fragment:
//
//	https://site/cdn/e/xxx.bin#tira-enc=aes-256-ctr&k=<base64url-key>&n=<base64url-nonce>
//
// Fragments are never sent to the server: DownloadImage strips the fragment
// before fetching and decrypts the payload after download, so what lands on
// disk is the plain image. The convention is transport-level and
// site-agnostic — no host names appear here; the parser owns the site logic
// of where k/n come from.
const encFragmentCipher = "aes-256-ctr"

// encParams carries the key material parsed from a #tira-enc fragment.
type encParams struct {
	cipher string
	key    []byte
	nonce  []byte
}

// pageDirectives carries the transport-level directives a parser script
// encodes in a page image URL fragment (#tira-enc=..., #tira-referer=...).
// Fragments are never sent to the server: DownloadImage strips them before
// fetching (see splitImageDirectives), so what lands on disk is unaffected.
type pageDirectives struct {
	enc *encParams
	// referer overrides the Referer header candidates when hasReferer is
	// true. An empty referer with hasReferer true means "send no Referer
	// header at all" — some CDNs answer a foreign Referer with a 200
	// placeholder instead of the image, which no status code can detect.
	referer    string
	hasReferer bool
}

// splitImageDirectives separates the fetchable URL from the directives
// encoded in its fragment. It returns the URL without fragment and zero
// directives when the URL carries no recognized directive.
func splitImageDirectives(rawURL string) (string, pageDirectives, error) {
	var dir pageDirectives
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL, dir, fmt.Errorf("parsing image url: %w", err)
	}
	if u.Fragment == "" {
		return rawURL, dir, nil
	}
	fetchURL := rawURL[:len(rawURL)-len(u.Fragment)-1]
	q, err := url.ParseQuery(u.Fragment)
	if err != nil {
		return "", dir, fmt.Errorf("parsing image url fragment: %w", err)
	}
	if q.Get("tira-enc") != "" {
		if q.Get("tira-enc") != encFragmentCipher {
			return "", dir, fmt.Errorf("unsupported page encryption %q", q.Get("tira-enc"))
		}
		key, err := decodeB64(q.Get("k"))
		if err != nil || len(key) == 0 {
			return "", dir, fmt.Errorf("invalid page encryption key")
		}
		if len(key) != 16 && len(key) != 24 && len(key) != 32 {
			return "", dir, fmt.Errorf("invalid page encryption key length %d", len(key))
		}
		nonce, err := decodeB64(q.Get("n"))
		if err != nil || len(nonce) == 0 {
			return "", dir, fmt.Errorf("invalid page encryption nonce")
		}
		if len(nonce) != 8 {
			return "", dir, fmt.Errorf("invalid page encryption nonce length %d", len(nonce))
		}
		dir.enc = &encParams{cipher: encFragmentCipher, key: key, nonce: nonce}
	}
	if q.Has("tira-referer") {
		ref := strings.TrimSpace(q.Get("tira-referer"))
		if ref != "" {
			ru, err := url.Parse(ref)
			if err != nil || (ru.Scheme != "http" && ru.Scheme != "https") || ru.Host == "" {
				return "", dir, fmt.Errorf("invalid tira-referer %q: want an http(s) URL or empty", q.Get("tira-referer"))
			}
			ref = ru.String()
		}
		dir.referer = ref
		dir.hasReferer = true
	}
	return fetchURL, dir, nil
}

// decodeB64 decodes unpadded or padded base64 in URL-safe or standard
// alphabet. Sites emit keys in base64url without padding; accept the padded
// and standard variants so a stray encoder change stays harmless.
func decodeB64(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("empty value")
	}
	if b, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.URLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.StdEncoding.DecodeString(s)
}

// decryptAESCTR XORs data with the AES-CTR keystream for key/nonce. The
// counter block is the 8-byte nonce followed by 8 zero bytes with a 64-bit
// counter, matching the browser-side decryption readers perform.
func decryptAESCTR(data, key, nonce []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("creating cipher: %w", err)
	}
	var iv [16]byte
	copy(iv[:], nonce)
	out := make([]byte, len(data))
	cipher.NewCTR(block, iv[:]).XORKeyStream(out, data)
	return out, nil
}

// sniffImageMIME reports the MIME type from magic bytes, or "" when the
// payload matches no known image signature.
func sniffImageMIME(data []byte) string {
	if len(data) < 4 {
		return ""
	}
	if data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return "image/jpeg"
	}
	if data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 {
		return "image/png"
	}
	if data[0] == 0x47 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x38 {
		return "image/gif"
	}
	if len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return "image/webp"
	}
	if len(data) >= 12 && string(data[4:8]) == "ftyp" &&
		(string(data[8:12]) == "avif" || string(data[8:12]) == "avis") {
		return "image/avif"
	}
	return ""
}

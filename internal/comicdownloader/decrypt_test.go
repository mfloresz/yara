package comicdownloader

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"context"
)

// encryptFixture produces AES-CTR ciphertext with the toonkotsu wire format:
// 8-byte nonce, counter block = nonce + 8 zero bytes.
func encryptFixture(t *testing.T, plain, key, nonce []byte) []byte {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("new cipher: %v", err)
	}
	var iv [16]byte
	copy(iv[:], nonce)
	out := make([]byte, len(plain))
	cipher.NewCTR(block, iv[:]).XORKeyStream(out, plain)
	return out
}

func TestDownloadImageDecryptsFragmentPages(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand key: %v", err)
	}
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatalf("rand nonce: %v", err)
	}
	plain := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00, 'p', 'a', 'g', 'e', '1'}
	ciphertext := encryptFixture(t, plain, key, nonce)

	var gotPath, gotFragment string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotFragment = r.URL.Fragment
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(ciphertext)
	}))
	defer srv.Close()

	pageURL := srv.URL + "/cdn/e/xxx.bin#tira-enc=aes-256-ctr&k=" +
		base64.RawURLEncoding.EncodeToString(key) + "&n=" + base64.RawURLEncoding.EncodeToString(nonce)
	dl := NewDownloaderWithClient(NewHTTPClientWithTransport(http.DefaultTransport))
	blob, mimeType, err := dl.DownloadImage(context.Background(), pageURL, "")
	if err != nil {
		t.Fatalf("DownloadImage: %v", err)
	}
	if string(blob) != string(plain) {
		t.Errorf("decrypted payload mismatch: got %x, want %x", blob, plain)
	}
	if mimeType != "image/jpeg" {
		t.Errorf("mimeType = %q, want image/jpeg (sniffed from decrypted bytes)", mimeType)
	}
	if gotPath != "/cdn/e/xxx.bin" {
		t.Errorf("server saw path %q, want /cdn/e/xxx.bin", gotPath)
	}
	if gotFragment != "" {
		t.Errorf("server saw fragment %q, want none (fragments must not be sent)", gotFragment)
	}
}

func TestDownloadImagePlainURLPassthrough(t *testing.T) {
	plain := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 'p', 'n', 'g'}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(plain)
	}))
	defer srv.Close()

	dl := NewDownloaderWithClient(NewHTTPClientWithTransport(http.DefaultTransport))
	blob, mimeType, err := dl.DownloadImage(context.Background(), srv.URL+"/c.png", "")
	if err != nil {
		t.Fatalf("DownloadImage: %v", err)
	}
	if string(blob) != string(plain) {
		t.Errorf("payload mismatch")
	}
	if mimeType != "image/png" {
		t.Errorf("mimeType = %q, want image/png", mimeType)
	}
}

func TestDownloadImageBadEncryptionFragment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte{0xFF, 0xD8, 0xFF, 0xE0})
	}))
	defer srv.Close()
	dl := NewDownloaderWithClient(NewHTTPClientWithTransport(http.DefaultTransport))

	for name, frag := range map[string]string{
		"unknown cipher":  "tira-enc=aes-128-cbc&k=QUJDRA&n=QUJDRA",
		"short key":       "tira-enc=aes-256-ctr&k=QUJDRA&n=QUJDREVGSEk",
		"short nonce":     "tira-enc=aes-256-ctr&k=QUJDREVGSElKS0xNTk9QUVJTVFVWV1hZWg&n=QUJD",
		"missing key":     "tira-enc=aes-256-ctr&n=QUJDREVGSEk",
		"unparseable b64": "tira-enc=aes-256-ctr&k=!!!&n=???",
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := dl.DownloadImage(context.Background(), srv.URL+"/x.bin#"+frag, "")
			if err == nil {
				t.Errorf("expected error for fragment %q", frag)
			}
		})
	}
}

func TestSplitEncryptionFragmentNonEncFragment(t *testing.T) {
	fetchURL, dir, err := splitImageDirectives("https://example.com/i.jpg#section-2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dir.enc != nil {
		t.Errorf("expected nil params for non-encryption fragment")
	}
	if dir.hasReferer {
		t.Errorf("expected no referer directive for plain fragment")
	}
	if fetchURL != "https://example.com/i.jpg" {
		t.Errorf("fetchURL = %q, want stripped url", fetchURL)
	}
}

func TestSniffImageMIME(t *testing.T) {
	cases := map[string][]byte{
		"image/jpeg": {0xFF, 0xD8, 0xFF, 0xE0},
		"image/png":  {0x89, 0x50, 0x4E, 0x47},
		"image/gif":  {'G', 'I', 'F', '8', '9'},
		"image/webp": []byte{'R', 'I', 'F', 'F', 0x01, 0x02, 0x03, 0x04, 'W', 'E', 'B', 'P'},
		"":           {0x00, 0x01, 0x02, 0x03},
	}
	for want, head := range cases {
		if got := sniffImageMIME(head); got != want {
			t.Errorf("sniffImageMIME(%x) = %q, want %q", head, got, want)
		}
	}
	if got := sniffImageMIME([]byte{0xFF}); got != "" {
		t.Errorf("short input: got %q, want empty", got)
	}
}

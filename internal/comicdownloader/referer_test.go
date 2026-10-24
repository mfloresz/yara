package comicdownloader

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
)

var refererTestJPEG = []byte{0xFF, 0xD8, 0xFF, 0xE0, 'p', 'a', 'g', 'e'}

// refererCaptureServer serves a valid JPEG while recording the Referer
// header and any fragment the client (incorrectly) sends.
func refererCaptureServer(gotReferer, gotFragment *string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*gotReferer = r.Header.Get("Referer")
		*gotFragment = r.URL.Fragment
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(refererTestJPEG)
	}))
}

func TestDownloadImageHonorsRefererFragment(t *testing.T) {
	var gotReferer, gotFragment string
	srv := refererCaptureServer(&gotReferer, &gotFragment)
	defer srv.Close()

	dl := NewDownloaderWithClient(NewHTTPClientWithTransport(http.DefaultTransport))
	blob, _, err := dl.DownloadImage(context.Background(),
		srv.URL+"/p1.jpg#tira-referer=https://mangadex.org/", "https://other.example/chapter")
	if err != nil {
		t.Fatalf("DownloadImage: %v", err)
	}
	if string(blob) != string(refererTestJPEG) {
		t.Errorf("payload mismatch")
	}
	if gotReferer != "https://mangadex.org/" {
		t.Errorf("Referer = %q, want the fragment directive (hint origin must not win)", gotReferer)
	}
	if gotFragment != "" {
		t.Errorf("server saw fragment %q, want none (fragments must not be sent)", gotFragment)
	}
}

func TestDownloadImageEmptyRefererFragmentSendsNone(t *testing.T) {
	var gotReferer, gotFragment string
	srv := refererCaptureServer(&gotReferer, &gotFragment)
	defer srv.Close()

	dl := NewDownloaderWithClient(NewHTTPClientWithTransport(http.DefaultTransport))
	// The hint would normally seed an origin here; the empty directive
	// suppresses the header entirely.
	_, _, err := dl.DownloadImage(context.Background(),
		srv.URL+"/p1.jpg#tira-referer=", "https://other.example/chapter")
	if err != nil {
		t.Fatalf("DownloadImage: %v", err)
	}
	if gotReferer != "" {
		t.Errorf("Referer = %q, want no header", gotReferer)
	}
	if gotFragment != "" {
		t.Errorf("server saw fragment %q, want none", gotFragment)
	}
}

func TestDownloadImageInvalidRefererFragment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(refererTestJPEG)
	}))
	defer srv.Close()
	dl := NewDownloaderWithClient(NewHTTPClientWithTransport(http.DefaultTransport))

	for _, frag := range []string{
		"tira-referer=notaurl",
		"tira-referer=ftp://host/x.jpg",
		"tira-referer=http://",
		"tira-referer=%zz",
	} {
		t.Run(frag, func(t *testing.T) {
			_, _, err := dl.DownloadImage(context.Background(), srv.URL+"/x.jpg#"+frag, "")
			if err == nil {
				t.Errorf("expected error for fragment %q", frag)
			}
		})
	}
}

func TestSplitImageDirectivesCombined(t *testing.T) {
	key := base64.RawURLEncoding.EncodeToString(make([]byte, 16))
	nonce := base64.RawURLEncoding.EncodeToString(make([]byte, 8))
	fetchURL, dir, err := splitImageDirectives(
		"https://example.com/e.bin#tira-enc=aes-256-ctr&k=" + key + "&n=" + nonce + "&tira-referer=https://cdn.example/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fetchURL != "https://example.com/e.bin" {
		t.Errorf("fetchURL = %q, want stripped url", fetchURL)
	}
	if dir.enc == nil || len(dir.enc.key) != 16 || len(dir.enc.nonce) != 8 {
		t.Errorf("enc params not parsed alongside referer: %+v", dir.enc)
	}
	if !dir.hasReferer || dir.referer != "https://cdn.example/" {
		t.Errorf("referer = %q (present=%v), want https://cdn.example/", dir.referer, dir.hasReferer)
	}
}

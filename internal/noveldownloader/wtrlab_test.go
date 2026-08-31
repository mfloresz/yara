package noveldownloader

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// wtrLabStubClient returns canned payloads keyed by URL for GET requests and
// hands back a single canned response for the reader POST, so parser tests run
// without hitting the live site.
type wtrLabStubClient struct {
	getResponses map[string]string
	readerResp   string
	lastPostBody string
}

func (c *wtrLabStubClient) Fetch(ctx context.Context, url string) ([]byte, error) {
	body, ok := c.getResponses[url]
	if !ok {
		return nil, fmt.Errorf("no stub for %s", url)
	}
	return []byte(body), nil
}

func (c *wtrLabStubClient) FetchDocument(ctx context.Context, url string) (*goquery.Document, error) {
	body, err := c.Fetch(ctx, url)
	if err != nil {
		return nil, err
	}
	return goquery.NewDocumentFromReader(strings.NewReader(string(body)))
}

func (c *wtrLabStubClient) Do(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodPost || !strings.HasSuffix(req.URL.Path, "/api/reader/get") {
		return nil, fmt.Errorf("no stub for %s %s", req.Method, req.URL)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	c.lastPostBody = string(body)
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(bytes.NewReader([]byte(c.readerResp))),
		Header:     make(http.Header),
	}, nil
}

// wtrLabStubNovelPage is a minimal wtr-lab.com novel page carrying the
// __NEXT_DATA__ JSON the parser reads its metadata from.
const wtrLabStubNovelPage = `<!DOCTYPE html><html><head><script id="__NEXT_DATA__" type="application/json">{
  "props": {
    "pageProps": {
      "serie": {
        "serie_data": {
          "id": 84999,
          "slug": "im-playing-the-role-of-a-beautiful-powerful-and-tragic-big-shot-in-the-infinite-world",
          "raw_id": 88651,
          "data": {
            "title": "I’m Playing the Role of a Beautiful, Powerful, and Tragic Big Shot in the Infinite World",
            "author": "CrazyYang",
            "description": "Dual male protagonists/Dihua/Infinite flow",
            "image": "https://img.wtr-lab.com/cdn/series/cover.png"
          },
          "chapter_count": 348
        }
      }
    }
  },
  "page": "/[locale]/novel/[raw_id]/[serie_slug]"
}</script></head><body></body></html>`

const wtrLabStubChapters = `{"chapters":[
  {"serie_id":84999,"id":46909272,"order":1,"title":"Chapter 1 Am I an NPC?","name":"第1章 我是NPC? ? ?"},
  {"serie_id":84999,"id":46909273,"order":2,"title":"Chapter 2 The Collapsed Fairy Tale House (1)","name":"第2章 崩坏的童话屋 (1)"},
  {"serie_id":84999,"id":46909274,"order":3,"title":"Chapter 3 The Collapsed Fairy Tale House (2)","name":"第3章 崩坏的童话屋 (2)"}
]}`

const wtrLabStubReaderTurnstile = `{"success":false,"requireTurnstile":true,"message":"Please complete the Turnstile challenge to continue reading"}`

const wtrLabStubReaderLocked = `{"success":false,"code":"1401","error":"You are not logged in!","chapter":{"id":46909632,"title":"Chapter 160 The Tingfeng Pavilion Massacre (1)","locked":true}}`

const wtrLabNovelURL = "https://wtr-lab.com/en/novel/88651/im-playing-the-role-of-a-beautiful-powerful-and-tragic-big-shot-in-the-infinite-world"

func wtrLabStubURLs() map[string]string {
	return map[string]string{
		wtrLabNovelURL: wtrLabStubNovelPage,
		"https://wtr-lab.com/api/chapters/88651": wtrLabStubChapters,
	}
}

// wtrLabEncryptFixture produces an "arr:" blob exactly like the reader API
// does: JSON array of paragraphs, AES-256-GCM with the site's fixed key and a
// non-standard 16-byte IV.
func wtrLabEncryptFixture(t *testing.T, paras []string) string {
	t.Helper()
	plain, err := json.Marshal(paras)
	if err != nil {
		t.Fatalf("marshaling fixture: %v", err)
	}
	iv := []byte("0123456789abcdef") // 16 bytes
	block, err := aes.NewCipher(wtrLabContentKey)
	if err != nil {
		t.Fatalf("creating cipher: %v", err)
	}
	aead, err := cipher.NewGCMWithNonceSize(block, len(iv))
	if err != nil {
		t.Fatalf("creating gcm: %v", err)
	}
	sealed := aead.Seal(nil, iv, plain, nil)
	ct := sealed[:len(plain)]
	tag := sealed[len(plain):]
	return "arr:" +
		base64.StdEncoding.EncodeToString(iv) + ":" +
		base64.StdEncoding.EncodeToString(tag) + ":" +
		base64.StdEncoding.EncodeToString(ct)
}

// wtrLabReaderOKResponse builds a success response for the "web" service with
// an encrypted body.
func wtrLabReaderOKResponse(t *testing.T) string {
	t.Helper()
	blob := wtrLabEncryptFixture(t, []string{
		"星星在夜空中闪烁，四周一片混沌。",
		"白澈整个人生无可恋的呈大字型躺在地上，身旁悬浮着一个灰色的光球。",
		"白澈抬手捂住耳朵：“啊啊啊啊啊，别说了别说了。”",
	})
	resp, err := json.Marshal(map[string]any{
		"success": true,
		"chapter": map[string]any{"id": 46909272, "title": "Chapter 1 Am I an NPC?", "locked": false},
		"data": map[string]any{
			"data": map[string]any{
				"body":  blob,
				"title": "第1章 我是NPC？？？",
			},
		},
	})
	if err != nil {
		t.Fatalf("marshaling reader response: %v", err)
	}
	return string(resp)
}

func TestWTRLabCanHandle(t *testing.T) {
	p := NewWTRLabParser()
	cases := []struct {
		url  string
		want bool
	}{
		{wtrLabNovelURL, true},
		{wtrLabNovelURL + "/chapter-1", true},
		{"https://www.wtr-lab.com/en/novel/88651/some-slug", true},
		{"https://wtr-lab.com/es/novel/88651/some-slug", true},
		{"https://example.com/en/novel/88651/some-slug", false},
		{"https://wtr-lab.com/ranking/daily", false},
		{"https://wtr-lab.com/en/profile/foo", false},
	}
	for _, c := range cases {
		if got := p.CanHandle(c.url); got != c.want {
			t.Errorf("CanHandle(%q) = %v, want %v", c.url, got, c.want)
		}
	}
}

func TestWTRLabGetNovelInfo(t *testing.T) {
	p := NewWTRLabParser()
	client := &wtrLabStubClient{getResponses: wtrLabStubURLs()}

	info, err := p.GetNovelInfo(context.Background(), client, wtrLabNovelURL)
	if err != nil {
		t.Fatalf("GetNovelInfo: %v", err)
	}

	if info.Title != "I’m Playing the Role of a Beautiful, Powerful, and Tragic Big Shot in the Infinite World" {
		t.Errorf("unexpected title: %q", info.Title)
	}
	if info.Author != "CrazyYang" {
		t.Errorf("unexpected author: %q", info.Author)
	}
	if info.Description != "Dual male protagonists/Dihua/Infinite flow" {
		t.Errorf("unexpected description: %q", info.Description)
	}
	if info.CoverURL != "https://img.wtr-lab.com/cdn/series/cover.png" {
		t.Errorf("unexpected coverURL: %q", info.CoverURL)
	}
	if len(info.Chapters) != 3 {
		t.Fatalf("expected 3 chapters, got %d", len(info.Chapters))
	}
	first := info.Chapters[0]
	if first.Title != "Chapter 1 Am I an NPC?" {
		t.Errorf("unexpected first title: %q", first.Title)
	}
	wantFirstURL := wtrLabNovelURL + "/chapter-1"
	if first.URL != wantFirstURL {
		t.Errorf("unexpected first URL: %q, want %q", first.URL, wantFirstURL)
	}
}

func TestWTRLabGetChapterURLs(t *testing.T) {
	p := NewWTRLabParser()
	client := &wtrLabStubClient{getResponses: wtrLabStubURLs()}

	chapters, err := p.GetChapterURLs(context.Background(), client, nil, wtrLabNovelURL)
	if err != nil {
		t.Fatalf("GetChapterURLs: %v", err)
	}
	if len(chapters) != 3 {
		t.Fatalf("expected 3 chapters, got %d", len(chapters))
	}
	for i, ch := range chapters {
		want := fmt.Sprintf("%s/chapter-%d", wtrLabNovelURL, i+1)
		if ch.URL != want {
			t.Errorf("chapter %d URL = %q, want %q", i+1, ch.URL, want)
		}
	}
}

func TestWTRLabParseChapterWebService(t *testing.T) {
	p := NewWTRLabParser()
	client := &wtrLabStubClient{
		getResponses: wtrLabStubURLs(),
		readerResp:   wtrLabReaderOKResponse(t),
	}

	ch, err := p.ParseChapter(context.Background(), client, wtrLabNovelURL+"/chapter-1")
	if err != nil {
		t.Fatalf("ParseChapter: %v", err)
	}
	if ch.Title != "Chapter 1 Am I an NPC?" {
		t.Errorf("unexpected title: %q", ch.Title)
	}
	// The raw Chinese text must survive the round-trip (encrypt -> decrypt)
	// and be wrapped in <p> elements so html->markdown keeps paragraph breaks.
	for _, want := range []string{
		"<p>星星在夜空中闪烁，四周一片混沌。</p>",
		"<p>白澈整个人生无可恋的呈大字型躺在地上，身旁悬浮着一个灰色的光球。</p>",
	} {
		if !strings.Contains(ch.Content, want) {
			t.Errorf("content missing %q:\n%s", want, ch.Content)
		}
	}
}

func TestWTRLabParseChapterPlainArray(t *testing.T) {
	// Some services return an unencrypted JSON array body; the parser must
	// accept that form too.
	p := NewWTRLabParser()
	resp, err := json.Marshal(map[string]any{
		"success": true,
		"chapter": map[string]any{"title": "Chapter 2", "locked": false},
		"data": map[string]any{
			"data": map[string]any{
				"body":  []string{"第一段。", "第二段。"},
				"title": "第2章",
			},
		},
	})
	if err != nil {
		t.Fatalf("marshaling: %v", err)
	}
	client := &wtrLabStubClient{
		getResponses: wtrLabStubURLs(),
		readerResp:   string(resp),
	}

	ch, err := p.ParseChapter(context.Background(), client, wtrLabNovelURL+"/chapter-2")
	if err != nil {
		t.Fatalf("ParseChapter: %v", err)
	}
	if !strings.Contains(ch.Content, "<p>第一段。</p>") || !strings.Contains(ch.Content, "<p>第二段。</p>") {
		t.Errorf("content missing paragraphs: %q", ch.Content)
	}
}

func TestWTRLabParseChapterTurnstile(t *testing.T) {
	p := NewWTRLabParser()
	client := &wtrLabStubClient{
		getResponses: wtrLabStubURLs(),
		readerResp:   wtrLabStubReaderTurnstile,
	}

	_, err := p.ParseChapter(context.Background(), client, wtrLabNovelURL+"/chapter-1")
	if err == nil {
		t.Fatal("expected error for Turnstile response, got nil")
	}
	if !strings.Contains(err.Error(), "quota") && !strings.Contains(err.Error(), "Turnstile") {
		t.Errorf("error should mention quota/Turnstile, got: %v", err)
	}
}

func TestWTRLabParseChapterLocked(t *testing.T) {
	p := NewWTRLabParser()
	client := &wtrLabStubClient{
		getResponses: wtrLabStubURLs(),
		readerResp:   wtrLabStubReaderLocked,
	}

	_, err := p.ParseChapter(context.Background(), client, wtrLabNovelURL+"/chapter-160")
	if err == nil {
		t.Fatal("expected error for locked chapter, got nil")
	}
	if !strings.Contains(err.Error(), "locked") {
		t.Errorf("error should mention locked, got: %v", err)
	}
}

func TestWTRLabParseChapterBadURL(t *testing.T) {
	p := NewWTRLabParser()
	client := &wtrLabStubClient{getResponses: wtrLabStubURLs()}

	if _, err := p.ParseChapter(context.Background(), client, wtrLabNovelURL); err == nil {
		t.Error("expected error for novel page URL, got nil")
	}
	if _, err := p.ParseChapter(context.Background(), client, "https://example.com/en/novel/1/slug/chapter-1"); err == nil {
		t.Error("expected error for non-wtr-lab URL, got nil")
	}
}

func TestWTRLabDecryptContentStr(t *testing.T) {
	// str: payloads decrypt to plain text; the parser splits it into lines.
	plain := "第一行。\n第二行。\n"
	iv := []byte("0123456789abcdef")
	block, err := aes.NewCipher(wtrLabContentKey)
	if err != nil {
		t.Fatalf("creating cipher: %v", err)
	}
	aead, err := cipher.NewGCMWithNonceSize(block, len(iv))
	if err != nil {
		t.Fatalf("creating gcm: %v", err)
	}
	sealed := aead.Seal(nil, iv, []byte(plain), nil)
	blob := "str:" +
		base64.StdEncoding.EncodeToString(iv) + ":" +
		base64.StdEncoding.EncodeToString(sealed[len(plain):]) + ":" +
		base64.StdEncoding.EncodeToString(sealed[:len(plain)])

	paras, err := wtrLabDecryptContent(blob)
	if err != nil {
		t.Fatalf("DecryptContent: %v", err)
	}
	if len(paras) != 2 || paras[0] != "第一行。" || paras[1] != "第二行。" {
		t.Errorf("unexpected paragraphs: %v", paras)
	}
}

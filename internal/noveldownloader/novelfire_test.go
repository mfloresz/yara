package noveldownloader

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Novelfire numbers chapter URLs sequentially (chapter-92, chapter-93, ...)
// while titles keep the novel's own numbering: prologues shift the sequence
// ("c-1: Prologue" at chapter-1 pushing "Chapter 1" to chapter-2) and split
// chapters produce decimals ("Chapter 92.1" and "Chapter 92.2" at chapter-93
// and chapter-94). Deriving the canonical order from the title collapses
// both decimals to 92, so the parser must report the URL number as Order.
func TestNovelfireChapterListUsesURLNumberAsOrder(t *testing.T) {
	var items []string
	items = append(items, `<li><a href="chapter-1"><span class="chapter-title">c-1: Prologue</span></a></li>`)
	for n := 2; n <= 92; n++ {
		items = append(items, fmt.Sprintf(`<li><a href="chapter-%d"><span class="chapter-title">Chapter %d: Filler</span></a></li>`, n, n-1))
	}
	items = append(items,
		`<li><a href="chapter-93"><span class="chapter-title">Chapter 92.1: Collapse of Xyrus</span></a></li>`,
		`<li><a href="chapter-94"><span class="chapter-title">Chapter 92.2: Bird's Cage</span></a></li>`,
	)
	listHTML := `<!doctype html><html><head></head><body>
<div class="main-head"><h1>Decimal Test Novel</h1></div>
<ul class="chapter-list">` + strings.Join(items, "") + `</ul>
</body></html>`

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, listHTML)
	}))
	defer mock.Close()

	transport := &delayTestTransport{rewrites: map[string]string{"novelfire.net": mock.URL}}
	dl := NewDownloaderWithClient(NewHTTPClientWithTransport(transport))

	info, err := dl.GetNovelInfo(context.Background(), "https://novelfire.net/book/decimal-test-novel")
	if err != nil {
		t.Fatalf("get novel info: %v", err)
	}
	if len(info.Chapters) != 94 {
		t.Fatalf("expected 94 chapters, got %d", len(info.Chapters))
	}

	seen := make(map[int]string, len(info.Chapters))
	for _, ch := range info.Chapters {
		if ch.Order <= 0 {
			t.Errorf("chapter %q (%s) has no order", ch.Title, ch.URL)
			continue
		}
		if prev, dup := seen[ch.Order]; dup {
			t.Errorf("order %d shared by %q and %q", ch.Order, prev, ch.Title)
		}
		seen[ch.Order] = ch.Title
	}
	if got := info.Chapters[92].Order; got != 93 {
		t.Errorf("Chapter 92.1 order: got %d, want 93", got)
	}
	if got := info.Chapters[93].Order; got != 94 {
		t.Errorf("Chapter 92.2 order: got %d, want 94", got)
	}
}

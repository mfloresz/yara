package noveldownloader

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// empireNovelStubClient returns canned payloads keyed by URL so parser tests
// run without hitting the live site.
type empireNovelStubClient struct {
	responses map[string]string
}

func (c *empireNovelStubClient) Fetch(ctx context.Context, url string) ([]byte, error) {
	body, ok := c.responses[url]
	if !ok {
		return nil, fmt.Errorf("no stub for %s", url)
	}
	return []byte(body), nil
}

func (c *empireNovelStubClient) FetchDocument(ctx context.Context, url string) (*goquery.Document, error) {
	body, err := c.Fetch(ctx, url)
	if err != nil {
		return nil, err
	}
	return goquery.NewDocumentFromReader(strings.NewReader(string(body)))
}

func (c *empireNovelStubClient) Do(req *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("unexpected Do call")
}

var _ HTTPClient = (*empireNovelStubClient)(nil)

const empireNovelStubChapterURL = "https://www.empirenovel.com/novel/family-management-game-in-immortal-continent/1263"

// New layout (no h3): the page h1 is the truncated novel title and the
// chapter title is the first line of the reader content.
const empireNovelStubChapterNewLayout = `<!DOCTYPE html><html><head><title>Family Management Game In Immortal Continent Chapter 1263 | Empire Novel</title></head><body>
<h1 class="text-center text-uppercase h4 font-weight-bolder"><a href="/novel/family-management-game-in-immortal-continent" class="text-light"> Family Management Game In Immortal Conti... </a></h1>
<div id="read-novel" class="mx-2 mx-sm-5 p-1 p-sm-5" style="font-size: 16px;"><p><strong>Chapter 1263: Chapter 1257: Leave The Rest To Your Elders</strong></p><br><p>Then there was the Martial Academy.</p><br><p>When the Blood Moon arrived, the Six Masters changed.</p></div>
</body></html>`

// Legacy layout: chapter title lives in an h3 inside the content area.
const empireNovelStubChapterLegacyLayout = `<!DOCTYPE html><html><head><title>Some Novel Chapter 5 | Empire Novel</title></head><body>
<h1 class="text-center text-uppercase h4 font-weight-bolder"><a href="/novel/some-novel" class="text-light"> Some Nove... </a></h1>
<div id="read-novel" class="mx-2 mx-sm-5 p-1 p-sm-5"><h3>Chapter 5: Old Title</h3><p>First body paragraph.</p><p>Second body paragraph.</p></div>
</body></html>`

const empireNovelStubChapterLegacyURL = "https://www.empirenovel.com/novel/some-novel/5"

func TestEmpireNovelParseChapterNewLayout(t *testing.T) {
	p := NewEmpireNovelParser()
	client := &empireNovelStubClient{responses: map[string]string{
		empireNovelStubChapterURL: empireNovelStubChapterNewLayout,
	}}

	ch, err := p.ParseChapter(context.Background(), client, empireNovelStubChapterURL)
	if err != nil {
		t.Fatalf("ParseChapter: %v", err)
	}
	wantTitle := "Chapter 1263: Chapter 1257: Leave The Rest To Your Elders"
	if ch.Title != wantTitle {
		t.Errorf("title = %q, want %q", ch.Title, wantTitle)
	}
	// The title line must not be duplicated as the first content paragraph.
	if strings.Contains(ch.Content, wantTitle) {
		t.Errorf("content should not contain the title line:\n%s", ch.Content)
	}
	for _, want := range []string{
		"<p>Then there was the Martial Academy.</p>",
		"<p>When the Blood Moon arrived, the Six Masters changed.</p>",
	} {
		if !strings.Contains(ch.Content, want) {
			t.Errorf("content missing %q:\n%s", want, ch.Content)
		}
	}
}

func TestEmpireNovelParseChapterLegacyLayout(t *testing.T) {
	p := NewEmpireNovelParser()
	client := &empireNovelStubClient{responses: map[string]string{
		empireNovelStubChapterLegacyURL: empireNovelStubChapterLegacyLayout,
	}}

	ch, err := p.ParseChapter(context.Background(), client, empireNovelStubChapterLegacyURL)
	if err != nil {
		t.Fatalf("ParseChapter: %v", err)
	}
	if ch.Title != "Chapter 5: Old Title" {
		t.Errorf("title = %q, want %q", ch.Title, "Chapter 5: Old Title")
	}
	// Legacy path keeps every paragraph as content.
	for _, want := range []string{
		"<p>First body paragraph.</p>",
		"<p>Second body paragraph.</p>",
	} {
		if !strings.Contains(ch.Content, want) {
			t.Errorf("content missing %q:\n%s", want, ch.Content)
		}
	}
}

func TestEmpireNovelParseChapterTitleFallback(t *testing.T) {
	// No h3 and no content: fall back to the chapter number from the URL
	// instead of the page h1 (which is the novel title, not the chapter).
	p := NewEmpireNovelParser()
	page := `<!DOCTYPE html><html><body><h1>Some Nove...</h1><div id="read-novel" class="mx-2 mx-sm-5 p-1 p-sm-5"></div></body></html>`
	client := &empireNovelStubClient{responses: map[string]string{
		empireNovelStubChapterURL: page,
	}}

	ch, err := p.ParseChapter(context.Background(), client, empireNovelStubChapterURL)
	if err != nil {
		t.Fatalf("ParseChapter: %v", err)
	}
	if ch.Title != "Chapter 1263" {
		t.Errorf("title = %q, want %q", ch.Title, "Chapter 1263")
	}
}

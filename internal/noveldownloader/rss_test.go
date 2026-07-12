package noveldownloader

import "testing"

func TestParseRSSChapters(t *testing.T) {
	raw := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Some Novel</title>
    <item>
      <title>Chapter 1</title>
      <link>https://example.com/story/some-novel/chapter-1/</link>
    </item>
    <item>
      <title>Chapter 2</title>
      <link>https://example.com/story/some-novel/chapter-2/</link>
    </item>
    <item>
      <title>No Link</title>
      <link></link>
    </item>
  </channel>
</rss>`)

	chapters := parseRSSChapters(raw)
	if len(chapters) != 2 {
		t.Fatalf("expected 2 chapters, got %d: %+v", len(chapters), chapters)
	}
	if chapters[0].Title != "Chapter 1" || chapters[0].URL != "https://example.com/story/some-novel/chapter-1/" {
		t.Errorf("unexpected first chapter: %+v", chapters[0])
	}
	if chapters[1].URL != "https://example.com/story/some-novel/chapter-2/" {
		t.Errorf("unexpected second chapter URL: %q", chapters[1].URL)
	}
}

func TestParseRSSChaptersInvalid(t *testing.T) {
	if got := parseRSSChapters([]byte("not xml at all")); got != nil {
		t.Errorf("expected nil for invalid feed, got %+v", got)
	}
}

func TestTitleCase(t *testing.T) {
	cases := map[string]string{
		"":                 "",
		"masked-replicant": "Masked Replicant",
		"a_b_c":            "A B C",
		"single":           "Single",
	}
	for in, want := range cases {
		if got := titleCase(in); got != want {
			t.Errorf("titleCase(%q) = %q, want %q", in, got, want)
		}
	}
}

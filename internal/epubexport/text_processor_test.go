package epubexport

import (
	"strings"
	"testing"
)

func TestEscapeXML(t *testing.T) {
	in := `a & b < c > d "e" 'f'`
	want := `a &amp; b &lt; c &gt; d &quot;e&quot; &apos;f&apos;`
	if got := escapeXML(in); got != want {
		t.Errorf("escapeXML = %q, want %q", got, want)
	}
}

func TestApplyInlineMarkdown(t *testing.T) {
	cases := []struct{ in, want string }{
		{"***both***", "<b><i>both</i></b>"},
		{"**bold**", "<b>bold</b>"},
		{"*italic*", "<i>italic</i>"},
		{"plain text", "plain text"},
	}
	for _, c := range cases {
		if got := applyInlineMarkdown(c.in); got != c.want {
			t.Errorf("applyInlineMarkdown(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestConvertUnderlineItalics(t *testing.T) {
	t.Run("word boundary underscores become italics", func(t *testing.T) {
		if got := convertUnderlineItalics("a _word_ here"); got != "a <i>word</i> here" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("snake_case is preserved", func(t *testing.T) {
		in := "hp_current and mp_max"
		if got := convertUnderlineItalics(in); got != in {
			t.Errorf("snake_case mangled: got %q, want %q", got, in)
		}
	})
	t.Run("no underscores unchanged", func(t *testing.T) {
		if got := convertUnderlineItalics("plain"); got != "plain" {
			t.Errorf("got %q", got)
		}
	})
}

func TestConvertListRuns(t *testing.T) {
	t.Run("run of markers becomes list items", func(t *testing.T) {
		in := "- one\n- two\n- three"
		want := "<li>one</li>\n<li>two</li>\n<li>three</li>"
		if got := convertListRuns(in); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("single marker left as dialogue dash", func(t *testing.T) {
		in := "- lonely line"
		if got := convertListRuns(in); got != in {
			t.Errorf("single dash converted: got %q", got)
		}
	})
	t.Run("non-list lines untouched", func(t *testing.T) {
		in := "regular paragraph\nanother line"
		if got := convertListRuns(in); got != in {
			t.Errorf("got %q", got)
		}
	})
}

func TestProcessChapter(t *testing.T) {
	t.Run("headings", func(t *testing.T) {
		got := ProcessChapter("# H1\n\n## H2\n\n### H3")
		for _, want := range []string{"<h1>H1</h1>", "<h2>H2</h2>", "<h3>H3</h3>"} {
			if !strings.Contains(got, want) {
				t.Errorf("ProcessChapter missing %q in %q", want, got)
			}
		}
	})

	t.Run("paragraph with line breaks", func(t *testing.T) {
		got := ProcessChapter("line one\nline two")
		if !strings.Contains(got, "<p>line one<br/>line two</p>") {
			t.Errorf("got %q", got)
		}
	})

	t.Run("separator becomes hr", func(t *testing.T) {
		got := ProcessChapter("before\n\n---\n\nafter")
		if !strings.Contains(got, "<hr/>") {
			t.Errorf("expected <hr/>, got %q", got)
		}
	})

	t.Run("blockquote", func(t *testing.T) {
		got := ProcessChapter("> quoted text")
		if !strings.Contains(got, "<blockquote>quoted text</blockquote>") {
			t.Errorf("got %q", got)
		}
	})

	t.Run("list block wrapped in ul", func(t *testing.T) {
		got := ProcessChapter("- a\n- b")
		if !strings.Contains(got, "<ul>") || !strings.Contains(got, "<li>a</li>") {
			t.Errorf("got %q", got)
		}
	})

	t.Run("xml escaping and emphasis", func(t *testing.T) {
		got := ProcessChapter("a & b with **bold**")
		if !strings.Contains(got, "&amp;") {
			t.Errorf("expected escaped ampersand, got %q", got)
		}
		if !strings.Contains(got, "<b>bold</b>") {
			t.Errorf("expected bold, got %q", got)
		}
	})

	t.Run("blank input yields empty", func(t *testing.T) {
		if got := ProcessChapter("   \n\n  "); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}

func TestIsLiOnlyBlock(t *testing.T) {
	if !isLiOnlyBlock("<li>a</li>\n<li>b</li>") {
		t.Error("expected li-only block to be recognized")
	}
	if isLiOnlyBlock("<li>a</li>\nplain") {
		t.Error("mixed block should not be li-only")
	}
	if isLiOnlyBlock("\n\n") {
		t.Error("empty block should not be li-only")
	}
}

package noveldownloader

import (
	"testing"
)

func TestStripLeadingTitle(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		chapterTitle string
		expected     string
	}{
		{
			name:         "single hash heading first line",
			input:        "# Chapter 1\n\nThis is the content.",
			chapterTitle: "Chapter 1",
			expected:     "This is the content.",
		},
		{
			name:         "double hash heading first line",
			input:        "## Chapter 1\n\nSome content here.",
			chapterTitle: "Chapter 1",
			expected:     "Some content here.",
		},
		{
			name:         "triple hash heading first line",
			input:        "### Chapter 1\n\nContent after heading.",
			chapterTitle: "Chapter 1",
			expected:     "Content after heading.",
		},
		{
			name:         "quadruple hash heading first line",
			input:        "#### Chapter 1\n\nContent here.",
			chapterTitle: "Chapter 1",
			expected:     "Content here.",
		},
		{
			name:         "no heading - plain text",
			input:        "This is the content.\n\nMore content.",
			chapterTitle: "Chapter 1",
			expected:     "This is the content.\n\nMore content.",
		},
		{
			name:         "heading in body, not first line",
			input:        "First paragraph.\n\n# Heading in body\n\nMore content.",
			chapterTitle: "Chapter 1",
			expected:     "First paragraph.\n\n# Heading in body\n\nMore content.",
		},
		{
			name:         "five hashes - not in range",
			input:        "##### Chapter 1\n\nContent.",
			chapterTitle: "Chapter 1",
			expected:     "##### Chapter 1\n\nContent.",
		},
		{
			name:         "empty content",
			input:        "",
			chapterTitle: "Chapter 1",
			expected:     "",
		},
		{
			name:         "only heading",
			input:        "# Chapter 1",
			chapterTitle: "Chapter 1",
			expected:     "",
		},
		{
			name:         "heading with leading newline",
			input:        "\n# Chapter 1\n\nContent.",
			chapterTitle: "Chapter 1",
			expected:     "Content.",
		},
		{
			name:         "hash without space - not a heading",
			input:        "#not a heading\n\nContent.",
			chapterTitle: "Chapter 1",
			expected:     "#not a heading\n\nContent.",
		},
		{
			name:         "single line no heading",
			input:        "Just one line of content.",
			chapterTitle: "Chapter 1",
			expected:     "Just one line of content.",
		},
		{
			name:         "first line matches title exactly",
			input:        "第1章 初次掌控\n\nContent here.",
			chapterTitle: "第1章 初次掌控",
			expected:     "Content here.",
		},
		{
			name:         "first line matches title without numeric prefix",
			input:        "第1章 初次掌控\n\nContent here.",
			chapterTitle: "1.第1章 初次掌控",
			expected:     "Content here.",
		},
		{
			name:         "first line matches title with different numeric prefix",
			input:        "第1章 初次掌控\n\nContent here.",
			chapterTitle: "01.第1章 初次掌控",
			expected:     "Content here.",
		},
		{
			name:         "first line does not match title",
			input:        "Different text\n\nContent here.",
			chapterTitle: "第1章 初次掌控",
			expected:     "Different text\n\nContent here.",
		},
		{
			name:         "empty chapter title - no removal",
			input:        "Some content\n\nMore content.",
			chapterTitle: "",
			expected:     "Some content\n\nMore content.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripLeadingTitle(tt.input, tt.chapterTitle)
			if got != tt.expected {
				t.Errorf("stripLeadingTitle() = %q, want %q", got, tt.expected)
			}
		})
	}
}

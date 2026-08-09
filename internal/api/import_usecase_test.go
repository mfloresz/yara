package api

import (
	"encoding/json"
	"testing"

	"translator-server/internal/noveldownloader"
	"translator-server/internal/store"
)

func diffChapterURL(order int, title, url string) noveldownloader.ChapterURL {
	return noveldownloader.ChapterURL{Order: order, Title: title, URL: url}
}

func TestDiffNewChapters(t *testing.T) {
	base := []noveldownloader.ChapterURL{
		diffChapterURL(1, "Chapter 1", "https://src.test/1"),
		diffChapterURL(2, "Chapter 2", "https://src.test/2"),
		diffChapterURL(3, "Chapter 3", "https://src.test/3"),
		diffChapterURL(4, "Chapter 4", "https://src.test/4"),
		diffChapterURL(5, "Chapter 5", "https://src.test/5"),
	}

	tests := []struct {
		name           string
		chapters       []noveldownloader.ChapterURL
		existingOrders map[int]bool
		existingTitles map[string]bool
		startChapter   int
		endChapter     int
		wantNew        int
		wantFirst      int
		wantLast       int
		wantStartOrder int
		wantOrders     []int
	}{
		{
			name:           "all new",
			chapters:       base,
			wantNew:        5,
			wantFirst:      1,
			wantLast:       5,
			wantStartOrder: 1,
			wantOrders:     []int{1, 2, 3, 4, 5},
		},
		{
			name:           "existing by order is skipped",
			chapters:       base,
			existingOrders: map[int]bool{2: true, 4: true},
			wantNew:        3,
			wantFirst:      1,
			wantLast:       5,
			wantStartOrder: 1,
			wantOrders:     []int{1, 3, 5},
		},
		{
			name:           "existing by title is skipped",
			chapters:       base,
			existingTitles: map[string]bool{"Chapter 3": true},
			wantNew:        4,
			wantFirst:      1,
			wantLast:       5,
			wantStartOrder: 1,
			wantOrders:     []int{1, 2, 4, 5},
		},
		{
			name:           "range filter keeps only the accepted window",
			chapters:       base,
			startChapter:   3,
			endChapter:     4,
			wantNew:        2,
			wantFirst:      3,
			wantLast:       4,
			wantStartOrder: 3,
			wantOrders:     []int{3, 4},
		},
		{
			name:           "range filter start only",
			chapters:       base,
			startChapter:   4,
			wantNew:        2,
			wantFirst:      4,
			wantLast:       5,
			wantStartOrder: 4,
			wantOrders:     []int{4, 5},
		},
		{
			name:           "range filter end only",
			chapters:       base,
			endChapter:     2,
			wantNew:        2,
			wantFirst:      1,
			wantLast:       2,
			wantStartOrder: 1,
			wantOrders:     []int{1, 2},
		},
		{
			name: "position falls back to index plus one",
			chapters: []noveldownloader.ChapterURL{
				diffChapterURL(0, "First", "https://src.test/a"),
				diffChapterURL(0, "Second", "https://src.test/b"),
			},
			wantNew:        2,
			wantFirst:      0,
			wantLast:       0,
			wantStartOrder: 1,
			wantOrders:     []int{1, 2},
		},
		{
			name: "empty title gets the position fallback title",
			chapters: []noveldownloader.ChapterURL{
				diffChapterURL(0, "", "https://src.test/x"),
			},
			wantNew:        1,
			wantFirst:      0,
			wantLast:       0,
			wantStartOrder: 1,
			wantOrders:     []int{1},
		},
		{
			name: "first and last track only numbered chapters",
			chapters: []noveldownloader.ChapterURL{
				diffChapterURL(0, "Prologue", "https://src.test/p"),
				diffChapterURL(7, "Chapter 7", "https://src.test/7"),
			},
			wantNew:        2,
			wantFirst:      7,
			wantLast:       7,
			wantStartOrder: 1,
			wantOrders:     []int{1, 7},
		},
		{
			name: "start order is the position of the first accepted chapter",
			chapters: []noveldownloader.ChapterURL{
				diffChapterURL(1, "Chapter 1", "https://src.test/1"),
				diffChapterURL(2, "Chapter 2", "https://src.test/2"),
				diffChapterURL(3, "Chapter 3", "https://src.test/3"),
			},
			existingOrders: map[int]bool{1: true, 2: true},
			wantNew:        1,
			wantFirst:      3,
			wantLast:       3,
			wantStartOrder: 3,
			wantOrders:     []int{3},
		},
		{
			name: "nothing new",
			chapters: []noveldownloader.ChapterURL{
				diffChapterURL(1, "Chapter 1", "https://src.test/1"),
				diffChapterURL(2, "Chapter 2", "https://src.test/2"),
			},
			existingOrders: map[int]bool{1: true, 2: true},
			wantNew:        0,
			wantFirst:      0,
			wantLast:       0,
			wantStartOrder: 0,
			wantOrders:     []int{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			diff := diffNewChapters(tc.chapters, tc.existingOrders, tc.existingTitles, tc.startChapter, tc.endChapter)
			if diff.newAvailable != tc.wantNew {
				t.Errorf("newAvailable: got %d, want %d", diff.newAvailable, tc.wantNew)
			}
			if diff.firstNew != tc.wantFirst {
				t.Errorf("firstNew: got %d, want %d", diff.firstNew, tc.wantFirst)
			}
			if diff.lastNew != tc.wantLast {
				t.Errorf("lastNew: got %d, want %d", diff.lastNew, tc.wantLast)
			}
			if diff.startOrder != tc.wantStartOrder {
				t.Errorf("startOrder: got %d, want %d", diff.startOrder, tc.wantStartOrder)
			}
			if len(diff.newChapters) != len(tc.wantOrders) {
				t.Fatalf("newChapters: got %d entries, want %d", len(diff.newChapters), len(tc.wantOrders))
			}
			for i, want := range tc.wantOrders {
				if diff.newChapters[i].Order != want {
					t.Errorf("newChapters[%d].Order: got %d, want %d", i, diff.newChapters[i].Order, want)
				}
			}
		})
	}
}

func TestDiffNewChaptersTitleFallback(t *testing.T) {
	diff := diffNewChapters(
		[]noveldownloader.ChapterURL{
			diffChapterURL(0, "", "https://src.test/a"),
			diffChapterURL(5, "", "https://src.test/5"),
		},
		nil, nil, 0, 0,
	)
	if diff.newChapters[0].Title != "Chapter 1" {
		t.Errorf("index-fallback title: got %q, want %q", diff.newChapters[0].Title, "Chapter 1")
	}
	if diff.newChapters[1].Title != "Chapter 5" {
		t.Errorf("order-fallback title: got %q, want %q", diff.newChapters[1].Title, "Chapter 5")
	}
	if diff.newChapters[1].Order != 5 {
		t.Errorf("order-fallback order: got %d, want 5", diff.newChapters[1].Order)
	}
}

func TestBuildDownloadJob(t *testing.T) {
	chapters := []store.DownloadChapterInfo{
		{URL: "https://src.test/3", Title: "Chapter 3", Order: 3},
		{URL: "https://src.test/4", Title: "Chapter 4", Order: 4},
	}

	tests := []struct {
		name           string
		reDownload     bool
		wantRedownload bool
	}{
		{name: "plain download has no reDownload flag", reDownload: false, wantRedownload: false},
		{name: "re-download sets the flag", reDownload: true, wantRedownload: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			job := buildDownloadJob("novel-1", "https://src.test/index", chapters, 3, "en", "es", tc.reDownload)

			if job.NovelID != "novel-1" {
				t.Errorf("NovelID: got %q, want %q", job.NovelID, "novel-1")
			}
			if job.Status != "pending" {
				t.Errorf("Status: got %q, want %q", job.Status, "pending")
			}
			if job.Operation != "download" {
				t.Errorf("Operation: got %q, want %q", job.Operation, "download")
			}
			if job.ChapterIDs != "[]" {
				t.Errorf("ChapterIDs: got %q, want %q", job.ChapterIDs, "[]")
			}
			if job.TotalChapters != 2 {
				t.Errorf("TotalChapters: got %d, want 2", job.TotalChapters)
			}

			var options map[string]any
			if err := json.Unmarshal([]byte(job.OptionsJSON), &options); err != nil {
				t.Fatalf("decode options JSON: %v", err)
			}
			if got, _ := options["url"].(string); got != "https://src.test/index" {
				t.Errorf("options.url: got %q, want %q", got, "https://src.test/index")
			}
			if got := int(options["startOrder"].(float64)); got != 3 {
				t.Errorf("options.startOrder: got %d, want 3", got)
			}
			if got, _ := options["sourceLanguage"].(string); got != "en" {
				t.Errorf("options.sourceLanguage: got %q, want %q", got, "en")
			}
			if got, _ := options["targetLanguage"].(string); got != "es" {
				t.Errorf("options.targetLanguage: got %q, want %q", got, "es")
			}
			rawChapters, ok := options["chapters"].([]any)
			if !ok || len(rawChapters) != 2 {
				t.Fatalf("options.chapters: got %#v, want 2 entries", options["chapters"])
			}
			first, ok := rawChapters[0].(map[string]any)
			if !ok {
				t.Fatalf("options.chapters[0]: not an object")
			}
			if got, _ := first["url"].(string); got != "https://src.test/3" {
				t.Errorf("options.chapters[0].url: got %q, want %q", got, "https://src.test/3")
			}
			if got, _ := first["title"].(string); got != "Chapter 3" {
				t.Errorf("options.chapters[0].title: got %q, want %q", got, "Chapter 3")
			}
			if got := int(first["order"].(float64)); got != 3 {
				t.Errorf("options.chapters[0].order: got %d, want 3", got)
			}
			_, hasRedownload := options["reDownload"]
			if hasRedownload != tc.wantRedownload {
				t.Errorf("options.reDownload present: got %v, want %v", hasRedownload, tc.wantRedownload)
			}
			if tc.wantRedownload {
				if got := options["reDownload"].(bool); !got {
					t.Errorf("options.reDownload: got false, want true")
				}
			}
		})
	}
}

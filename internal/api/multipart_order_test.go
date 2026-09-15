package api

import (
	"testing"

	"translator-server/internal/noveldownloader"
	"translator-server/internal/store"
)

// Multi-part source chapters share one site number (e.g. "ASFTB 23 ... 1" and
// "ASFTB 23 ... 2" both parse to 23). Regression test: claiming orders for a
// batch must never hand out a duplicate that would hit the
// (novel, chapter_order) unique index.
func TestClaimChapterOrderMultipart(t *testing.T) {
	chs := []noveldownloader.ChapterURL{
		{URL: "https://example.com/23-1", Title: "ASFTB 23: Have You Forgotten Who Your Man Is 1"},
		{URL: "https://example.com/23-2", Title: "ASFTB 23: Have You Forgotten Who Your Man Is 2"},
		{URL: "https://example.com/24", Title: "ASFTB 24: Li Lingfeng Isn't Going to Beat Me to Death, Right?"},
	}
	seen := map[int]bool{}
	orders := make([]int, 0, len(chs))
	for i, ch := range chs {
		orders = append(orders, claimChapterOrder(seen, chapterOrderOf(ch), i+1))
	}
	if orders[0] != 23 {
		t.Errorf("first part should keep the site number 23, got %d", orders[0])
	}
	seenCheck := map[int]bool{}
	for _, o := range orders {
		if seenCheck[o] {
			t.Fatalf("duplicate order %d in %v", o, orders)
		}
		seenCheck[o] = true
	}
	if orders[1] == 23 {
		t.Errorf("second part must not reuse 23, got %v", orders)
	}
}

// A re-download plan must not schedule two source entries onto the same
// stored record: the second part would overwrite the first part's content.
func TestPlanRedownloadMultipartSchedulesOnce(t *testing.T) {
	existing := store.Chapter{ID: "ch23", ChapterOrder: 23, Title: "ASFTB 23: Have You Forgotten Who Your Man Is 1"}
	plan := planRedownload(
		[]noveldownloader.ChapterURL{
			{URL: "https://example.com/23-1", Title: "ASFTB 23: Have You Forgotten Who Your Man Is 1"},
			{URL: "https://example.com/23-2", Title: "ASFTB 23: Have You Forgotten Who Your Man Is 2"},
		},
		map[int]store.Chapter{23: existing},
		map[string]store.Chapter{existing.Title: existing},
		0, 0,
	)
	if len(plan.chapters) != 1 {
		t.Fatalf("expected 1 scheduled chapter, got %d", len(plan.chapters))
	}
	if plan.chapters[0].ChapterID != "ch23" {
		t.Errorf("expected ChapterID ch23, got %q", plan.chapters[0].ChapterID)
	}
}

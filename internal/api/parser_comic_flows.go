package api

import (
	"context"
	"fmt"
	"time"

	"translator-server/internal/comicdownloader"
	"translator-server/internal/store"
)

// fetchChapterPagesThroughScript resolves the script for the comic's source
// URL and returns one chapter's title plus its page image URLs in reading
// order. The comic URL scopes resolution; the chapter URL is the page to
// parse.
func (s *Server) fetchChapterPagesThroughScript(ctx context.Context, userID, comicURL, chapterURL, chapterTitle string) (title string, pages []string, err error) {
	entry, err := s.resolveScriptForExecution(ctx, userID, comicURL)
	if err != nil {
		return "", nil, err
	}
	script, err := s.routedScript(userID, entry)
	if err != nil {
		return "", nil, err
	}
	chapter, err := script.Chapter(ctx, chapterURL)
	if err != nil {
		return "", nil, fmt.Errorf("parser %s: %w", entry.name(), err)
	}
	title = chapter.Title
	if title == "" {
		title = chapterTitle
	}
	return title, chapter.Pages, nil
}

// downloadComicChapterPages fetches one chapter's page URLs through its
// script, retrying transient failures with backoff (same policy as the novel
// chapter retry).
func (s *Server) downloadComicChapterPages(ctx context.Context, userID, comicURL string, chInfo store.DownloadComicChapterInfo) (*comicdownloader.Chapter, error) {
	var lastErr error
	for attempt := 0; attempt <= chapterDownloadMaxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt) * chapterDownloadRetryBaseDelay
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
		title, pages, err := s.fetchChapterPagesThroughScript(ctx, userID, comicURL, chInfo.URL, chInfo.Title)
		if err == nil && len(pages) > 0 {
			return &comicdownloader.Chapter{Title: title, Pages: pages, SourceURL: chInfo.URL}, nil
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("empty download result for %s", chInfo.URL)
		}
	}
	return nil, lastErr
}

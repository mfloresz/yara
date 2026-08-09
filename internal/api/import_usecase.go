package api

import (
	"encoding/json"
	"fmt"

	"translator-server/internal/noveldownloader"
	"translator-server/internal/store"
)

// chapterDiff summarizes which chapters of a freshly parsed source list are
// new relative to the stored chapters of a novel.
type chapterDiff struct {
	// newAvailable is the number of accepted (new) chapters.
	newAvailable int
	// firstNew/lastNew are the lowest/highest chapter numbers among the new
	// chapters that carry a known number (chNum > 0).
	firstNew int
	lastNew  int
	// startOrder is the position of the first accepted chapter.
	startOrder int
	// newChapters carries the accepted chapters ready for a download job.
	newChapters []store.DownloadChapterInfo
}

// diffNewChapters filters a freshly parsed chapter list against the chapters
// already stored for a novel (by order number and by title) and optionally by
// a chapter range. pos is the chapter number when the source knows it, and the
// list index + 1 otherwise. Callers that only need the counts ignore
// newChapters.
func diffNewChapters(chapters []noveldownloader.ChapterURL, existingOrders map[int]bool, existingTitles map[string]bool, startChapter, endChapter int) chapterDiff {
	diff := chapterDiff{newChapters: make([]store.DownloadChapterInfo, 0, len(chapters))}
	for i, ch := range chapters {
		chNum := chapterOrderOf(ch)
		if chNum > 0 && existingOrders[chNum] {
			continue
		}
		if existingTitles[ch.Title] {
			continue
		}
		pos := chNum
		if pos <= 0 {
			pos = i + 1
		}
		if startChapter > 0 && pos < startChapter {
			continue
		}
		if endChapter > 0 && pos > endChapter {
			continue
		}
		diff.newAvailable++
		if diff.startOrder == 0 {
			diff.startOrder = pos
		}
		if chNum > 0 {
			if diff.firstNew == 0 || chNum < diff.firstNew {
				diff.firstNew = chNum
			}
			if chNum > diff.lastNew {
				diff.lastNew = chNum
			}
		}
		chTitle := ch.Title
		if chTitle == "" {
			chTitle = fmt.Sprintf("Chapter %d", pos)
		}
		chOrder := chapterOrderOf(ch)
		if chOrder <= 0 {
			chOrder = pos
		}
		diff.newChapters = append(diff.newChapters, store.DownloadChapterInfo{
			URL:   ch.URL,
			Title: chTitle,
			Order: chOrder,
		})
	}
	return diff
}

// buildDownloadJob builds a pending download job plus its options JSON for
// fetching chapters from url. reDownload marks the job as a re-download
// (existing chapters get their original content refreshed in place).
func buildDownloadJob(novelID, url string, chapters []store.DownloadChapterInfo, startOrder int, sourceLang, targetLang string, reDownload bool) *store.Job {
	options := map[string]any{
		"url":            url,
		"chapters":       chapters,
		"startOrder":     startOrder,
		"sourceLanguage": sourceLang,
		"targetLanguage": targetLang,
	}
	if reDownload {
		options["reDownload"] = true
	}
	optionsJSON, _ := json.Marshal(options)
	return &store.Job{
		NovelID:       novelID,
		Status:        "pending",
		Operation:     "download",
		ChapterIDs:    "[]",
		OptionsJSON:   string(optionsJSON),
		TotalChapters: len(chapters),
	}
}

// createAndEnqueueDownloadJob persists job and hands it to the download
// worker. On success jobID is the persisted job id. queueFull reports an
// enqueue rejection (enqueueJob already persisted the job as failed); the
// caller decides how to surface it. err is only set when the job could not be
// created.
func (s *Server) createAndEnqueueDownloadJob(userID string, job *store.Job) (jobID string, queueFull bool, err error) {
	if err := s.Store.CreateJob(userID, job); err != nil {
		return "", false, err
	}
	if !s.enqueueJob(job.ID) {
		return job.ID, true, nil
	}
	return job.ID, false, nil
}

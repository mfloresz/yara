package store

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func (s *Store) ListChaptersAccessible(userID, novelID string) ([]Chapter, error) {
	if _, err := s.GetNovelAccessible(userID, novelID); err != nil {
		return nil, err
	}
	// Reading/display/export order is the user-controlled position; excluded
	// chapters are hidden everywhere except restore-oriented listings.
	records, err := s.App.FindRecordsByFilter(ChaptersCollection, "novel = {:novel} && excluded = false", "position,chapter_order", 5000, 0, dbx.Params{"novel": novelID})
	if err != nil {
		return nil, err
	}
	out := make([]Chapter, 0, len(records))
	for _, record := range records {
		out = append(out, chapterFromRecord(record))
	}
	return out, nil
}

func (s *Store) ListAllChapterSummariesAccessible(userID, novelID string) ([]ChapterSummary, error) {
	if _, err := s.GetNovelAccessible(userID, novelID); err != nil {
		return nil, err
	}
	records, err := s.App.FindRecordsByFilter(ChaptersCollection, "novel = {:novel} && excluded = false", "position,chapter_order", 5000, 0, dbx.Params{"novel": novelID})
	if err != nil {
		return nil, err
	}
	out := make([]ChapterSummary, 0, len(records))
	for _, record := range records {
		out = append(out, chapterSummaryFromRecord(record))
	}
	return out, nil
}

func (s *Store) ListEligibleChapterSummariesAccessible(userID, novelID, operation string) ([]ChapterSummary, error) {
	if _, err := s.GetNovelAccessible(userID, novelID); err != nil {
		return nil, err
	}
	var filter string
	switch operation {
	case "refine":
		filter = "novel = {:novel} && excluded = false && (status = 'translated' || status = 'failed') && translated_content != ''"
	default:
		operation = "translate"
		filter = "novel = {:novel} && excluded = false && (status = 'pending' || status = 'failed') && original_content != ''"
	}
	records, err := s.App.FindRecordsByFilter(ChaptersCollection, filter, "position,chapter_order", 5000, 0, dbx.Params{"novel": novelID})
	if err != nil {
		return nil, err
	}
	out := make([]ChapterSummary, 0, len(records))
	for _, record := range records {
		out = append(out, chapterSummaryFromRecord(record))
	}
	return out, nil
}

func (s *Store) ListChapterSummariesAccessible(userID, novelID string, limit, offset int) ([]ChapterSummary, int, error) {
	novel, err := s.GetNovelAccessible(userID, novelID)
	if err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 5000 {
		limit = 5000
	}
	if offset < 0 {
		offset = 0
	}
	records, err := s.App.FindRecordsByFilter(ChaptersCollection, "novel = {:novel} && excluded = false", "position,chapter_order", limit, offset, dbx.Params{"novel": novelID})
	if err != nil {
		return nil, 0, err
	}
	out := make([]ChapterSummary, 0, len(records))
	for _, record := range records {
		out = append(out, chapterSummaryFromRecord(record))
	}
	return out, novel.ChapterCount, nil
}

func (s *Store) GetChapterStatsAccessible(userID, novelID string) (*ChapterStats, error) {
	novel, err := s.GetNovelAccessible(userID, novelID)
	if err != nil {
		return nil, err
	}
	return &ChapterStats{
		TotalChapters:        novel.ChapterCount,
		CompletedChapters:    novel.CompletedCount,
		TranslatedChapters:   novel.TranslatedCount,
		OriginalCharacters:   novel.OriginalCharCount,
		TranslatedCharacters: novel.TranslatedCharCount,
		RefinedCharacters:    novel.RefinedCharCount,
		TotalCharacters:      novel.TotalCharCount,
		MaxChapterOrder:      novel.MaxChapterOrder,
	}, nil
}

func (s *Store) RecalculateNovelStats(novelID string) error {
	novelRecord, err := s.App.FindRecordById(NovelsCollection, novelID)
	if err != nil {
		return ErrNotFound
	}
	records, err := s.App.FindRecordsByFilter(ChaptersCollection, "novel = {:novel}", "", 5000, 0, dbx.Params{"novel": novelID})
	if err != nil {
		return err
	}
	stats := &ChapterStats{}
	for _, record := range records {
		order := asInt(record.GetFloat("chapter_order"), 0)
		if order > stats.MaxChapterOrder {
			stats.MaxChapterOrder = order
		}
		// Excluded chapters keep their record/content/source order but must not
		// count toward visible chapter counts, progress, or character totals.
		if record.GetBool("excluded") {
			continue
		}
		stats.TotalChapters++
		status := record.GetString("status")
		if status == "translated" || status == "refined" || status == "done" {
			stats.TranslatedChapters++
		}
		if status == "refined" || status == "done" {
			stats.CompletedChapters++
		}
		originalChars, translatedChars, refinedChars := charCountsFromRecord(record)
		stats.OriginalCharacters += originalChars
		stats.TranslatedCharacters += translatedChars
		stats.RefinedCharacters += refinedChars
	}
	stats.TotalCharacters = stats.OriginalCharacters + stats.TranslatedCharacters + stats.RefinedCharacters
	novelRecord.Set("chapter_count", stats.TotalChapters)
	novelRecord.Set("translated_count", stats.TranslatedChapters)
	novelRecord.Set("completed_count", stats.CompletedChapters)
	novelRecord.Set("original_char_count", stats.OriginalCharacters)
	novelRecord.Set("translated_char_count", stats.TranslatedCharacters)
	novelRecord.Set("refined_char_count", stats.RefinedCharacters)
	novelRecord.Set("total_char_count", stats.TotalCharacters)
	novelRecord.Set("max_chapter_order", stats.MaxChapterOrder)
	return s.App.Save(novelRecord)
}

func setCharCounts(record *core.Record, original, translated, refined string) {
	record.Set("original_char_count", len(original))
	record.Set("translated_char_count", len(translated))
	record.Set("refined_char_count", len(refined))
}

func charCountsFromRecord(record *core.Record) (original, translated, refined int) {
	return asInt(record.GetFloat("original_char_count"), 0),
		asInt(record.GetFloat("translated_char_count"), 0),
		asInt(record.GetFloat("refined_char_count"), 0)
}

func chapterSummaryFromRecord(record *core.Record) ChapterSummary {
	original := record.GetString("original_content")
	translated := record.GetString("translated_content")
	refined := record.GetString("refined_content")
	originalChars, translatedChars, refinedChars := charCountsFromRecord(record)
	return ChapterSummary{
		ID:                   record.Id,
		NovelID:              record.GetString("novel"),
		ChapterOrder:         asInt(record.GetFloat("chapter_order"), 0),
		Position:             asInt(record.GetFloat("position"), 0),
		Excluded:             record.GetBool("excluded"),
		Title:                record.GetString("title"),
		TranslatedTitle:      record.GetString("translated_title"),
		Status:               defaultString(record.GetString("status"), "pending"),
		ErrorMessage:         record.GetString("error_message"),
		HasOriginalContent:   strings.TrimSpace(original) != "",
		HasTranslatedContent: strings.TrimSpace(translated) != "",
		HasRefinedContent:    strings.TrimSpace(refined) != "",
		OriginalChars:        originalChars,
		TranslatedChars:      translatedChars,
		RefinedChars:         refinedChars,
		CreatedAt:            record.GetDateTime("created").String(),
		UpdatedAt:            record.GetDateTime("updated").String(),
	}
}

func (s *Store) GetChapterAccessible(userID, novelID, chapterID string) (*Chapter, error) {
	novel, err := s.GetNovelAccessible(userID, novelID)
	if err != nil {
		return nil, err
	}
	record, err := s.App.FindRecordById(ChaptersCollection, chapterID)
	if err != nil || record.GetString("novel") != novelID {
		return nil, ErrNotFound
	}
	// Excluded chapters are private to the owner: non-owners must not be able
	// to read them by ID even when the novel is public.
	if record.GetBool("excluded") && novel.OwnerID != userID {
		return nil, ErrNotFound
	}
	chapter := chapterFromRecord(record)
	return &chapter, nil
}

func (s *Store) UpsertChapter(userID, novelID string, chapter *Chapter) (*Chapter, error) {
	return s.upsertChapter(userID, novelID, chapter, true)
}

func (s *Store) UpsertChapterWithoutStats(userID, novelID string, chapter *Chapter) (*Chapter, error) {
	return s.upsertChapter(userID, novelID, chapter, false)
}

func (s *Store) upsertChapter(userID, novelID string, chapter *Chapter, recalcStats bool) (*Chapter, error) {
	if _, err := s.GetOwnedNovel(userID, novelID); err != nil {
		return nil, err
	}
	var record *core.Record
	var err error
	isNew := false
	if strings.TrimSpace(chapter.ID) != "" {
		record, err = s.App.FindRecordById(ChaptersCollection, chapter.ID)
		if err != nil {
			return nil, ErrNotFound
		}
		if record.GetString("novel") != novelID {
			return nil, ErrForbidden
		}
	} else {
		collection, cErr := s.App.FindCollectionByNameOrId(ChaptersCollection)
		if cErr != nil {
			return nil, cErr
		}
		record = core.NewRecord(collection)
		record.Set("novel", novelID)
		isNew = true
	}
	// New chapters append after the current maximum position and are never
	// created excluded. Existing records keep their position/excluded unless
	// explicitly changed through the reorder/visibility endpoints.
	if isNew {
		maxPos, posErr := s.maxChapterPosition(novelID)
		if posErr != nil {
			return nil, posErr
		}
		record.Set("position", maxPos+1)
		record.Set("excluded", false)
	}
	status := strings.TrimSpace(chapter.Status)
	if status == "" {
		status = record.GetString("status")
	}
	if status == "" {
		status = "pending"
	}

	record.Set("chapter_order", chapter.ChapterOrder)
	if chapter.Title != "" {
		record.Set("title", chapter.Title)
	} else if record.IsNew() {
		record.Set("title", "")
	}
	if chapter.TranslatedTitle != "" {
		record.Set("translated_title", chapter.TranslatedTitle)
	} else if record.IsNew() {
		record.Set("translated_title", "")
	}
	if chapter.OriginalContent != "" {
		record.Set("original_content", chapter.OriginalContent)
	} else if record.IsNew() {
		record.Set("original_content", "")
	}
	if chapter.TranslatedContent != "" {
		record.Set("translated_content", chapter.TranslatedContent)
	} else if record.IsNew() {
		record.Set("translated_content", "")
	}
	if chapter.RefinedContent != "" {
		record.Set("refined_content", chapter.RefinedContent)
	} else if record.IsNew() {
		record.Set("refined_content", "")
	}
	if chapter.OriginalContent != "" || chapter.TranslatedContent != "" || chapter.RefinedContent != "" || record.IsNew() {
		setCharCounts(record,
			record.GetString("original_content"),
			record.GetString("translated_content"),
			record.GetString("refined_content"),
		)
	}
	record.Set("status", status)
	if chapter.ErrorMessage != "" {
		record.Set("error_message", chapter.ErrorMessage)
	} else if record.IsNew() {
		record.Set("error_message", "")
	}
	if err := s.App.Save(record); err != nil {
		return nil, err
	}
	if recalcStats {
		if err := s.RecalculateNovelStats(novelID); err != nil {
			return nil, err
		}
	}
	stored := chapterFromRecord(record)
	return &stored, nil
}

// ExcludeChapter logically deletes a chapter: the record, content, ID and
// source order are retained, but the chapter is hidden from normal lists,
// eligible jobs, navigation and EPUB exports until restored.
func (s *Store) ExcludeChapter(userID, novelID, chapterID string) error {
	return s.SetChapterExcluded(userID, novelID, chapterID, true)
}

// SetChapterExcluded flips the visibility flag of a single chapter (exclude
// or restore). Excluding is rejected while the novel has active jobs so an
// in-flight download/translate/refine job is never silently mutated.
func (s *Store) SetChapterExcluded(userID, novelID, chapterID string, excluded bool) error {
	if _, err := s.GetOwnedNovel(userID, novelID); err != nil {
		return err
	}
	record, err := s.App.FindRecordById(ChaptersCollection, chapterID)
	if err != nil || record.GetString("novel") != novelID {
		return ErrNotFound
	}
	if record.GetBool("excluded") == excluded {
		return nil
	}
	if excluded {
		active, err := s.HasActiveJobsForNovel(novelID)
		if err != nil {
			return err
		}
		if active {
			return ErrActiveJobs
		}
	}
	record.Set("excluded", excluded)
	if err := s.App.Save(record); err != nil {
		return err
	}
	return s.RecalculateNovelStats(novelID)
}

// BulkExcludeChapters logically deletes several chapters at once. Chapters
// that do not belong to the novel are skipped, mirroring the previous
// bulk-delete behavior.
func (s *Store) BulkExcludeChapters(userID, novelID string, ids []string) (int, error) {
	if _, err := s.GetOwnedNovel(userID, novelID); err != nil {
		return 0, err
	}
	if len(ids) > 0 {
		active, err := s.HasActiveJobsForNovel(novelID)
		if err != nil {
			return 0, err
		}
		if active {
			return 0, ErrActiveJobs
		}
	}
	excluded := 0
	for _, id := range ids {
		record, err := s.App.FindRecordById(ChaptersCollection, id)
		if err != nil || record.GetString("novel") != novelID {
			continue
		}
		if record.GetBool("excluded") {
			excluded++
			continue
		}
		record.Set("excluded", true)
		if err := s.App.Save(record); err != nil {
			return excluded, err
		}
		excluded++
	}
	if err := s.RecalculateNovelStats(novelID); err != nil {
		return excluded, err
	}
	return excluded, nil
}

func (s *Store) UpdateChapterStatus(chapterID, status, errorMessage string) error {
	record, err := s.App.FindRecordById(ChaptersCollection, chapterID)
	if err != nil {
		return ErrNotFound
	}
	record.Set("status", status)
	record.Set("error_message", errorMessage)
	if err := s.App.Save(record); err != nil {
		return err
	}
	return s.RecalculateNovelStats(record.GetString("novel"))
}

func (s *Store) UpdateChapterStatusForUser(userID, novelID, chapterID, status, errorMessage string) error {
	if _, err := s.GetOwnedNovel(userID, novelID); err != nil {
		return err
	}
	record, err := s.App.FindRecordById(ChaptersCollection, chapterID)
	if err != nil || record.GetString("novel") != novelID {
		return ErrNotFound
	}
	record.Set("status", status)
	record.Set("error_message", errorMessage)
	if err := s.App.Save(record); err != nil {
		return err
	}
	return s.RecalculateNovelStats(novelID)
}

func (s *Store) SaveChapterTranslation(chapterID, translatedTitle, translatedContent, refinedContent, status string) error {
	return s.saveChapterTranslation(chapterID, translatedTitle, translatedContent, refinedContent, status, true)
}

func (s *Store) SaveRefinedContentIfUnchanged(chapterID, expectedTranslatedContent, refinedContent, status string) (applied bool, err error) {
	record, err := s.App.FindRecordById(ChaptersCollection, chapterID)
	if err != nil {
		return false, ErrNotFound
	}
	if record.GetString("translated_content") != expectedTranslatedContent {
		return false, nil
	}
	if refinedContent != "" {
		record.Set("refined_content", refinedContent)
	}
	if status != "" {
		record.Set("status", status)
	}
	record.Set("error_message", "")
	setCharCounts(record, record.GetString("original_content"), record.GetString("translated_content"), record.GetString("refined_content"))
	// Re-fetch and re-verify right before save to narrow the race window
	// with any concurrent goroutine that might have modified the chapter.
	fresh, err := s.App.FindRecordById(ChaptersCollection, chapterID)
	if err != nil {
		return false, ErrNotFound
	}
	if fresh.GetString("translated_content") != expectedTranslatedContent {
		return false, nil
	}
	if err := s.App.Save(record); err != nil {
		return false, err
	}
	if err := s.RecalculateNovelStats(record.GetString("novel")); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) SaveChapterTranslationFast(chapterID, translatedTitle, translatedContent, refinedContent, status string) error {
	return s.saveChapterTranslation(chapterID, translatedTitle, translatedContent, refinedContent, status, false)
}

func (s *Store) saveChapterTranslation(chapterID, translatedTitle, translatedContent, refinedContent, status string, recalcStats bool) error {
	record, err := s.App.FindRecordById(ChaptersCollection, chapterID)
	if err != nil {
		return ErrNotFound
	}
	if translatedTitle != "" {
		record.Set("translated_title", translatedTitle)
	}
	if translatedContent != "" {
		record.Set("translated_content", translatedContent)
	}
	if refinedContent != "" {
		record.Set("refined_content", refinedContent)
	}
	if status != "" {
		record.Set("status", status)
	}
	record.Set("error_message", "")
	setCharCounts(record, record.GetString("original_content"), record.GetString("translated_content"), record.GetString("refined_content"))
	if err := s.App.Save(record); err != nil {
		return err
	}
	if !recalcStats {
		return nil
	}
	return s.RecalculateNovelStats(record.GetString("novel"))
}

func (s *Store) UpdateChapterStatusFast(chapterID, status, errorMessage string) error {
	record, err := s.App.FindRecordById(ChaptersCollection, chapterID)
	if err != nil {
		return ErrNotFound
	}
	record.Set("status", status)
	record.Set("error_message", errorMessage)
	return s.App.Save(record)
}

func (s *Store) UpdateChaptersStatusFast(chapterIDs []string, status, errorMessage string) error {
	for _, chapterID := range chapterIDs {
		if strings.TrimSpace(chapterID) == "" {
			continue
		}
		record, err := s.App.FindRecordById(ChaptersCollection, chapterID)
		if err != nil {
			continue
		}
		record.Set("status", status)
		record.Set("error_message", errorMessage)
		if err := s.App.Save(record); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ReconcileProcessingChaptersForJob(jobID string) error {
	job, err := s.GetJob(jobID)
	if err != nil {
		return err
	}
	chapterIDs := []string{}
	if trimmed := strings.TrimSpace(job.ChapterIDs); trimmed != "" && trimmed != "[]" {
		if err := json.Unmarshal([]byte(trimmed), &chapterIDs); err != nil {
			return err
		}
	} else {
		// Jobs without explicit chapter ids cover the whole novel (same rule as
		// LoadJobChapters), so the chapters marked processing by the handler must
		// be derived the same way instead of iterating an empty list.
		chapters, _, err := s.LoadJobChapters(job)
		if err != nil {
			return err
		}
		chapterIDs = make([]string, 0, len(chapters))
		for _, chapter := range chapters {
			chapterIDs = append(chapterIDs, chapter.ID)
		}
	}
	mutated := false
	for _, chapterID := range chapterIDs {
		record, err := s.App.FindRecordById(ChaptersCollection, chapterID)
		if err != nil {
			continue
		}
		if record.GetString("status") != "processing" {
			continue
		}
		status := "pending"
		if strings.TrimSpace(record.GetString("refined_content")) != "" {
			status = "refined"
		} else if strings.TrimSpace(record.GetString("translated_content")) != "" {
			status = "translated"
		}
		record.Set("status", status)
		record.Set("error_message", "")
		if err := s.App.Save(record); err != nil {
			return err
		}
		mutated = true
	}
	if mutated {
		return s.RecalculateNovelStats(job.NovelID)
	}
	return nil
}

func (s *Store) GetMaxChapterOrder(userID, novelID string) (int, error) {
	if _, err := s.GetNovelAccessible(userID, novelID); err != nil {
		return 0, err
	}
	records, err := s.App.FindRecordsByFilter(ChaptersCollection, "novel = {:novel}", "-chapter_order", 1, 0, dbx.Params{"novel": novelID})
	if err != nil {
		return 0, err
	}
	if len(records) == 0 {
		return 0, nil
	}
	return asInt(records[0].GetFloat("chapter_order"), 0), nil
}

func (s *Store) GetExistingChapterURLs(userID, novelID string) (map[string]bool, error) {
	if _, err := s.GetNovelAccessible(userID, novelID); err != nil {
		return nil, err
	}
	total, err := s.totalChapterCount(novelID)
	if err != nil {
		return nil, err
	}
	// Source synchronization must consider excluded chapters as existing so a
	// later import never recreates an intentionally excluded chapter.
	records, err := s.App.FindRecordsByFilter(ChaptersCollection, "novel = {:novel}", "chapter_order", dynamicChapterLimit(total), 0, dbx.Params{"novel": novelID})
	if err != nil {
		return nil, err
	}
	existing := make(map[string]bool, len(records))
	for _, record := range records {
		title := record.GetString("title")
		if title != "" {
			existing[title] = true
		}
	}
	return existing, nil
}

func (s *Store) GetExistingChapterOrders(userID, novelID string) (map[int]bool, error) {
	if _, err := s.GetNovelAccessible(userID, novelID); err != nil {
		return nil, err
	}
	total, err := s.totalChapterCount(novelID)
	if err != nil {
		return nil, err
	}
	// Same policy as GetExistingChapterURLs: excluded records still occupy
	// their source order and must not be reported as missing.
	records, err := s.App.FindRecordsByFilter(ChaptersCollection, "novel = {:novel}", "chapter_order", dynamicChapterLimit(total), 0, dbx.Params{"novel": novelID})
	if err != nil {
		return nil, err
	}
	existing := make(map[int]bool, len(records))
	for _, record := range records {
		order := asInt(record.GetFloat("chapter_order"), 0)
		if order > 0 {
			existing[order] = true
		}
	}
	return existing, nil
}

type ChapterGap struct {
	From  int `json:"from"`
	To    int `json:"to"`
	Count int `json:"count"`
}

func (s *Store) GetChapterGaps(userID, novelID string) ([]ChapterGap, error) {
	if _, err := s.GetNovelAccessible(userID, novelID); err != nil {
		return nil, err
	}
	total, err := s.totalChapterCount(novelID)
	if err != nil {
		return nil, err
	}
	// Gap detection includes excluded records: an excluded chapter still
	// occupies its source order, so it must not be reported as missing.
	records, err := s.App.FindRecordsByFilter(ChaptersCollection, "novel = {:novel} && chapter_order > 0", "chapter_order", dynamicChapterLimit(total), 0, dbx.Params{"novel": novelID})
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, nil
	}
	orders := make([]int, 0, len(records))
	for _, record := range records {
		orders = append(orders, asInt(record.GetFloat("chapter_order"), 0))
	}
	var gaps []ChapterGap
	for i := 1; i < len(orders); i++ {
		prev := orders[i-1]
		curr := orders[i]
		if curr-prev > 1 {
			gaps = append(gaps, ChapterGap{
				From:  prev + 1,
				To:    curr - 1,
				Count: curr - prev - 1,
			})
		}
	}
	return gaps, nil
}

// GetExcludedChapterOrders returns the source orders of excluded chapters.
// The frontend uses this to suppress fake "missing chapter" warnings when
// visible chapter numbers jump across an excluded chapter.
func (s *Store) GetExcludedChapterOrders(userID, novelID string) ([]int, error) {
	if _, err := s.GetNovelAccessible(userID, novelID); err != nil {
		return nil, err
	}
	records, err := s.App.FindRecordsByFilter(ChaptersCollection, "novel = {:novel} && excluded = true && chapter_order > 0", "chapter_order", 5000, 0, dbx.Params{"novel": novelID})
	if err != nil {
		return nil, err
	}
	orders := make([]int, 0, len(records))
	for _, record := range records {
		orders = append(orders, asInt(record.GetFloat("chapter_order"), 0))
	}
	return orders, nil
}

// totalChapterCount counts every chapter record of a novel, including
// excluded ones, so source-matching queries can size their fetch limit
// without dropping excluded records at the tail.
func (s *Store) totalChapterCount(novelID string) (int, error) {
	total, err := s.App.CountRecords(ChaptersCollection, dbx.HashExp{"novel": novelID})
	if err != nil {
		return 0, err
	}
	return int(total), nil
}

func dynamicChapterLimit(chapterCount int) int {
	limit := chapterCount + 500
	if limit < 5000 {
		return 5000
	}
	return limit
}

// GetMaxChapterPosition returns the highest user-controlled position across
// all chapters of the novel, including excluded ones. New chapters append
// after this value.
func (s *Store) GetMaxChapterPosition(userID, novelID string) (int, error) {
	if _, err := s.GetNovelAccessible(userID, novelID); err != nil {
		return 0, err
	}
	return s.maxChapterPosition(novelID)
}

func (s *Store) maxChapterPosition(novelID string) (int, error) {
	records, err := s.App.FindRecordsByFilter(ChaptersCollection, "novel = {:novel}", "-position", 1, 0, dbx.Params{"novel": novelID})
	if err != nil {
		return 0, err
	}
	if len(records) == 0 {
		return 0, nil
	}
	return asInt(records[0].GetFloat("position"), 0), nil
}

// ListExcludedChapterSummariesAccessible lists logically deleted chapters so
// the owner can restore them. Excluded chapters are private: only the owner
// may list them, even for public novels. They are sorted by source order.
func (s *Store) ListExcludedChapterSummariesAccessible(userID, novelID string) ([]ChapterSummary, error) {
	if _, err := s.GetOwnedNovel(userID, novelID); err != nil {
		return nil, err
	}
	records, err := s.App.FindRecordsByFilter(ChaptersCollection, "novel = {:novel} && excluded = true", "chapter_order", 5000, 0, dbx.Params{"novel": novelID})
	if err != nil {
		return nil, err
	}
	out := make([]ChapterSummary, 0, len(records))
	for _, record := range records {
		out = append(out, chapterSummaryFromRecord(record))
	}
	return out, nil
}

// ReorderChapters applies a complete, atomic reorder of a novel's chapters.
//
// Contract: chapterIds must contain every chapter of the novel (visible and
// excluded) exactly once — a permutation. Positions are assigned densely
// 1..N in the given order. Empty, duplicate, foreign or partial lists are
// rejected with ErrInvalidReorder. The operation is rejected with
// ErrActiveJobs while the novel has pending/running jobs so in-flight
// processing is never silently reordered. chapter_order (the source order) is
// never touched, and IDs/content/status/progress stay stable.
func (s *Store) ReorderChapters(userID, novelID string, chapterIDs []string) error {
	if _, err := s.GetOwnedNovel(userID, novelID); err != nil {
		return err
	}
	if len(chapterIDs) == 0 {
		return fmt.Errorf("%w: chapterIds must not be empty", ErrInvalidReorder)
	}
	seen := make(map[string]struct{}, len(chapterIDs))
	for _, id := range chapterIDs {
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("%w: chapterIds contains an empty id", ErrInvalidReorder)
		}
		if _, dup := seen[id]; dup {
			return fmt.Errorf("%w: duplicate chapter id %q", ErrInvalidReorder, id)
		}
		seen[id] = struct{}{}
	}

	all, err := s.App.FindRecordsByFilter(ChaptersCollection, "novel = {:novel}", "chapter_order", 5000, 0, dbx.Params{"novel": novelID})
	if err != nil {
		return err
	}
	if len(all) != len(chapterIDs) {
		return fmt.Errorf("%w: expected %d chapter ids (every chapter of the novel), got %d", ErrInvalidReorder, len(all), len(chapterIDs))
	}
	byID := make(map[string]*core.Record, len(all))
	for _, record := range all {
		byID[record.Id] = record
	}
	for _, id := range chapterIDs {
		if byID[id] == nil {
			return fmt.Errorf("%w: chapter id %q does not belong to this novel", ErrInvalidReorder, id)
		}
	}

	active, err := s.HasActiveJobsForNovel(novelID)
	if err != nil {
		return err
	}
	if active {
		return ErrActiveJobs
	}

	// Two-phase write inside a transaction: first move every chapter to a
	// unique temporary position to free the original values, then write the
	// final dense positions. This avoids unique-index collisions on
	// (novel, position) during arbitrary swaps.
	return s.App.RunInTransaction(func(txApp core.App) error {
		for i, id := range chapterIDs {
			record, err := txApp.FindRecordById(ChaptersCollection, id)
			if err != nil {
				return err
			}
			record.Set("position", -(i + 1))
			if err := txApp.Save(record); err != nil {
				return err
			}
		}
		for i, id := range chapterIDs {
			record, err := txApp.FindRecordById(ChaptersCollection, id)
			if err != nil {
				return err
			}
			record.Set("position", i+1)
			if err := txApp.Save(record); err != nil {
				return err
			}
		}
		return nil
	})
}

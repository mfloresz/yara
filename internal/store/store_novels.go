package store

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"path"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
)

func (s *Store) CreateNovel(ownerID string, novel *Novel) error {
	novel.Status = normalizeNovelStatus(novel.Status)
	novel.Tags = jsonString(parseNovelTagsJSON(novel.Tags), "[]")
	collection, err := s.App.FindCollectionByNameOrId(NovelsCollection)
	if err != nil {
		return err
	}
	record := core.NewRecord(collection)
	record.Set("owner", ownerID)
	applyNovelToRecord(record, novel)
	if err := s.App.Save(record); err != nil {
		return err
	}
	stored, err := s.GetNovelAccessible(ownerID, record.Id)
	if err != nil {
		return err
	}
	*novel = *stored
	return nil
}

// maxListLimit is the maximum number of novels that can be requested in a single list/search call.
// API clients can pass limit up to this value; the default when unset is 100.
const maxListLimit = 1000

// NovelSortField is the sort field accepted by GET /api/db/novels.
type NovelSortField string

const (
	NovelSortTitle    NovelSortField = "title"
	NovelSortCreated  NovelSortField = "created"
	NovelSortLastRead NovelSortField = "lastRead"
)

// Sort order values accepted by GET /api/db/novels.
const (
	SortOrderAsc  = "asc"
	SortOrderDesc = "desc"
)

// normalizeNovelSortField validates a sort field, defaulting to title.
func normalizeNovelSortField(sortField string) NovelSortField {
	switch NovelSortField(sortField) {
	case NovelSortCreated, NovelSortLastRead:
		return NovelSortField(sortField)
	default:
		return NovelSortTitle
	}
}

// normalizeNovelSortOrder validates a sort order, defaulting to ascending.
func normalizeNovelSortOrder(order string) string {
	if order == SortOrderDesc {
		return SortOrderDesc
	}
	return SortOrderAsc
}

// normalizeListPagination clamps limit/offset to the values accepted by the API.
func normalizeListPagination(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = 100
	} else if limit > maxListLimit {
		limit = maxListLimit
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func (s *Store) ListNovels(userID string, limit int, offset int, sortField string, sortOrder string) ([]Novel, bool, error) {
	sortField = string(normalizeNovelSortField(sortField))
	sortOrder = normalizeNovelSortOrder(sortOrder)
	limit, offset = normalizeListPagination(limit, offset)

	filter := "owner = {:owner} || is_public = true"

	if sortField == string(NovelSortCreated) {
		// DB-level sort keeps offset pagination consistent for arbitrarily large libraries.
		// The UI treats "asc" as most-recent-first for created, so asc maps to -created.
		dbSort := "-created"
		if sortOrder == SortOrderDesc {
			dbSort = "created"
		}
		// Request limit+1 to detect whether more results exist.
		fetchLimit := limit + 1
		records, err := s.App.FindRecordsByFilter(NovelsCollection, filter, dbSort, fetchLimit, offset, dbx.Params{"owner": userID})
		if err != nil {
			return nil, false, err
		}
		hasMore := len(records) > limit
		if hasMore {
			records = records[:limit]
		}
		out := s.novelsFromRecords(records)
		s.populateLastReadAt(out, userID)
		return out, hasMore, nil
	}

	// title and lastRead depend on display-level data (target-or-source title and
	// per-user reading progress), so sort the full result set in memory (unbounded
	// fetch) and slice the sorted window. Pages stay globally consistent for the
	// same query, and all novels are reachable regardless of library size.
	records, err := s.App.FindRecordsByFilter(NovelsCollection, filter, "-created", 0, 0, dbx.Params{"owner": userID})
	if err != nil {
		return nil, false, err
	}
	all := s.novelsFromRecords(records)
	s.populateLastReadAt(all, userID)
	sortNovelsInMemory(all, NovelSortField(sortField), sortOrder)
	page, hasMore := paginateNovels(all, limit, offset)
	return page, hasMore, nil
}

// SearchNovels searches novels by title, author, or series matching the given query.
// Supports pagination via limit/offset, scoped to novels the user owns or are public.
func (s *Store) SearchNovels(userID, query string, limit int, offset int, sortField string, sortOrder string) ([]Novel, bool, error) {
	sortField = string(normalizeNovelSortField(sortField))
	sortOrder = normalizeNovelSortOrder(sortOrder)
	limit, offset = normalizeListPagination(limit, offset)
	if query == "" {
		offset = 0
	}

	// Search across title, author, and series fields (both source and target)
	// Note: field names must match the schema exactly (snake_case, not camelCase)
	filter := "(owner = {:owner} || is_public = true) && " +
		"(source_title ~ {:q} || source_author ~ {:q} || source_series ~ {:q} || " +
		"target_title ~ {:q} || target_author ~ {:q} || target_series ~ {:q})"

	if sortField == string(NovelSortCreated) {
		// The UI treats "asc" as most-recent-first for created, so asc maps to -created.
		dbSort := "-created"
		if sortOrder == SortOrderDesc {
			dbSort = "created"
		}
		fetchLimit := limit + 1
		records, err := s.App.FindRecordsByFilter(NovelsCollection, filter, dbSort, fetchLimit, offset, dbx.Params{"owner": userID, "q": query})
		if err != nil {
			return nil, false, err
		}
		hasMore := len(records) > limit
		if hasMore {
			records = records[:limit]
		}
		out := s.novelsFromRecords(records)
		s.populateLastReadAt(out, userID)
		return out, hasMore, nil
	}

	records, err := s.App.FindRecordsByFilter(NovelsCollection, filter, "-created", 0, 0, dbx.Params{"owner": userID, "q": query})
	if err != nil {
		return nil, false, err
	}
	all := s.novelsFromRecords(records)
	s.populateLastReadAt(all, userID)
	sortNovelsInMemory(all, NovelSortField(sortField), sortOrder)
	page, hasMore := paginateNovels(all, limit, offset)
	return page, hasMore, nil
}

// paginateNovels slices a fully-sorted slice into the requested page.
func paginateNovels(sorted []Novel, limit, offset int) ([]Novel, bool) {
	if offset >= len(sorted) {
		return []Novel{}, false
	}
	end := offset + limit
	hasMore := end < len(sorted)
	if end > len(sorted) {
		end = len(sorted)
	}
	return sorted[offset:end], hasMore
}

// sortNovelsInMemory orders novels by display title, created time, or last read
// time using the same semantics the UI applies, so server pagination matches the
// order users see. Novels with no last read time always sort last.
func sortNovelsInMemory(novels []Novel, sortField NovelSortField, order string) {
	desc := order == SortOrderDesc
	switch sortField {
	case NovelSortCreated:
		sort.SliceStable(novels, func(i, j int) bool {
			return compareTimestampStrings(novels[i].CreatedAt, novels[j].CreatedAt, desc)
		})
	case NovelSortLastRead:
		sort.SliceStable(novels, func(i, j int) bool {
			return compareLastReadStrings(novels[i].LastReadAt, novels[j].LastReadAt, desc)
		})
	default: // title
		sort.SliceStable(novels, func(i, j int) bool {
			left, right := novelDisplayTitle(novels[i]), novelDisplayTitle(novels[j])
			c := compareTitleStrings(left, right)
			if c != 0 {
				if desc {
					return c > 0
				}
				return c < 0
			}
			// Deterministic tiebreak so pagination stays stable across requests.
			if desc {
				return novels[i].ID > novels[j].ID
			}
			return novels[i].ID < novels[j].ID
		})
	}
}

// novelDisplayTitle mirrors the UI's getNovelDisplayTitle: prefer the target title,
// fall back to the source title.
func novelDisplayTitle(n Novel) string {
	if n.TargetTitle != "" {
		return n.TargetTitle
	}
	return n.SourceTitle
}

// compareTimestampStrings orders ISO timestamps using the UI convention: for both
// created and lastRead, "asc" means most-recent first and "desc" means oldest first.
func compareTimestampStrings(a, b string, desc bool) bool {
	if desc {
		return a < b
	}
	return a > b
}

// compareLastReadStrings keeps novels without a last read time at the end
// regardless of direction, matching the UI sort behavior.
func compareLastReadStrings(a, b string, desc bool) bool {
	if a == "" {
		return false
	}
	if b == "" {
		return true
	}
	return compareTimestampStrings(a, b, desc)
}

// compareTitleStrings is a case-insensitive title comparison for stable ordering.
func compareTitleStrings(a, b string) int {
	la, lb := strings.ToLower(a), strings.ToLower(b)
	if la != lb {
		if la < lb {
			return -1
		}
		return 1
	}
	return strings.Compare(a, b)
}

// novelsFromRecords converts PocketBase records to Novel structs.
func (s *Store) novelsFromRecords(records []*core.Record) []Novel {
	out := make([]Novel, 0, len(records))
	for _, record := range records {
		out = append(out, s.novelFromRecord(record))
	}
	return out
}

// populateLastReadAt fetches reading progress for all novels and fills LastReadAt.
func (s *Store) populateLastReadAt(novels []Novel, userID string) {
	if len(novels) == 0 {
		return
	}
	progressRecords, err := s.App.FindRecordsByFilter(
		ReadingProgressCollection,
		"user = {:user}",
		"-updated",
		0, 0,
		dbx.Params{"user": userID},
	)
	if err != nil {
		return
	}
	lastReadMap := make(map[string]string)
	for _, pr := range progressRecords {
		novelID := pr.GetString("novel")
		if _, exists := lastReadMap[novelID]; !exists {
			lastReadMap[novelID] = pr.GetString("updated")
		}
	}
	for i := range novels {
		novels[i].LastReadAt = lastReadMap[novels[i].ID]
	}
}

func (s *Store) ListOwnedNovelsWithURL(ownerID string) ([]Novel, error) {
	const pageSize = 200
	var out []Novel
	offset := 0
	for {
		records, err := s.App.FindRecordsByFilter(NovelsCollection, "owner = {:owner} && url != '' && status != 'completed' && status != 'cancelled'", "-created", pageSize, offset, dbx.Params{"owner": ownerID})
		if err != nil {
			return nil, err
		}
		for _, record := range records {
			out = append(out, s.novelFromRecord(record))
		}
		if len(records) < pageSize {
			break
		}
		offset += pageSize
	}
	if out == nil {
		out = []Novel{}
	}
	return out, nil
}

func (s *Store) ListOwnedNovelsWithTranslationStats(ownerID string) ([]Novel, error) {
	const pageSize = 200
	var out []Novel
	offset := 0
	for {
		records, err := s.App.FindRecordsByFilter(NovelsCollection, "owner = {:owner} && status != 'cancelled'", "-updated", pageSize, offset, dbx.Params{"owner": ownerID})
		if err != nil {
			return nil, err
		}
		for _, record := range records {
			out = append(out, s.novelFromRecord(record))
		}
		if len(records) < pageSize {
			break
		}
		offset += pageSize
	}
	if out == nil {
		out = []Novel{}
	}
	return out, nil
}

func (s *Store) GetOwnedNovelChapterIDsByStatus(userID, novelID string) (pendingIDs []string, err error) {
	if _, err := s.GetOwnedNovel(userID, novelID); err != nil {
		return nil, err
	}
	records, err := s.App.FindRecordsByFilter(ChaptersCollection, "novel = {:novel} && (status = 'pending' || (original_content != '' && translated_content = ''))", "chapter_order", 5000, 0, dbx.Params{"novel": novelID})
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.Id)
	}
	return ids, nil
}

func (s *Store) GetNovelAccessible(userID, novelID string) (*Novel, error) {
	record, err := s.App.FindRecordById(NovelsCollection, novelID)
	if err != nil {
		return nil, ErrNotFound
	}
	if record.GetString("owner") != userID && !record.GetBool("is_public") {
		return nil, ErrNotFound
	}
	novel := s.novelFromRecord(record)
	return &novel, nil
}

func (s *Store) GetOwnedNovel(userID, novelID string) (*Novel, error) {
	record, err := s.App.FindRecordById(NovelsCollection, novelID)
	if err != nil {
		return nil, ErrNotFound
	}
	if record.GetString("owner") != userID {
		return nil, ErrForbidden
	}
	novel := s.novelFromRecord(record)
	return &novel, nil
}

func (s *Store) UpdateNovel(userID, novelID string, patch map[string]any) (*Novel, error) {
	record, err := s.App.FindRecordById(NovelsCollection, novelID)
	if err != nil {
		return nil, ErrNotFound
	}
	if record.GetString("owner") != userID {
		return nil, ErrForbidden
	}
	for key, value := range patch {
		switch key {
		case "sourceLanguage":
			record.Set("source_language", value)
		case "targetLanguage":
			record.Set("target_language", value)
		case "sourceTitle":
			record.Set("source_title", value)
		case "sourceAuthor":
			record.Set("source_author", value)
		case "sourceDescription":
			record.Set("source_description", value)
		case "sourceSeries":
			record.Set("source_series", value)
		case "sourceNumber":
			record.Set("source_number", value)
		case "targetTitle":
			record.Set("target_title", value)
		case "targetAuthor":
			record.Set("target_author", value)
		case "targetDescription":
			record.Set("target_description", value)
		case "targetSeries":
			record.Set("target_series", value)
		case "targetNumber":
			record.Set("target_number", value)
		case "glossary":
			record.Set("glossary", jsonString(value, "[]"))
		case "prompts":
			overrides := ParseNovelPromptOverrides(value)
			record.Set("translation_system_prompt", overrides.Translation.SystemPrompt)
			record.Set("translation_user_prompt", overrides.Translation.UserPrompt)
			record.Set("refine_system_prompt", overrides.Refine.SystemPrompt)
			record.Set("refine_user_prompt", overrides.Refine.UserPrompt)
			record.Set("check_system_prompt", overrides.Check.SystemPrompt)
			record.Set("check_user_prompt", overrides.Check.UserPrompt)
		case "notes":
			record.Set("notes", value)
		case "aiOptions":
			record.Set("ai_options", jsonString(value, "{}"))
		case "translationOptions":
			record.Set("translation_options", jsonString(value, "{}"))
		case "cleanupRules":
			record.Set("cleanup_rules", jsonString(value, "[]"))
		case "url":
			record.Set("url", value)
		case "customCommands":
			record.Set("custom_commands", value)
		case "status":
			record.Set("status", normalizeNovelStatus(fmt.Sprint(value)))
		case "tags":
			record.Set("tags", jsonString(normalizeNovelTagsValue(value), "[]"))
		case "isPublic":
			record.Set("is_public", value)
		}
	}
	if err := s.App.Save(record); err != nil {
		return nil, err
	}
	updated := s.novelFromRecord(record)
	return &updated, nil
}

func (s *Store) UpdateNovelCheckResult(novelID, checkedAt string, newChapters int) error {
	record, err := s.App.FindRecordById(NovelsCollection, novelID)
	if err != nil {
		return ErrNotFound
	}
	record.Set("last_checked_at", checkedAt)
	record.Set("last_check_new_chapters", newChapters)
	return s.App.Save(record)
}

func (s *Store) UpdateNovelGlossary(userID, novelID, glossaryJSON string) error {
	record, err := s.App.FindRecordById(NovelsCollection, novelID)
	if err != nil {
		return ErrNotFound
	}
	if record.GetString("owner") != userID {
		return ErrForbidden
	}
	record.Set("glossary", glossaryJSON)
	return s.App.Save(record)
}

func (s *Store) DeleteNovel(userID, novelID string) error {
	record, err := s.App.FindRecordById(NovelsCollection, novelID)
	if err != nil {
		return ErrNotFound
	}
	if record.GetString("owner") != userID {
		return ErrForbidden
	}
	return s.App.Delete(record)
}

func (s *Store) SetNovelVisibility(userID, novelID string, isPublic bool) (*Novel, error) {
	return s.UpdateNovel(userID, novelID, map[string]any{"isPublic": isPublic})
}

func (s *Store) ListNovelTagSuggestions(userID, query string, limit int) ([]string, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	records, err := s.App.FindRecordsByFilter(NovelsCollection, "owner = {:owner}", "-updated", 5000, 0, dbx.Params{"owner": userID})
	if err != nil {
		return nil, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	seen := make(map[string]string)
	for _, record := range records {
		for _, tag := range parseNovelTagsJSON(record.GetString("tags")) {
			if query != "" && !strings.Contains(strings.ToLower(tag), query) {
				continue
			}
			key := strings.ToLower(tag)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = tag
		}
	}
	out := make([]string, 0, len(seen))
	for _, tag := range seen {
		out = append(out, tag)
	}
	sort.SliceStable(out, func(i, j int) bool {
		left := strings.ToLower(out[i])
		right := strings.ToLower(out[j])
		leftPrefix := query != "" && strings.HasPrefix(left, query)
		rightPrefix := query != "" && strings.HasPrefix(right, query)
		if leftPrefix != rightPrefix {
			return leftPrefix
		}
		return left < right
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *Store) ListNovelSeriesSuggestions(userID, query string, limit int) ([]string, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	records, err := s.App.FindRecordsByFilter(NovelsCollection, "owner = {:owner}", "-updated", 5000, 0, dbx.Params{"owner": userID})
	if err != nil {
		return nil, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	seen := make(map[string]string)
	for _, record := range records {
		for _, field := range []string{"source_series", "target_series"} {
			series := strings.TrimSpace(record.GetString(field))
			if series == "" {
				continue
			}
			if query != "" && !strings.Contains(strings.ToLower(series), query) {
				continue
			}
			key := strings.ToLower(series)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = series
		}
	}
	out := make([]string, 0, len(seen))
	for _, series := range seen {
		out = append(out, series)
	}
	sort.SliceStable(out, func(i, j int) bool {
		left := strings.ToLower(out[i])
		right := strings.ToLower(out[j])
		leftPrefix := query != "" && strings.HasPrefix(left, query)
		rightPrefix := query != "" && strings.HasPrefix(right, query)
		if leftPrefix != rightPrefix {
			return leftPrefix
		}
		return left < right
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *Store) CopyNovel(userID, novelID string) (*Novel, error) {
	novel, err := s.GetNovelAccessible(userID, novelID)
	if err != nil {
		return nil, err
	}

	// Read cover and thumbnail blobs from the source novel's record
	// so we can replicate the files to the clone.
	var coverBlob []byte
	var coverMime string
	var thumbBlob []byte
	if novel.CoverFile != "" {
		coverBlob, coverMime, err = s.readNovelFileBlob(novelID, novel.CoverFile)
		if err != nil {
			return nil, fmt.Errorf("read cover blob: %w", err)
		}
	}
	if novel.ThumbnailFile != "" && thumbBlob == nil {
		thumbBlob, _, err = s.readNovelFileBlob(novelID, novel.ThumbnailFile)
		if err != nil {
			return nil, fmt.Errorf("read thumbnail blob: %w", err)
		}
	}

	clone := *novel
	clone.ID = ""
	clone.OwnerID = userID
	clone.IsPublic = false
	clone.CoverPath = ""
	clone.CoverFile = ""
	clone.ThumbnailPath = ""
	clone.ThumbnailFile = ""
	clone.ChapterCount = 0
	clone.TranslatedCount = 0
	clone.CompletedCount = 0
	clone.OriginalCharCount = 0
	clone.TranslatedCharCount = 0
	clone.RefinedCharCount = 0
	clone.TotalCharCount = 0
	clone.MaxChapterOrder = 0
	if err := s.CreateNovel(userID, &clone); err != nil {
		return nil, err
	}

	// Replicate cover and thumbnail files to the clone record.
	if coverBlob != nil {
		if err := s.attachNovelCover(clone.ID, coverBlob, coverMime); err != nil {
			return nil, fmt.Errorf("copy cover: %w", err)
		}
	}
	if thumbBlob != nil {
		s.attachCoverThumbnail(clone.ID, thumbBlob)
	}

	chapters, err := s.ListChaptersAccessible(userID, novelID)
	if err != nil {
		return nil, err
	}
	for _, chapter := range chapters {
		chapter.ID = ""
		chapter.NovelID = clone.ID
		if _, err := s.UpsertChapterWithoutStats(userID, clone.ID, &chapter); err != nil {
			return nil, err
		}
	}
	if err := s.RecalculateNovelStats(clone.ID); err != nil {
		return nil, err
	}
	freshClone, err := s.GetOwnedNovel(userID, clone.ID)
	if err != nil {
		return nil, err
	}
	return freshClone, nil
}

func (s *Store) ImportEpubNovel(input *ImportEpubNovelInput) (*ImportEpubNovelResult, error) {
	if input == nil {
		return nil, fmt.Errorf("import input required")
	}
	resultNovel := &Novel{
		SourceLanguage:     input.SourceLanguage,
		TargetLanguage:     input.TargetLanguage,
		SourceTitle:        input.SourceTitle,
		SourceAuthor:       clampText(input.SourceAuthor, 250),
		SourceDescription:  clampText(input.SourceDescription, 5000),
		SourceSeries:       input.SourceSeries,
		SourceNumber:       input.SourceNumber,
		Status:             "completed",
		Tags:               "[]",
		Glossary:           "[]",
		AIOptions:          "{}",
		TranslationOptions: "{}",
		CleanupRules:       "[]",
	}
	if err := s.CreateNovel(input.OwnerID, resultNovel); err != nil {
		return nil, err
	}
	if len(input.CoverBlob) > 0 {
		if err := s.attachNovelCover(resultNovel.ID, input.CoverBlob, input.CoverMime); err != nil {
			return nil, err
		}
	}
	for idx, chapter := range input.Chapters {
		_, err := s.UpsertChapterWithoutStats(input.OwnerID, resultNovel.ID, &Chapter{
			ChapterOrder:    idx + 1,
			Title:           clampText(chapter.Title, 500),
			OriginalContent: chapter.Content,
			Status:          "pending",
		})
		if err != nil {
			return nil, err
		}
	}
	if err := s.RecalculateNovelStats(resultNovel.ID); err != nil {
		return nil, err
	}
	epub, err := s.UpsertEpub(input.OwnerID, &Epub{NovelID: resultNovel.ID, FileKind: "original", SourceVariant: "original", Label: "EPUB original"}, input.FileName, input.MimeType, input.FileBlob)
	if err != nil {
		return nil, err
	}
	fresh, err := s.GetOwnedNovel(input.OwnerID, resultNovel.ID)
	if err != nil {
		return nil, err
	}
	*resultNovel = *fresh
	return &ImportEpubNovelResult{Novel: *resultNovel, Epub: *epub, ChaptersImported: len(input.Chapters)}, nil
}

func (s *Store) ImportUrlNovel(input *ImportUrlNovelInput) (*ImportUrlNovelResult, error) {
	if input == nil {
		return nil, fmt.Errorf("import input required")
	}
	novel := &Novel{
		URL:                input.URL,
		SourceLanguage:     input.SourceLanguage,
		SourceTitle:        strings.TrimSpace(input.SourceTitle),
		SourceAuthor:       clampText(input.SourceAuthor, 250),
		SourceDescription:  clampText(input.SourceDescription, 5000),
		TargetLanguage:     input.TargetLanguage,
		Status:             "ongoing",
		Tags:               "[]",
		Glossary:           "[]",
		AIOptions:          "{}",
		TranslationOptions: "{}",
		CleanupRules:       "[]",
	}
	if err := s.CreateNovel(input.OwnerID, novel); err != nil {
		return nil, err
	}
	fresh, err := s.GetOwnedNovel(input.OwnerID, novel.ID)
	if err != nil {
		return nil, err
	}
	*novel = *fresh
	return &ImportUrlNovelResult{Novel: *novel, ChaptersImported: 0}, nil
}

func (s *Store) ImportZipNovel(input *ImportZipNovelInput) (*ImportZipNovelResult, error) {
	if input == nil {
		return nil, fmt.Errorf("import input required")
	}
	meta := struct {
		SourceLanguage    string `json:"sourceLanguage"`
		TargetLanguage    string `json:"targetLanguage"`
		URL               string `json:"url"`
		SourceTitle       string `json:"sourceTitle"`
		SourceAuthor      string `json:"sourceAuthor"`
		SourceDescription string `json:"sourceDescription"`
		TargetTitle       string `json:"targetTitle"`
		TargetAuthor      string `json:"targetAuthor"`
		TargetDescription string `json:"targetDescription"`
		SourceSeries      string `json:"sourceSeries"`
		SourceNumber      string `json:"sourceNumber"`
		TargetSeries      string `json:"targetSeries"`
		TargetNumber      string `json:"targetNumber"`
		Notes             string `json:"notes"`
		CustomCommands    string `json:"customCommands"`
		Status            string `json:"status"`
		IsPublic          bool   `json:"isPublic"`
	}{}
	if err := json.Unmarshal([]byte(input.MetadataJSON), &meta); err != nil {
		return nil, fmt.Errorf("invalid metadata.json: %w", err)
	}
	canonicalSourceTitle := strings.TrimSpace(meta.SourceTitle)
	canonicalSourceAuthor := clampText(meta.SourceAuthor, 250)
	canonicalSourceDescription := clampText(meta.SourceDescription, 5000)
	if canonicalSourceTitle == "" {
		return nil, fmt.Errorf("sourceTitle is required in metadata.json")
	}
	if meta.SourceLanguage == "" {
		return nil, fmt.Errorf("sourceLanguage is required in metadata.json")
	}
	if meta.TargetLanguage == "" {
		return nil, fmt.Errorf("targetLanguage is required in metadata.json")
	}
	resultNovel := &Novel{
		SourceLanguage:     meta.SourceLanguage,
		TargetLanguage:     meta.TargetLanguage,
		URL:                meta.URL,
		SourceTitle:        canonicalSourceTitle,
		SourceAuthor:       canonicalSourceAuthor,
		SourceDescription:  canonicalSourceDescription,
		SourceSeries:       meta.SourceSeries,
		SourceNumber:       meta.SourceNumber,
		TargetTitle:        meta.TargetTitle,
		TargetAuthor:       meta.TargetAuthor,
		TargetDescription:  meta.TargetDescription,
		TargetSeries:       meta.TargetSeries,
		TargetNumber:       meta.TargetNumber,
		Notes:              meta.Notes,
		CustomCommands:     meta.CustomCommands,
		Status:             normalizeNovelStatus(meta.Status),
		Tags:               "[]",
		IsPublic:           meta.IsPublic,
		Glossary:           "[]",
		AIOptions:          "{}",
		TranslationOptions: "{}",
		CleanupRules:       "[]",
	}
	if err := s.CreateNovel(input.OwnerID, resultNovel); err != nil {
		return nil, err
	}
	if len(input.CoverBlob) > 0 {
		if err := s.attachNovelCover(resultNovel.ID, input.CoverBlob, input.CoverMime); err != nil {
			return nil, err
		}
	}
	for _, chapter := range input.Chapters {
		status := "pending"
		if strings.TrimSpace(chapter.TranslatedContent) != "" {
			status = "translated"
		}
		_, err := s.UpsertChapterWithoutStats(input.OwnerID, resultNovel.ID, &Chapter{
			ChapterOrder:      chapter.Order,
			Title:             chapter.Title,
			TranslatedTitle:   chapter.TranslatedTitle,
			OriginalContent:   chapter.OriginalContent,
			TranslatedContent: chapter.TranslatedContent,
			Status:            status,
		})
		if err != nil {
			return nil, err
		}
	}
	if err := s.RecalculateNovelStats(resultNovel.ID); err != nil {
		return nil, err
	}
	fresh, err := s.GetOwnedNovel(input.OwnerID, resultNovel.ID)
	if err != nil {
		return nil, err
	}
	*resultNovel = *fresh
	return &ImportZipNovelResult{Novel: *resultNovel, ChaptersImported: len(input.Chapters)}, nil
}

func (s *Store) attachNovelCover(novelID string, blob []byte, mimeType string) error {
	collection, err := s.App.FindCollectionByNameOrId(NovelsCollection)
	if err != nil {
		return err
	}
	record, err := s.App.FindRecordById(collection, novelID)
	if err != nil {
		return err
	}
	ext := coverExtension(mimeType)
	name := "cover" + ext
	file, err := filesystem.NewFileFromBytes(blob, name)
	if err != nil {
		return err
	}
	record.Set("cover", []*filesystem.File{file})
	if err := s.App.Save(record); err != nil {
		return err
	}
	s.attachCoverThumbnail(novelID, blob)
	return nil
}

func (s *Store) AttachCoverBlob(novelID string, blob []byte, mimeType string) error {
	return s.attachNovelCover(novelID, blob, mimeType)
}

func (s *Store) UpdateNovelCover(userID, novelID string, blob []byte, mimeType string) (*Novel, error) {
	if _, err := s.GetOwnedNovel(userID, novelID); err != nil {
		return nil, err
	}
	if err := s.attachNovelCover(novelID, blob, mimeType); err != nil {
		return nil, err
	}
	return s.GetOwnedNovel(userID, novelID)
}

func (s *Store) readNovelFileBlob(novelID, fileName string) ([]byte, string, error) {
	collection, err := s.App.FindCollectionByNameOrId(NovelsCollection)
	if err != nil {
		return nil, "", err
	}
	record, err := s.App.FindRecordById(collection, novelID)
	if err != nil {
		return nil, "", err
	}
	fsys, err := s.App.NewFilesystem()
	if err != nil {
		return nil, "", err
	}
	defer fsys.Close()

	fileKey := record.BaseFilesPath() + "/" + fileName
	reader, err := fsys.GetReader(fileKey)
	if err != nil {
		return nil, "", fmt.Errorf("get reader for %s: %w", fileKey, err)
	}
	defer reader.Close()

	blob, err := io.ReadAll(reader)
	if err != nil {
		return nil, "", fmt.Errorf("read file %s: %w", fileName, err)
	}
	mime := mime.TypeByExtension(path.Ext(fileName))
	if mime == "" {
		mime = "application/octet-stream"
	}
	return blob, mime, nil
}

func coverExtension(mimeType string) string {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/svg+xml":
		return ".svg"
	}
	return ".jpg"
}

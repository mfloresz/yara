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

// ListNovelOptions carries the optional library filters accepted by
// GET /api/v1/novels. All values default to "no filter" and are validated by
// normalizeListNovelOptions.
type ListNovelOptions struct {
	Tag         string // "" = no filter; exact match, case/accent-insensitive
	Author      string // "" = no filter; exact match, case-insensitive across source/target author
	Series      string // "" = no filter; exact match, case-insensitive across source/target series
	Shared      string // "all" | "own" | "shared"
	Progress    string // "all" | "translated" | "completed" | "ongoing"
	SearchField string // "all" | "title" | "author" | "series"; scopes ?q matching
}

// Search field values accepted by GET /api/v1/novels (?q&field=).
const (
	SearchFieldAll    = "all"
	SearchFieldTitle  = "title"
	SearchFieldAuthor = "author"
	SearchFieldSeries = "series"
	SearchFieldTags   = "tags"
)

// normalizeSearchField validates a ?field= value, mapping unknown values to the
// "all fields" default instead of erroring (same leniency as shared/progress).
func normalizeSearchField(field string) string {
	switch field {
	case SearchFieldTitle, SearchFieldAuthor, SearchFieldSeries, SearchFieldTags:
		return field
	default:
		return SearchFieldAll
	}
}

// normalizeListNovelOptions validates option values, mapping unknown values to
// the "no filter" default instead of erroring.
func normalizeListNovelOptions(opts ListNovelOptions) ListNovelOptions {
	switch opts.Shared {
	case "own", "shared":
	default:
		opts.Shared = "all"
	}
	switch opts.Progress {
	case "translated", "completed", "ongoing":
	default:
		opts.Progress = "all"
	}
	opts.Tag = strings.TrimSpace(opts.Tag)
	opts.Author = strings.TrimSpace(opts.Author)
	opts.Series = strings.TrimSpace(opts.Series)
	opts.SearchField = normalizeSearchField(strings.TrimSpace(opts.SearchField))
	return opts
}

// buildScopeFilter returns the visibility scope clause (shared filter) plus the
// progress clause as raw SQL (AND/OR, not fexpr &&/||) with the :owner param
// bound through dbx.Params. The scope is parenthesized so an appended AND
// progress clause cannot bind to only one arm of the OR scope.
func buildScopeFilter(opts ListNovelOptions) string {
	var scope string
	switch opts.Shared {
	case "own":
		scope = "owner = {:owner}"
	case "shared":
		scope = "owner != {:owner} AND is_public = 1"
	default:
		scope = "(owner = {:owner} OR is_public = 1)"
	}
	switch opts.Progress {
	case "translated":
		// A novel whose source and target languages match needs no translation, so
		// translated_count stays at 0 forever and it would never satisfy
		// translated_count = chapter_count. Treat it as translated by definition.
		return scope + " AND chapter_count > 0 AND (source_language = target_language OR translated_count = chapter_count)"
	case "completed":
		return scope + " AND status = 'completed'"
	case "ongoing":
		return scope + " AND status = 'ongoing'"
	}
	return scope
}

// filterNovelsByTag keeps only novels whose tags include tag, compared in
// canonical form (case/accent-insensitive).
func filterNovelsByTag(novels []Novel, tag string) []Novel {
	key := normalizeTagKey(tag)
	out := make([]Novel, 0, len(novels))
	for _, n := range novels {
		for _, t := range parseNovelTagsJSON(n.Tags) {
			if normalizeTagKey(t) == key {
				out = append(out, n)
				break
			}
		}
	}
	return out
}

// filterNovelsByStringFields keeps only novels where any of the values picked
// from the novel equals value exactly (case-insensitive, trimmed). Used for
// the author/series facet filters.
func filterNovelsByStringFields(novels []Novel, value string, pick func(Novel) []string) []Novel {
	out := make([]Novel, 0, len(novels))
	for _, n := range novels {
		for _, v := range pick(n) {
			if strings.EqualFold(strings.TrimSpace(v), value) {
				out = append(out, n)
				break
			}
		}
	}
	return out
}

// novelListColumns is the narrow column set behind ListNovels/SearchNovels.
// The heavy per-novel blobs (glossary can reach several MB, plus the 8 prompt
// fields, notes, ai/translation options, cleanup rules and custom commands)
// are never displayed by list/search/suggestion flows — the library grid and
// the search dropdown request sparse fieldsets without them — so hydrating
// them turned every library render into a full-table blob read. Single-novel
// endpoints (GET /novels/{id}, POST /novels) keep full record hydration and
// still return the heavy fields. Descriptions are kept: the default (no
// ?fields=) list response includes them and they are small in practice.
// One entry per column: dbx quotes each Select() argument as a whole column
// expression, so a multi-column string would break the query.
var novelListColumns = []string{
	"id", "owner", "is_public",
	"source_language", "target_language",
	"source_title", "source_author", "source_series", "source_number",
	"target_title", "target_author", "target_series", "target_number",
	"source_description", "target_description",
	"status", "tags", "url", "cover", "thumbnail",
	"chapter_count", "translated_count", "completed_count",
	"original_char_count", "translated_char_count", "refined_char_count",
	"total_char_count", "max_chapter_order",
	"last_checked_at", "last_check_new_chapters",
	"created", "updated",
}

// novelListRow mirrors novelListColumns for dbx struct scanning. Numerics and
// booleans are scanned as float64 because SQLite NUMERIC values come back with
// INTEGER or REAL storage class depending on the value.
type novelListRow struct {
	ID                   string  `db:"id"`
	Owner                string  `db:"owner"`
	IsPublic             float64 `db:"is_public"`
	SourceLanguage       string  `db:"source_language"`
	TargetLanguage       string  `db:"target_language"`
	SourceTitle          string  `db:"source_title"`
	SourceAuthor         string  `db:"source_author"`
	SourceSeries         string  `db:"source_series"`
	SourceNumber         string  `db:"source_number"`
	TargetTitle          string  `db:"target_title"`
	TargetAuthor         string  `db:"target_author"`
	TargetSeries         string  `db:"target_series"`
	TargetNumber         string  `db:"target_number"`
	SourceDescription    string  `db:"source_description"`
	TargetDescription    string  `db:"target_description"`
	Status               string  `db:"status"`
	Tags                 string  `db:"tags"`
	URL                  string  `db:"url"`
	Cover                string  `db:"cover"`
	Thumbnail            string  `db:"thumbnail"`
	ChapterCount         float64 `db:"chapter_count"`
	TranslatedCount      float64 `db:"translated_count"`
	CompletedCount       float64 `db:"completed_count"`
	OriginalCharCount    float64 `db:"original_char_count"`
	TranslatedCharCount  float64 `db:"translated_char_count"`
	RefinedCharCount     float64 `db:"refined_char_count"`
	TotalCharCount       float64 `db:"total_char_count"`
	MaxChapterOrder      float64 `db:"max_chapter_order"`
	LastCheckedAt        string  `db:"last_checked_at"`
	LastCheckNewChapters float64 `db:"last_check_new_chapters"`
	Created              string  `db:"created"`
	Updated              string  `db:"updated"`
}

// firstFileName extracts the first file name from a PocketBase file-field
// column, which stores either a JSON array of names or a bare filename.
func firstFileName(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var names []string
	if err := json.Unmarshal([]byte(raw), &names); err != nil || len(names) == 0 {
		return raw
	}
	return names[0]
}

func novelFromListRow(row novelListRow) Novel {
	coverFile := firstFileName(row.Cover)
	thumbFile := firstFileName(row.Thumbnail)
	return Novel{
		ID:                   row.ID,
		OwnerID:              row.Owner,
		SourceLanguage:       row.SourceLanguage,
		TargetLanguage:       row.TargetLanguage,
		SourceTitle:          row.SourceTitle,
		SourceAuthor:         row.SourceAuthor,
		SourceDescription:    row.SourceDescription,
		SourceSeries:         row.SourceSeries,
		SourceNumber:         row.SourceNumber,
		TargetTitle:          row.TargetTitle,
		TargetAuthor:         row.TargetAuthor,
		TargetDescription:    row.TargetDescription,
		TargetSeries:         row.TargetSeries,
		TargetNumber:         row.TargetNumber,
		Glossary:             "[]",
		AIOptions:            "{}",
		TranslationOptions:   "{}",
		CleanupRules:         "[]",
		URL:                  row.URL,
		Status:               normalizeNovelStatus(row.Status),
		Tags:                 jsonString(parseNovelTagsJSON(row.Tags), "[]"),
		CoverFile:            coverFile,
		CoverPath:            novelCoverURL(row.ID, coverFile),
		ThumbnailFile:        thumbFile,
		ThumbnailPath:        novelCoverURL(row.ID, thumbFile),
		IsPublic:             row.IsPublic != 0,
		ChapterCount:         asInt(row.ChapterCount, 0),
		TranslatedCount:      asInt(row.TranslatedCount, 0),
		CompletedCount:       asInt(row.CompletedCount, 0),
		OriginalCharCount:    asInt(row.OriginalCharCount, 0),
		TranslatedCharCount:  asInt(row.TranslatedCharCount, 0),
		RefinedCharCount:     asInt(row.RefinedCharCount, 0),
		TotalCharCount:       asInt(row.TotalCharCount, 0),
		MaxChapterOrder:      asInt(row.MaxChapterOrder, 0),
		LastCheckedAt:        row.LastCheckedAt,
		LastCheckNewChapters: asInt(row.LastCheckNewChapters, 0),
		CreatedAt:            row.Created,
		UpdatedAt:            row.Updated,
	}
}

// findNovelListRows runs one projected list query. where is raw SQL with
// {:name} placeholders; limit 0 means unbounded.
func (s *Store) findNovelListRows(where string, params dbx.Params, limit, offset int, orderBys ...string) ([]Novel, error) {
	query := s.App.DB().
		Select(novelListColumns...).
		From(NovelsCollection).
		Where(dbx.NewExp(where, params)).
		OrderBy(orderBys...)
	if limit > 0 {
		query = query.Limit(int64(limit))
	}
	if offset > 0 {
		query = query.Offset(int64(offset))
	}
	rows := []novelListRow{}
	if err := query.All(&rows); err != nil {
		return nil, err
	}
	out := make([]Novel, 0, len(rows))
	for _, row := range rows {
		out = append(out, novelFromListRow(row))
	}
	return out, nil
}

func (s *Store) ListNovels(userID string, limit int, offset int, sortField string, sortOrder string, opts ListNovelOptions) ([]Novel, bool, error) {
	sortField = string(normalizeNovelSortField(sortField))
	sortOrder = normalizeNovelSortOrder(sortOrder)
	limit, offset = normalizeListPagination(limit, offset)
	opts = normalizeListNovelOptions(opts)

	where := buildScopeFilter(opts)
	params := dbx.Params{"owner": userID}

	// Tag/author/series filtering is Go-level post-filtering over the full
	// scope: running it after the DB offset would leave holes in pages and make
	// meta.total inconsistent, so force the in-memory route when one is active.
	if opts.Tag == "" && opts.Author == "" && opts.Series == "" && sortField == string(NovelSortCreated) {
		// DB-level sort keeps offset pagination consistent for arbitrarily large libraries.
		// The UI treats "asc" as most-recent-first for created, so asc maps to -created.
		dbSort := "created DESC"
		if sortOrder == SortOrderDesc {
			dbSort = "created ASC"
		}
		// Request limit+1 to detect whether more results exist.
		page, err := s.findNovelListRows(where, params, limit+1, offset, dbSort)
		if err != nil {
			return nil, false, err
		}
		hasMore := len(page) > limit
		if hasMore {
			page = page[:limit]
		}
		s.populateLastReadAt(page, userID)
		return page, hasMore, nil
	}

	// title and lastRead depend on display-level data (target-or-source title and
	// per-user reading progress), so sort the full result set in memory (unbounded
	// fetch) and slice the sorted window. Pages stay globally consistent for the
	// same query, and all novels are reachable regardless of library size. Rows
	// use the narrow list projection, so the unbounded fetch no longer
	// transfers each novel's glossary/prompts.
	all, err := s.findNovelListRows(where, params, 0, 0, "created DESC")
	if err != nil {
		return nil, false, err
	}
	s.populateLastReadAt(all, userID)
	if opts.Tag != "" {
		all = filterNovelsByTag(all, opts.Tag)
	}
	if opts.Author != "" {
		all = filterNovelsByStringFields(all, opts.Author, func(n Novel) []string {
			return []string{n.SourceAuthor, n.TargetAuthor}
		})
	}
	if opts.Series != "" {
		all = filterNovelsByStringFields(all, opts.Series, func(n Novel) []string {
			return []string{n.SourceSeries, n.TargetSeries}
		})
	}
	sortNovelsInMemory(all, NovelSortField(sortField), sortOrder)
	page, hasMore := paginateNovels(all, limit, offset)
	return page, hasMore, nil
}

// SearchNovels searches novels by title, author, series, or tags matching the
// given query. opts.SearchField scopes the match: "title" (source/target
// title), "author" (source/target author), "series" (source/target series),
// "tags" (tag list), or "all" (default: title + author + series + tags).
// Supports pagination via limit/offset, scoped to novels the user owns or are
// public.
func (s *Store) SearchNovels(userID, query string, limit int, offset int, sortField string, sortOrder string, opts ListNovelOptions) ([]Novel, bool, error) {
	sortField = string(normalizeNovelSortField(sortField))
	sortOrder = normalizeNovelSortOrder(sortOrder)
	limit, offset = normalizeListPagination(limit, offset)
	opts = normalizeListNovelOptions(opts)
	if query == "" {
		offset = 0
	}

	// Search across title, author, series, and tags fields (source and target
	// variants). Field names must match the schema exactly. LIKE mirrors the
	// semantics of the previous fexpr `~` operator (case-insensitive for ASCII,
	// unescaped %).
	like := "%" + query + "%"
	matchClause := "(source_title LIKE {:like} OR target_title LIKE {:like} OR " +
		"source_author LIKE {:like} OR target_author LIKE {:like} OR " +
		"source_series LIKE {:like} OR target_series LIKE {:like} OR tags LIKE {:like})"
	switch opts.SearchField {
	case SearchFieldTitle:
		matchClause = "(source_title LIKE {:like} OR target_title LIKE {:like})"
	case SearchFieldAuthor:
		matchClause = "(source_author LIKE {:like} OR target_author LIKE {:like})"
	case SearchFieldSeries:
		matchClause = "(source_series LIKE {:like} OR target_series LIKE {:like})"
	case SearchFieldTags:
		matchClause = "tags LIKE {:like}"
	}
	where := buildScopeFilter(opts) + " AND " + matchClause
	params := dbx.Params{"owner": userID, "like": like}

	if opts.Tag == "" && opts.Author == "" && opts.Series == "" && sortField == string(NovelSortCreated) {
		// The UI treats "asc" as most-recent-first for created, so asc maps to -created.
		dbSort := "created DESC"
		if sortOrder == SortOrderDesc {
			dbSort = "created ASC"
		}
		page, err := s.findNovelListRows(where, params, limit+1, offset, dbSort)
		if err != nil {
			return nil, false, err
		}
		hasMore := len(page) > limit
		if hasMore {
			page = page[:limit]
		}
		s.populateLastReadAt(page, userID)
		return page, hasMore, nil
	}

	all, err := s.findNovelListRows(where, params, 0, 0, "created DESC")
	if err != nil {
		return nil, false, err
	}
	s.populateLastReadAt(all, userID)
	if opts.Tag != "" {
		all = filterNovelsByTag(all, opts.Tag)
	}
	if opts.Author != "" {
		all = filterNovelsByStringFields(all, opts.Author, func(n Novel) []string {
			return []string{n.SourceAuthor, n.TargetAuthor}
		})
	}
	if opts.Series != "" {
		all = filterNovelsByStringFields(all, opts.Series, func(n Novel) []string {
			return []string{n.SourceSeries, n.TargetSeries}
		})
	}
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

// populateLastReadAt fetches reading progress for all novels and fills LastReadAt.
// Narrow projection (novel, updated): the previous version hydrated every
// progress record of the user on every library listing.
func (s *Store) populateLastReadAt(novels []Novel, userID string) {
	if len(novels) == 0 {
		return
	}
	rows := []struct {
		Novel   string `db:"novel"`
		Updated string `db:"updated"`
	}{}
	err := s.App.DB().Select("novel", "updated").From(ReadingProgressCollection).
		Where(dbx.NewExp("user = {:user}", dbx.Params{"user": userID})).
		OrderBy("updated DESC").
		All(&rows)
	if err != nil {
		return
	}
	lastReadMap := make(map[string]string, len(rows))
	for _, row := range rows {
		if _, exists := lastReadMap[row.Novel]; !exists {
			lastReadMap[row.Novel] = row.Updated
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
	records, err := s.App.FindRecordsByFilter(ChaptersCollection, "novel = {:novel} && excluded = false && (status = 'pending' || (original_content != '' && translated_content = ''))", "position,chapter_order", 5000, 0, dbx.Params{"novel": novelID})
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

// GetNovelCoverFile returns the novel record plus the file name to serve as
// its cover image (the thumbnail when present, the full cover otherwise).
// Access follows the novel's visibility: owner or is_public.
func (s *Store) GetNovelCoverFile(userID, novelID string) (*core.Record, string, error) {
	record, err := s.App.FindRecordById(NovelsCollection, novelID)
	if err != nil {
		return nil, "", ErrNotFound
	}
	if record.GetString("owner") != userID && !record.GetBool("is_public") {
		return nil, "", ErrForbidden
	}
	if name := firstString(record.GetStringSlice("thumbnail")); name != "" {
		return record, name, nil
	}
	if name := firstString(record.GetStringSlice("cover")); name != "" {
		return record, name, nil
	}
	return nil, "", ErrNotFound
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
			record.Set("source_language", normalizeLanguageCode(fmt.Sprint(value)))
		case "targetLanguage":
			record.Set("target_language", normalizeLanguageCode(fmt.Sprint(value)))
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
			record.Set("title_system_prompt", overrides.Title.SystemPrompt)
			record.Set("title_user_prompt", overrides.Title.UserPrompt)
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

// ClearNovelPendingNewChapters resets the pending "new chapters" counter once
// those chapters have actually been downloaded. last_checked_at is left
// untouched: the check did happen, only its result is now consumed.
func (s *Store) ClearNovelPendingNewChapters(novelID string) error {
	record, err := s.App.FindRecordById(NovelsCollection, novelID)
	if err != nil {
		return ErrNotFound
	}
	record.Set("last_check_new_chapters", 0)
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
	// Narrow projection: only the tags column is read, instead of hydrating up
	// to 5000 full novel records (glossary and prompts included).
	rows := []struct {
		Tags string `db:"tags"`
	}{}
	if err := s.App.DB().Select("tags").From(NovelsCollection).
		Where(dbx.NewExp("owner = {:owner}", dbx.Params{"owner": userID})).
		OrderBy("updated DESC").
		Limit(5000).
		All(&rows); err != nil {
		return nil, err
	}
	query = strings.TrimSpace(query)
	seen := make(map[string]string)
	for _, row := range rows {
		for _, tag := range parseNovelTagsJSON(row.Tags) {
			if query != "" && !strings.Contains(normalizeTagKey(tag), normalizeTagKey(query)) {
				continue
			}
			key := normalizeTagKey(tag)
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
	// Narrow projection: only the two series columns are read.
	rows := []struct {
		SourceSeries string `db:"source_series"`
		TargetSeries string `db:"target_series"`
	}{}
	if err := s.App.DB().Select("source_series", "target_series").From(NovelsCollection).
		Where(dbx.NewExp("owner = {:owner}", dbx.Params{"owner": userID})).
		OrderBy("updated DESC").
		Limit(5000).
		All(&rows); err != nil {
		return nil, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	seen := make(map[string]string)
	for _, row := range rows {
		for _, field := range []string{row.SourceSeries, row.TargetSeries} {
			series := strings.TrimSpace(field)
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

func (s *Store) ListNovelAuthorSuggestions(userID, query string, limit int) ([]string, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	// Narrow projection: only the two author columns are read.
	rows := []struct {
		SourceAuthor string `db:"source_author"`
		TargetAuthor string `db:"target_author"`
	}{}
	if err := s.App.DB().Select("source_author", "target_author").From(NovelsCollection).
		Where(dbx.NewExp("owner = {:owner}", dbx.Params{"owner": userID})).
		OrderBy("updated DESC").
		Limit(5000).
		All(&rows); err != nil {
		return nil, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	seen := make(map[string]string)
	for _, row := range rows {
		for _, field := range []string{row.SourceAuthor, row.TargetAuthor} {
			author := strings.TrimSpace(field)
			if author == "" {
				continue
			}
			if query != "" && !strings.Contains(strings.ToLower(author), query) {
				continue
			}
			key := strings.ToLower(author)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = author
		}
	}
	out := make([]string, 0, len(seen))
	for _, author := range seen {
		out = append(out, author)
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

// ListOwnedSeriesProgress aggregates chapter translation progress per series
// (target series falling back to source) in one GROUP BY, so the assistant's
// series questions ("which series are fully translated?") are a single query
// instead of a per-novel stats walk. Status semantics mirror
// v_agent_novel_progress: translated = translated/refined/done, excluded
// chapters never count. complete filters the result: "complete" keeps series
// with at least one chapter and none pending, "incomplete" the rest. query is
// a case-insensitive substring match on the series name. Filtering and the
// limit apply in Go so the SQL stays a plain aggregate and the LIMIT lands
// after the filters.
func (s *Store) ListOwnedSeriesProgress(userID, query, complete string, limit int) ([]SeriesProgress, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	rows := []struct {
		Series             string `db:"series"`
		Novels             int    `db:"novels"`
		ChaptersTotal      int    `db:"chapters_total"`
		ChaptersTranslated int    `db:"chapters_translated"`
	}{}
	err := s.App.DB().NewQuery(
		// The owner filter sits in both the outer query and the chapter
		// aggregate, so the chapters scan never touches another user's rows.
		"SELECT COALESCE(NULLIF(n.target_series, ''), n.source_series) AS series," +
			" COUNT(*) AS novels," +
			" COALESCE(SUM(c.chapters_total), 0) AS chapters_total," +
			" COALESCE(SUM(c.chapters_translated), 0) AS chapters_translated" +
			" FROM " + NovelsCollection + " n LEFT JOIN (" +
			"SELECT ch.novel AS novel_id," +
			" COUNT(*) AS chapters_total," +
			" SUM(CASE WHEN ch.status IN ('translated','refined','done') THEN 1 ELSE 0 END) AS chapters_translated" +
			" FROM " + ChaptersCollection + " ch JOIN " + NovelsCollection + " n2 ON n2.id = ch.novel" +
			" WHERE n2.owner = {:owner} AND ch.excluded = 0" +
			" GROUP BY ch.novel) c ON c.novel_id = n.id" +
			" WHERE n.owner = {:owner} AND COALESCE(NULLIF(n.target_series, ''), n.source_series) != ''" +
			" GROUP BY series ORDER BY series",
	).Bind(dbx.Params{"owner": userID}).All(&rows)
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(strings.TrimSpace(query))
	out := make([]SeriesProgress, 0, len(rows))
	for _, row := range rows {
		pending := row.ChaptersTotal - row.ChaptersTranslated
		fully := row.ChaptersTotal > 0 && pending == 0
		switch complete {
		case "complete":
			if !fully {
				continue
			}
		case "incomplete":
			if fully {
				continue
			}
		}
		if q != "" && !strings.Contains(strings.ToLower(row.Series), q) {
			continue
		}
		out = append(out, SeriesProgress{
			Series:             row.Series,
			Novels:             row.Novels,
			ChaptersTotal:      row.ChaptersTotal,
			ChaptersTranslated: row.ChaptersTranslated,
			ChaptersPending:    pending,
			FullyTranslated:    fully,
		})
		if len(out) >= limit {
			break
		}
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
	// The novel metadata (glossary, descriptions, prompts, ...) is global to
	// the novel and was fully copied above: the chapter loop must not re-read
	// it. Positions are taken verbatim from the source (dense-fallback for
	// legacy unpositioned rows) and every chapter is inserted in one
	// transaction — the previous loop went through UpsertChapterWithoutStats,
	// re-hydrating the full novel record and the highest-positioned chapter
	// once per chapter.
	useSourcePositions := true
	for _, chapter := range chapters {
		if chapter.Position <= 0 {
			useSourcePositions = false
			break
		}
	}
	for i := range chapters {
		chapters[i].ID = ""
		chapters[i].NovelID = clone.ID
		if !useSourcePositions {
			chapters[i].Position = i + 1
		}
	}
	if _, err := s.insertChaptersBulk(clone.ID, chapters); err != nil {
		return nil, err
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
	// Build all chapters first, then insert them in one transaction: per-chapter
	// upserts re-read the novel and recalculated positions once per chapter.
	chapterInputs := make([]Chapter, 0, len(input.Chapters))
	for idx, chapter := range input.Chapters {
		chapterInputs = append(chapterInputs, Chapter{
			ChapterOrder:    idx + 1,
			Position:        idx + 1,
			Title:           clampText(chapter.Title, 500),
			OriginalContent: chapter.Content,
			Status:          "pending",
		})
	}
	chapterIDs, err := s.insertChaptersBulk(resultNovel.ID, chapterInputs)
	if err != nil {
		return nil, err
	}
	if err := s.insertNovelImagesBulk(resultNovel.ID, importedEpubChapterImages(input.Chapters, chapterIDs)); err != nil {
		return nil, err
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
	// Build all chapters first, then insert them in one transaction (see
	// ImportEpubNovel). Source order 0 falls back to the dense index, matching
	// the previous auto-append behavior.
	chapterInputs := make([]Chapter, 0, len(input.Chapters))
	for _, chapter := range input.Chapters {
		status := "pending"
		if strings.TrimSpace(chapter.TranslatedContent) != "" {
			status = "translated"
		}
		chapterInputs = append(chapterInputs, Chapter{
			ChapterOrder:      chapter.Order,
			Title:             chapter.Title,
			TranslatedTitle:   chapter.TranslatedTitle,
			OriginalContent:   chapter.OriginalContent,
			TranslatedContent: chapter.TranslatedContent,
			Status:            status,
		})
	}
	for i := range chapterInputs {
		chapterInputs[i].Position = i + 1
		if chapterInputs[i].ChapterOrder == 0 {
			chapterInputs[i].ChapterOrder = i + 1
		}
	}
	chapterIDs, err := s.insertChaptersBulk(resultNovel.ID, chapterInputs)
	if err != nil {
		return nil, err
	}
	if err := s.insertNovelImagesBulk(resultNovel.ID, importedZipChapterImages(input.Chapters, chapterIDs)); err != nil {
		return nil, err
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

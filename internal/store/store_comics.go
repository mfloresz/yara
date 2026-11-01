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

func (s *Store) CreateComic(ownerID string, comic *Comic) error {
	comic.Status = normalizeComicStatus(comic.Status)
	comic.Tags = jsonString(parseComicTagsJSON(comic.Tags), "[]")
	collection, err := s.App.FindCollectionByNameOrId(ComicsCollection)
	if err != nil {
		return err
	}
	record := core.NewRecord(collection)
	record.Set("owner", ownerID)
	applyComicToRecord(record, comic)
	if err := s.App.Save(record); err != nil {
		return err
	}
	stored, err := s.GetComicAccessible(ownerID, record.Id)
	if err != nil {
		return err
	}
	*comic = *stored
	return nil
}

// ComicSortField is the sort field accepted by GET /api/db/comics.
type ComicSortField string

const (
	ComicSortTitle    ComicSortField = "title"
	ComicSortCreated  ComicSortField = "created"
	ComicSortLastRead ComicSortField = "lastRead"
)

// normalizeComicSortField validates a sort field, defaulting to title.
func normalizeComicSortField(sortField string) ComicSortField {
	switch ComicSortField(sortField) {
	case ComicSortCreated, ComicSortLastRead:
		return ComicSortField(sortField)
	default:
		return ComicSortTitle
	}
}

// normalizeComicSortOrder validates a sort order, defaulting to ascending.
func normalizeComicSortOrder(order string) string {
	if order == SortOrderDesc {
		return SortOrderDesc
	}
	return SortOrderAsc
}

// normalizeListComicOptions validates option values, mapping unknown values to
// the "no filter" default instead of erroring.
func normalizeListComicOptions(opts ListComicOptions) ListComicOptions {
	switch opts.Shared {
	case "own", "shared":
	default:
		opts.Shared = "all"
	}
	switch opts.Progress {
	case "downloaded", "completed", "ongoing":
	default:
		opts.Progress = "all"
	}
	opts.Tag = strings.TrimSpace(opts.Tag)
	opts.Author = strings.TrimSpace(opts.Author)
	opts.Series = strings.TrimSpace(opts.Series)
	opts.SearchField = normalizeSearchField(strings.TrimSpace(opts.SearchField))
	return opts
}

// normalizeListPagination clamps limit/offset to the values accepted by the API.
// ListComicOptions carries the optional library filters accepted by
// GET /api/v1/comics. All values default to "no filter" and are validated by
// normalizeListComicOptions.
type ListComicOptions struct {
	Tag         string // "" = no filter; exact match, case/accent-insensitive
	Author      string // "" = no filter; exact match, case-insensitive across source/target author
	Series      string // "" = no filter; exact match, case-insensitive across source/target series
	Shared      string // "all" | "own" | "shared"
	Progress    string // "all" | "downloaded" | "completed" | "ongoing"
	SearchField string // "all" | "title" | "author" | "series"; scopes ?q matching
}

// buildScopeFilter returns the visibility scope clause (shared filter) plus the
// progress clause as raw SQL (AND/OR, not fexpr &&/||) with the :owner param
// bound through dbx.Params. The scope is parenthesized so an appended AND
// progress clause cannot bind to only one arm of the OR scope.
func buildComicScopeFilter(opts ListComicOptions) string {
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
	case "downloaded":
		return scope + " AND chapter_count > 0 AND downloaded_count = chapter_count"
	case "completed":
		return scope + " AND status = 'completed'"
	case "ongoing":
		return scope + " AND status = 'ongoing'"
	}
	return scope
}

// filterComicsByTag keeps only comics whose tags include tag, compared in
// canonical form (case/accent-insensitive).
func filterComicsByTag(comics []Comic, tag string) []Comic {
	key := normalizeTagKey(tag)
	out := make([]Comic, 0, len(comics))
	for _, n := range comics {
		for _, t := range parseComicTagsJSON(n.Tags) {
			if normalizeTagKey(t) == key {
				out = append(out, n)
				break
			}
		}
	}
	return out
}

// filterComicsByStringFields keeps only comics where any of the values picked
// from the comic equals value exactly (case-insensitive, trimmed). Used for
// the author/series facet filters.
func filterComicsByStringFields(comics []Comic, value string, pick func(Comic) []string) []Comic {
	out := make([]Comic, 0, len(comics))
	for _, n := range comics {
		for _, v := range pick(n) {
			if strings.EqualFold(strings.TrimSpace(v), value) {
				out = append(out, n)
				break
			}
		}
	}
	return out
}

// comicListColumns is the narrow column set behind ListComics/SearchComics.
// The heavy per-comic blobs (glossary can reach several MB, plus the 8 prompt
// fields, notes, ai/translation options, cleanup rules and custom commands)
// are never displayed by list/search/suggestion flows — the library grid and
// the search dropdown request sparse fieldsets without them — so hydrating
// them turned every library render into a full-table blob read. Single-comic
// endpoints (GET /comics/{id}, POST /comics) keep full record hydration and
// still return the heavy fields. Descriptions are kept: the default (no
// ?fields=) list response includes them and they are small in practice.
// One entry per column: dbx quotes each Select() argument as a whole column
// expression, so a multi-column string would break the query.
var comicListColumns = []string{
	"id", "owner", "is_public",
	"source_language", "target_language",
	"source_title", "source_author", "source_series", "source_number",
	"target_title", "target_author", "target_series", "target_number",
	"source_description", "target_description",
	"status", "tags", "url", "cover", "thumbnail",
	"chapter_count", "downloaded_count", "max_chapter_order",
	"last_checked_at", "last_check_new_chapters",
	"created", "updated",
}

// comicListRow mirrors comicListColumns for dbx struct scanning. Numerics and
// booleans are scanned as float64 because SQLite NUMERIC values come back with
// INTEGER or REAL storage class depending on the value.
type comicListRow struct {
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
	DownloadedCount      float64 `db:"downloaded_count"`
	MaxChapterOrder      float64 `db:"max_chapter_order"`
	LastCheckedAt        string  `db:"last_checked_at"`
	LastCheckNewChapters float64 `db:"last_check_new_chapters"`
	Created              string  `db:"created"`
	Updated              string  `db:"updated"`
}

// firstFileName extracts the first file name from a PocketBase file-field
// column, which stores either a JSON array of names or a bare filename.
func comicFromListRow(row comicListRow) Comic {
	coverFile := firstFileName(row.Cover)
	thumbFile := firstFileName(row.Thumbnail)
	return Comic{
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
		URL:                  row.URL,
		Status:               normalizeComicStatus(row.Status),
		Tags:                 jsonString(parseComicTagsJSON(row.Tags), "[]"),
		CoverFile:            coverFile,
		CoverPath:            comicCoverURL(row.ID, coverFile),
		ThumbnailFile:        thumbFile,
		ThumbnailPath:        comicCoverURL(row.ID, thumbFile),
		IsPublic:             row.IsPublic != 0,
		ChapterCount:         asInt(row.ChapterCount, 0),
		DownloadedCount:      asInt(row.DownloadedCount, 0),
		MaxChapterOrder:      asInt(row.MaxChapterOrder, 0),
		LastCheckedAt:        row.LastCheckedAt,
		LastCheckNewChapters: asInt(row.LastCheckNewChapters, 0),
		CreatedAt:            row.Created,
		UpdatedAt:            row.Updated,
	}
}

// findComicListRows runs one projected list query. where is raw SQL with
// {:name} placeholders; limit 0 means unbounded.
func (s *Store) findComicListRows(where string, params dbx.Params, limit, offset int, orderBys ...string) ([]Comic, error) {
	query := s.App.DB().
		Select(comicListColumns...).
		From(ComicsCollection).
		Where(dbx.NewExp(where, params)).
		OrderBy(orderBys...)
	if limit > 0 {
		query = query.Limit(int64(limit))
	}
	if offset > 0 {
		query = query.Offset(int64(offset))
	}
	rows := []comicListRow{}
	if err := query.All(&rows); err != nil {
		return nil, err
	}
	out := make([]Comic, 0, len(rows))
	for _, row := range rows {
		out = append(out, comicFromListRow(row))
	}
	return out, nil
}

func (s *Store) ListComics(userID string, limit int, offset int, sortField string, sortOrder string, opts ListComicOptions) ([]Comic, bool, error) {
	sortField = string(normalizeComicSortField(sortField))
	sortOrder = normalizeComicSortOrder(sortOrder)
	limit, offset = normalizeListPagination(limit, offset)
	opts = normalizeListComicOptions(opts)

	where := buildComicScopeFilter(opts)
	params := dbx.Params{"owner": userID}

	// Tag/author/series filtering is Go-level post-filtering over the full
	// scope: running it after the DB offset would leave holes in pages and make
	// meta.total inconsistent, so force the in-memory route when one is active.
	if opts.Tag == "" && opts.Author == "" && opts.Series == "" && sortField == string(ComicSortCreated) {
		// DB-level sort keeps offset pagination consistent for arbitrarily large libraries.
		// The UI treats "asc" as most-recent-first for created, so asc maps to -created.
		dbSort := "created DESC"
		if sortOrder == SortOrderDesc {
			dbSort = "created ASC"
		}
		// Request limit+1 to detect whether more results exist.
		page, err := s.findComicListRows(where, params, limit+1, offset, dbSort)
		if err != nil {
			return nil, false, err
		}
		hasMore := len(page) > limit
		if hasMore {
			page = page[:limit]
		}
		s.populateComicLastReadAt(page, userID)
		return page, hasMore, nil
	}

	// title and lastRead depend on display-level data (target-or-source title and
	// per-user reading progress), so sort the full result set in memory (unbounded
	// fetch) and slice the sorted window. Pages stay globally consistent for the
	// same query, and all comics are reachable regardless of library size. Rows
	// use the narrow list projection, so the unbounded fetch no longer
	// transfers each comic's glossary/prompts.
	all, err := s.findComicListRows(where, params, 0, 0, "created DESC")
	if err != nil {
		return nil, false, err
	}
	s.populateComicLastReadAt(all, userID)
	if opts.Tag != "" {
		all = filterComicsByTag(all, opts.Tag)
	}
	if opts.Author != "" {
		all = filterComicsByStringFields(all, opts.Author, func(n Comic) []string {
			return []string{n.SourceAuthor, n.TargetAuthor}
		})
	}
	if opts.Series != "" {
		all = filterComicsByStringFields(all, opts.Series, func(n Comic) []string {
			return []string{n.SourceSeries, n.TargetSeries}
		})
	}
	sortComicsInMemory(all, ComicSortField(sortField), sortOrder)
	page, hasMore := paginateComics(all, limit, offset)
	return page, hasMore, nil
}

// SearchComics searches comics by title, author, series, or tags matching the
// given query. opts.SearchField scopes the match: "title" (source/target
// title), "author" (source/target author), "series" (source/target series),
// "tags" (tag list), or "all" (default: title + author + series + tags).
// Supports pagination via limit/offset, scoped to comics the user owns or are
// public.
func (s *Store) SearchComics(userID, query string, limit int, offset int, sortField string, sortOrder string, opts ListComicOptions) ([]Comic, bool, error) {
	sortField = string(normalizeComicSortField(sortField))
	sortOrder = normalizeComicSortOrder(sortOrder)
	limit, offset = normalizeListPagination(limit, offset)
	opts = normalizeListComicOptions(opts)
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
	where := buildComicScopeFilter(opts) + " AND " + matchClause
	params := dbx.Params{"owner": userID, "like": like}

	if opts.Tag == "" && opts.Author == "" && opts.Series == "" && sortField == string(ComicSortCreated) {
		// The UI treats "asc" as most-recent-first for created, so asc maps to -created.
		dbSort := "created DESC"
		if sortOrder == SortOrderDesc {
			dbSort = "created ASC"
		}
		page, err := s.findComicListRows(where, params, limit+1, offset, dbSort)
		if err != nil {
			return nil, false, err
		}
		hasMore := len(page) > limit
		if hasMore {
			page = page[:limit]
		}
		s.populateComicLastReadAt(page, userID)
		return page, hasMore, nil
	}

	all, err := s.findComicListRows(where, params, 0, 0, "created DESC")
	if err != nil {
		return nil, false, err
	}
	s.populateComicLastReadAt(all, userID)
	if opts.Tag != "" {
		all = filterComicsByTag(all, opts.Tag)
	}
	if opts.Author != "" {
		all = filterComicsByStringFields(all, opts.Author, func(n Comic) []string {
			return []string{n.SourceAuthor, n.TargetAuthor}
		})
	}
	if opts.Series != "" {
		all = filterComicsByStringFields(all, opts.Series, func(n Comic) []string {
			return []string{n.SourceSeries, n.TargetSeries}
		})
	}
	sortComicsInMemory(all, ComicSortField(sortField), sortOrder)
	page, hasMore := paginateComics(all, limit, offset)
	return page, hasMore, nil
}

// paginateComics slices a fully-sorted slice into the requested page.
func paginateComics(sorted []Comic, limit, offset int) ([]Comic, bool) {
	if offset >= len(sorted) {
		return []Comic{}, false
	}
	end := offset + limit
	hasMore := end < len(sorted)
	if end > len(sorted) {
		end = len(sorted)
	}
	return sorted[offset:end], hasMore
}

// sortComicsInMemory orders comics by display title, created time, or last read
// time using the same semantics the UI applies, so server pagination matches the
// order users see. Comics with no last read time always sort last.
func sortComicsInMemory(comics []Comic, sortField ComicSortField, order string) {
	desc := order == SortOrderDesc
	switch sortField {
	case ComicSortCreated:
		sort.SliceStable(comics, func(i, j int) bool {
			return compareTimestampStrings(comics[i].CreatedAt, comics[j].CreatedAt, desc)
		})
	case ComicSortLastRead:
		sort.SliceStable(comics, func(i, j int) bool {
			return compareLastReadStrings(comics[i].LastReadAt, comics[j].LastReadAt, desc)
		})
	default: // title
		sort.SliceStable(comics, func(i, j int) bool {
			left, right := comicDisplayTitle(comics[i]), comicDisplayTitle(comics[j])
			c := compareTitleStrings(left, right)
			if c != 0 {
				if desc {
					return c > 0
				}
				return c < 0
			}
			// Deterministic tiebreak so pagination stays stable across requests.
			if desc {
				return comics[i].ID > comics[j].ID
			}
			return comics[i].ID < comics[j].ID
		})
	}
}

// comicDisplayTitle mirrors the UI's getComicDisplayTitle: prefer the target title,
// fall back to the source title.
func comicDisplayTitle(n Comic) string {
	if n.TargetTitle != "" {
		return n.TargetTitle
	}
	return n.SourceTitle
}

// populateLastReadAt fetches reading progress for all comics and fills LastReadAt.
// Narrow projection (comic, updated): the previous version hydrated every
// progress record of the user on every library listing.
func (s *Store) populateComicLastReadAt(comics []Comic, userID string) {
	if len(comics) == 0 {
		return
	}
	rows := []struct {
		Comic   string `db:"comic"`
		Updated string `db:"updated"`
	}{}
	err := s.App.DB().Select("comic", "updated").From(ComicReadingProgressCollection).
		Where(dbx.NewExp("user = {:user}", dbx.Params{"user": userID})).
		OrderBy("updated DESC").
		All(&rows)
	if err != nil {
		return
	}
	lastReadMap := make(map[string]string, len(rows))
	for _, row := range rows {
		if _, exists := lastReadMap[row.Comic]; !exists {
			lastReadMap[row.Comic] = row.Updated
		}
	}
	for i := range comics {
		comics[i].LastReadAt = lastReadMap[comics[i].ID]
	}
}

func (s *Store) ListOwnedComicsWithURL(ownerID string) ([]Comic, error) {
	const pageSize = 200
	var out []Comic
	offset := 0
	for {
		records, err := s.App.FindRecordsByFilter(ComicsCollection, "owner = {:owner} && url != '' && status != 'completed' && status != 'cancelled'", "-created", pageSize, offset, dbx.Params{"owner": ownerID})
		if err != nil {
			return nil, err
		}
		for _, record := range records {
			out = append(out, s.comicFromRecord(record))
		}
		if len(records) < pageSize {
			break
		}
		offset += pageSize
	}
	if out == nil {
		out = []Comic{}
	}
	return out, nil
}

func (s *Store) GetOwnedComicChapterIDsByStatus(userID, comicID string) (pendingIDs []string, err error) {
	if _, err := s.GetOwnedComic(userID, comicID); err != nil {
		return nil, err
	}
	records, err := s.App.FindRecordsByFilter(ComicChaptersCollection, "comic = {:comic} && excluded = false && status = 'pending'", "position,chapter_order", 5000, 0, dbx.Params{"comic": comicID})
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.Id)
	}
	return ids, nil
}

func (s *Store) GetComicAccessible(userID, comicID string) (*Comic, error) {
	record, err := s.App.FindRecordById(ComicsCollection, comicID)
	if err != nil {
		return nil, ErrNotFound
	}
	if record.GetString("owner") != userID && !record.GetBool("is_public") {
		return nil, ErrNotFound
	}
	comic := s.comicFromRecord(record)
	return &comic, nil
}

// GetComicCoverFile returns the comic record plus the file name to serve as
// its cover image (the thumbnail when present, the full cover otherwise).
// Access follows the comic's visibility: owner or is_public.
func (s *Store) GetComicCoverFile(userID, comicID string) (*core.Record, string, error) {
	record, err := s.App.FindRecordById(ComicsCollection, comicID)
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

func (s *Store) GetOwnedComic(userID, comicID string) (*Comic, error) {
	record, err := s.App.FindRecordById(ComicsCollection, comicID)
	if err != nil {
		return nil, ErrNotFound
	}
	if record.GetString("owner") != userID {
		return nil, ErrForbidden
	}
	comic := s.comicFromRecord(record)
	return &comic, nil
}

func (s *Store) UpdateComic(userID, comicID string, patch map[string]any) (*Comic, error) {
	record, err := s.App.FindRecordById(ComicsCollection, comicID)
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
		case "notes":
			record.Set("notes", value)
		case "url":
			record.Set("url", value)
		case "status":
			record.Set("status", normalizeComicStatus(fmt.Sprint(value)))
		case "tags":
			record.Set("tags", jsonString(normalizeComicTagsValue(value), "[]"))
		case "isPublic":
			record.Set("is_public", value)
		}
	}
	if err := s.App.Save(record); err != nil {
		return nil, err
	}
	updated := s.comicFromRecord(record)
	return &updated, nil
}

func (s *Store) UpdateComicCheckResult(comicID, checkedAt string, newChapters int) error {
	record, err := s.App.FindRecordById(ComicsCollection, comicID)
	if err != nil {
		return ErrNotFound
	}
	record.Set("last_checked_at", checkedAt)
	record.Set("last_check_new_chapters", newChapters)
	return s.App.Save(record)
}

// ClearComicPendingNewChapters resets the pending "new chapters" counter once
// those chapters have actually been downloaded. last_checked_at is left
// untouched: the check did happen, only its result is now consumed.
func (s *Store) ClearComicPendingNewChapters(comicID string) error {
	record, err := s.App.FindRecordById(ComicsCollection, comicID)
	if err != nil {
		return ErrNotFound
	}
	record.Set("last_check_new_chapters", 0)
	return s.App.Save(record)
}

func (s *Store) DeleteComic(userID, comicID string) error {
	record, err := s.App.FindRecordById(ComicsCollection, comicID)
	if err != nil {
		return ErrNotFound
	}
	if record.GetString("owner") != userID {
		return ErrForbidden
	}
	return s.App.Delete(record)
}

func (s *Store) SetComicVisibility(userID, comicID string, isPublic bool) (*Comic, error) {
	return s.UpdateComic(userID, comicID, map[string]any{"isPublic": isPublic})
}

// ListOwnedSeriesProgress aggregates chapter translation progress per series
// (target series falling back to source) in one GROUP BY, so the assistant's
// series questions ("which series are fully translated?") are a single query
// instead of a per-comic stats walk. Status semantics mirror
// v_agent_comic_progress: translated = translated/refined/done, excluded
// chapters never count. complete filters the result: "complete" keeps series
// with at least one chapter and none pending, "incomplete" the rest. query is
// a case-insensitive substring match on the series name. Filtering and the
// limit apply in Go so the SQL stays a plain aggregate and the LIMIT lands
// after the filters.
func (s *Store) ImportUrlComic(input *ImportUrlComicInput) (*ImportUrlComicResult, error) {
	if input == nil {
		return nil, fmt.Errorf("import input required")
	}
	comic := &Comic{
		URL:               input.URL,
		SourceLanguage:    input.SourceLanguage,
		SourceTitle:       strings.TrimSpace(input.SourceTitle),
		SourceAuthor:      clampText(input.SourceAuthor, 250),
		SourceDescription: clampText(input.SourceDescription, 5000),
		TargetLanguage:    input.TargetLanguage,
		Status:            "ongoing",
		Tags:              "[]",
	}
	if err := s.CreateComic(input.OwnerID, comic); err != nil {
		return nil, err
	}
	fresh, err := s.GetOwnedComic(input.OwnerID, comic.ID)
	if err != nil {
		return nil, err
	}
	*comic = *fresh
	return &ImportUrlComicResult{Comic: *comic, ChaptersImported: 0}, nil
}

func (s *Store) ImportZipComic(input *ImportZipComicInput) (*ImportZipComicResult, error) {
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
		Status            string `json:"status"`
		IsPublic          bool   `json:"isPublic"`
	}{}
	if err := json.Unmarshal([]byte(input.MetadataJSON), &meta); err != nil {
		return nil, fmt.Errorf("invalid metadata.json: %w", err)
	}
	canonicalSourceTitle := strings.TrimSpace(meta.SourceTitle)
	if canonicalSourceTitle == "" {
		return nil, fmt.Errorf("sourceTitle is required in metadata.json")
	}
	if meta.SourceLanguage == "" {
		return nil, fmt.Errorf("sourceLanguage is required in metadata.json")
	}
	if meta.TargetLanguage == "" {
		return nil, fmt.Errorf("targetLanguage is required in metadata.json")
	}
	resultComic := &Comic{
		SourceLanguage:    meta.SourceLanguage,
		TargetLanguage:    meta.TargetLanguage,
		URL:               meta.URL,
		SourceTitle:       canonicalSourceTitle,
		SourceAuthor:      clampText(meta.SourceAuthor, 250),
		SourceDescription: clampText(meta.SourceDescription, 5000),
		SourceSeries:      meta.SourceSeries,
		SourceNumber:      meta.SourceNumber,
		TargetTitle:       meta.TargetTitle,
		TargetAuthor:      meta.TargetAuthor,
		TargetDescription: meta.TargetDescription,
		TargetSeries:      meta.TargetSeries,
		TargetNumber:      meta.TargetNumber,
		Notes:             meta.Notes,
		Status:            normalizeComicStatus(meta.Status),
		Tags:              "[]",
		IsPublic:          meta.IsPublic,
	}
	if err := s.CreateComic(input.OwnerID, resultComic); err != nil {
		return nil, err
	}
	if len(input.CoverBlob) > 0 {
		if err := s.attachComicCover(resultComic.ID, input.CoverBlob, input.CoverMime); err != nil {
			return nil, err
		}
	}
	// Build all chapters first, then insert them in one transaction; each
	// chapter's images become its pages in reading order.
	chapterInputs := make([]ComicChapter, 0, len(input.Chapters))
	for _, chapter := range input.Chapters {
		chapterInputs = append(chapterInputs, ComicChapter{
			ChapterOrder: chapter.Order,
			Title:        chapter.Title,
			Status:       ComicChapterPending,
		})
	}
	for i := range chapterInputs {
		chapterInputs[i].Position = i + 1
		if chapterInputs[i].ChapterOrder == 0 {
			chapterInputs[i].ChapterOrder = i + 1
		}
	}
	chapterIDs, err := s.insertComicChaptersBulk(resultComic.ID, chapterInputs)
	if err != nil {
		return nil, err
	}
	for i, chapter := range input.Chapters {
		uploads := make([]ComicPageUpload, 0, len(chapter.Images))
		for j, img := range chapter.Images {
			uploads = append(uploads, ComicPageUpload{
				Data:      img.Blob,
				MimeType:  img.MimeType,
				SourceURL: "",
				Order:     j + 1,
			})
		}
		if err := s.ReplaceComicChapterPages(chapterIDs[i], uploads); err != nil {
			return nil, err
		}
	}
	if err := s.RecalculateComicStats(resultComic.ID); err != nil {
		return nil, err
	}
	fresh, err := s.GetOwnedComic(input.OwnerID, resultComic.ID)
	if err != nil {
		return nil, err
	}
	*resultComic = *fresh
	return &ImportZipComicResult{Comic: *resultComic, ChaptersImported: len(input.Chapters)}, nil
}

func (s *Store) attachComicCover(comicID string, blob []byte, mimeType string) error {
	collection, err := s.App.FindCollectionByNameOrId(ComicsCollection)
	if err != nil {
		return err
	}
	record, err := s.App.FindRecordById(collection, comicID)
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
	s.attachCoverThumbnail(comicID, blob)
	return nil
}

func (s *Store) AttachComicCoverBlob(comicID string, blob []byte, mimeType string) error {
	return s.attachComicCover(comicID, blob, mimeType)
}

func (s *Store) UpdateComicCover(userID, comicID string, blob []byte, mimeType string) (*Comic, error) {
	if _, err := s.GetOwnedComic(userID, comicID); err != nil {
		return nil, err
	}
	if err := s.attachComicCover(comicID, blob, mimeType); err != nil {
		return nil, err
	}
	return s.GetOwnedComic(userID, comicID)
}

func (s *Store) readComicFileBlob(comicID, fileName string) ([]byte, string, error) {
	collection, err := s.App.FindCollectionByNameOrId(ComicsCollection)
	if err != nil {
		return nil, "", err
	}
	record, err := s.App.FindRecordById(collection, comicID)
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

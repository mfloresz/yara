package store

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

func (s *Store) ListChaptersAccessible(userID, novelID string) ([]Chapter, error) {
	if _, err := s.GetNovelAccessible(userID, novelID); err != nil {
		return nil, err
	}
	// Reading/display/export order is the user-controlled position; excluded
	// chapters are hidden everywhere except restore-oriented listings.
	total, err := s.totalChapterCount(novelID)
	if err != nil {
		return nil, err
	}
	records, err := s.App.FindRecordsByFilter(ChaptersCollection, "novel = {:novel} && excluded = false", "position,chapter_order", dynamicChapterLimit(total), 0, dbx.Params{"novel": novelID})
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
	total, err := s.totalChapterCount(novelID)
	if err != nil {
		return nil, err
	}
	return s.findChapterSummaries(
		"novel = {:novel} AND excluded = 0",
		dynamicChapterLimit(total), 0,
		dbx.Params{"novel": novelID},
		"position ASC", "chapter_order ASC",
	)
}

func (s *Store) ListEligibleChapterSummariesAccessible(userID, novelID, operation string) ([]ChapterSummary, error) {
	if _, err := s.GetNovelAccessible(userID, novelID); err != nil {
		return nil, err
	}
	// Same eligibility filters as before the projection rewrite; the content
	// columns are referenced in the WHERE clause but never selected.
	where := "novel = {:novel} AND excluded = 0 AND (status = 'pending' OR status = 'failed') AND original_content != ''"
	if operation == "refine" {
		where = "novel = {:novel} AND excluded = 0 AND (status = 'translated' OR status = 'failed') AND translated_content != ''"
	}
	total, err := s.totalChapterCount(novelID)
	if err != nil {
		return nil, err
	}
	return s.findChapterSummaries(
		where,
		dynamicChapterLimit(total), 0,
		dbx.Params{"novel": novelID},
		"position ASC", "chapter_order ASC",
	)
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
	summaries, err := s.findChapterSummaries(
		"novel = {:novel} AND excluded = 0",
		limit, offset,
		dbx.Params{"novel": novelID},
		"position ASC", "chapter_order ASC",
	)
	if err != nil {
		return nil, 0, err
	}
	return summaries, novel.ChapterCount, nil
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
	// Single aggregate over narrow columns: the previous version loaded every
	// chapter record with full content, so each recalculation scaled with the
	// novel's total content size and was paid once per chapter during refine
	// jobs (SaveRefinedContentIfUnchanged). Record hydration has no column
	// projection, so this reads straight through dbx instead.
	// Semantics preserved from the record loop: excluded chapters count toward
	// max_chapter_order only; visible counters and char sums skip them.
	stats := struct {
		Total      int64   `db:"total"`
		Translated int64   `db:"translated"`
		Completed  int64   `db:"completed"`
		OrigChars  float64 `db:"orig_chars"`
		TransChars float64 `db:"trans_chars"`
		RefChars   float64 `db:"ref_chars"`
		MaxOrder   float64 `db:"max_order"`
	}{}
	err := s.App.DB().NewQuery(`
		SELECT
			COALESCE(SUM(CASE WHEN excluded = 0 THEN 1 ELSE 0 END), 0) AS total,
			COALESCE(SUM(CASE WHEN excluded = 0 AND status IN ('translated', 'refined', 'done') THEN 1 ELSE 0 END), 0) AS translated,
			COALESCE(SUM(CASE WHEN excluded = 0 AND status IN ('refined', 'done') THEN 1 ELSE 0 END), 0) AS completed,
			COALESCE(SUM(CASE WHEN excluded = 0 THEN original_char_count ELSE 0 END), 0) AS orig_chars,
			COALESCE(SUM(CASE WHEN excluded = 0 THEN translated_char_count ELSE 0 END), 0) AS trans_chars,
			COALESCE(SUM(CASE WHEN excluded = 0 THEN refined_char_count ELSE 0 END), 0) AS ref_chars,
			COALESCE(MAX(chapter_order), 0) AS max_order
		FROM chapters
		WHERE novel = {:novel}
	`).Bind(dbx.Params{"novel": novelID}).One(&stats)
	if err != nil {
		return err
	}
	originalChars := asInt(stats.OrigChars, 0)
	translatedChars := asInt(stats.TransChars, 0)
	refinedChars := asInt(stats.RefChars, 0)
	// Narrow counter UPDATE instead of a record Save: a Save rewrites the whole
	// novel row (glossary alone can reach several MB) once per recalculation.
	// `updated` is bumped explicitly to keep the autodate semantics of Save.
	res, err := s.App.DB().NewQuery(`
		UPDATE novels SET
			chapter_count = {:total},
			translated_count = {:translated},
			completed_count = {:completed},
			original_char_count = {:orig_chars},
			translated_char_count = {:trans_chars},
			refined_char_count = {:ref_chars},
			total_char_count = {:total_chars},
			max_chapter_order = {:max_order},
			updated = {:updated}
		WHERE id = {:id}
	`).Bind(dbx.Params{
		"total":      int(stats.Total),
		"translated": int(stats.Translated),
		"completed":  int(stats.Completed),
		"orig_chars": originalChars,
		"trans_chars": translatedChars,
		"ref_chars":   refinedChars,
		"total_chars": originalChars + translatedChars + refinedChars,
		"max_order":   asInt(stats.MaxOrder, 0),
		"updated":     types.NowDateTime().String(),
		"id":          novelID,
	}).Execute()
	if err != nil {
		return err
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return ErrNotFound
	}
	return nil
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

// chapterSummaryColumns is the narrow column set behind ChapterSummary. The
// content columns are only referenced through the has_* expressions — never
// selected — so listing a 1000-chapter novel no longer transfers its full
// text just to compute three booleans. TRIM only strips spaces (SQLite has no
// unicode-aware trim), so content holding only tabs/newlines would report
// has_* = true where the previous strings.TrimSpace check reported false;
// whitespace-only chapter content does not occur in practice.
const chapterSummaryColumns = `id, novel, chapter_order, position, excluded,
	title, translated_title, status, error_message,
	original_char_count, translated_char_count, refined_char_count,
	created, updated,
	TRIM(COALESCE(original_content, '')) <> '' AS has_original,
	TRIM(COALESCE(translated_content, '')) <> '' AS has_translated,
	TRIM(COALESCE(refined_content, '')) <> '' AS has_refined`

// chapterSummaryRow mirrors chapterSummaryColumns for dbx struct scanning.
// Numerics are scanned as float64 because SQLite NUMERIC values come back
// with INTEGER or REAL storage class depending on the value (and SUM()
// always returns REAL).
type chapterSummaryRow struct {
	ID              string  `db:"id"`
	Novel           string  `db:"novel"`
	ChapterOrder    float64 `db:"chapter_order"`
	Position        float64 `db:"position"`
	Excluded        float64 `db:"excluded"`
	Title           string  `db:"title"`
	TranslatedTitle string  `db:"translated_title"`
	Status          string  `db:"status"`
	ErrorMessage    string  `db:"error_message"`
	OriginalChars   float64 `db:"original_char_count"`
	TranslatedChars float64 `db:"translated_char_count"`
	RefinedChars    float64 `db:"refined_char_count"`
	HasOriginal     float64 `db:"has_original"`
	HasTranslated   float64 `db:"has_translated"`
	HasRefined      float64 `db:"has_refined"`
	Created         string  `db:"created"`
	Updated         string  `db:"updated"`
}

func (row chapterSummaryRow) summary() ChapterSummary {
	return ChapterSummary{
		ID:                   row.ID,
		NovelID:              row.Novel,
		ChapterOrder:         asInt(row.ChapterOrder, 0),
		Position:             asInt(row.Position, 0),
		Excluded:             row.Excluded != 0,
		Title:                row.Title,
		TranslatedTitle:      row.TranslatedTitle,
		Status:               defaultString(row.Status, "pending"),
		ErrorMessage:         row.ErrorMessage,
		HasOriginalContent:   row.HasOriginal != 0,
		HasTranslatedContent: row.HasTranslated != 0,
		HasRefinedContent:    row.HasRefined != 0,
		OriginalChars:        asInt(row.OriginalChars, 0),
		TranslatedChars:      asInt(row.TranslatedChars, 0),
		RefinedChars:         asInt(row.RefinedChars, 0),
		CreatedAt:            asDateTimeString(row.Created),
		UpdatedAt:            asDateTimeString(row.Updated),
	}
}

// asDateTimeString normalizes a raw autodate column value (TEXT in the
// PocketBase default date layout) exactly like Record.GetDateTime(...).String().
func asDateTimeString(raw string) string {
	dt, _ := types.ParseDateTime(raw)
	return dt.String()
}

// findChapterSummaries runs one projected summary query. where is raw SQL
// (AND, not fexpr &&) with {:name} placeholders bound through params.
func (s *Store) findChapterSummaries(where string, limit, offset int, params dbx.Params, orderBys ...string) ([]ChapterSummary, error) {
	query := s.App.DB().
		Select(chapterSummaryColumns).
		From("chapters").
		Where(dbx.NewExp(where, params)).
		OrderBy(orderBys...)
	if limit > 0 {
		query = query.Limit(int64(limit))
	}
	if offset > 0 {
		query = query.Offset(int64(offset))
	}
	rows := []chapterSummaryRow{}
	if err := query.All(&rows); err != nil {
		return nil, err
	}
	out := make([]ChapterSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.summary())
	}
	return out, nil
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

// GetChapterNeighborsAccessible returns the previous/next visible chapters in
// reading order (position, the user-controlled order). Excluded chapters are
// never neighbors. Each lookup is a single indexed query with LIMIT 1, so
// resolving neighbors costs O(log n) instead of loading the full list.
func (s *Store) GetChapterNeighborsAccessible(userID, novelID, chapterID string) (prev, next *ChapterSummary, err error) {
	current, err := s.GetChapterAccessible(userID, novelID, chapterID)
	if err != nil {
		return nil, nil, err
	}
	// Legacy rows may still carry position = 0 (pre-migration); fall back to
	// chapter_order comparisons so neighbors still resolve for them.
	key := "position"
	cur := current.Position
	if cur <= 0 {
		key = "chapter_order"
		cur = current.ChapterOrder
	}
	base := dbx.Params{"novel": novelID, "cur": cur}
	// key comes from the fixed position/chapter_order pair above, never from
	// user input, so concatenating it into the SQL where clause is safe.
	findOne := func(where, orderBy string) *ChapterSummary {
		summaries, qerr := s.findChapterSummaries(where, 1, 0, base, orderBy)
		if qerr != nil || len(summaries) == 0 {
			return nil
		}
		summary := summaries[0]
		return &summary
	}
	prev = findOne("novel = {:novel} AND excluded = 0 AND "+key+" < {:cur} AND "+key+" > 0", key+" DESC")
	next = findOne("novel = {:novel} AND excluded = 0 AND "+key+" > {:cur}", key+" ASC")
	return prev, next, nil
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
		if chapter.Position != 0 {
			curPos := asInt(record.GetFloat("position"), 0)
			if curPos == 0 {
				curPos, _ = s.maxChapterPosition(novelID)
				if curPos == 0 {
					curPos = 1
				}
			}
			if chapter.Position != curPos {
				if chapter.Position < 1 {
					return nil, fmt.Errorf("%w: position must be between 1 and %d", ErrInvalidReorder, curPos)
				}
				maxPos, _ := s.maxChapterPosition(novelID)
				if chapter.Position > maxPos {
					return nil, fmt.Errorf("%w: position must be between 1 and %d", ErrInvalidReorder, maxPos)
				}
				active, err := s.HasActiveJobsForNovel(novelID)
				if err != nil {
					return nil, err
				}
				if active {
					return nil, ErrActiveJobs
				}
				desiredPos := chapter.Position
				oldPos := curPos
				txErr := s.App.RunInTransaction(func(txApp core.App) error {
					records, err := txApp.FindRecordsByFilter(JobsCollection, "novel = {:novel} && (status = 'pending' || status = 'running')", "", 1, 0, dbx.Params{"novel": novelID})
					if err != nil {
						return err
					}
					if len(records) > 0 {
						return ErrActiveJobs
					}
					// Park the moved row at -1 so the shifted range never collides
					// with it, then shift with narrow ordered updates — the old
					// version hydrated every shifted chapter (content included)
					// and pushed a full record save per row.
					updated := types.NowDateTime().String()
					if _, err := txApp.DB().NewQuery(
						"UPDATE chapters SET position = -1, updated = {:updated} WHERE id = {:id}",
					).Bind(dbx.Params{"id": record.Id, "updated": updated}).Execute(); err != nil {
						return err
					}
					if desiredPos < oldPos {
						if err := shiftChapterPositions(txApp, novelID, desiredPos, oldPos-1, +1); err != nil {
							return err
						}
					} else {
						if err := shiftChapterPositions(txApp, novelID, oldPos+1, desiredPos, -1); err != nil {
							return err
						}
					}
					if _, err := txApp.DB().NewQuery(
						"UPDATE chapters SET position = {:pos}, updated = {:updated} WHERE id = {:id}",
					).Bind(dbx.Params{"id": record.Id, "pos": desiredPos, "updated": updated}).Execute(); err != nil {
						return err
					}
					return nil
				})
				if txErr != nil {
					return nil, txErr
				}
				// clear Position so the common update block does not try to
				// overwrite it again, and fall through to update other fields
				chapter.Position = 0
				record, _ = s.App.FindRecordById(ChaptersCollection, record.Id)
			} else {
				chapter.Position = 0
			}
		}
	} else {
		// New chapter: handle explicit position insertion with atomic shift.
		// When chapter.Position is 0 or omitted the chapter appends after the
		// current maximum position (backwards compatible). When a valid position
		// 1..max+1 is supplied and it is not the append position, the suffix
		// [pos, max] is shifted by +1 inside a transaction so the insert is
		// atomic and never leaves a hole or a duplicate under the
		// (novel, position) unique index. A shift is treated as a reorder and
		// is rejected with ErrActiveJobs while the novel has pending/running jobs.
		desiredPos := chapter.Position
		desiredOrder := chapter.ChapterOrder
		if desiredOrder == 0 {
			maxOrder, err := s.maxChapterOrder(novelID)
			if err != nil {
				return nil, err
			}
			desiredOrder = maxOrder + 1
		}
		maxPos, posErr := s.maxChapterPosition(novelID)
		if posErr != nil {
			return nil, posErr
		}
		needsPos := desiredPos != 0
		if !needsPos {
			desiredPos = maxPos + 1
		} else {
			if desiredPos < 1 || desiredPos > maxPos+1 {
				return nil, fmt.Errorf("%w: position must be between 1 and %d", ErrInvalidReorder, maxPos+1)
			}
		}
		needsShift := desiredPos <= maxPos
		if needsShift {
			active, err := s.HasActiveJobsForNovel(novelID)
			if err != nil {
				return nil, err
			}
			if active {
				return nil, ErrActiveJobs
			}
			var stored *Chapter
			txErr := s.App.RunInTransaction(func(txApp core.App) error {
				records, err := txApp.FindRecordsByFilter(JobsCollection, "novel = {:novel} && (status = 'pending' || status = 'running')", "", 1, 0, dbx.Params{"novel": novelID})
				if err != nil {
					return err
				}
				if len(records) > 0 {
					return ErrActiveJobs
				}
				// Shift the [desiredPos, maxPos] suffix up by one with narrow
				// ordered updates instead of a hydrated record loop; maxPos
				// +1 is always free, so descending order keeps the unique
				// (novel, position) index satisfied on every row.
				if err := shiftChapterPositions(txApp, novelID, desiredPos, maxPos, +1); err != nil {
					return err
				}
				collection, cErr := txApp.FindCollectionByNameOrId(ChaptersCollection)
				if cErr != nil {
					return cErr
				}
				newRec := core.NewRecord(collection)
				newRec.Set("novel", novelID)
				newRec.Set("position", desiredPos)
				newRec.Set("excluded", false)
				newRec.Set("chapter_order", desiredOrder)
				if chapter.Title != "" {
					newRec.Set("title", chapter.Title)
				}
				if chapter.TranslatedTitle != "" {
					newRec.Set("translated_title", chapter.TranslatedTitle)
				}
				if chapter.OriginalContent != "" {
					newRec.Set("original_content", chapter.OriginalContent)
				}
				if chapter.TranslatedContent != "" {
					newRec.Set("translated_content", chapter.TranslatedContent)
				}
				if chapter.RefinedContent != "" {
					newRec.Set("refined_content", chapter.RefinedContent)
				}
				setCharCounts(newRec,
					newRec.GetString("original_content"),
					newRec.GetString("translated_content"),
					newRec.GetString("refined_content"),
				)
				status := strings.TrimSpace(chapter.Status)
				if status == "" {
					status = "pending"
				}
				newRec.Set("status", status)
				if chapter.ErrorMessage != "" {
					newRec.Set("error_message", chapter.ErrorMessage)
				}
				if err := txApp.Save(newRec); err != nil {
					return err
				}
				ch := chapterFromRecord(newRec)
				stored = &ch
				return nil
			})
			if txErr != nil {
				return nil, txErr
			}
			if recalcStats {
				if err := s.RecalculateNovelStats(novelID); err != nil {
					return nil, err
				}
			}
			return stored, nil
		}
		// Append path (no shift needed) — use the normal single-record flow
		// but honour the auto-assigned chapter_order when the caller hid it.
		collection, cErr := s.App.FindCollectionByNameOrId(ChaptersCollection)
		if cErr != nil {
			return nil, cErr
		}
		record = core.NewRecord(collection)
		record.Set("novel", novelID)
		record.Set("position", desiredPos)
		record.Set("excluded", false)
		record.Set("chapter_order", desiredOrder)
		// chapter already carries the desired values; fall through to the
		// common field assignment below. Mark isNew so the position/excluded
		// block is not re-run.
		isNew = true
		// Prevent the generic isNew block below from overwriting the position
		// we just assigned.
		chapter.ChapterOrder = desiredOrder
		chapter.Position = 0
	}
	// Existing records keep their position/excluded unless explicitly moved
	// through the insertion path above or the reorder/visibility endpoints.
	// For updates where the caller supplies a new Position that differs from
	// the stored one, treat it as a move inside the same transaction pattern.
	// To keep the diff minimal this path only handles the append/shift for
	// new inserts; moving an existing chapter is done via ReorderChapters.
	// The ponytail note below still applies to the append path.
	//
	// ponytail: position assignment is not atomic — two concurrent creates
	// could read the same max and collide on the (novel,position) unique
	// index. The current worker serializes per-novel creates (download +
	// manual insert rarely overlap), so we surface the unique error to the
	// caller and rely on retry at the HTTP/handler layer. If parallel
	// imports per novel are added, switch to a DB-level sequence or
	// INSERT ... ON CONFLICT retry.
	if isNew && record.GetFloat("position") == 0 {
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

	if chapter.ChapterOrder != 0 {
		record.Set("chapter_order", chapter.ChapterOrder)
	} else if record.IsNew() && record.GetFloat("chapter_order") == 0 {
		// Auto-assigned above for the append path; keep it.
	}
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

// insertChaptersBulk inserts pre-filled chapters into a novel within one
// transaction. Callers assign the final Position/ChapterOrder values (the
// target novel is expected to be fresh, so no position shifting happens
// here) and content is stored verbatim. Import and copy flows use this to
// avoid a per-chapter novel re-read and to make the whole batch atomic — a
// crash can no longer leave a partially imported novel behind.
func (s *Store) insertChaptersBulk(novelID string, chapters []Chapter) error {
	if len(chapters) == 0 {
		return nil
	}
	collection, err := s.App.FindCollectionByNameOrId(ChaptersCollection)
	if err != nil {
		return err
	}
	return s.App.RunInTransaction(func(txApp core.App) error {
		for _, chapter := range chapters {
			record := core.NewRecord(collection)
			record.Set("novel", novelID)
			record.Set("position", chapter.Position)
			record.Set("excluded", false)
			record.Set("chapter_order", chapter.ChapterOrder)
			record.Set("title", chapter.Title)
			record.Set("translated_title", chapter.TranslatedTitle)
			record.Set("original_content", chapter.OriginalContent)
			record.Set("translated_content", chapter.TranslatedContent)
			record.Set("refined_content", chapter.RefinedContent)
			record.Set("status", defaultString(chapter.Status, "pending"))
			record.Set("error_message", chapter.ErrorMessage)
			setCharCounts(record, chapter.OriginalContent, chapter.TranslatedContent, chapter.RefinedContent)
			if err := txApp.Save(record); err != nil {
				return err
			}
		}
		return nil
	})
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
// bulk-delete behavior. Chapters already excluded are not counted as
// newly excluded — the return value is the number of state transitions.
func (s *Store) BulkExcludeChapters(userID, novelID string, ids []string) (int, error) {
	if _, err := s.GetOwnedNovel(userID, novelID); err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	active, err := s.HasActiveJobsForNovel(novelID)
	if err != nil {
		return 0, err
	}
	if active {
		return 0, ErrActiveJobs
	}
	// One conditional UPDATE instead of a record load + Save per id: foreign
	// ids match nothing (novel guard) and already-excluded rows are filtered by
	// excluded = 0, so RowsAffected is exactly the number of state transitions.
	const chunkSize = 500
	excluded := int64(0)
	updated := types.NowDateTime().String()
	for start := 0; start < len(ids); start += chunkSize {
		end := min(start+chunkSize, len(ids))
		set := newNarrowSet()
		set.add("excluded", true)
		set.add("updated", updated)
		set.bind("novel", novelID)
		placeholders, err := bindPlaceholders(set, ids[start:end])
		if err != nil {
			return int(excluded), err
		}
		rows, err := set.exec(s, ChaptersCollection,
			"novel = {:novel} AND excluded = 0 AND id IN ("+placeholders+")")
		if err != nil {
			return int(excluded), err
		}
		excluded += rows
	}
	if excluded > 0 {
		if err := s.RecalculateNovelStats(novelID); err != nil {
			return int(excluded), err
		}
	}
	return int(excluded), nil
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

// SaveRefinedContentIfUnchanged stores refinedContent only while
// translated_content still matches expectedTranslatedContent. The conditional
// UPDATE makes the check-and-write atomic (the previous re-read-reverify dance
// still raced) and avoids loading and rewriting the full chapter row per
// refined chapter. Stats are NOT recalculated here: refine jobs recalculate
// once at job end (RecalculateNovelStats after the chapter loop).
func (s *Store) SaveRefinedContentIfUnchanged(chapterID, expectedTranslatedContent, refinedContent, status string) (applied bool, err error) {
	set := newNarrowSet()
	set.add("error_message", "")
	set.add("updated", types.NowDateTime().String())
	if refinedContent != "" {
		set.add("refined_content", refinedContent)
		set.add("refined_char_count", len(refinedContent))
	}
	if status != "" {
		set.add("status", status)
	}
	set.bind("id", chapterID)
	set.bind("expected", expectedTranslatedContent)
	rows, err := set.exec(s, ChaptersCollection, "id = {:id} AND translated_content = {:expected}")
	if err != nil {
		return false, err
	}
	if rows > 0 {
		return true, nil
	}
	// No row matched: distinguish a stale baseline (not applied) from a missing
	// chapter (ErrNotFound), like the previous FindRecordById error mapping.
	exists, existErr := s.chapterExists(chapterID)
	if existErr != nil {
		return false, existErr
	}
	if !exists {
		return false, ErrNotFound
	}
	return false, nil
}

func (s *Store) chapterExists(chapterID string) (bool, error) {
	rows, err := s.App.CountRecords(ChaptersCollection, dbx.HashExp{"id": chapterID})
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

func (s *Store) saveChapterTranslation(chapterID, translatedTitle, translatedContent, refinedContent, status string, recalcStats bool) error {
	set := newNarrowSet()
	set.add("error_message", "")
	if translatedTitle != "" {
		set.add("translated_title", translatedTitle)
	}
	if translatedContent != "" {
		set.add("translated_content", translatedContent)
		set.add("translated_char_count", len(translatedContent))
	}
	if refinedContent != "" {
		set.add("refined_content", refinedContent)
		set.add("refined_char_count", len(refinedContent))
	}
	if status != "" {
		set.add("status", status)
	}
	set.add("updated", types.NowDateTime().String())
	set.bind("id", chapterID)
	rows, err := set.exec(s, ChaptersCollection, "id = {:id}")
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	if !recalcStats {
		return nil
	}
	novelID, err := s.chapterNovelID(chapterID)
	if err != nil {
		return err
	}
	return s.RecalculateNovelStats(novelID)
}

// chapterNovelID resolves the owning novel of a chapter with a narrow query.
func (s *Store) chapterNovelID(chapterID string) (string, error) {
	row := struct {
		Novel string `db:"novel"`
	}{}
	err := s.App.DB().Select("novel").From(ChaptersCollection).
		Where(dbx.NewExp("id = {:id}", dbx.Params{"id": chapterID})).One(&row)
	if err != nil {
		return "", ErrNotFound
	}
	return row.Novel, nil
}

func (s *Store) SaveChapterTranslationFast(chapterID, translatedTitle, translatedContent, refinedContent, status string) error {
	return s.saveChapterTranslation(chapterID, translatedTitle, translatedContent, refinedContent, status, false)
}

func (s *Store) UpdateChapterStatusFast(chapterID, status, errorMessage string) error {
	set := newNarrowSet()
	set.add("status", status)
	set.add("error_message", errorMessage)
	set.add("updated", types.NowDateTime().String())
	set.bind("id", chapterID)
	rows, err := set.exec(s, ChaptersCollection, "id = {:id}")
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpdateChaptersStatusFast(chapterIDs []string, status, errorMessage string) error {
	ids := make([]string, 0, len(chapterIDs))
	for _, chapterID := range chapterIDs {
		if strings.TrimSpace(chapterID) != "" {
			ids = append(ids, chapterID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	// One narrow UPDATE per chunk instead of a record Save per chapter.
	const chunkSize = 500
	for start := 0; start < len(ids); start += chunkSize {
		end := min(start+chunkSize, len(ids))
		set := newNarrowSet()
		set.add("status", status)
		set.add("error_message", errorMessage)
		set.add("updated", types.NowDateTime().String())
		placeholderSQL, err := bindPlaceholders(set, ids[start:end])
		if err != nil {
			return err
		}
		if _, err := set.exec(s, ChaptersCollection, "id IN ("+placeholderSQL+")"); err != nil {
			return err
		}
	}
	return nil
}

// bindPlaceholders binds values as {:idN} tokens and returns the matching
// "..." placeholder list for an IN clause, chunked by the caller to stay within
// SQLite's variable limit.
func bindPlaceholders(set *narrowSet, values []string) (string, error) {
	placeholders := make([]string, 0, len(values))
	for i, value := range values {
		if strings.TrimSpace(value) == "" {
			return "", fmt.Errorf("empty value in IN clause")
		}
		name := fmt.Sprintf("in%d", i)
		set.bind(name, value)
		placeholders = append(placeholders, "{:"+name+"}")
	}
	return strings.Join(placeholders, ", "), nil
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
		// LoadJobChapters), so the chapters marked processing by the handler
		// must be derived the same way — with a narrow id query instead of
		// hydrating every chapter's full content.
		rows := []struct {
			ID string `db:"id"`
		}{}
		if err := s.App.DB().Select("id").From(ChaptersCollection).
			Where(dbx.NewExp("novel = {:novel} AND excluded = 0", dbx.Params{"novel": job.NovelID})).
			All(&rows); err != nil {
			return err
		}
		for _, row := range rows {
			chapterIDs = append(chapterIDs, row.ID)
		}
	}
	if len(chapterIDs) == 0 {
		return nil
	}
	// Recover chapters stuck in 'processing' with conditional UPDATEs mirroring
	// the old per-record derivation (refined > translated > pending), instead
	// of a full-record load + Save per chapter.
	const chunkSize = 500
	mutated := int64(0)
	updated := types.NowDateTime().String()
	recoverySteps := []struct {
		status      string
		contentCond string
	}{
		{"refined", " AND TRIM(COALESCE(refined_content, '')) != ''"},
		{"translated", " AND TRIM(COALESCE(translated_content, '')) != '' AND TRIM(COALESCE(refined_content, '')) = ''"},
		{"pending", " AND TRIM(COALESCE(translated_content, '')) = '' AND TRIM(COALESCE(refined_content, '')) = ''"},
	}
	for start := 0; start < len(chapterIDs); start += chunkSize {
		end := min(start+chunkSize, len(chapterIDs))
		for _, step := range recoverySteps {
			set := newNarrowSet()
			set.add("status", step.status)
			set.add("error_message", "")
			set.add("updated", updated)
			set.bind("novel", job.NovelID)
			placeholders, err := bindPlaceholders(set, chapterIDs[start:end])
			if err != nil {
				return err
			}
			rows, err := set.exec(s, ChaptersCollection,
				"novel = {:novel} AND status = 'processing' AND id IN ("+placeholders+")"+step.contentCond)
			if err != nil {
				return err
			}
			mutated += rows
		}
	}
	if mutated > 0 {
		return s.RecalculateNovelStats(job.NovelID)
	}
	return nil
}

func (s *Store) GetMaxChapterOrder(userID, novelID string) (int, error) {
	if _, err := s.GetNovelAccessible(userID, novelID); err != nil {
		return 0, err
	}
	return s.maxChapterOrder(novelID)
}

// chapterTitleRow / chapterOrderRow are the narrow projections backing the
// source-synchronization helpers below.
type chapterTitleRow struct {
	Title string `db:"title"`
}

type chapterOrderRow struct {
	ChapterOrder float64 `db:"chapter_order"`
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
	rows := []chapterTitleRow{}
	err = s.App.DB().Select("title").From("chapters").
		Where(dbx.NewExp("novel = {:novel}", dbx.Params{"novel": novelID})).
		OrderBy("chapter_order ASC").
		Limit(int64(dynamicChapterLimit(total))).
		All(&rows)
	if err != nil {
		return nil, err
	}
	existing := make(map[string]bool, len(rows))
	for _, row := range rows {
		if row.Title != "" {
			existing[row.Title] = true
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
	rows := []chapterOrderRow{}
	err = s.App.DB().Select("chapter_order").From("chapters").
		Where(dbx.NewExp("novel = {:novel}", dbx.Params{"novel": novelID})).
		OrderBy("chapter_order ASC").
		Limit(int64(dynamicChapterLimit(total))).
		All(&rows)
	if err != nil {
		return nil, err
	}
	existing := make(map[int]bool, len(rows))
	for _, row := range rows {
		order := asInt(row.ChapterOrder, 0)
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
	rows := []chapterOrderRow{}
	err = s.App.DB().Select("chapter_order").From("chapters").
		Where(dbx.NewExp("novel = {:novel} AND chapter_order > 0", dbx.Params{"novel": novelID})).
		OrderBy("chapter_order ASC").
		Limit(int64(dynamicChapterLimit(total))).
		All(&rows)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	orders := make([]int, 0, len(rows))
	for _, row := range rows {
		orders = append(orders, asInt(row.ChapterOrder, 0))
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
	total, err := s.totalChapterCount(novelID)
	if err != nil {
		return nil, err
	}
	rows := []chapterOrderRow{}
	err = s.App.DB().Select("chapter_order").From("chapters").
		Where(dbx.NewExp("novel = {:novel} AND excluded = 1 AND chapter_order > 0", dbx.Params{"novel": novelID})).
		OrderBy("chapter_order ASC").
		Limit(int64(dynamicChapterLimit(total))).
		All(&rows)
	if err != nil {
		return nil, err
	}
	orders := make([]int, 0, len(rows))
	for _, row := range rows {
		orders = append(orders, asInt(row.ChapterOrder, 0))
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

// maxChapterColumn reads MAX(column) straight through dbx instead of hydrating
// the highest-ordered record, whose full content columns would be transferred
// just to read one number. column comes from the fixed callers below, never
// from user input.
func (s *Store) maxChapterColumn(column, novelID string) (int, error) {
	row := struct {
		MaxValue float64 `db:"max_value"`
	}{}
	err := s.App.DB().NewQuery(
		"SELECT COALESCE(MAX(" + column + "), 0) AS max_value FROM chapters WHERE novel = {:novel}",
	).Bind(dbx.Params{"novel": novelID}).One(&row)
	if err != nil {
		return 0, err
	}
	return asInt(row.MaxValue, 0), nil
}

func (s *Store) maxChapterOrder(novelID string) (int, error) {
	return s.maxChapterColumn("chapter_order", novelID)
}

func (s *Store) maxChapterPosition(novelID string) (int, error) {
	return s.maxChapterColumn("position", novelID)
}

// ListExcludedChapterSummariesAccessible lists logically deleted chapters so
// the owner can restore them. Excluded chapters are private: only the owner
// may list them, even for public novels. They are sorted by source order.
func (s *Store) ListExcludedChapterSummariesAccessible(userID, novelID string) ([]ChapterSummary, error) {
	if _, err := s.GetOwnedNovel(userID, novelID); err != nil {
		return nil, err
	}
	total, err := s.totalChapterCount(novelID)
	if err != nil {
		return nil, err
	}
	return s.findChapterSummaries(
		"novel = {:novel} AND excluded = 1",
		dynamicChapterLimit(total), 0,
		dbx.Params{"novel": novelID},
		"chapter_order ASC",
	)
}

// shiftChapterPositions moves every chapter position in [from, to] by delta
// (±1) inside the caller's transaction. Rows are updated one narrow statement
// at a time, in the order that keeps every intermediate (novel, position)
// value unique (descending for +1, ascending for -1): a single
// "SET position = position + 1" cannot be used because SQLite enforces the
// unique index row by row. Callers must first park any colliding row (e.g. the
// moved chapter at -1). Content columns are neither read nor rewritten.
func shiftChapterPositions(app core.App, novelID string, from, to, delta int) error {
	if delta != 1 && delta != -1 {
		return fmt.Errorf("position shift delta must be ±1, got %d", delta)
	}
	if from > to {
		return nil
	}
	order := "position ASC"
	if delta == 1 {
		order = "position DESC"
	}
	rows := []struct {
		ID string `db:"id"`
	}{}
	if err := app.DB().Select("id").From(ChaptersCollection).
		Where(dbx.NewExp("novel = {:novel} AND position >= {:from} AND position <= {:to}",
			dbx.Params{"novel": novelID, "from": from, "to": to})).
		OrderBy(order).All(&rows); err != nil {
		return err
	}
	updated := types.NowDateTime().String()
	for _, row := range rows {
		if _, err := app.DB().NewQuery(
			"UPDATE chapters SET position = position + {:delta}, updated = {:updated} WHERE id = {:id}",
		).Bind(dbx.Params{"delta": delta, "updated": updated, "id": row.ID}).Execute(); err != nil {
			return err
		}
	}
	return nil
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

	total, err := s.totalChapterCount(novelID)
	if err != nil {
		return err
	}
	// Narrow id projection: the permutation check only needs ids, never the
	// full chapter rows (content included) the record query used to hydrate.
	all := []struct {
		ID string `db:"id"`
	}{}
	if err := s.App.DB().Select("id").From(ChaptersCollection).
		Where(dbx.NewExp("novel = {:novel}", dbx.Params{"novel": novelID})).
		Limit(int64(dynamicChapterLimit(total))).
		All(&all); err != nil {
		return err
	}
	if len(all) != len(chapterIDs) {
		return fmt.Errorf("%w: expected %d chapter ids (every chapter of the novel), got %d", ErrInvalidReorder, len(all), len(chapterIDs))
	}
	byID := make(map[string]struct{}, len(all))
	for _, row := range all {
		byID[row.ID] = struct{}{}
	}
	for _, id := range chapterIDs {
		if _, ok := byID[id]; !ok {
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

	// Two-phase write inside a transaction: first free every current position
	// (negation is unique because the current values are unique and
	// non-negative; a lone legacy 0 row stays 0 without colliding), then assign
	// the dense final positions 1..N in the given order. Both phases use
	// narrow statements: no record hydration, no full-row rewrite, no
	// unique-index collisions.
	// Re-check HasActiveJobs inside the transaction so a job created between
	// the fast-path check above and the tx start is not missed.
	return s.App.RunInTransaction(func(txApp core.App) error {
		records, err := txApp.FindRecordsByFilter(JobsCollection, "novel = {:novel} && (status = 'pending' || status = 'running')", "", 1, 0, dbx.Params{"novel": novelID})
		if err != nil {
			return err
		}
		if len(records) > 0 {
			return ErrActiveJobs
		}
		updated := types.NowDateTime().String()
		if _, err := txApp.DB().NewQuery(
			"UPDATE chapters SET position = -position, updated = {:updated} WHERE novel = {:novel}",
		).Bind(dbx.Params{"novel": novelID, "updated": updated}).Execute(); err != nil {
			return err
		}
		for i, id := range chapterIDs {
			if _, err := txApp.DB().NewQuery(
				"UPDATE chapters SET position = {:pos}, updated = {:updated} WHERE id = {:id} AND novel = {:novel}",
			).Bind(dbx.Params{"pos": i + 1, "updated": updated, "id": id, "novel": novelID}).Execute(); err != nil {
				return err
			}
		}
		return nil
	})
}

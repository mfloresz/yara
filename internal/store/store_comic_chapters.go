package store

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

func (s *Store) ListComicChapters(userID, comicID string) ([]ComicChapter, error) {
	if _, err := s.GetComicAccessible(userID, comicID); err != nil {
		return nil, err
	}
	// Reading/display/export order is the user-controlled position; excluded
	// chapters are hidden everywhere except restore-oriented listings.
	total, err := s.totalComicChapterCount(comicID)
	if err != nil {
		return nil, err
	}
	records, err := s.App.FindRecordsByFilter(ComicChaptersCollection, "comic = {:comic} && excluded = false", "position,chapter_order", dynamicComicChapterLimit(total), 0, dbx.Params{"comic": comicID})
	if err != nil {
		return nil, err
	}
	out := make([]ComicChapter, 0, len(records))
	for _, record := range records {
		out = append(out, comicChapterFromRecord(record))
	}
	return out, nil
}

func (s *Store) ListAllComicChapterSummaries(userID, comicID string) ([]ComicChapterSummary, error) {
	if _, err := s.GetComicAccessible(userID, comicID); err != nil {
		return nil, err
	}
	total, err := s.totalComicChapterCount(comicID)
	if err != nil {
		return nil, err
	}
	return s.findComicChapterSummaries(
		"comic = {:comic} AND excluded = 0",
		dynamicComicChapterLimit(total), 0,
		dbx.Params{"comic": comicID},
		"position ASC", "chapter_order ASC",
	)
}

func (s *Store) ListComicChapterSummaries(userID, comicID string, limit, offset int) ([]ComicChapterSummary, int, error) {
	comic, err := s.GetComicAccessible(userID, comicID)
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
	summaries, err := s.findComicChapterSummaries(
		"comic = {:comic} AND excluded = 0",
		limit, offset,
		dbx.Params{"comic": comicID},
		"position ASC", "chapter_order ASC",
	)
	if err != nil {
		return nil, 0, err
	}
	return summaries, comic.ChapterCount, nil
}

// GetOwnedComicChapter returns one chapter of a comic the user owns, without the
// "public comics are readable" leniency GetComicChapter allows. The agent
// assistant uses it so its tools can only ever touch the caller's own library.
func (s *Store) GetOwnedComicChapter(userID, comicID, chapterID string) (*ComicChapter, error) {
	if _, err := s.GetOwnedComic(userID, comicID); err != nil {
		return nil, err
	}
	record, err := s.App.FindRecordById(ComicChaptersCollection, chapterID)
	if err != nil || record.GetString("comic") != comicID {
		return nil, ErrNotFound
	}
	chapter := comicChapterFromRecord(record)
	return &chapter, nil
}

// GetOwnedComicChapterStats returns aggregate chapter stats for a comic the user
// owns, following the same ownership policy as GetOwnedComicChapter.
func (s *Store) GetOwnedComicChapterStats(userID, comicID string) (*ComicChapterStats, error) {
	comic, err := s.GetOwnedComic(userID, comicID)
	if err != nil {
		return nil, err
	}
	return s.comicChapterStatsForComic(comic)
}

// GetOwnedComicChapterSummaries lists chapter summaries of a comic the user owns,
// following the same ownership policy as GetOwnedComicChapter.
func (s *Store) GetOwnedComicChapterSummaries(userID, comicID string, limit, offset int) ([]ComicChapterSummary, int, error) {
	comic, err := s.GetOwnedComic(userID, comicID)
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
	summaries, err := s.findComicChapterSummaries(
		"comic = {:comic} AND excluded = 0",
		limit, offset,
		dbx.Params{"comic": comicID},
		"position ASC", "chapter_order ASC",
	)
	if err != nil {
		return nil, 0, err
	}
	return summaries, comic.ChapterCount, nil
}

// maxOrderGuard caps an unbounded upper chapter-order filter. Real comics do
// not come close; the bound just keeps the generated filter finite.
// GetOwnedComicChapterSummariesInOrderRange returns the chapter summaries of an
// owned comic whose chapter_order falls in [minOrder, maxOrder].
//
// A bound of -1 means "unbounded on that side", so the model can ask for
// "everything from chapter 40 onwards" without inventing a ceiling. Range
// addressing lets it inspect a contiguous slice of a long comic directly,
// instead of walking the whole list in fixed pages.
func (s *Store) GetOwnedComicChapterSummariesInOrderRange(userID, comicID string, minOrder, maxOrder, limit int) ([]ComicChapterSummary, int, error) {
	comic, err := s.GetOwnedComic(userID, comicID)
	if err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 5000 {
		limit = 5000
	}
	if minOrder < 0 {
		minOrder = 0
	}
	if maxOrder < 0 || maxOrder > maxOrderGuard {
		maxOrder = maxOrderGuard
	}
	summaries, err := s.findComicChapterSummaries(
		"comic = {:comic} AND excluded = 0 AND chapter_order >= {:min} AND chapter_order <= {:max}",
		limit, 0,
		dbx.Params{"comic": comicID, "min": minOrder, "max": maxOrder},
		"chapter_order ASC",
	)
	if err != nil {
		return nil, 0, err
	}
	return summaries, comic.ChapterCount, nil
}

func (s *Store) GetComicChapterStats(userID, comicID string) (*ComicChapterStats, error) {
	comic, err := s.GetComicAccessible(userID, comicID)
	if err != nil {
		return nil, err
	}
	return s.comicChapterStatsForComic(comic)
}

// comicChapterStatsForComic builds ComicChapterStats from an already-authorized comic.
// comicChapterStatsForComic builds ComicChapterStats from an already-authorized
// comic. Failed/partial chapters are counted with one narrow query (the comic
// row only carries the aggregate counters the library grid renders).
func (s *Store) comicChapterStatsForComic(comic *Comic) (*ComicChapterStats, error) {
	row := struct {
		Failed float64 `db:"failed"`
	}{}
	err := s.App.DB().NewQuery(
		"SELECT COALESCE(SUM(CASE WHEN status IN ('failed', '" + ComicChapterPartial + "') THEN 1 ELSE 0 END), 0) AS failed" +
			" FROM comic_chapters WHERE comic = {:comic} AND excluded = 0",
	).Bind(dbx.Params{"comic": comic.ID}).One(&row)
	if err != nil {
		return nil, err
	}
	return &ComicChapterStats{
		TotalChapters:      comic.ChapterCount,
		DownloadedChapters: comic.DownloadedCount,
		FailedChapters:     asInt(row.Failed, 0),
		MaxChapterOrder:    comic.MaxChapterOrder,
	}, nil
}

func (s *Store) RecalculateComicStats(comicID string) error {
	// Single aggregate over narrow columns; excluded chapters count toward
	// max_chapter_order only, mirroring the novel stats semantics.
	stats := struct {
		Total      int64   `db:"total"`
		Downloaded int64   `db:"downloaded"`
		MaxOrder   float64 `db:"max_order"`
	}{}
	err := s.App.DB().NewQuery(`
		SELECT
			COALESCE(SUM(CASE WHEN excluded = 0 THEN 1 ELSE 0 END), 0) AS total,
			COALESCE(SUM(CASE WHEN excluded = 0 AND status = 'done' THEN 1 ELSE 0 END), 0) AS downloaded,
			COALESCE(MAX(chapter_order), 0) AS max_order
		FROM comic_chapters
		WHERE comic = {:comic}
	`).Bind(dbx.Params{"comic": comicID}).One(&stats)
	if err != nil {
		return err
	}
	res, err := s.App.DB().NewQuery(`
		UPDATE comics SET
			chapter_count = {:total},
			downloaded_count = {:downloaded},
			max_chapter_order = {:max_order},
			updated = {:updated}
		WHERE id = {:id}
	`).Bind(dbx.Params{
		"total":      int(stats.Total),
		"downloaded": int(stats.Downloaded),
		"max_order":  asInt(stats.MaxOrder, 0),
		"updated":    types.NowDateTime().String(),
		"id":         comicID,
	}).Execute()
	if err != nil {
		return err
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return ErrNotFound
	}
	return nil
}

// comicChapterSummaryFromRecord builds a ComicChapterSummary from a full record fetch
// (FindRecordsByFilter). Prefer the SQL-projected findComicChapterSummaries for
// list endpoints; this helper serves small targeted queries (e.g. the manga
// eligible scan) that already hold records.
func comicChapterSummaryFromRecord(record *core.Record) ComicChapterSummary {
	pageCount := asInt(record.GetFloat("page_count"), 0)
	return ComicChapterSummary{
		ID:           record.Id,
		ComicID:      record.GetString("comic"),
		ChapterOrder: asInt(record.GetFloat("chapter_order"), 0),
		Position:     asInt(record.GetFloat("position"), 0),
		Excluded:     record.GetBool("excluded"),
		Title:        record.GetString("title"),
		Status:       defaultString(record.GetString("status"), ComicChapterPending),
		ErrorMessage: record.GetString("error_message"),
		PageCount:    pageCount,
		HasPages:     pageCount > 0,
		CreatedAt:    record.GetDateTime("created").String(),
		UpdatedAt:    record.GetDateTime("updated").String(),
	}
}

// comicChapterSummaryColumns is the narrow column set behind ComicChapterSummary. The
// content columns are only referenced through the has_* expressions — never
// selected — so listing a 1000-chapter comic no longer transfers its full
// text just to compute three booleans. TRIM only strips spaces (SQLite has no
// unicode-aware trim), so content holding only tabs/newlines would report
// has_* = true where the previous strings.TrimSpace check reported false;
// whitespace-only chapter content does not occur in practice.
const comicChapterSummaryColumns = `id, comic, chapter_order, position, excluded,
	title, status, error_message, page_count,
	created, updated`

// comicChapterSummaryRow mirrors comicChapterSummaryColumns for dbx struct scanning.
// Numerics are scanned as float64 because SQLite NUMERIC values come back
// with INTEGER or REAL storage class depending on the value (and SUM()
// always returns REAL).
type comicChapterSummaryRow struct {
	ID           string  `db:"id"`
	Comic        string  `db:"comic"`
	ChapterOrder float64 `db:"chapter_order"`
	Position     float64 `db:"position"`
	Excluded     float64 `db:"excluded"`
	Title        string  `db:"title"`
	Status       string  `db:"status"`
	ErrorMessage string  `db:"error_message"`
	PageCount    float64 `db:"page_count"`
	Created      string  `db:"created"`
	Updated      string  `db:"updated"`
}

func (row comicChapterSummaryRow) summary() ComicChapterSummary {
	pageCount := asInt(row.PageCount, 0)
	return ComicChapterSummary{
		ID:           row.ID,
		ComicID:      row.Comic,
		ChapterOrder: asInt(row.ChapterOrder, 0),
		Position:     asInt(row.Position, 0),
		Excluded:     row.Excluded != 0,
		Title:        row.Title,
		Status:       defaultString(row.Status, ComicChapterPending),
		ErrorMessage: row.ErrorMessage,
		PageCount:    pageCount,
		HasPages:     pageCount > 0,
		CreatedAt:    asDateTimeString(row.Created),
		UpdatedAt:    asDateTimeString(row.Updated),
	}
}

// asDateTimeString normalizes a raw autodate column value (TEXT in the
// PocketBase default date layout) exactly like Record.GetDateTime(...).String().
// findComicChapterSummaries runs one projected summary query. where is raw SQL
// (AND, not fexpr &&) with {:name} placeholders bound through params.
func (s *Store) findComicChapterSummaries(where string, limit, offset int, params dbx.Params, orderBys ...string) ([]ComicChapterSummary, error) {
	query := s.App.DB().
		Select(comicChapterSummaryColumns).
		From("chapters").
		Where(dbx.NewExp(where, params)).
		OrderBy(orderBys...)
	if limit > 0 {
		query = query.Limit(int64(limit))
	}
	if offset > 0 {
		query = query.Offset(int64(offset))
	}
	rows := []comicChapterSummaryRow{}
	if err := query.All(&rows); err != nil {
		return nil, err
	}
	out := make([]ComicChapterSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.summary())
	}
	return out, nil
}

func (s *Store) GetComicChapter(userID, comicID, chapterID string) (*ComicChapter, error) {
	comic, err := s.GetComicAccessible(userID, comicID)
	if err != nil {
		return nil, err
	}
	record, err := s.App.FindRecordById(ComicChaptersCollection, chapterID)
	if err != nil || record.GetString("comic") != comicID {
		return nil, ErrNotFound
	}
	// Excluded chapters are private to the owner: non-owners must not be able
	// to read them by ID even when the comic is public.
	if record.GetBool("excluded") && comic.OwnerID != userID {
		return nil, ErrNotFound
	}
	chapter := comicChapterFromRecord(record)
	return &chapter, nil
}

// GetComicChapterNeighbors returns the previous/next visible chapters in
// reading order (position, the user-controlled order). Excluded chapters are
// never neighbors. Each lookup is a single indexed query with LIMIT 1, so
// resolving neighbors costs O(log n) instead of loading the full list.
func (s *Store) GetComicChapterNeighbors(userID, comicID, chapterID string) (prev, next *ComicChapterSummary, err error) {
	current, err := s.GetComicChapter(userID, comicID, chapterID)
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
	base := dbx.Params{"comic": comicID, "cur": cur}
	// key comes from the fixed position/chapter_order pair above, never from
	// user input, so concatenating it into the SQL where clause is safe.
	findOne := func(where, orderBy string) *ComicChapterSummary {
		summaries, qerr := s.findComicChapterSummaries(where, 1, 0, base, orderBy)
		if qerr != nil || len(summaries) == 0 {
			return nil
		}
		summary := summaries[0]
		return &summary
	}
	prev = findOne("comic = {:comic} AND excluded = 0 AND "+key+" < {:cur} AND "+key+" > 0", key+" DESC")
	next = findOne("comic = {:comic} AND excluded = 0 AND "+key+" > {:cur}", key+" ASC")
	return prev, next, nil
}

func (s *Store) UpsertComicChapter(userID, comicID string, chapter *ComicChapter) (*ComicChapter, error) {
	return s.upsertComicChapter(userID, comicID, chapter, true)
}

func (s *Store) UpsertComicChapterWithoutStats(userID, comicID string, chapter *ComicChapter) (*ComicChapter, error) {
	return s.upsertComicChapter(userID, comicID, chapter, false)
}

func (s *Store) upsertComicChapter(userID, comicID string, chapter *ComicChapter, recalcStats bool) (*ComicChapter, error) {
	if _, err := s.GetOwnedComic(userID, comicID); err != nil {
		return nil, err
	}
	var record *core.Record
	var err error
	isNew := false
	if strings.TrimSpace(chapter.ID) != "" {
		record, err = s.App.FindRecordById(ComicChaptersCollection, chapter.ID)
		if err != nil {
			return nil, ErrNotFound
		}
		if record.GetString("comic") != comicID {
			return nil, ErrForbidden
		}
		if chapter.Position != 0 {
			curPos := asInt(record.GetFloat("position"), 0)
			if curPos == 0 {
				curPos, _ = s.maxComicChapterPosition(comicID)
				if curPos == 0 {
					curPos = 1
				}
			}
			if chapter.Position != curPos {
				if chapter.Position < 1 {
					return nil, fmt.Errorf("%w: position must be between 1 and %d", ErrInvalidReorder, curPos)
				}
				maxPos, _ := s.maxComicChapterPosition(comicID)
				if chapter.Position > maxPos {
					return nil, fmt.Errorf("%w: position must be between 1 and %d", ErrInvalidReorder, maxPos)
				}
				active, err := s.HasActiveJobsForComic(comicID)
				if err != nil {
					return nil, err
				}
				if active {
					return nil, ErrActiveJobs
				}
				desiredPos := chapter.Position
				oldPos := curPos
				txErr := s.App.RunInTransaction(func(txApp core.App) error {
					records, err := txApp.FindRecordsByFilter(JobsCollection, "comic = {:comic} && (status = 'pending' || status = 'running')", "", 1, 0, dbx.Params{"comic": comicID})
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
						"UPDATE comic_chapters SET position = -1, updated = {:updated} WHERE id = {:id}",
					).Bind(dbx.Params{"id": record.Id, "updated": updated}).Execute(); err != nil {
						return err
					}
					if desiredPos < oldPos {
						if err := shiftComicChapterPositions(txApp, comicID, desiredPos, oldPos-1, +1); err != nil {
							return err
						}
					} else {
						if err := shiftComicChapterPositions(txApp, comicID, oldPos+1, desiredPos, -1); err != nil {
							return err
						}
					}
					if _, err := txApp.DB().NewQuery(
						"UPDATE comic_chapters SET position = {:pos}, updated = {:updated} WHERE id = {:id}",
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
				record, _ = s.App.FindRecordById(ComicChaptersCollection, record.Id)
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
		// (comic, position) unique index. A shift is treated as a reorder and
		// is rejected with ErrActiveJobs while the comic has pending/running jobs.
		desiredPos := chapter.Position
		desiredOrder := chapter.ChapterOrder
		if desiredOrder == 0 {
			maxOrder, err := s.maxComicChapterOrder(comicID)
			if err != nil {
				return nil, err
			}
			desiredOrder = maxOrder + 1
		}
		maxPos, posErr := s.maxComicChapterPosition(comicID)
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
			active, err := s.HasActiveJobsForComic(comicID)
			if err != nil {
				return nil, err
			}
			if active {
				return nil, ErrActiveJobs
			}
			var stored *ComicChapter
			txErr := s.App.RunInTransaction(func(txApp core.App) error {
				records, err := txApp.FindRecordsByFilter(JobsCollection, "comic = {:comic} && (status = 'pending' || status = 'running')", "", 1, 0, dbx.Params{"comic": comicID})
				if err != nil {
					return err
				}
				if len(records) > 0 {
					return ErrActiveJobs
				}
				// Shift the [desiredPos, maxPos] suffix up by one with narrow
				// ordered updates instead of a hydrated record loop; maxPos
				// +1 is always free, so descending order keeps the unique
				// (comic, position) index satisfied on every row.
				if err := shiftComicChapterPositions(txApp, comicID, desiredPos, maxPos, +1); err != nil {
					return err
				}
				collection, cErr := txApp.FindCollectionByNameOrId(ComicChaptersCollection)
				if cErr != nil {
					return cErr
				}
				newRec := core.NewRecord(collection)
				newRec.Set("comic", comicID)
				newRec.Set("position", desiredPos)
				newRec.Set("excluded", false)
				newRec.Set("chapter_order", desiredOrder)
				if chapter.Title != "" {
					newRec.Set("title", chapter.Title)
				}
				status := strings.TrimSpace(chapter.Status)
				if status == "" {
					status = ComicChapterPending
				}
				newRec.Set("status", status)
				if chapter.ErrorMessage != "" {
					newRec.Set("error_message", chapter.ErrorMessage)
				}
				if err := txApp.Save(newRec); err != nil {
					return err
				}
				ch := comicChapterFromRecord(newRec)
				stored = &ch
				return nil
			})
			if txErr != nil {
				return nil, txErr
			}
			if recalcStats {
				if err := s.RecalculateComicStats(comicID); err != nil {
					return nil, err
				}
			}
			return stored, nil
		}
		// Append path (no shift needed) — use the normal single-record flow
		// but honour the auto-assigned chapter_order when the caller hid it.
		collection, cErr := s.App.FindCollectionByNameOrId(ComicChaptersCollection)
		if cErr != nil {
			return nil, cErr
		}
		record = core.NewRecord(collection)
		record.Set("comic", comicID)
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
	// new inserts; moving an existing chapter is done via ReorderComicChapters.
	// The ponytail note below still applies to the append path.
	//
	// ponytail: position assignment is not atomic — two concurrent creates
	// could read the same max and collide on the (comic,position) unique
	// index. The current worker serializes per-comic creates (download +
	// manual insert rarely overlap), so we surface the unique error to the
	// caller and rely on retry at the HTTP/handler layer. If parallel
	// imports per comic are added, switch to a DB-level sequence or
	// INSERT ... ON CONFLICT retry.
	if isNew && record.GetFloat("position") == 0 {
		maxPos, posErr := s.maxComicChapterPosition(comicID)
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
		status = ComicChapterPending
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
	// source_key is written on create and on every parser-driven update, but is
	// never cleared by an empty input: a re-download that omits it must not
	// erase the identity the new/missing diff relies on.
	if chapter.SourceKey != "" {
		record.Set("source_key", chapter.SourceKey)
	} else if record.IsNew() {
		record.Set("source_key", "")
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
		if err := s.RecalculateComicStats(comicID); err != nil {
			return nil, err
		}
	}
	stored := comicChapterFromRecord(record)
	return &stored, nil
}

// insertComicChaptersBulk inserts pre-filled chapters into a comic within one
// transaction. Callers assign the final Position/ChapterOrder values (the
// target comic is expected to be fresh, so no position shifting happens
// here) and content is stored verbatim. Import and copy flows use this to
// avoid a per-chapter comic re-read and to make the whole batch atomic — a
// crash can no longer leave a partially imported comic behind. It returns
// the created chapter IDs in input order so callers can attach related
// records (e.g. inline images).
func (s *Store) insertComicChaptersBulk(comicID string, chapters []ComicChapter) ([]string, error) {
	ids := make([]string, 0, len(chapters))
	if len(chapters) == 0 {
		return ids, nil
	}
	collection, err := s.App.FindCollectionByNameOrId(ComicChaptersCollection)
	if err != nil {
		return nil, err
	}
	err = s.App.RunInTransaction(func(txApp core.App) error {
		for _, chapter := range chapters {
			record := core.NewRecord(collection)
			record.Set("comic", comicID)
			record.Set("position", chapter.Position)
			record.Set("excluded", false)
			record.Set("chapter_order", chapter.ChapterOrder)
			record.Set("title", chapter.Title)
			record.Set("status", defaultString(chapter.Status, ComicChapterPending))
			record.Set("error_message", chapter.ErrorMessage)
			if err := txApp.Save(record); err != nil {
				return err
			}
			ids = append(ids, record.Id)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// ExcludeComicChapter logically deletes a chapter: the record, content, ID and
// source order are retained, but the chapter is hidden from normal lists,
// eligible jobs, navigation and EPUB exports until restored.
func (s *Store) ExcludeComicChapter(userID, comicID, chapterID string) error {
	return s.SetComicChapterExcluded(userID, comicID, chapterID, true)
}

// SetComicChapterExcluded flips the visibility flag of a single chapter (exclude
// or restore). Excluding is rejected while the comic has active jobs so an
// in-flight download/translate/refine job is never silently mutated.
func (s *Store) SetComicChapterExcluded(userID, comicID, chapterID string, excluded bool) error {
	if _, err := s.GetOwnedComic(userID, comicID); err != nil {
		return err
	}
	record, err := s.App.FindRecordById(ComicChaptersCollection, chapterID)
	if err != nil || record.GetString("comic") != comicID {
		return ErrNotFound
	}
	if record.GetBool("excluded") == excluded {
		return nil
	}
	if excluded {
		active, err := s.HasActiveJobsForComic(comicID)
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
	return s.RecalculateComicStats(comicID)
}

// BulkExcludeComicChapters logically deletes several chapters at once. Chapters
// that do not belong to the comic are skipped, mirroring the previous
// bulk-delete behavior. Chapters already excluded are not counted as
// newly excluded — the return value is the number of state transitions.
func (s *Store) BulkExcludeComicChapters(userID, comicID string, ids []string) (int, error) {
	if _, err := s.GetOwnedComic(userID, comicID); err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	active, err := s.HasActiveJobsForComic(comicID)
	if err != nil {
		return 0, err
	}
	if active {
		return 0, ErrActiveJobs
	}
	// One conditional UPDATE instead of a record load + Save per id: foreign
	// ids match nothing (comic guard) and already-excluded rows are filtered by
	// excluded = 0, so RowsAffected is exactly the number of state transitions.
	const chunkSize = 500
	excluded := int64(0)
	updated := types.NowDateTime().String()
	for start := 0; start < len(ids); start += chunkSize {
		end := min(start+chunkSize, len(ids))
		set := newNarrowSet()
		set.add("excluded", true)
		set.add("updated", updated)
		set.bind("comic", comicID)
		placeholders, err := bindPlaceholders(set, ids[start:end])
		if err != nil {
			return int(excluded), err
		}
		rows, err := set.exec(s, ComicChaptersCollection,
			"comic = {:comic} AND excluded = 0 AND id IN ("+placeholders+")")
		if err != nil {
			return int(excluded), err
		}
		excluded += rows
	}
	if excluded > 0 {
		if err := s.RecalculateComicStats(comicID); err != nil {
			return int(excluded), err
		}
	}
	return int(excluded), nil
}

// allowedComicChapterStatuses mirrors the chapters collection SelectField values
// minus "processing" (a transient state the translation worker owns). The
// bulk helpers below write with raw UPDATEs, which bypass PocketBase field
// validation, so the closed set is gated here.
var allowedComicChapterStatuses = map[string]bool{
	ComicChapterPending: true,
	ComicChapterDone:    true,
	ComicChapterFailed:  true,
}

// ListOwnedComicChapterIDsInOrderRange returns just the chapter ids of the comic —
// excluded chapters never enter, matching LoadJobChapters — optionally bounded
// by an inclusive chapter_order range. Only the id column is read: the agent's
// create_job resolves chapters to build the job and mark them processing, and
// neither step needs the bodies.
func (s *Store) ListOwnedComicChapterIDsInOrderRange(userID, comicID string, from, to *int) ([]string, error) {
	if _, err := s.GetOwnedComic(userID, comicID); err != nil {
		return nil, err
	}
	where := "comic = {:comic} AND excluded = 0"
	params := dbx.Params{"comic": comicID}
	if from != nil && *from >= 0 {
		where += " AND chapter_order >= {:fromOrder}"
		params["fromOrder"] = *from
	}
	if to != nil && *to >= 0 {
		where += " AND chapter_order <= {:toOrder}"
		params["toOrder"] = *to
	}
	rows := []struct {
		ID string `db:"id"`
	}{}
	if err := s.App.DB().Select("id").From(ComicChaptersCollection).
		Where(dbx.NewExp(where, params)).
		OrderBy("chapter_order ASC").
		All(&rows); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.ID)
	}
	return out, nil
}

// BulkSetComicChapterStatus applies one status change to every non-excluded,
// non-processing chapter whose chapter_order falls in the inclusive range,
// with a single conditional UPDATE — no rows are loaded. Refused while the
// comic has active jobs: a bulk flip racing a translate/refine job corrupts
// the worker's progress accounting.
func (s *Store) BulkSetComicChapterStatus(userID, comicID string, fromOrder, toOrder int, status, errorMessage string) (int, error) {
	if _, err := s.GetOwnedComic(userID, comicID); err != nil {
		return 0, err
	}
	if !allowedComicChapterStatuses[status] {
		return 0, fmt.Errorf("%w: invalid status %q", ErrInvalidInput, status)
	}
	if fromOrder > toOrder {
		fromOrder, toOrder = toOrder, fromOrder
	}
	active, err := s.HasActiveJobsForComic(comicID)
	if err != nil {
		return 0, err
	}
	if active {
		return 0, ErrActiveJobs
	}
	set := newNarrowSet()
	set.add("status", status)
	set.add("error_message", errorMessage)
	set.add("updated", types.NowDateTime().String())
	set.bind("comic", comicID)
	set.bind("fromOrder", fromOrder)
	set.bind("toOrder", toOrder)
	rows, err := set.exec(s, ComicChaptersCollection,
		"comic = {:comic} AND excluded = 0 AND status != 'processing' AND chapter_order BETWEEN {:fromOrder} AND {:toOrder}")
	if err != nil {
		return 0, err
	}
	if rows > 0 {
		if err := s.RecalculateComicStats(comicID); err != nil {
			return int(rows), err
		}
	}
	return int(rows), nil
}

// BulkExcludeComicChaptersInOrderRange flips the visibility flag of every chapter
// whose chapter_order falls in the inclusive range, in one conditional UPDATE
// (rows already at the target state are filtered out so RowsAffected is the
// number of state transitions). Same active-jobs guard as BulkExcludeComicChapters.
func (s *Store) BulkExcludeComicChaptersInOrderRange(userID, comicID string, fromOrder, toOrder int, excluded bool) (int, error) {
	if _, err := s.GetOwnedComic(userID, comicID); err != nil {
		return 0, err
	}
	if fromOrder > toOrder {
		fromOrder, toOrder = toOrder, fromOrder
	}
	active, err := s.HasActiveJobsForComic(comicID)
	if err != nil {
		return 0, err
	}
	if active {
		return 0, ErrActiveJobs
	}
	set := newNarrowSet()
	set.add("excluded", excluded)
	set.add("updated", types.NowDateTime().String())
	set.bind("comic", comicID)
	set.bind("fromOrder", fromOrder)
	set.bind("toOrder", toOrder)
	set.bind("wasExcluded", !excluded)
	rows, err := set.exec(s, ComicChaptersCollection,
		"comic = {:comic} AND excluded = {:wasExcluded} AND chapter_order BETWEEN {:fromOrder} AND {:toOrder}")
	if err != nil {
		return 0, err
	}
	if rows > 0 {
		if err := s.RecalculateComicStats(comicID); err != nil {
			return int(rows), err
		}
	}
	return int(rows), nil
}

// ComicChapterBrief is the metadata sliver the reading-progress tools need: enough
// to say where the reader is without ever loading the chapter body.
type ComicChapterBrief struct {
	ID           string `json:"id"`
	ComicID      string `json:"comicId"`
	ChapterOrder int    `json:"chapterOrder"`
	Position     int    `json:"position"`
	Title        string `json:"title"`
	Status       string `json:"status"`
	PageCount    int    `json:"pageCount"`
}

// GetOwnedComicChapterBrief reads four metadata columns of one chapter. The comic
// ownership guard comes first, so a foreign chapter id is indistinguishable
// from a missing one.
func (s *Store) GetOwnedComicChapterBrief(userID, comicID, chapterID string) (*ComicChapterBrief, error) {
	if _, err := s.GetOwnedComic(userID, comicID); err != nil {
		return nil, err
	}
	return s.ownedComicChapterBrief("comic = {:comic} AND id = {:chapterId}", dbx.Params{
		"comic": comicID, "chapterId": chapterID,
	})
}

// FindOwnedComicChapterBriefByOrder resolves the first non-excluded chapter at a
// given chapter_order — how the assistant turns "I'm on chapter 30" into a
// progress record.
func (s *Store) FindOwnedComicChapterBriefByOrder(userID, comicID string, order int) (*ComicChapterBrief, error) {
	if _, err := s.GetOwnedComic(userID, comicID); err != nil {
		return nil, err
	}
	return s.ownedComicChapterBrief("comic = {:comic} AND excluded = 0 AND chapter_order = {:order}", dbx.Params{
		"comic": comicID, "order": order,
	})
}

func (s *Store) ownedComicChapterBrief(where string, params dbx.Params) (*ComicChapterBrief, error) {
	rows := []struct {
		ID           string  `db:"id"`
		Comic        string  `db:"comic"`
		ChapterOrder int     `db:"chapter_order"`
		Position     float64 `db:"position"`
		Title        string  `db:"title"`
		Status       string  `db:"status"`
		PageCount    float64 `db:"page_count"`
	}{}
	if err := s.App.DB().Select("id", "comic", "chapter_order", "position", "title", "status", "page_count").
		From(ComicChaptersCollection).
		Where(dbx.NewExp(where, params)).
		OrderBy("chapter_order ASC").
		Limit(1).
		All(&rows); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	row := rows[0]
	return &ComicChapterBrief{
		ID:           row.ID,
		ComicID:      row.Comic,
		ChapterOrder: row.ChapterOrder,
		Position:     asInt(row.Position, 0),
		Title:        row.Title,
		Status:       row.Status,
		PageCount:    asInt(row.PageCount, 0),
	}, nil
}

func (s *Store) UpdateComicChapterStatus(chapterID, status, errorMessage string) error {
	record, err := s.App.FindRecordById(ComicChaptersCollection, chapterID)
	if err != nil {
		return ErrNotFound
	}
	record.Set("status", status)
	record.Set("error_message", errorMessage)
	if err := s.App.Save(record); err != nil {
		return err
	}
	return s.RecalculateComicStats(record.GetString("comic"))
}

func (s *Store) UpdateComicChapterStatusForUser(userID, comicID, chapterID, status, errorMessage string) error {
	if _, err := s.GetOwnedComic(userID, comicID); err != nil {
		return err
	}
	record, err := s.App.FindRecordById(ComicChaptersCollection, chapterID)
	if err != nil || record.GetString("comic") != comicID {
		return ErrNotFound
	}
	record.Set("status", status)
	record.Set("error_message", errorMessage)
	if err := s.App.Save(record); err != nil {
		return err
	}
	return s.RecalculateComicStats(comicID)
}

// DeleteComicChapterRecord physically removes one chapter row. Page rows cascade
// via the chapter_pages.chapter relation. Only used to roll back a chapter
// created moments earlier whose page download then failed — normal deletes
// stay logical (ExcludeComicChapter) so content remains restorable.
func (s *Store) DeleteComicChapterRecord(chapterID string) error {
	record, err := s.App.FindRecordById(ComicChaptersCollection, chapterID)
	if err != nil {
		return ErrNotFound
	}
	return s.App.Delete(record)
}

// ChapterEdits carries the optional chapter fields accepted by
// UpdateChapterEdits. nil leaves the field unchanged; an empty string clears
// it.
// UpdateChapterEdits applies field-level content edits to one chapter with
// ownership checks, keeping the per-chapter char counts and comic stats
// current. Content edits are refused while the comic has active jobs: a
// running translate/refine job would either overwrite the edit or race it.
// ComicChapterSearchHit is one SearchOwnedComicChapters match: the chapter summary
// plus which fields matched and a short snippet around the first content hit.
type ComicChapterSearchHit struct {
	ComicChapterSummary
	MatchedFields []string `json:"matchedFields"`
	Snippet       string   `json:"snippet"`
}

// searchSnippetWindow is the number of context chars shown around a match.
// SearchOwnedComicChapters runs a case-sensitive literal substring search over
// chapter titles of one comic the user owns. Comic chapters carry no text
// bodies, so the title is the whole searchable surface. instr() is used
// instead of LIKE because the contract is a literal, case-sensitive match.
// limit defaults to 10, max 25.
func (s *Store) SearchOwnedComicChapters(userID, comicID, query string, limit int) ([]ComicChapterSearchHit, error) {
	if _, err := s.GetOwnedComic(userID, comicID); err != nil {
		return nil, err
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("%w: empty search query", ErrInvalidInput)
	}
	if limit <= 0 {
		limit = 10
	}
	if limit > 25 {
		limit = 25
	}
	rows := []struct {
		ID           string `db:"id"`
		ChapterOrder int64  `db:"chapter_order"`
		Title        string `db:"title"`
		Status       string `db:"status"`
		Excluded     bool   `db:"excluded"`
	}{}
	err := s.App.DB().NewQuery(`
		SELECT id, chapter_order, title, status, excluded
		FROM comic_chapters
		WHERE comic = {:comic} AND instr(title, {:query}) > 0
		ORDER BY chapter_order
		LIMIT {:limit}
	`).Bind(dbx.Params{
		"comic": comicID,
		"query": query,
		"limit": limit,
	}).All(&rows)
	if err != nil {
		return nil, err
	}
	hits := make([]ComicChapterSearchHit, 0, len(rows))
	for _, row := range rows {
		hits = append(hits, ComicChapterSearchHit{
			ComicChapterSummary: ComicChapterSummary{
				ID:           row.ID,
				ComicID:      comicID,
				ChapterOrder: int(row.ChapterOrder),
				Excluded:     row.Excluded,
				Title:        row.Title,
				Status:       row.Status,
			},
			MatchedFields: []string{"title"},
		})
	}
	return hits, nil
}

// buildSearchSnippet cuts a ±searchSnippetWindow window around the first
// occurrence of query in body, collapsing newlines for the tool result.
//
// The window edges are snapped to rune boundaries. `strings.Index` returns a
// byte offset and a byte-sized window can land inside a multi-byte character —
// the agent serves accented and CJK text, so the raw slice produced invalid
// UTF-8 in roughly a third of emoji/CJK bodies, and that corrupt snippet went
// straight into the model-facing tool result.
func (s *Store) comicChapterExists(chapterID string) (bool, error) {
	rows, err := s.App.CountRecords(ComicChaptersCollection, dbx.HashExp{"id": chapterID})
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

// comicChapterComicID resolves the owning comic of a chapter with a narrow query.
func (s *Store) comicChapterComicID(chapterID string) (string, error) {
	row := struct {
		Comic string `db:"comic"`
	}{}
	err := s.App.DB().Select("comic").From(ComicChaptersCollection).
		Where(dbx.NewExp("id = {:id}", dbx.Params{"id": chapterID})).One(&row)
	if err != nil {
		return "", ErrNotFound
	}
	return row.Comic, nil
}

func (s *Store) UpdateComicChapterStatusFast(chapterID, status, errorMessage string) error {
	set := newNarrowSet()
	set.add("status", status)
	set.add("error_message", errorMessage)
	set.add("updated", types.NowDateTime().String())
	set.bind("id", chapterID)
	rows, err := set.exec(s, ComicChaptersCollection, "id = {:id}")
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpdateComicChaptersStatusFast(chapterIDs []string, status, errorMessage string) error {
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
		if _, err := set.exec(s, ComicChaptersCollection, "id IN ("+placeholderSQL+")"); err != nil {
			return err
		}
	}
	return nil
}

// bindPlaceholders binds values as {:idN} tokens and returns the matching
// "..." placeholder list for an IN clause, chunked by the caller to stay within
// SQLite's variable limit.
func (s *Store) ReconcileProcessingComicChaptersForJob(jobID string) error {
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
		// Jobs without explicit chapter ids cover the whole comic (same rule as
		// LoadJobChapters), so the chapters marked processing by the handler
		// must be derived the same way — with a narrow id query instead of
		// hydrating every chapter's full content.
		rows := []struct {
			ID string `db:"id"`
		}{}
		if err := s.App.DB().Select("id").From(ComicChaptersCollection).
			Where(dbx.NewExp("comic = {:comic} AND excluded = 0", dbx.Params{"comic": job.ComicID})).
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
	// A comic chapter stuck in 'processing' recovers to done when its pages
	// landed, pending otherwise.
	recoverySteps := []struct {
		status      string
		contentCond string
	}{
		{ComicChapterDone, " AND COALESCE(page_count, 0) > 0"},
		{ComicChapterPending, " AND COALESCE(page_count, 0) = 0"},
	}
	const chunkSize = 500
	mutated := int64(0)
	updated := types.NowDateTime().String()
	for start := 0; start < len(chapterIDs); start += chunkSize {
		end := min(start+chunkSize, len(chapterIDs))
		for _, step := range recoverySteps {
			set := newNarrowSet()
			set.add("status", step.status)
			set.add("error_message", "")
			set.add("updated", updated)
			set.bind("comic", job.ComicID)
			placeholders, err := bindPlaceholders(set, chapterIDs[start:end])
			if err != nil {
				return err
			}
			rows, err := set.exec(s, ComicChaptersCollection,
				"comic = {:comic} AND status = 'processing' AND id IN ("+placeholders+")"+step.contentCond)
			if err != nil {
				return err
			}
			mutated += rows
		}
	}
	if mutated > 0 {
		return s.RecalculateComicStats(job.ComicID)
	}
	return nil
}

func (s *Store) GetMaxComicChapterOrder(userID, comicID string) (int, error) {
	if _, err := s.GetComicAccessible(userID, comicID); err != nil {
		return 0, err
	}
	return s.maxComicChapterOrder(comicID)
}

// comicChapterTitleRow / comicChapterOrderRow are the narrow projections backing the
// source-synchronization helpers below.
type comicChapterTitleRow struct {
	Title string `db:"title"`
}

type comicChapterOrderRow struct {
	ChapterOrder float64 `db:"chapter_order"`
}

type comicChapterKeyRow struct {
	SourceKey string `db:"source_key"`
}

// GetExistingComicChapterKeys returns the source_key values stored for a comic's
// chapters. Rows written before the source_key field existed are simply absent
// from the set, which is what drives the title/order fallback in the sync diff.
func (s *Store) GetExistingComicChapterKeys(userID, comicID string) (map[string]bool, error) {
	if _, err := s.GetComicAccessible(userID, comicID); err != nil {
		return nil, err
	}
	total, err := s.totalComicChapterCount(comicID)
	if err != nil {
		return nil, err
	}
	rows := []comicChapterKeyRow{}
	err = s.App.DB().Select("source_key").From(ComicChaptersCollection).
		Where(dbx.NewExp("comic = {:comic}", dbx.Params{"comic": comicID})).
		OrderBy("chapter_order ASC").
		Limit(int64(dynamicComicChapterLimit(total))).
		All(&rows)
	if err != nil {
		return nil, err
	}
	existing := make(map[string]bool, len(rows))
	for _, row := range rows {
		if row.SourceKey != "" {
			existing[row.SourceKey] = true
		}
	}
	return existing, nil
}

func (s *Store) GetExistingComicChapterURLs(userID, comicID string) (map[string]bool, error) {
	if _, err := s.GetComicAccessible(userID, comicID); err != nil {
		return nil, err
	}
	total, err := s.totalComicChapterCount(comicID)
	if err != nil {
		return nil, err
	}
	// Source synchronization must consider excluded chapters as existing so a
	// later import never recreates an intentionally excluded chapter.
	rows := []comicChapterTitleRow{}
	err = s.App.DB().Select("title").From(ComicChaptersCollection).
		Where(dbx.NewExp("comic = {:comic}", dbx.Params{"comic": comicID})).
		OrderBy("chapter_order ASC").
		Limit(int64(dynamicComicChapterLimit(total))).
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

func (s *Store) GetExistingComicChapterOrders(userID, comicID string) (map[int]bool, error) {
	if _, err := s.GetComicAccessible(userID, comicID); err != nil {
		return nil, err
	}
	total, err := s.totalComicChapterCount(comicID)
	if err != nil {
		return nil, err
	}
	// Same policy as GetExistingComicChapterURLs: excluded records still occupy
	// their source order and must not be reported as missing.
	rows := []comicChapterOrderRow{}
	err = s.App.DB().Select("chapter_order").From(ComicChaptersCollection).
		Where(dbx.NewExp("comic = {:comic}", dbx.Params{"comic": comicID})).
		OrderBy("chapter_order ASC").
		Limit(int64(dynamicComicChapterLimit(total))).
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

// ComicChapterSyncMeta is the identity projection of a stored chapter that the
// source-sync backfill reasons about. It never carries chapter content.
type ComicChapterSyncMeta struct {
	ID           string
	ChapterOrder int
	Title        string
	SourceKey    string
}

// comicChapterSyncRow is the narrow projection backing ListComicChapterSyncMeta.
type comicChapterSyncRow struct {
	ID           string  `db:"id"`
	ChapterOrder float64 `db:"chapter_order"`
	Title        string  `db:"title"`
	SourceKey    string  `db:"source_key"`
}

// ListComicChapterSyncMeta returns the identity fields of every chapter of a
// comic, excluded ones included: an excluded chapter still occupies its
// source order, same policy as the GetExistingChapter* helpers.
func (s *Store) ListComicChapterSyncMeta(userID, comicID string) ([]ComicChapterSyncMeta, error) {
	if _, err := s.GetComicAccessible(userID, comicID); err != nil {
		return nil, err
	}
	total, err := s.totalComicChapterCount(comicID)
	if err != nil {
		return nil, err
	}
	rows := []comicChapterSyncRow{}
	err = s.App.DB().Select("id", "chapter_order", "title", "source_key").From(ComicChaptersCollection).
		Where(dbx.NewExp("comic = {:comic}", dbx.Params{"comic": comicID})).
		OrderBy("chapter_order ASC").
		Limit(int64(dynamicComicChapterLimit(total))).
		All(&rows)
	if err != nil {
		return nil, err
	}
	out := make([]ComicChapterSyncMeta, 0, len(rows))
	for _, row := range rows {
		out = append(out, ComicChapterSyncMeta{
			ID:           row.ID,
			ChapterOrder: asInt(row.ChapterOrder, 0),
			Title:        row.Title,
			SourceKey:    row.SourceKey,
		})
	}
	return out, nil
}

// BackfillComicSourceKeys writes the given source keys onto their chapters, but
// only while the stored key is still empty: the backfill is a one-way
// migration from the legacy heuristic to keyed identity and must never
// overwrite a key some earlier sync already recorded.
func (s *Store) BackfillComicSourceKeys(userID, comicID string, keysByChapterID map[string]string) (int, error) {
	if len(keysByChapterID) == 0 {
		return 0, nil
	}
	if _, err := s.GetComicAccessible(userID, comicID); err != nil {
		return 0, err
	}
	updated := 0
	err := s.App.RunInTransaction(func(txApp core.App) error {
		now := types.NowDateTime().String()
		for id, key := range keysByChapterID {
			if strings.TrimSpace(key) == "" {
				continue
			}
			res, err := txApp.DB().NewQuery(
				"UPDATE comic_chapters SET source_key = {:key}, updated = {:updated} WHERE id = {:id} AND comic = {:comic} AND source_key = ''",
			).Bind(dbx.Params{"key": key, "updated": now, "id": id, "comic": comicID}).Execute()
			if err != nil {
				return err
			}
			// Count only rows the write actually claimed: a concurrent sync
			// may have filled the key first (the WHERE clause then matches
			// nothing), and that must not inflate the reported count.
			if affected, err := res.RowsAffected(); err == nil {
				updated += int(affected)
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return updated, nil
}

// GetExcludedComicChapterOrders returns the source orders of excluded chapters.
// The frontend uses this to suppress fake "missing chapter" warnings when
// visible chapter numbers jump across an excluded chapter.
func (s *Store) GetExcludedComicChapterOrders(userID, comicID string) ([]int, error) {
	if _, err := s.GetComicAccessible(userID, comicID); err != nil {
		return nil, err
	}
	total, err := s.totalComicChapterCount(comicID)
	if err != nil {
		return nil, err
	}
	rows := []comicChapterOrderRow{}
	err = s.App.DB().Select("chapter_order").From(ComicChaptersCollection).
		Where(dbx.NewExp("comic = {:comic} AND excluded = 1 AND chapter_order > 0", dbx.Params{"comic": comicID})).
		OrderBy("chapter_order ASC").
		Limit(int64(dynamicComicChapterLimit(total))).
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

// totalComicChapterCount counts every chapter record of a comic, including
// excluded ones, so source-matching queries can size their fetch limit
// without dropping excluded records at the tail.
func (s *Store) totalComicChapterCount(comicID string) (int, error) {
	total, err := s.App.CountRecords(ComicChaptersCollection, dbx.HashExp{"comic": comicID})
	if err != nil {
		return 0, err
	}
	return int(total), nil
}

func dynamicComicChapterLimit(chapterCount int) int {
	limit := chapterCount + 500
	if limit < 5000 {
		return 5000
	}
	return limit
}

// GetMaxComicChapterPosition returns the highest user-controlled position across
// all chapters of the comic, including excluded ones. New chapters append
// after this value.
func (s *Store) GetMaxComicChapterPosition(userID, comicID string) (int, error) {
	if _, err := s.GetComicAccessible(userID, comicID); err != nil {
		return 0, err
	}
	return s.maxComicChapterPosition(comicID)
}

// maxComicChapterColumn reads MAX(column) straight through dbx instead of hydrating
// the highest-ordered record, whose full content columns would be transferred
// just to read one number. column comes from the fixed callers below, never
// from user input.
func (s *Store) maxComicChapterColumn(column, comicID string) (int, error) {
	row := struct {
		MaxValue float64 `db:"max_value"`
	}{}
	err := s.App.DB().NewQuery(
		"SELECT COALESCE(MAX(" + column + "), 0) AS max_value FROM comic_chapters WHERE comic = {:comic}",
	).Bind(dbx.Params{"comic": comicID}).One(&row)
	if err != nil {
		return 0, err
	}
	return asInt(row.MaxValue, 0), nil
}

func (s *Store) maxComicChapterOrder(comicID string) (int, error) {
	return s.maxComicChapterColumn("chapter_order", comicID)
}

func (s *Store) maxComicChapterPosition(comicID string) (int, error) {
	return s.maxComicChapterColumn("position", comicID)
}

// ListExcludedComicChapterSummaries lists logically deleted chapters so
// the owner can restore them. Excluded chapters are private: only the owner
// may list them, even for public comics. They are sorted by source order.
func (s *Store) ListExcludedComicChapterSummaries(userID, comicID string) ([]ComicChapterSummary, error) {
	if _, err := s.GetOwnedComic(userID, comicID); err != nil {
		return nil, err
	}
	total, err := s.totalComicChapterCount(comicID)
	if err != nil {
		return nil, err
	}
	return s.findComicChapterSummaries(
		"comic = {:comic} AND excluded = 1",
		dynamicComicChapterLimit(total), 0,
		dbx.Params{"comic": comicID},
		"chapter_order ASC",
	)
}

// shiftComicChapterPositions moves every chapter position in [from, to] by delta
// (±1) inside the caller's transaction. Rows are updated one narrow statement
// at a time, in the order that keeps every intermediate (comic, position)
// value unique (descending for +1, ascending for -1): a single
// "SET position = position + 1" cannot be used because SQLite enforces the
// unique index row by row. Callers must first park any colliding row (e.g. the
// moved chapter at -1). Content columns are neither read nor rewritten.
func shiftComicChapterPositions(app core.App, comicID string, from, to, delta int) error {
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
	if err := app.DB().Select("id").From(ComicChaptersCollection).
		Where(dbx.NewExp("comic = {:comic} AND position >= {:from} AND position <= {:to}",
			dbx.Params{"comic": comicID, "from": from, "to": to})).
		OrderBy(order).All(&rows); err != nil {
		return err
	}
	updated := types.NowDateTime().String()
	for _, row := range rows {
		if _, err := app.DB().NewQuery(
			"UPDATE comic_chapters SET position = position + {:delta}, updated = {:updated} WHERE id = {:id}",
		).Bind(dbx.Params{"delta": delta, "updated": updated, "id": row.ID}).Execute(); err != nil {
			return err
		}
	}
	return nil
}

// ReorderComicChapters applies a complete, atomic reorder of a comic's chapters.
//
// Contract: chapterIds must contain every chapter of the comic (visible and
// excluded) exactly once — a permutation. Positions are assigned densely
// 1..N in the given order. Empty, duplicate, foreign or partial lists are
// rejected with ErrInvalidReorder. The operation is rejected with
// ErrActiveJobs while the comic has pending/running jobs so in-flight
// processing is never silently reordered. chapter_order (the source order) is
// never touched, and IDs/content/status/progress stay stable.
func (s *Store) ReorderComicChapters(userID, comicID string, chapterIDs []string) error {
	if _, err := s.GetOwnedComic(userID, comicID); err != nil {
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

	total, err := s.totalComicChapterCount(comicID)
	if err != nil {
		return err
	}
	// Narrow id projection: the permutation check only needs ids, never the
	// full chapter rows (content included) the record query used to hydrate.
	all := []struct {
		ID string `db:"id"`
	}{}
	if err := s.App.DB().Select("id").From(ComicChaptersCollection).
		Where(dbx.NewExp("comic = {:comic}", dbx.Params{"comic": comicID})).
		Limit(int64(dynamicComicChapterLimit(total))).
		All(&all); err != nil {
		return err
	}
	if len(all) != len(chapterIDs) {
		return fmt.Errorf("%w: expected %d chapter ids (every chapter of the comic), got %d", ErrInvalidReorder, len(all), len(chapterIDs))
	}
	byID := make(map[string]struct{}, len(all))
	for _, row := range all {
		byID[row.ID] = struct{}{}
	}
	for _, id := range chapterIDs {
		if _, ok := byID[id]; !ok {
			return fmt.Errorf("%w: chapter id %q does not belong to this comic", ErrInvalidReorder, id)
		}
	}

	active, err := s.HasActiveJobsForComic(comicID)
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
		records, err := txApp.FindRecordsByFilter(JobsCollection, "comic = {:comic} && (status = 'pending' || status = 'running')", "", 1, 0, dbx.Params{"comic": comicID})
		if err != nil {
			return err
		}
		if len(records) > 0 {
			return ErrActiveJobs
		}
		updated := types.NowDateTime().String()
		if _, err := txApp.DB().NewQuery(
			"UPDATE comic_chapters SET position = -position, updated = {:updated} WHERE comic = {:comic}",
		).Bind(dbx.Params{"comic": comicID, "updated": updated}).Execute(); err != nil {
			return err
		}
		for i, id := range chapterIDs {
			if _, err := txApp.DB().NewQuery(
				"UPDATE comic_chapters SET position = {:pos}, updated = {:updated} WHERE id = {:id} AND comic = {:comic}",
			).Bind(dbx.Params{"pos": i + 1, "updated": updated, "id": id, "comic": comicID}).Execute(); err != nil {
				return err
			}
		}
		return nil
	})
}

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	_ "modernc.org/sqlite"
)

// Agent analytics SQL: one read-only query surface for the library assistant.
//
// The model never reaches the application database. Each turn is served from a
// private in-memory SQLite database that is populated, per request, with the
// requesting owner's novels and chapters and nothing else. Isolation is
// structural rather than policed: user rows, superuser rows, provider keys,
// other users' sessions and every base table are simply not present in that
// database, so no SQL the model can write can reach them — a subquery over
// _superusers, a JOIN onto agent_sessions or a second statement all fail at
// the engine before any data is touched.
//
// Ownership is therefore enforced by the backend, not by the model: the rows
// loaded into the sandbox are selected by owner id server-side, and the
// sandbox has no owner column to filter on, so there is nothing for the model
// to override or omit.
//
// Columns mirror the two analytics surfaces the assistant is documented
// against:
//
//	v_agent_novel_progress   — one row per novel with aggregate counters
//	v_agent_chapter_overview — one row per chapter, metadata only
//
// Chapter bodies are never loaded into the sandbox; get_chapter serves those.

const (
	// AgentAnalyticsNovelView / AgentAnalyticsChapterView are the only
	// relations the sandbox contains, and the only ones the model may reference.
	AgentAnalyticsNovelView   = "v_agent_novel_progress"
	AgentAnalyticsChapterView = "v_agent_chapter_overview"

	agentAnalyticsMaxLimit      = 200
	agentAnalyticsDefaultLimit  = 50
	agentAnalyticsMaxCellChars  = 400
	agentAnalyticsMaxResultBody = 64 << 10 // 64 KB of JSON payload
	agentAnalyticsTimeout       = 5 * time.Second
)

// agentNovelProgressRow / agentChapterOverviewRow mirror the sandbox table
// columns. Values are aggregated in Go while building the snapshot, so the
// model's filters and ORDER BY run over already-reduced rows.
type agentNovelProgressRow struct {
	NovelID          string
	Title            string
	Author           string
	Status           string
	SourceLanguage   string
	TargetLanguage   string
	IsPublic         bool
	HasDescription   bool
	Total            int
	Translated       int
	Completed        int
	Pending          int
	OriginalChars    int64
	TranslatedChars  int64
	RefinedChars     int64
	MaxChapterOrder  int
	Updated          string
}

type agentChapterOverviewRow struct {
	NovelID         string
	ChapterID       string
	ChapterOrder    int
	Title           string
	TranslatedTitle string
	Status          string
	Excluded        bool
	OriginalChars   int64
	TranslatedChars int64
	RefinedChars    int64
	ErrorMessage    string
	Updated         string
}

var (
	// agentAnalyticsForbiddenWords blocks statement keywords. It is applied to
	// the query with string literals removed, so a LIKE pattern such as
	// '%update%' no longer trips it while real statements still do.
	agentAnalyticsForbiddenWords = regexp.MustCompile(`(?i)\b(insert|update|delete|drop|alter|create|attach|detach|pragma|vacuum|reindex|load_extension|readfile|writefile)\b`)
	agentAnalyticsForbiddenChars = regexp.MustCompile(`;|--|/\*|\*/`)
	agentAnalyticsLeadingWord    = regexp.MustCompile(`(?i)^\s*(select|with)\b`)
	// agentAnalyticsRelation finds every FROM/JOIN target, including the ones
	// inside subqueries and CTEs, so nothing can be smuggled past the
	// allowlist by nesting the read.
	agentAnalyticsRelation  = regexp.MustCompile(`(?is)\b(?:from|join)\s+([A-Za-z_][A-Za-z0-9_]*)`)
	// agentAnalyticsCTEName matches "WITH x AS (" / ", y AS (" declarations.
	agentAnalyticsCTEName   = regexp.MustCompile(`(?is)(?:\bwith\b|,)\s*([A-Za-z_][A-Za-z0-9_]*)\s+as\s*\(`)
	agentAnalyticsStringLit = regexp.MustCompile(`'(?:[^']|'')*'|"(?:[^"]|"")*"`)
	agentAnalyticsUIDChars   = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
)

// agentAnalyticsAllowlist is the closed set of relations the model may read.
var agentAnalyticsAllowlist = map[string]bool{
	AgentAnalyticsNovelView:   true,
	AgentAnalyticsChapterView: true,
}

// stripAgentLiterals blanks out quoted string literals so keyword checks run
// against SQL structure, not against user data that happens to spell a
// forbidden word.
func stripAgentLiterals(query string) string {
	return agentAnalyticsStringLit.ReplaceAllString(query, "''")
}

// validateAgentAnalyticsSQL rejects anything that is not a single,
// comment-free SELECT whose every relation is one of the two sandbox tables.
// Error strings double as model-facing teaching messages.
func validateAgentAnalyticsSQL(query string) error {
	trimmed := strings.TrimSpace(query)
	trimmed = strings.TrimSuffix(trimmed, ";")
	if strings.TrimSpace(trimmed) == "" {
		return fmt.Errorf("empty query: write one SELECT over %s or %s", AgentAnalyticsNovelView, AgentAnalyticsChapterView)
	}
	if agentAnalyticsForbiddenChars.MatchString(trimmed) {
		return fmt.Errorf("only a single statement without ';' or comments is allowed")
	}
	if !agentAnalyticsLeadingWord.MatchString(trimmed) {
		return fmt.Errorf("only SELECT (or WITH ... SELECT) queries are allowed")
	}
	structural := stripAgentLiterals(trimmed)
	if agentAnalyticsForbiddenWords.MatchString(structural) {
		return fmt.Errorf("read-only surface: mutations and pragmas are not allowed")
	}
	relations := agentAnalyticsRelation.FindAllStringSubmatch(structural, -1)
	if len(relations) == 0 {
		return fmt.Errorf("queries must read %s or %s", AgentAnalyticsNovelView, AgentAnalyticsChapterView)
	}
	// Names bound by the query's own CTEs read nothing on their own: they
	// resolve to the allowed views they were built from, which are checked
	// separately.
	ctes := agentAnalyticsCTEName.FindAllStringSubmatch(structural, -1)
	bound := make(map[string]bool, len(ctes))
	for _, cte := range ctes {
		bound[strings.ToLower(cte[1])] = true
	}
	for _, rel := range relations {
		name := strings.ToLower(strings.Trim(rel[1], `"`))
		if bound[name] || agentAnalyticsAllowlist[name] {
			continue
		}
		// The allowlist is what makes this safe, so the message says why:
		// the sandbox only ever holds the caller's own library rows.
		return fmt.Errorf("relation %q does not exist here: this surface only exposes the caller's own novels and chapters through %s and %s", name, AgentAnalyticsNovelView, AgentAnalyticsChapterView)
	}
	return nil
}

// agentAnalyticsRO returns the dedicated in-memory handle. It never opens
// data.db: the sandbox is a separate, empty database populated per request.
func (s *Store) agentAnalyticsRO() (*sql.DB, error) {
	s.agentAnalyticsOnce.Do(func() {
		db, err := sql.Open("sqlite", ":memory:")
		if err == nil {
			// One connection keeps every statement of a request on the same
			// private in-memory database.
			db.SetMaxOpenConns(1)
			db.SetMaxIdleConns(1)
		}
		s.agentAnalyticsRODB = db
		s.agentAnalyticsROErr = err
	})
	return s.agentAnalyticsRODB, s.agentAnalyticsROErr
}

// RunAgentAnalyticsQuery validates and runs one read-only analytics query for
// the given user and returns the model-facing JSON payload. Rows are capped
// (truncation is reported), cells are truncated, and the whole result is
// byte-capped.
func (s *Store) RunAgentAnalyticsQuery(ctx context.Context, userID, query string, limit int) (string, error) {
	if !agentAnalyticsUIDChars.MatchString(userID) {
		return "", fmt.Errorf("invalid user id")
	}
	if err := validateAgentAnalyticsSQL(query); err != nil {
		return "", err
	}
	if limit <= 0 {
		limit = agentAnalyticsDefaultLimit
	}
	if limit > agentAnalyticsMaxLimit {
		limit = agentAnalyticsMaxLimit
	}

	db, err := s.agentAnalyticsRO()
	if err != nil {
		return "", fmt.Errorf("open analytics sandbox: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, agentAnalyticsTimeout)
	defer cancel()

	// ponytail: the sandbox is rebuilt from the owner's rows on every query
	// (one aggregate over novels + one over their chapters), so a call costs
	// O(the caller's own library) rather than O(1) and a 10k-chapter library
	// pays ~10k inserts per query_library call. Ceiling is fine for personal
	// libraries; if it ever bites, cache the snapshot per user and invalidate
	// it on novel/chapter writes instead of rebuilding.
	s.agentAnalyticsMu.Lock()
	defer s.agentAnalyticsMu.Unlock()
	if err := s.populateAgentSandbox(ctx, db, userID); err != nil {
		return "", err
	}

	// Outer LIMIT: the model's own LIMIT/ORDER BY survive inside the
	// subquery; the wrapper only caps the payload. limit+1 probes for
	// truncation without claiming more rows than the caller asked for.
	wrapped := fmt.Sprintf("SELECT * FROM ( %s ) agent_result LIMIT %d", query, limit+1)

	rows, err := db.QueryContext(ctx, wrapped)
	if err != nil {
		return "", fmt.Errorf("query failed (only %s and %s exist here): %w", AgentAnalyticsNovelView, AgentAnalyticsChapterView, err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return "", err
	}
	result := AgentAnalyticsResult{Columns: columns, Rows: [][]any{}}
	totalBytes := 0
	for rows.Next() {
		raw := make([]any, len(columns))
		ptrs := make([]any, len(columns))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return "", err
		}
		row := make([]any, len(columns))
		for i, cell := range raw {
			row[i] = truncateAgentCell(cell, &totalBytes)
		}
		if len(result.Rows) == limit {
			// The extra row only proves there was more; do not report it.
			result.Truncated = true
			result.Note = fmt.Sprintf("results truncated at %d rows; refine the query (filters, smaller projection) and query again", limit)
			break
		}
		result.Rows = append(result.Rows, row)
		if totalBytes > agentAnalyticsMaxResultBody {
			result.Truncated = true
			result.Note = fmt.Sprintf("results truncated at %d rows; refine the query (filters, smaller projection) and query again", limit)
			break
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(result.Rows) == 0 {
		result.Note = "no rows matched"
	}

	out, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// populateAgentSandbox (re)creates the two sandbox tables and fills them with
// the owner's own library rows. The WHERE clauses are the ownership filter:
// they are written here, server-side, and are not part of anything the model
// can influence.
func (s *Store) populateAgentSandbox(ctx context.Context, db *sql.DB, ownerID string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, ddl := range []string{
		"DROP TABLE IF EXISTS " + AgentAnalyticsNovelView,
		"DROP TABLE IF EXISTS " + AgentAnalyticsChapterView,
		fmt.Sprintf(`CREATE TABLE %s (
	novel_id TEXT, title TEXT, author TEXT, status TEXT, source_language TEXT,
	target_language TEXT, is_public INTEGER, has_description INTEGER, total INTEGER,
	translated INTEGER, completed INTEGER, pending INTEGER, original_chars INTEGER,
	translated_chars INTEGER, refined_chars INTEGER, max_chapter_order INTEGER,
	updated TEXT)`, AgentAnalyticsNovelView),
		fmt.Sprintf(`CREATE TABLE %s (
	novel_id TEXT, chapter_id TEXT, chapter_order INTEGER, title TEXT,
	translated_title TEXT, status TEXT, excluded INTEGER, original_chars INTEGER,
	translated_chars INTEGER, refined_chars INTEGER, error_message TEXT,
	updated TEXT)`, AgentAnalyticsChapterView),
	} {
		if _, err := tx.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("prepare agent sandbox: %w", err)
		}
	}

	novels, err := s.agentSnapshotNovels(ownerID)
	if err != nil {
		return err
	}
	novelStmt, err := tx.PrepareContext(ctx, "INSERT INTO "+AgentAnalyticsNovelView+
		" (novel_id, title, author, status, source_language, target_language, is_public, has_description, total, translated, completed, pending, original_chars, translated_chars, refined_chars, max_chapter_order, updated)"+
		" VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)")
	if err != nil {
		return err
	}
	defer novelStmt.Close()
	for _, n := range novels {
		if _, err := novelStmt.ExecContext(ctx, n.NovelID, n.Title, n.Author, n.Status, n.SourceLanguage,
			n.TargetLanguage, boolToInt(n.IsPublic), boolToInt(n.HasDescription), n.Total, n.Translated,
			n.Completed, n.Pending, n.OriginalChars, n.TranslatedChars, n.RefinedChars, n.MaxChapterOrder, n.Updated); err != nil {
			return err
		}
	}

	chapters, err := s.agentSnapshotChapters(ownerID)
	if err != nil {
		return err
	}
	chapterStmt, err := tx.PrepareContext(ctx, "INSERT INTO "+AgentAnalyticsChapterView+
		" (novel_id, chapter_id, chapter_order, title, translated_title, status, excluded, original_chars, translated_chars, refined_chars, error_message, updated)"+
		" VALUES (?,?,?,?,?,?,?,?,?,?,?,?)")
	if err != nil {
		return err
	}
	defer chapterStmt.Close()
	for _, c := range chapters {
		if _, err := chapterStmt.ExecContext(ctx, c.NovelID, c.ChapterID, c.ChapterOrder, c.Title,
			c.TranslatedTitle, c.Status, boolToInt(c.Excluded), c.OriginalChars, c.TranslatedChars,
			c.RefinedChars, c.ErrorMessage, c.Updated); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// agentSnapshotNovels aggregates the owner's novels in one pass. Counters are
// computed here rather than read from the novel's pre-aggregated fields so the
// sandbox always reports the same numbers as the assistant's other tools:
// only non-excluded chapters count, and pending is the complement of
// translated/refined/done.
func (s *Store) agentSnapshotNovels(ownerID string) ([]agentNovelProgressRow, error) {
	rows := []struct {
		ID              string `db:"id"`
		SourceTitle     string `db:"source_title"`
		TargetTitle     string `db:"target_title"`
		SourceAuthor    string `db:"source_author"`
		TargetAuthor    string `db:"target_author"`
		Status          string `db:"status"`
		SourceLanguage  string `db:"source_language"`
		TargetLanguage  string `db:"target_language"`
		IsPublic        bool   `db:"is_public"`
		TargetDescr     string `db:"target_description"`
		OriginalChars   int64  `db:"original_chars"`
		TranslatedChars int64  `db:"translated_chars"`
		RefinedChars    int64  `db:"refined_chars"`
		Total           int    `db:"total"`
		Translated      int    `db:"translated"`
		Completed       int    `db:"completed"`
		Pending         int    `db:"pending"`
		MaxOrder        int    `db:"max_order"`
		Updated         string `db:"updated"`
	}{}
	if err := s.App.DB().NewQuery(`
		SELECT n.id, n.source_title, n.target_title, n.source_author, n.target_author,
			n.status, n.source_language, n.target_language, n.is_public, n.target_description,
			COALESCE(SUM(CASE WHEN c.excluded = 0 THEN c.original_char_count ELSE 0 END), 0) AS original_chars,
			COALESCE(SUM(CASE WHEN c.excluded = 0 THEN c.translated_char_count ELSE 0 END), 0) AS translated_chars,
			COALESCE(SUM(CASE WHEN c.excluded = 0 THEN c.refined_char_count ELSE 0 END), 0) AS refined_chars,
			COALESCE(SUM(CASE WHEN c.excluded = 0 THEN 1 ELSE 0 END), 0) AS total,
			COALESCE(SUM(CASE WHEN c.excluded = 0 AND c.status IN ('translated','refined','done') THEN 1 ELSE 0 END), 0) AS translated,
			COALESCE(SUM(CASE WHEN c.excluded = 0 AND c.status IN ('refined','done') THEN 1 ELSE 0 END), 0) AS completed,
			COALESCE(SUM(CASE WHEN c.excluded = 0 AND c.status NOT IN ('translated','refined','done') THEN 1 ELSE 0 END), 0) AS pending,
			COALESCE(MAX(c.chapter_order), 0) AS max_order,
			n.updated
		FROM novels n
		LEFT JOIN chapters c ON c.novel = n.id
		WHERE n.owner = {:owner}
		GROUP BY n.id
	`).Bind(dbx.Params{"owner": ownerID}).All(&rows); err != nil {
		return nil, err
	}
	out := make([]agentNovelProgressRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, agentNovelProgressRow{
			NovelID:         r.ID,
			Title:           firstNonBlank(r.TargetTitle, r.SourceTitle),
			Author:          firstNonBlank(r.TargetAuthor, r.SourceAuthor),
			Status:          r.Status,
			SourceLanguage:  r.SourceLanguage,
			TargetLanguage:  r.TargetLanguage,
			IsPublic:        r.IsPublic,
			HasDescription:  strings.TrimSpace(r.TargetDescr) != "",
			Total:           r.Total,
			Translated:      r.Translated,
			Completed:       r.Completed,
			Pending:         r.Pending,
			OriginalChars:   r.OriginalChars,
			TranslatedChars: r.TranslatedChars,
			RefinedChars:    r.RefinedChars,
			MaxChapterOrder: r.MaxOrder,
			Updated:         r.Updated,
		})
	}
	return out, nil
}

// agentSnapshotChapters returns the owner's chapter metadata rows, ordered by
// novel then chapter order.
func (s *Store) agentSnapshotChapters(ownerID string) ([]agentChapterOverviewRow, error) {
	rows := []struct {
		NovelID        string `db:"novel_id"`
		ID             string `db:"chapter_id"`
		ChapterOrder   int    `db:"chapter_order"`
		Title          string `db:"title"`
		TranslatedTitl string `db:"translated_title"`
		Status         string `db:"status"`
		Excluded       bool   `db:"excluded"`
		OriginalChars  int64  `db:"original_chars"`
		TranslatedChar int64  `db:"translated_chars"`
		RefinedChars   int64  `db:"refined_chars"`
		ErrorMessage   string `db:"error_message"`
		Updated        string `db:"updated"`
	}{}
	if err := s.App.DB().NewQuery(`
		SELECT c.novel AS novel_id, c.id AS chapter_id, c.chapter_order, c.title,
			c.translated_title, c.status, c.excluded,
			COALESCE(c.original_char_count, 0) AS original_chars,
			COALESCE(c.translated_char_count, 0) AS translated_chars,
			COALESCE(c.refined_char_count, 0) AS refined_chars,
			COALESCE(c.error_message, '') AS error_message,
			c.updated
		FROM chapters c
		JOIN novels n ON n.id = c.novel
		WHERE n.owner = {:owner}
		ORDER BY c.novel, c.chapter_order
	`).Bind(dbx.Params{"owner": ownerID}).All(&rows); err != nil {
		return nil, err
	}
	out := make([]agentChapterOverviewRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, agentChapterOverviewRow{
			NovelID:         r.NovelID,
			ChapterID:       r.ID,
			ChapterOrder:    r.ChapterOrder,
			Title:           r.Title,
			TranslatedTitle: r.TranslatedTitl,
			Status:          r.Status,
			Excluded:        r.Excluded,
			OriginalChars:   r.OriginalChars,
			TranslatedChars: r.TranslatedChar,
			RefinedChars:    r.RefinedChars,
			ErrorMessage:    r.ErrorMessage,
			Updated:         r.Updated,
		})
	}
	return out, nil
}

func firstNonBlank(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// AgentAnalyticsResult is the JSON-friendly shape returned to the model.
type AgentAnalyticsResult struct {
	Columns   []string `json:"columns"`
	Rows      [][]any  `json:"rows"`
	Truncated bool     `json:"truncated,omitempty"`
	Note      string   `json:"note,omitempty"`
}

// truncateAgentCell stringifies one cell, capping long text (chapter content
// never reaches the sandbox, but titles and error messages can still be long)
// and accumulating the payload size. The cut happens on a rune boundary so
// multi-byte titles stay valid UTF-8.
func truncateAgentCell(cell any, totalBytes *int) any {
	var s string
	switch v := cell.(type) {
	case nil:
		return nil
	case bool:
		s = strconv.FormatBool(v)
	case int64:
		return v
	case float64:
		return v
	case []byte:
		s = string(v)
	default:
		s = fmt.Sprint(v)
	}
	*totalBytes += len(s)
	if len(s) > agentAnalyticsMaxCellChars {
		return TruncateRunes(s, agentAnalyticsMaxCellChars) + "…"
	}
	return s
}

// TruncateRunes cuts s to at most maxChars runes, never splitting a multi-byte
// character. The agent serves accented and CJK text, so a byte-wise cut would
// emit invalid UTF-8 at every boundary.
func TruncateRunes(s string, maxChars int) string {
	if maxChars <= 0 {
		return ""
	}
	count := 0
	for i := range s {
		if count == maxChars {
			return s[:i]
		}
		count++
	}
	return s
}

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Agent analytics SQL: one read-only query surface for the library assistant.
//
// The model never sees base tables. It queries two curated views that carry
// no user data beyond novel/chapter metadata and no chapter bodies at all:
//
//	v_agent_novel_progress  — one row per novel with aggregate counters
//	v_agent_chapter_overview — one row per chapter, metadata only
//
// Ownership is enforced server-side, not by the model: per request the
// handler recreates temp views on a dedicated single-connection read-only
// handle, pre-filtered by owner, and the model's SQL is rewritten to hit the
// scoped temp views. Mutations are impossible at the engine level (mode=ro);
// a keyword/table blocklist and single-statement checks close the rest.

const (
	// AgentAnalyticsNovelView / AgentAnalyticsChapterView are the only
	// relations the model is allowed to reference.
	AgentAnalyticsNovelView   = "v_agent_novel_progress"
	AgentAnalyticsChapterView = "v_agent_chapter_overview"

	agentScopeNovelsTempView   = "agent_scope_novels"
	agentScopeChaptersTempView = "agent_scope_chapters"

	agentAnalyticsMaxLimit      = 200
	agentAnalyticsDefaultLimit  = 50
	agentAnalyticsMaxCellChars  = 400
	agentAnalyticsMaxResultBody = 64 << 10 // 64 KB of JSON payload
	agentAnalyticsTimeout       = 5 * time.Second
)

// ensureAgentAnalyticsViews (re)creates the analytics views. DROP+CREATE on
// every boot keeps view definitions evolving without a migration flag; they
// are derived data with no stored state.
func (s *Store) ensureAgentAnalyticsViews() error {
	for _, ddl := range []string{
		fmt.Sprintf("DROP VIEW IF EXISTS %s", AgentAnalyticsNovelView),
		fmt.Sprintf("DROP VIEW IF EXISTS %s", AgentAnalyticsChapterView),
		fmt.Sprintf(`CREATE VIEW %s AS
SELECT
	n.owner                                             AS owner_id,
	n.id                                                AS novel_id,
	COALESCE(NULLIF(n.target_title, ''), n.source_title) AS title,
	COALESCE(NULLIF(n.target_author, ''), n.source_author) AS author,
	n.status                                            AS status,
	n.source_language                                   AS source_language,
	n.target_language                                   AS target_language,
	n.is_public                                         AS is_public,
	(TRIM(COALESCE(n.target_description, '')) != '')    AS has_description,
	COALESCE(SUM(CASE WHEN c.excluded = 0 THEN 1 ELSE 0 END), 0)                        AS total,
	COALESCE(SUM(CASE WHEN c.excluded = 0 AND c.status IN ('translated','refined','done') THEN 1 ELSE 0 END), 0) AS translated,
	COALESCE(SUM(CASE WHEN c.excluded = 0 AND c.status IN ('refined','done') THEN 1 ELSE 0 END), 0) AS completed,
	COALESCE(SUM(CASE WHEN c.excluded = 0 AND c.status NOT IN ('translated','refined','done') THEN 1 ELSE 0 END), 0) AS pending,
	COALESCE(SUM(CASE WHEN c.excluded = 0 THEN c.original_char_count ELSE 0 END), 0)    AS original_chars,
	COALESCE(SUM(CASE WHEN c.excluded = 0 THEN c.translated_char_count ELSE 0 END), 0)  AS translated_chars,
	COALESCE(SUM(CASE WHEN c.excluded = 0 THEN c.refined_char_count ELSE 0 END), 0)     AS refined_chars,
	COALESCE(MAX(c.chapter_order), 0)                   AS max_chapter_order,
	n.updated                                           AS updated
FROM novels n
LEFT JOIN chapters c ON c.novel = n.id
GROUP BY n.id`, AgentAnalyticsNovelView),
		fmt.Sprintf(`CREATE VIEW %s AS
SELECT
	n.owner                    AS owner_id,
	c.novel                    AS novel_id,
	c.id                       AS chapter_id,
	c.chapter_order            AS chapter_order,
	c.title                    AS title,
	c.translated_title         AS translated_title,
	c.status                   AS status,
	c.excluded                 AS excluded,
	c.original_char_count      AS original_chars,
	c.translated_char_count    AS translated_chars,
	c.refined_char_count       AS refined_chars,
	c.error_message            AS error_message,
	c.updated                  AS updated
FROM chapters c
JOIN novels n ON n.id = c.novel`, AgentAnalyticsChapterView),
	} {
		if _, err := s.App.DB().NewQuery(ddl).Execute(); err != nil {
			return fmt.Errorf("ensure agent analytics view: %w", err)
		}
	}
	return nil
}

// AgentAnalyticsResult is the JSON-friendly shape returned to the model.
type AgentAnalyticsResult struct {
	Columns   []string `json:"columns"`
	Rows      [][]any  `json:"rows"`
	Truncated bool     `json:"truncated,omitempty"`
	Note      string   `json:"note,omitempty"`
}

var (
	agentAnalyticsForbiddenWords = regexp.MustCompile(`(?i)\b(insert|update|delete|drop|alter|create|replace|pragma|attach|detach|vacuum|reindex|analyze|load_extension|readfile|writefile)\b`)
	// Every PB collection and internal table except the two analytics views.
	agentAnalyticsForbiddenTables = regexp.MustCompile(`(?i)\b(users|providers|user_provider_settings|user_prompt_settings|user_translation_settings|novels|chapters|translation_jobs|epubs|reading_progress|worker_tokens|invitations|shared_provider_keys|prompt_overrides|password_resets|migrations|_collections|_externalAuths|_mfas|_otps|_params|sqlite_sequence|sqlite_master|sqlite_schema)\b`)
	agentAnalyticsForbiddenChars  = regexp.MustCompile(`;|--|/\*|\*/`)
	agentAnalyticsLeadingWord     = regexp.MustCompile(`(?i)^\s*(select|with)\b`)
	agentAnalyticsUsesView        = regexp.MustCompile(`v_agent_`)
	agentAnalyticsUIDChars        = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
)

// validateAgentAnalyticsSQL rejects anything that is not a single, flat,
// comment-free SELECT over the analytics views. Error strings double as
// model-facing teaching messages.
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
	if agentAnalyticsForbiddenWords.MatchString(trimmed) {
		return fmt.Errorf("read-only surface: mutations and pragmas are not allowed")
	}
	if agentAnalyticsForbiddenTables.MatchString(trimmed) {
		return fmt.Errorf("base tables are not readable here: query only %s and %s", AgentAnalyticsNovelView, AgentAnalyticsChapterView)
	}
	if !agentAnalyticsUsesView.MatchString(trimmed) {
		return fmt.Errorf("queries must reference %s or %s", AgentAnalyticsNovelView, AgentAnalyticsChapterView)
	}
	return nil
}

// agentAnalyticsRO returns the dedicated single-connection read-only handle.
func (s *Store) agentAnalyticsRO() (*sql.DB, error) {
	s.agentAnalyticsOnce.Do(func() {
		dsn := "file:" + filepath.Join(s.App.DataDir(), "data.db") + "?mode=ro&_pragma=busy_timeout(5000)"
		db, err := sql.Open("sqlite", dsn)
		if err == nil {
			db.SetMaxOpenConns(1)
			db.SetMaxIdleConns(1)
		}
		s.agentAnalyticsRODB = db
		s.agentAnalyticsROErr = err
	})
	return s.agentAnalyticsRODB, s.agentAnalyticsROErr
}

// rescopeAgentViews recreates the per-user temp views on the single
// connection. MaxOpenConns(1) serializes requests, so the drop/create pair
// can never interleave with another user's query.
func rescopeAgentViews(ctx context.Context, db *sql.DB, ownerID string) error {
	for _, stmt := range []string{
		"DROP VIEW IF EXISTS temp." + agentScopeNovelsTempView,
		"DROP VIEW IF EXISTS temp." + agentScopeChaptersTempView,
		fmt.Sprintf("CREATE TEMP VIEW %s AS SELECT * FROM %s WHERE owner_id = '%s'", agentScopeNovelsTempView, AgentAnalyticsNovelView, ownerID),
		fmt.Sprintf("CREATE TEMP VIEW %s AS SELECT * FROM %s WHERE owner_id = '%s'", agentScopeChaptersTempView, AgentAnalyticsChapterView, ownerID),
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("scope agent analytics views: %w", err)
		}
	}
	return nil
}

// rewriteAgentViews points the model's SQL at the pre-scoped temp views.
func rewriteAgentViews(query string) string {
	replaced := regexp.MustCompile(`(?i)`+AgentAnalyticsNovelView).ReplaceAllString(query, agentScopeNovelsTempView)
	replaced = regexp.MustCompile(`(?i)`+AgentAnalyticsChapterView).ReplaceAllString(replaced, agentScopeChaptersTempView)
	return replaced
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
		return "", fmt.Errorf("open analytics handle: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, agentAnalyticsTimeout)
	defer cancel()

	if err := rescopeAgentViews(ctx, db, userID); err != nil {
		return "", err
	}

	// Outer LIMIT: the model's own LIMIT/ORDER BY survive inside the
	// subquery; the wrapper only caps the payload.
	wrapped := fmt.Sprintf("SELECT * FROM ( %s ) agent_result LIMIT %d", rewriteAgentViews(query), limit+1)

	rows, err := db.QueryContext(ctx, wrapped)
	if err != nil {
		return "", fmt.Errorf("query failed (only the two v_agent_* views exist here): %w", err)
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
		result.Rows = append(result.Rows, row)
		if len(result.Rows) > limit || totalBytes > agentAnalyticsMaxResultBody {
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

// truncateAgentCell stringifies one cell, capping long text (chapter content
// never reaches these views, but titles and error messages can still be long)
// and accumulating the payload size.
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
		return s[:agentAnalyticsMaxCellChars] + "…"
	}
	return s
}

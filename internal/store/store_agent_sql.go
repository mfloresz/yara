package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"modernc.org/sqlite"
)

// Agent analytics SQL: one read-only query surface for the library assistant.
//
// The model never reaches the application database. Each turn is served from a
// private SQLite sandbox, built per request from the requesting owner's novels
// and chapters and nothing else. Isolation is structural rather than policed:
// user rows, superuser rows, provider keys, other users' sessions and every
// base table are simply not present in that database, so no SQL the model can
// write can reach them — a subquery over _superusers, a JOIN onto
// agent_sessions or a second statement all fail at the engine before any data
// is touched.
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
//
// The sandbox holds no owner column: rows are selected by owner id before
// they are inserted, so the model has nothing to filter on and nothing to
// override.
//
// Why a per-request copy instead of a read-only handle on data.db:
//
//   - A read-only handle would expose the real base tables by name (novels,
//     chapters, users, agent_sessions, _superusers). Blocking those by name
//     means maintaining an allowlist that must track every PocketBase internal
//     table, and one miss leaks. The copy cannot leak what it does not contain.
//   - The copy scales with the OWNER's library, not the whole install: a
//     100-novel library copies 100 rows, whether the server holds 1 user or
//     1,000. Cost is per-user, not multiplied by tenant count.
//
// How the copy is built — ATTACH + CREATE TABLE AS SELECT, not row-by-row:
//
// The obvious implementation reads the owner's rows in Go and INSERTs them into
// the sandbox one at a time. That round-trips every row through Go twice, and
// it dominated the cost: at 16k chapters the read took ~213ms and the insert
// another ~150ms, so every chapter-level question paid ~360ms of pure copying
// before SQLite did any work.
//
// Attaching data.db read-only and letting SQLite materialise the view itself
// (CREATE TABLE ... AS SELECT) does the same transfer entirely inside the
// engine: ~6ms for the same 16k rows, a ~57x difference. The ownership filter
// is still a server-written WHERE clause over the owner's id; nothing the model
// writes reaches it.
//
// The attach is scoped and then destroyed, never left open:
//
//   - The sandbox is a temp FILE database, not :memory:. database/sql treats
//     ":memory:" as per-connection, so the builder must pin one connection
//     (SetMaxOpenConns(1)) or a second statement would silently see an empty
//     database. A file removes that class of bug entirely: the builder opens
//     the file, creates the views, and closes the handle. The model's query
//     then opens the SAME file through a brand-new connection that can only
//     reach what the builder left in it.
//   - The builder does not DETACH. It closes. Once that handle is gone there is
//     no attachment left to name: `SELECT * FROM novels` fails with "no such
//     table" for the same reason it does against any unrelated relation.
//   - data.db is attached with mode=ro, so even inside the builder window the
//     source cannot be written.
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

	// agentAnalyticsMaxCellBytes bounds any single string or BLOB the engine
	// will materialise (SQLITE_LIMIT_LENGTH). Without it the payload limits
	// above are cosmetic: they are applied after rows.Next()/Scan() has already
	// built the value, so a single cell like
	// replace(hex(zeroblob(10000000)),'0','AAAA') allocates ~76MB and a
	// zeroblob(200000000) ~381MB before Go ever sees a byte, then arrives as a
	// 400-rune truncated cell the model pays full price for. Measured: with the
	// limit the same queries fail in ~90us with "string or blob too big" and
	// 0MB allocated, while ordinary aggregates are unaffected.
	agentAnalyticsMaxCellBytes = 1 << 20 // 1 MB
	// agentAnalyticsMaxExprDepth bounds expression-tree nesting, which is what a
	// deeply nested recursive CTE walks up. The query deadline catches those
	// too, but this fails them in microseconds instead of seconds.
	agentAnalyticsMaxExprDepth = 64
)

var (
	// agentAnalyticsForbiddenWords blocks statement keywords. It is applied to
	// the query with string literals removed, so a LIKE pattern such as
	// '%update%' no longer trips it while real statements still do.
	agentAnalyticsForbiddenWords = regexp.MustCompile(`(?i)\b(insert|update|delete|drop|alter|create|attach|detach|pragma|vacuum|reindex|load_extension|readfile|writefile)\b`)
	agentAnalyticsForbiddenChars = regexp.MustCompile(`;|--|/\*|\*/`)
	agentAnalyticsLeadingWord    = regexp.MustCompile(`(?i)^\s*(select|with)\b`)
	// agentAnalyticsRelation finds FROM/JOIN targets so a query must read at
	// least one of the two sandbox views. This is a requirement, not an
	// allowlist: naming any other relation is not rejected here because the
	// sandbox holds nothing else, so the engine fails it on its own.
	//
	// Matched against the raw query rather than the literal-stripped one, so a
	// quoted relation name ("v_agent_novel_progress") still counts.
	agentAnalyticsRelation  = regexp.MustCompile(`(?is)\b(?:from|join)\s+["'\x60]?([A-Za-z_][A-Za-z0-9_]*)`)
	agentAnalyticsStringLit = regexp.MustCompile(`'(?:[^']|'')*'|"(?:[^"]|"")*"`)
	agentAnalyticsUIDChars  = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
)

// stripAgentLiterals blanks out quoted string literals so keyword checks run
// against SQL structure, not against user data that happens to spell a
// forbidden word.
func stripAgentLiterals(query string) string {
	return agentAnalyticsStringLit.ReplaceAllString(query, "''")
}

// validateAgentAnalyticsSQL rejects anything that is not a single,
// comment-free SELECT that reads one of the two sandbox views.
//
// The only thing it polices is statement shape. It deliberately does NOT
// police which relations the query names: the sandbox is a private database
// holding only the caller's own rows, so a reference to anything else fails
// at the engine with "no such table" and reaches no data. An earlier version
// tried to enforce an allowlist by scanning every FROM/JOIN target, which was
// both incomplete (SQLite's legacy comma join is not a FROM/JOIN keyword, so
// `FROM v_agent_novel_progress, "sqlite_master"` slipped through) and
// unnecessary. Error strings double as model-facing teaching messages.
func validateAgentAnalyticsSQL(query string) error {
	trimmed := strings.TrimSpace(query)
	trimmed = strings.TrimSuffix(trimmed, ";")
	if strings.TrimSpace(trimmed) == "" {
		return fmt.Errorf("empty query: write one SELECT over %s or %s", AgentAnalyticsNovelView, AgentAnalyticsChapterView)
	}
	// Structure first, then the checks that must not see user data. Running
	// the character/word blocklist on the raw query made a novel titled
	// "A -- B" (or any title containing ';', '--' or '/*') fail validation
	// with a message about statement shape, which the model reads as "my SQL
	// is malformed" and cannot act on. Literals are blanked first so those
	// checks only ever see SQL structure.
	structural := stripAgentLiterals(trimmed)
	if agentAnalyticsForbiddenChars.MatchString(structural) {
		return fmt.Errorf("only a single statement without ';' or comments is allowed")
	}
	if !agentAnalyticsLeadingWord.MatchString(strings.TrimSpace(structural)) {
		return fmt.Errorf("only SELECT (or WITH ... SELECT) queries are allowed")
	}
	if agentAnalyticsForbiddenWords.MatchString(structural) {
		return fmt.Errorf("read-only surface: mutations and pragmas are not allowed")
	}
	for _, rel := range agentAnalyticsRelation.FindAllStringSubmatch(trimmed, -1) {
		name := strings.ToLower(rel[1])
		if name == AgentAnalyticsNovelView || name == AgentAnalyticsChapterView {
			return nil
		}
	}
	return fmt.Errorf("queries must read %s or %s", AgentAnalyticsNovelView, AgentAnalyticsChapterView)
}

// agentAnalyticsRO opens the sandbox as a temp file database and pins the one
// connection the engine limits are set on.
//
// ponytail: a temp file per query rather than a pooled handle. Opening an
// in-memory SQLite database is cheap relative to the materialisation that
// follows, and a fresh handle per request removes any chance of one user's
// sandbox outliving its query. If open cost ever showed up in a profile, pool
// the file handles here instead.
//
// The engine limits are set on the pinned connection because sqlite3_limit is
// per-connection, not per-database: a limit applied to a connection the pool
// later replaced would silently not apply at all. SetMaxOpenConns(1) is what
// makes the pin real, and the DSN is private-cache so the file is not shared.
func agentAnalyticsRO(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?cache=private&_pragma=query_only(1)")
	if err != nil {
		return nil, err
	}
	// One connection: it keeps every statement of a request on the same
	// database AND keeps the engine limits below attached to the connection
	// they were set on.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return db, nil
}

// setAgentAnalyticsLimits applies the engine-level bounds on the connection the
// query will run on. It must be called on the same *sql.Conn that later runs
// the statement, since sqlite3_limit is per-connection.
func setAgentAnalyticsLimits(ctx context.Context, conn *sql.Conn) error {
	if _, err := sqlite.Limit(conn, sqliteLimitLength, agentAnalyticsMaxCellBytes); err != nil {
		return fmt.Errorf("set cell size limit: %w", err)
	}
	if _, err := sqlite.Limit(conn, sqliteLimitExprDepth, agentAnalyticsMaxExprDepth); err != nil {
		return fmt.Errorf("set expression depth limit: %w", err)
	}
	return nil
}

// SQLITE_LIMIT_* ids from <sqlite3.h>, as used by sqlite3_limit.
const (
	sqliteLimitLength    = 0 // SQLITE_LIMIT_LENGTH
	sqliteLimitExprDepth = 3 // SQLITE_LIMIT_EXPR_DEPTH
)

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

	ctx, cancel := context.WithTimeout(ctx, agentAnalyticsTimeout)
	defer cancel()

	// ponytail: the sandbox is rebuilt from the owner's rows on every query, so
	// a call costs O(the caller's own library) rather than O(1). It is
	// materialised inside SQLite (see the ATTACH note at the top of this file),
	// which is what keeps that proportional but small: ~6ms at 16k chapters.
	// Only the view the query names is built, so a novel-level question never
	// pays for chapter rows at all.
	needNovels, needChapters := agentAnalyticsReadsRelations(trimmedAgentQuery(query))
	sandboxPath, err := s.buildAgentSandbox(ctx, userID, needNovels, needChapters)
	if err != nil {
		// A sandbox that cannot be created is an environment problem, not a
		// bad query, and retrying the same SQL cannot fix it. Say so plainly:
		// the model stops trying this tool and answers from the other eleven
		// instead of looping on a query that will never run.
		return "", fmt.Errorf("library analytics is unavailable on this server (%w); answer from the other tools instead", err)
	}
	// RemoveAll, not Remove: it drops the per-query directory as well as the
	// file inside it. Removing only the file left an empty directory behind on
	// every single query.
	defer os.RemoveAll(filepath.Dir(sandboxPath))

	db, err := agentAnalyticsRO(sandboxPath)
	if err != nil {
		return "", fmt.Errorf("open analytics sandbox: %w", err)
	}
	defer db.Close()

	// Outer LIMIT: the model's own LIMIT/ORDER BY survive inside the
	// subquery; the wrapper only caps the payload. limit+1 probes for
	// truncation without claiming more rows than the caller asked for.
	wrapped := fmt.Sprintf("SELECT * FROM ( %s ) agent_result LIMIT %d", query, limit+1)

	conn, err := db.Conn(ctx)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	if err := setAgentAnalyticsLimits(ctx, conn); err != nil {
		return "", err
	}

	rows, err := conn.QueryContext(ctx, wrapped)
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
			// Report what was actually returned, not the row cap: telling the
			// model "truncated at 200 rows" when the payload cap cut it at 5
			// sends it off to re-run a query that was never the problem.
			result.Truncated = true
			result.Note = fmt.Sprintf("results cut to %d rows to stay under %d KB; project fewer columns or add filters",
				len(result.Rows), agentAnalyticsMaxResultBody>>10)
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

// agentSandboxDirName is the subdirectory of the server's data dir where the
// per-query sandboxes are created.
//
// It lives under the data dir rather than in os.TempDir() for two reasons, both
// of which are correctness issues on a real deployment:
//
//   - Termux has no /tmp. os.TempDir() falls back to /tmp on any GOOS=linux
//     build, and the Makefile's Termux target (linux-arm64) is exactly that, so
//     the runtime GOOS check for the /data/local/tmp branch never fires. On a
//     device the directory simply does not exist and os.MkdirTemp fails
//     outright, taking query_library down entirely. The data dir is already
//     resolved, already writable and already holds the database we attach.
//   - /tmp is usually tmpfs, so the sandbox would occupy RAM for the duration
//     of every query. The data dir is on the filesystem the server already
//     stores its library in, so the copy costs disk instead.
const agentSandboxDirName = "agent-sandbox"

// agentSandboxRoot returns the directory holding per-query sandboxes, creating
// it if needed.
//
// Kept separate from buildAgentSandbox so the resolution and its failure
// modes are testable in one place: every caller funnels through here.
func (s *Store) agentSandboxRoot() (string, error) {
	root := filepath.Join(s.App.DataDir(), agentSandboxDirName)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	return root, nil
}

// buildAgentSandbox materialises the two views for one owner into a fresh temp
// file and returns its path.
//
// The views are created by SQLite itself from an attached, read-only copy of
// data.db, so no row ever round-trips through Go. The attachment lives only
// inside this function: the builder handle is closed before the path is
// returned, so by the time the model can run a statement there is nothing left
// to name but the file — which contains two tables and no owner column.
//
// The owner id is a bound parameter, never interpolated into SQL text.
func (s *Store) buildAgentSandbox(ctx context.Context, ownerID string, needNovels, needChapters bool) (string, error) {
	root, err := s.agentSandboxRoot()
	if err != nil {
		return "", fmt.Errorf("create analytics sandbox directory: %w", err)
	}
	dir, err := os.MkdirTemp(root, "query-")
	if err != nil {
		return "", fmt.Errorf("create analytics sandbox directory: %w", err)
	}
	// The directory is ours to remove: the caller defers RemoveAll on the
	// returned file's parent, and this is the path it is removing. Until the
	// build succeeds nothing else owns it, so failures clean up here.
	path := filepath.Join(dir, "sandbox.db")
	built := false
	defer func() {
		if !built {
			os.RemoveAll(dir)
		}
	}()

	builder, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		return "", fmt.Errorf("open analytics sandbox: %w", err)
	}
	builder.SetMaxOpenConns(1)
	builder.SetMaxIdleConns(1)

	// Close before returning: the attachment does not outlive this handle.
	defer func() {
		if cerr := builder.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close analytics sandbox builder: %w", cerr)
		}
	}()

	// mode=ro: even inside this short window the source database cannot be
	// written, and the engine refuses any write through the attachment.
	attach := "file:" + filepath.Join(s.App.DataDir(), "data.db") + "?mode=ro"
	if _, err := builder.ExecContext(ctx, "ATTACH DATABASE ? AS src", attach); err != nil {
		return "", fmt.Errorf("attach library database: %w", err)
	}

	// Both tables are always created, so a query naming a view the caller did
	// not ask for still fails with "no such table" rather than a confusing
	// error; only the materialisation is conditional.
	for _, stmt := range []struct {
		name string
		sql  string
		need bool
	}{
		{AgentAnalyticsNovelView, agentNovelProgressCTAS, needNovels},
		{AgentAnalyticsChapterView, agentChapterOverviewCTAS, needChapters},
	} {
		if _, err := builder.ExecContext(ctx, "CREATE TABLE "+stmt.name+" AS "+stmt.sql, ownerID); err != nil {
			return "", fmt.Errorf("prepare agent sandbox: %w", err)
		}
		if !stmt.need {
			// Emptied so the view exists with the right shape but no rows,
			// which is what a novel-only question should see.
			if _, err := builder.ExecContext(ctx, "DELETE FROM "+stmt.name); err != nil {
				return "", fmt.Errorf("trim agent sandbox: %w", err)
			}
		}
	}

	built = true
	return path, nil
}

// agentNovelProgressCTAS / agentChapterOverviewCTAS materialise the two
// analytics surfaces. Columns and aggregate semantics mirror
// RecalculateNovelStats and the assistant's other read tools: only
// non-excluded chapters count, and pending is the complement of
// translated/refined/done. The WHERE clause is the ownership filter —
// server-written, bound as a parameter, and not something the model can
// influence.
const agentNovelProgressCTAS = `
	SELECT n.id AS novel_id,
		COALESCE(NULLIF(n.target_title, ''), n.source_title) AS title,
		COALESCE(NULLIF(n.target_author, ''), n.source_author) AS author,
		n.status AS status,
		n.source_language AS source_language,
		n.target_language AS target_language,
		n.is_public AS is_public,
		CASE WHEN TRIM(COALESCE(n.target_description, '')) <> '' THEN 1 ELSE 0 END AS has_description,
		COALESCE(SUM(CASE WHEN c.excluded = 0 THEN 1 ELSE 0 END), 0) AS total,
		COALESCE(SUM(CASE WHEN c.excluded = 0 AND c.status IN ('translated','refined','done') THEN 1 ELSE 0 END), 0) AS translated,
		COALESCE(SUM(CASE WHEN c.excluded = 0 AND c.status IN ('refined','done') THEN 1 ELSE 0 END), 0) AS completed,
		COALESCE(SUM(CASE WHEN c.excluded = 0 AND c.status NOT IN ('translated','refined','done') THEN 1 ELSE 0 END), 0) AS pending,
		COALESCE(SUM(CASE WHEN c.excluded = 0 THEN c.original_char_count ELSE 0 END), 0) AS original_chars,
		COALESCE(SUM(CASE WHEN c.excluded = 0 THEN c.translated_char_count ELSE 0 END), 0) AS translated_chars,
		COALESCE(SUM(CASE WHEN c.excluded = 0 THEN c.refined_char_count ELSE 0 END), 0) AS refined_chars,
		COALESCE(MAX(c.chapter_order), 0) AS max_chapter_order,
		n.updated AS updated
	FROM src.novels n
	LEFT JOIN src.chapters c ON c.novel = n.id
	WHERE n.owner = ?
	GROUP BY n.id`

const agentChapterOverviewCTAS = `
	SELECT c.novel AS novel_id,
		c.id AS chapter_id,
		c.chapter_order AS chapter_order,
		c.title AS title,
		c.translated_title AS translated_title,
		c.status AS status,
		c.excluded AS excluded,
		COALESCE(c.original_char_count, 0) AS original_chars,
		COALESCE(c.translated_char_count, 0) AS translated_chars,
		COALESCE(c.refined_char_count, 0) AS refined_chars,
		COALESCE(c.error_message, '') AS error_message,
		c.updated AS updated
	FROM src.chapters c
	JOIN src.novels n ON n.id = c.novel
	WHERE n.owner = ?`

// trimmedAgentQuery strips surrounding whitespace and a trailing semicolon,
// matching what validateAgentAnalyticsSQL accepts, so the relation scan below
// sees the same statement that will be executed.
func trimmedAgentQuery(query string) string {
	return strings.TrimSuffix(strings.TrimSpace(query), ";")
}

// agentAnalyticsReadsRelations reports which sandbox views the query names.
// A view nobody named stays empty, and the engine reports "no such table" —
// the same error it already produced for any other relation, so this adds no
// new failure mode while letting novel-only questions skip the chapter copy.
func agentAnalyticsReadsRelations(query string) (novels, chapters bool) {
	for _, rel := range agentAnalyticsRelation.FindAllStringSubmatch(query, -1) {
		switch strings.ToLower(rel[1]) {
		case AgentAnalyticsNovelView:
			novels = true
		case AgentAnalyticsChapterView:
			chapters = true
		}
	}
	return novels, chapters
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
	if utf8.RuneCountInString(s) > agentAnalyticsMaxCellChars {
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

package store

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase"

	"translator-server/internal/secure"
)

// agentAnalyticsTestStore boots a real PocketBase against a temp dir with the
// analytics views in place.
func agentAnalyticsTestStore(t *testing.T) (*Store, func()) {
	t.Helper()
	dataDir := t.TempDir()
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: dataDir})
	if err := app.Bootstrap(); err != nil {
		t.Fatalf("bootstrap pocketbase: %v", err)
	}
	encryptor, err := secure.NewEncryptorFromConfig("", dataDir+"/app.key")
	if err != nil {
		t.Fatalf("create encryptor: %v", err)
	}
	st := New(app, encryptor)
	if err := st.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	return st, func() { app.ResetBootstrapState() }
}

// seedAnalyticsLibrary creates two users, each owning a novel with chapters,
// so cross-user scoping is observable.
func seedAnalyticsLibrary(t *testing.T, st *Store) (aliceID, aliceNovelID, bobID, bobNovelID string) {
	t.Helper()
	alice, err := st.CreateUser("analytics-alice@example.com", "secret123", "Alice")
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	bob, err := st.CreateUser("analytics-bob@example.com", "secret123", "Bob")
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}

	aliceNovel := &Novel{SourceTitle: "Casi completa", TargetTitle: "Casi completa", Status: "ongoing", SourceLanguage: "en", TargetLanguage: "es"}
	if err := st.CreateNovel(alice.User.ID, aliceNovel); err != nil {
		t.Fatalf("create alice novel: %v", err)
	}
	// 12 chapters, 5 translated → pending 7 (< 10).
	for i := 1; i <= 12; i++ {
		status := "pending"
		if i <= 5 {
			status = "translated"
		}
		if _, err := st.UpsertChapter(alice.User.ID, aliceNovel.ID, &Chapter{
			ChapterOrder:    i,
			Title:           "Capítulo",
			Status:          status,
			OriginalContent: "contenido original",
		}); err != nil {
			t.Fatalf("upsert alice chapter %d: %v", i, err)
		}
	}

	bobNovel := &Novel{SourceTitle: "Ajena", TargetTitle: "Ajena", Status: "ongoing", SourceLanguage: "en", TargetLanguage: "es"}
	if err := st.CreateNovel(bob.User.ID, bobNovel); err != nil {
		t.Fatalf("create bob novel: %v", err)
	}
	for i := 1; i <= 2; i++ {
		if _, err := st.UpsertChapter(bob.User.ID, bobNovel.ID, &Chapter{
			ChapterOrder:    i,
			Title:           "Capítulo",
			Status:          "pending",
			OriginalContent: "contenido ajeno",
		}); err != nil {
			t.Fatalf("upsert bob chapter %d: %v", i, err)
		}
	}
	return alice.User.ID, aliceNovel.ID, bob.User.ID, bobNovel.ID
}

func TestAgentAnalyticsQueryScopedAndReadOnly(t *testing.T) {
	st, cleanup := agentAnalyticsTestStore(t)
	defer cleanup()
	aliceID, aliceNovelID, bobID, bobNovelID := seedAnalyticsLibrary(t, st)
	ctx := context.Background()

	// The motivating query: novels missing fewer than 10 chapters.
	out, err := st.RunAgentAnalyticsQuery(ctx, aliceID,
		"SELECT novel_id, title, pending FROM "+AgentAnalyticsNovelView+" WHERE pending < 10", 50)
	if err != nil {
		t.Fatalf("analytics query: %v", err)
	}
	aliceRows := parseAgentAnalyticsRows(t, out)
	if len(aliceRows) != 1 {
		t.Fatalf("expected exactly alice's novel, got %s", out)
	}
	// JSON numbers unmarshal as float64.
	if aliceRows[0]["novel_id"] != aliceNovelID || aliceRows[0]["pending"] != float64(7) {
		t.Fatalf("expected alice's novel with pending 7, got %v", aliceRows[0])
	}
	if strings.Contains(out, bobNovelID) || strings.Contains(out, "Ajena") {
		t.Fatalf("query leaked another user's novel: %s", out)
	}

	// Same query as bob returns only bob's rows.
	out, err = st.RunAgentAnalyticsQuery(ctx, bobID,
		"SELECT novel_id, title, pending FROM "+AgentAnalyticsNovelView+" WHERE pending < 10", 50)
	if err != nil {
		t.Fatalf("bob analytics query: %v", err)
	}
	if !strings.Contains(out, bobNovelID) || strings.Contains(out, aliceNovelID) {
		t.Fatalf("bob's query must not return alice's rows: %s", out)
	}

	// Chapter overview is scoped too.
	out, err = st.RunAgentAnalyticsQuery(ctx, aliceID,
		"SELECT chapter_id, status FROM "+AgentAnalyticsChapterView+" WHERE status = 'translated'", 50)
	if err != nil {
		t.Fatalf("chapter overview query: %v", err)
	}
	if got := strings.Count(out, `"translated"`); got != 5 {
		t.Fatalf("expected 5 translated chapters for alice, got %d in %s", got, out)
	}

	// Mutation attempts are rejected before reaching the engine.
	for _, query := range []string{
		"DELETE FROM " + AgentAnalyticsNovelView,
		"UPDATE " + AgentAnalyticsChapterView + " SET status = 'done'",
		"SELECT email FROM users",
		"SELECT * FROM novels",
		"SELECT 1",
		"SELECT 1; SELECT 2",
		"SELECT title FROM " + AgentAnalyticsNovelView + " -- drop table",
		"PRAGMA journal_mode",
	} {
		if _, err := st.RunAgentAnalyticsQuery(ctx, aliceID, query, 50); err == nil {
			t.Fatalf("query must be rejected: %q", query)
		}
	}

	// The views must be untouched after all the rejected attempts.
	out, err = st.RunAgentAnalyticsQuery(ctx, aliceID,
		"SELECT pending FROM "+AgentAnalyticsNovelView+" WHERE novel_id = '"+aliceNovelID+"'", 50)
	if err != nil {
		t.Fatalf("novel data must survive rejected attempts: %v", err)
	}
	rows := parseAgentAnalyticsRows(t, out)
	if len(rows) != 1 || rows[0]["pending"] != float64(7) {
		t.Fatalf("novel data must survive rejected attempts, got %v", rows)
	}
}

// TestValidateAgentAnalyticsSQL pins the only thing the validator polices:
// statement shape (one comment-free SELECT reading a sandbox view). It
// deliberately does not police which relations a query names — the sandbox
// holds only the caller's own rows, so a foreign relation fails at the engine.
// See TestAgentAnalyticsQueryRejectsForeignRelationsEndToEnd for that layer.
func TestValidateAgentAnalyticsSQL(t *testing.T) {
	allowed := []string{
		"SELECT novel_id, title, pending FROM v_agent_novel_progress WHERE pending < 10 ORDER BY pending",
		"SELECT count(*) FROM v_agent_chapter_overview WHERE status = 'pending'",
		"SELECT n.title, count(c.chapter_id) FROM v_agent_novel_progress n JOIN v_agent_chapter_overview c ON c.novel_id = n.novel_id GROUP BY n.title",
		// A literal that spells a forbidden word must not trip the check.
		"SELECT novel_id FROM v_agent_novel_progress WHERE title LIKE '%update%'",
		"SELECT novel_id FROM v_agent_novel_progress WHERE title = 'Create a novel'",
		// Reading through a subquery of a view is fine.
		"SELECT * FROM (SELECT * FROM v_agent_novel_progress) x",
		// CTEs: the trailing SELECT reads the CTE name, which resolves to the
		// views it was built from.
		"WITH low AS (SELECT novel_id FROM v_agent_novel_progress WHERE pending < 10) SELECT * FROM low",
		"WITH c AS (SELECT * FROM v_agent_chapter_overview) SELECT count(*) FROM c",
		// A quoted or backticked view name still counts as reading a view.
		`SELECT novel_id FROM "v_agent_novel_progress"`,
		"SELECT novel_id FROM `v_agent_novel_progress`",
	}
	for _, q := range allowed {
		if err := validateAgentAnalyticsSQL(q); err != nil {
			t.Errorf("expected query to be allowed, got %v: %s", err, q)
		}
	}

	rejected := []string{
		// Must read a sandbox view.
		"SELECT 1",
		"SELECT * FROM novels",
		"SELECT * FROM sqlite_master",
		`SELECT * FROM "sqlite_master"`,
		"SELECT * FROM users",
		// Statement/mutation surface.
		"SELECT novel_id FROM v_agent_novel_progress; DROP TABLE novels",
		"DELETE FROM v_agent_novel_progress",
		"INSERT INTO v_agent_novel_progress SELECT * FROM v_agent_novel_progress",
		"ATTACH DATABASE 'x' AS y",
		"SELECT novel_id FROM v_agent_novel_progress -- comment",
		"UPDATE novels SET title = 'x'",
		"PRAGMA journal_mode",
		"",
		"not a query",
	}
	for _, q := range rejected {
		if err := validateAgentAnalyticsSQL(q); err == nil {
			t.Errorf("expected query to be rejected, got nil: %s", q)
		}
	}
}

// TestAgentSandboxHoldsOnlyOwnerData is the structural guarantee behind the
// allowlist: the sandbox is a separate database that never contains users,
// superusers, sessions or any other user's novels, so even a query that
// slipped past validation would find nothing to read.
func TestAgentSandboxHoldsOnlyOwnerData(t *testing.T) {
	st, cleanup := agentAnalyticsTestStore(t)
	defer cleanup()
	aliceID, _, _, bobNovelID := seedAnalyticsLibrary(t, st)
	_ = bobNovelID
	ctx := context.Background()

	db, err := agentAnalyticsRO()
	if err != nil {
		t.Fatalf("open sandbox: %v", err)
	}
	defer db.Close()
	if err := st.populateAgentSandbox(ctx, db, aliceID, true, true); err != nil {
		t.Fatalf("populate sandbox: %v", err)
	}

	// Only the two documented relations exist in the sandbox.
	rows, err := db.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type IN ('table','view') ORDER BY name")
	if err != nil {
		t.Fatalf("list sandbox relations: %v", err)
	}
	defer rows.Close()
	names := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan relation name: %v", err)
		}
		names = append(names, name)
	}
	for _, name := range names {
		if name != AgentAnalyticsNovelView && name != AgentAnalyticsChapterView {
			t.Errorf("unexpected relation in sandbox: %q (all: %v)", name, names)
		}
	}

	// Alice's snapshot holds her novel and nothing of bob's.
	out, err := st.RunAgentAnalyticsQuery(ctx, aliceID, "SELECT novel_id FROM "+AgentAnalyticsNovelView, 50)
	if err != nil {
		t.Fatalf("alice query: %v", err)
	}
	if strings.Contains(out, bobNovelID) {
		t.Fatalf("sandbox snapshot must be alice-scoped, leaked bob's novel: %s", out)
	}
	if len(parseAgentAnalyticsRows(t, out)) != 1 {
		t.Fatalf("expected exactly alice's novel: %s", out)
	}
}

// TestAgentAnalyticsQueryRejectsForeignRelationsEndToEnd drives the payloads
// that used to escape the blocklist through the real entry point. They now
// pass validation — the validator only checks statement shape — and must fail
// because the sandbox has no such relation, reaching no data either way.
func TestAgentAnalyticsQueryRejectsForeignRelationsEndToEnd(t *testing.T) {
	st, cleanup := agentAnalyticsTestStore(t)
	defer cleanup()
	aliceID, _, _, _ := seedAnalyticsLibrary(t, st)
	ctx := context.Background()

	for _, query := range []string{
		"SELECT email, tokenKey FROM _superusers WHERE 'v_agent_'='v_agent_'",
		"SELECT (SELECT group_concat(messages) FROM agent_sessions) AS leak FROM " + AgentAnalyticsNovelView,
		"SELECT * FROM _authOrigins",
		"SELECT (SELECT group_concat(source_title) FROM novels) AS leak FROM " + AgentAnalyticsNovelView,
		"SELECT (SELECT group_concat(password) FROM _superusers) AS n FROM " + AgentAnalyticsNovelView,
		"SELECT * FROM sqlite_master",
		"SELECT * FROM users",
		// Regression: the legacy comma join is not a FROM/JOIN keyword, so an
		// allowlist built on those two missed it entirely. These base tables
		// have no counterpart in the sandbox, so the engine rejects them.
		"SELECT n.title, m.source_title FROM " + AgentAnalyticsNovelView + " n, novels m",
		"SELECT n.title, m.messages FROM " + AgentAnalyticsNovelView + " n, agent_sessions m",
		"SELECT n.title, m.email FROM " + AgentAnalyticsNovelView + " n, users m",
	} {
		if _, err := st.RunAgentAnalyticsQuery(ctx, aliceID, query, 50); err == nil {
			t.Errorf("query must be rejected: %q", query)
		}
	}
}

// TestAgentSandboxSchemaRevealsOnlyTheTwoViews pins what the model can learn
// from the sandbox's own sqlite_master. The comma join is reachable by design
// now that the allowlist is gone, so the guarantee it must uphold is that the
// schema it exposes describes the two generated views and nothing else — no
// application table, no other user's data, no base schema.
func TestAgentSandboxSchemaRevealsOnlyTheTwoViews(t *testing.T) {
	st, cleanup := agentAnalyticsTestStore(t)
	defer cleanup()
	aliceID, _, _, _ := seedAnalyticsLibrary(t, st)
	ctx := context.Background()

	// Every relation the sandbox knows about must be one of the two views.
	db, err := agentAnalyticsRO()
	if err != nil {
		t.Fatalf("open sandbox: %v", err)
	}
	defer db.Close()
	if err := st.populateAgentSandbox(ctx, db, aliceID, true, true); err != nil {
		t.Fatalf("populate sandbox: %v", err)
	}
	rows, err := db.QueryContext(ctx,
		"SELECT name FROM sqlite_master WHERE type IN ('table','view') ORDER BY name")
	if err != nil {
		t.Fatalf("list sandbox relations: %v", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if name != AgentAnalyticsNovelView && name != AgentAnalyticsChapterView {
			t.Errorf("sandbox exposes unexpected relation %q", name)
		}
		count++
	}
	if count != 2 {
		t.Errorf("expected exactly the 2 sandbox views, found %d", count)
	}
}

// TestAgentAnalyticsRunsConcurrentlyWithoutBlocking keeps the per-request
// sandbox honest: a slow query for one user must not stall another's. The
// previous shared handle serialized every analytics call behind a global mutex.
func TestAgentAnalyticsRunsConcurrentlyWithoutBlocking(t *testing.T) {
	st, cleanup := agentAnalyticsTestStore(t)
	defer cleanup()
	aliceID, _, _, _ := seedAnalyticsLibrary(t, st)

	// A cross join over the views forces real work, so overlap is observable.
	q := "SELECT count(*) FROM " + AgentAnalyticsNovelView + " a, " +
		AgentAnalyticsChapterView + " b WHERE a.novel_id = b.novel_id"

	const workers = 8
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func() {
			if _, err := st.RunAgentAnalyticsQuery(context.Background(), aliceID, q, 50); err != nil {
				errs <- err
				return
			}
			errs <- nil
		}()
	}
	for i := 0; i < workers; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent analytics query failed: %v", err)
		}
	}
}

// TestAgentAnalyticsTruncationStaysUnderLimit pins the row cap: the payload
// reports truncation without carrying the extra probe row.
func TestAgentAnalyticsTruncationStaysUnderLimit(t *testing.T) {
	st, cleanup := agentAnalyticsTestStore(t)
	defer cleanup()
	aliceID, _, _, _ := seedAnalyticsLibrary(t, st)

	// 12 chapters > limit 5: the payload must carry exactly 5 rows and say so.
	out, err := st.RunAgentAnalyticsQuery(context.Background(), aliceID,
		"SELECT chapter_id FROM "+AgentAnalyticsChapterView, 5)
	if err != nil {
		t.Fatalf("analytics query: %v", err)
	}
	var result struct {
		Rows      [][]any `json:"rows"`
		Truncated bool    `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("invalid payload: %v", err)
	}
	if len(result.Rows) != 5 {
		t.Fatalf("limit 5 must yield exactly 5 rows, got %d in %s", len(result.Rows), out)
	}
	if !result.Truncated {
		t.Fatalf("expected truncated flag, got %s", out)
	}

	// Under the cap: no truncation flag, every row present.
	out, err = st.RunAgentAnalyticsQuery(context.Background(), aliceID,
		"SELECT chapter_id FROM "+AgentAnalyticsChapterView, 50)
	if err != nil {
		t.Fatalf("analytics query: %v", err)
	}
	// A fresh struct: the omitted `truncated` field would keep the previous true.
	full := struct {
		Rows      [][]any `json:"rows"`
		Truncated bool    `json:"truncated"`
	}{}
	if err := json.Unmarshal([]byte(out), &full); err != nil {
		t.Fatalf("invalid payload: %v", err)
	}
	if len(full.Rows) != 12 || full.Truncated {
		t.Fatalf("expected 12 rows untruncated, got %d (truncated=%v) in %s", len(full.Rows), full.Truncated, out)
	}
}

// TestTruncateRunesKeepsMultibyteTextIntact guards the rune-safe cut shared by
// the analytics cells and the agent tool results.
func TestTruncateRunesKeepsMultibyteTextIntact(t *testing.T) {
	if got := TruncateRunes("áéíóúñ", 3); got != "áéí" {
		t.Errorf("expected 3 accented chars, got %q", got)
	}
	if got := TruncateRunes("第一章第二", 2); got != "第一" {
		t.Errorf("expected 2 CJK chars, got %q", got)
	}
	if got := TruncateRunes("abc", 10); got != "abc" {
		t.Errorf("short strings must pass through, got %q", got)
	}
	if got := TruncateRunes("abc", 0); got != "" {
		t.Errorf("zero budget must yield empty, got %q", got)
	}
}

// parseAgentAnalyticsRows converts the positional {columns, rows} payload
// into per-row maps for assertions.
func parseAgentAnalyticsRows(t *testing.T, payload string) []map[string]any {
	t.Helper()
	var result struct {
		Columns []string `json:"columns"`
		Rows    [][]any  `json:"rows"`
	}
	if err := json.Unmarshal([]byte(payload), &result); err != nil {
		t.Fatalf("invalid analytics payload %q: %v", payload, err)
	}
	rows := make([]map[string]any, 0, len(result.Rows))
	for _, row := range result.Rows {
		m := map[string]any{}
		for i, col := range result.Columns {
			m[col] = row[i]
		}
		rows = append(rows, m)
	}
	return rows
}

// TestValidateAgentAnalyticsSQLIgnoresDataInLiterals pins that the blocklist
// only ever sees SQL structure. Running it against the raw query made a novel
// titled "A -- B" (or any title containing ';', '--' or '/*') fail validation
// with a message about statement shape, which the model reads as "my SQL is
// malformed" and cannot act on. The data lives in string literals, which are
// blanked before the check runs.
func TestValidateAgentAnalyticsSQLIgnoresDataInLiterals(t *testing.T) {
	tolerated := []string{
		"SELECT * FROM " + AgentAnalyticsNovelView + " WHERE title LIKE '%A -- B%'",
		"SELECT * FROM " + AgentAnalyticsNovelView + " WHERE title LIKE '%; DROP TABLE%'",
		"SELECT * FROM " + AgentAnalyticsNovelView + " WHERE title LIKE '%/* x */%'",
		"SELECT * FROM " + AgentAnalyticsNovelView + " WHERE title = 'O''Brien -- update'",
		"SELECT * FROM " + AgentAnalyticsNovelView + " WHERE title LIKE '%\"quoted\" --%'",
		"SELECT * FROM " + AgentAnalyticsChapterView + " WHERE title LIKE '%-- INSERT INTO t%'",
	}
	for _, q := range tolerated {
		if err := validateAgentAnalyticsSQL(q); err != nil {
			t.Errorf("literal content should not trip validation:\n  query: %s\n  error: %v", q, err)
		}
	}

	// Structure violations must still be rejected.
	rejected := []string{
		"SELECT * FROM " + AgentAnalyticsNovelView + " WHERE 1=1; DROP TABLE x",
		"SELECT * FROM " + AgentAnalyticsNovelView + " -- comment",
		"DELETE FROM " + AgentAnalyticsNovelView,
		"SELECT * FROM users",
		"",
	}
	for _, q := range rejected {
		if err := validateAgentAnalyticsSQL(q); err == nil {
			t.Errorf("expected rejection, got none for: %s", q)
		}
	}
}

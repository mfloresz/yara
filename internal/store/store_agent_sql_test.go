package store

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

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
	// 12 chapters, 5 translated → chapters_pending 7 (< 10).
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
		"SELECT novel_id, title, chapters_pending FROM "+AgentAnalyticsNovelView+" WHERE chapters_pending < 10", 50)
	if err != nil {
		t.Fatalf("analytics query: %v", err)
	}
	aliceRows := parseAgentAnalyticsRows(t, out)
	if len(aliceRows) != 1 {
		t.Fatalf("expected exactly alice's novel, got %s", out)
	}
	// JSON numbers unmarshal as float64.
	if aliceRows[0]["novel_id"] != aliceNovelID || aliceRows[0]["chapters_pending"] != float64(7) {
		t.Fatalf("expected alice's novel with pending 7, got %v", aliceRows[0])
	}
	if strings.Contains(out, bobNovelID) || strings.Contains(out, "Ajena") {
		t.Fatalf("query leaked another user's novel: %s", out)
	}

	// Same query as bob returns only bob's rows.
	out, err = st.RunAgentAnalyticsQuery(ctx, bobID,
		"SELECT novel_id, title, chapters_pending FROM "+AgentAnalyticsNovelView+" WHERE chapters_pending < 10", 50)
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
		"SELECT chapters_pending FROM "+AgentAnalyticsNovelView+" WHERE novel_id = '"+aliceNovelID+"'", 50)
	if err != nil {
		t.Fatalf("novel data must survive rejected attempts: %v", err)
	}
	rows := parseAgentAnalyticsRows(t, out)
	if len(rows) != 1 || rows[0]["chapters_pending"] != float64(7) {
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
		"SELECT novel_id, title, chapters_pending FROM v_agent_novel_progress WHERE chapters_pending < 10 ORDER BY chapters_pending",
		"SELECT count(*) FROM v_agent_chapter_overview WHERE status = 'pending'",
		"SELECT n.title, count(c.chapter_id) FROM v_agent_novel_progress n JOIN v_agent_chapter_overview c ON c.novel_id = n.novel_id GROUP BY n.title",
		// A literal that spells a forbidden word must not trip the check.
		"SELECT novel_id FROM v_agent_novel_progress WHERE title LIKE '%update%'",
		"SELECT novel_id FROM v_agent_novel_progress WHERE title = 'Create a novel'",
		// Reading through a subquery of a view is fine.
		"SELECT * FROM (SELECT * FROM v_agent_novel_progress) x",
		// CTEs: the trailing SELECT reads the CTE name, which resolves to the
		// views it was built from.
		"WITH low AS (SELECT novel_id FROM v_agent_novel_progress WHERE chapters_pending < 10) SELECT * FROM low",
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

	db, err := agentAnalyticsRO(st.buildAgentSandboxForTest(t, aliceID))
	if err != nil {
		t.Fatalf("open sandbox: %v", err)
	}
	defer db.Close()

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
	db, err := agentAnalyticsRO(st.buildAgentSandboxForTest(t, aliceID))
	if err != nil {
		t.Fatalf("open sandbox: %v", err)
	}
	defer db.Close()
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

// TestAgentAnalyticsAcceptsTrailingSemicolon pins that the accepted trailing
// ';' survives execution. The validator trims it and the executor used to wrap
// the raw query, so the semicolon landed inside the subquery and a query the
// validator approved died with a syntax error. LLMs append ';' constantly.
func TestAgentAnalyticsAcceptsTrailingSemicolon(t *testing.T) {
	st, cleanup := agentAnalyticsTestStore(t)
	defer cleanup()
	aliceID, _, _, _ := seedAnalyticsLibrary(t, st)

	out, err := st.RunAgentAnalyticsQuery(context.Background(), aliceID,
		"SELECT novel_id, chapters_pending FROM "+AgentAnalyticsNovelView+";", 50)
	if err != nil {
		t.Fatalf("a trailing semicolon must execute, not fail: %v", err)
	}
	rows := parseAgentAnalyticsRows(t, out)
	if len(rows) != 1 || rows[0]["chapters_pending"] != float64(7) {
		t.Fatalf("expected alice's novel with pending 7, got %s", out)
	}
}

// TestAgentAnalyticsCommaJoinSeesBothViews pins the silent-empty-result bug:
// a comma join spans both views, but FROM/JOIN scanning only saw the first,
// so the chapter view was materialised and then emptied — and the query
// returned "no rows matched" as if it were the truth. Alice has 12 chapters;
// the join must count them, not zero.
func TestAgentAnalyticsCommaJoinSeesBothViews(t *testing.T) {
	st, cleanup := agentAnalyticsTestStore(t)
	defer cleanup()
	aliceID, _, _, _ := seedAnalyticsLibrary(t, st)

	out, err := st.RunAgentAnalyticsQuery(context.Background(), aliceID,
		"SELECT count(*) AS chapters FROM "+AgentAnalyticsNovelView+
			" n, "+AgentAnalyticsChapterView+" c WHERE c.novel_id = n.novel_id", 50)
	if err != nil {
		t.Fatalf("comma join across both views: %v", err)
	}
	rows := parseAgentAnalyticsRows(t, out)
	if len(rows) != 1 || rows[0]["chapters"] != float64(12) {
		t.Fatalf("expected 12 chapters from the comma join, got %s", out)
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

// buildAgentSandboxForTest materialises a sandbox and removes it when the test
// ends, so tests that need to inspect the sandbox file directly can do so
// without leaking directories.
func (s *Store) buildAgentSandboxForTest(t *testing.T, ownerID string) string {
	t.Helper()
	path, err := s.buildAgentSandbox(context.Background(), ownerID)
	if err != nil {
		t.Fatalf("build agent sandbox: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(filepath.Dir(path)) })
	return path
}

// TestAgentAnalyticsByteCapReportsTheRealRowCount pins that the payload cap
// tells the model how many rows it actually got.
//
// The byte cap used to reuse the row cap's message verbatim, so a result cut
// at 5 rows by the payload limit was reported as "truncated at 200 rows". The
// model is told to refine and re-query on truncation, so a wrong count sends
// it after a filter that was never the problem.
func TestAgentAnalyticsByteCapReportsTheRealRowCount(t *testing.T) {
	st, cleanup := agentAnalyticsTestStore(t)
	defer cleanup()
	aliceID, _, _, _ := seedAnalyticsLibrary(t, st)

	// Pad every title so 12 rows comfortably exceed the 64 KB payload cap
	// while staying under the 200-row cap.
	if _, err := st.App.DB().NewQuery(
		"UPDATE chapters SET title = 'x' || substr(hex(zeroblob(0)), 1, 0) || printf('%.*c', 9000, 'ñ')",
	).Execute(); err != nil {
		t.Fatalf("pad chapter titles: %v", err)
	}

	out, err := st.RunAgentAnalyticsQuery(context.Background(), aliceID,
		"SELECT chapter_id, title FROM "+AgentAnalyticsChapterView, 200)
	if err != nil {
		t.Fatalf("analytics query: %v", err)
	}
	var result struct {
		Rows      [][]any `json:"rows"`
		Truncated bool    `json:"truncated"`
		Note      string  `json:"note"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("invalid payload: %v", err)
	}
	if !result.Truncated {
		t.Fatalf("expected the payload cap to trip on padded titles, got %s", out)
	}
	if result.Note == "" {
		t.Fatal("expected an explanatory note when truncating")
	}
	// The note must name the row count actually returned, not the row cap.
	if want := strconv.Itoa(len(result.Rows)); !strings.Contains(result.Note, want) {
		t.Errorf("note = %q, want it to report the %d rows actually returned", result.Note, len(result.Rows))
	}
	if strings.Contains(result.Note, "200 rows") {
		t.Errorf("note = %q, must not claim the row cap (200) tripped when the payload cap did", result.Note)
	}
	if len(result.Rows) == 0 {
		t.Error("a truncated result should still carry the rows it did return")
	}
}

// TestAgentAnalyticsBoundsEngineSideAllocation pins that a validated query
// cannot make SQLite build an arbitrarily large value in engine memory.
//
// The payload limits (rows, cell chars, total bytes) are applied in Go after
// rows.Next()/Scan() has already materialised the cell, so on their own they
// bound nothing at the engine: a four-byte novel title is enough to write
// `SELECT printf('%.'||20000000||'d',1)`, which allocated ~1.1GB in ~3s and
// then arrived as a 400-rune truncated cell. SQLITE_LIMIT_LENGTH is the only
// thing that stops it, and it is a C API (sqlite3_limit), not a PRAGMA — it
// has to be set on the connection that runs the statement.
func TestAgentAnalyticsBoundsEngineSideAllocation(t *testing.T) {
	st, cleanup := agentAnalyticsTestStore(t)
	defer cleanup()
	aliceID, _, _, _ := seedAnalyticsLibrary(t, st)

	for _, q := range []string{
		"SELECT printf('%.'||20000000||'d',1) FROM " + AgentAnalyticsNovelView,
		"SELECT zeroblob(200000000) FROM " + AgentAnalyticsNovelView,
		"SELECT replace(hex(zeroblob(10000000)),'0','AAAA') FROM " + AgentAnalyticsNovelView,
	} {
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)

		begin := time.Now()
		_, err := st.RunAgentAnalyticsQuery(context.Background(), aliceID, q, 10)
		elapsed := time.Since(begin)
		runtime.ReadMemStats(&after)
		allocated := after.TotalAlloc - before.TotalAlloc

		// The engine must refuse the value, not build it and hand Go a
		// truncatable copy.
		if err == nil {
			t.Errorf("query %q should have been rejected by the engine limit", q)
		}
		const budget = 32 << 20
		if allocated > budget {
			t.Errorf("query %q allocated %d MB; the engine-level cell limit is not applied", q, allocated>>20)
		}
		if elapsed > 2*time.Second {
			t.Errorf("query %q took %s; the engine-level cell limit is not applied", q, elapsed)
		}
	}
}

// TestAgentAnalyticsEngineLimitDoesNotBreakRealQueries is the other half: the
// limit bounds pathological values without rejecting the aggregates the
// assistant actually runs.
func TestAgentAnalyticsEngineLimitDoesNotBreakRealQueries(t *testing.T) {
	st, cleanup := agentAnalyticsTestStore(t)
	defer cleanup()
	aliceID, aliceNovelID, _, _ := seedAnalyticsLibrary(t, st)

	for _, q := range []string{
		"SELECT title, chapters_total, chapters_pending FROM " + AgentAnalyticsNovelView + " WHERE chapters_pending < 10",
		"SELECT COUNT(*) FROM " + AgentAnalyticsChapterView,
		"SELECT novel_id, status, COUNT(*) FROM " + AgentAnalyticsChapterView + " GROUP BY novel_id, status",
		"SELECT novel_id, MAX(LENGTH(title)) FROM " + AgentAnalyticsChapterView + " GROUP BY novel_id",
		"SELECT SUM(original_chars) FROM " + AgentAnalyticsChapterView + " WHERE novel_id = '" + aliceNovelID + "'",
	} {
		out, err := st.RunAgentAnalyticsQuery(context.Background(), aliceID, q, 50)
		if err != nil {
			t.Errorf("query %q failed under the engine limits: %v", q, err)
			continue
		}
		if strings.Contains(out, "too big") {
			t.Errorf("query %q was rejected by the engine limit but is a legitimate aggregate", q)
		}
	}
}

// parseAgentAnalyticsRows unpacks an analytics payload into row maps.
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

// TestAgentSandboxDoesNotDependOnTmpDir is the Termux regression.
//
// The sandbox used to be created with os.MkdirTemp("", ...), which resolves
// through os.TempDir(). On any GOOS=linux build that falls back to /tmp, and
// the Makefile's Termux target (linux-arm64) is exactly that, so the
// runtime.GOOS == "android" branch that would pick /data/local/tmp never
// fires. On a real Android device /tmp does not exist, os.MkdirTemp fails
// outright, and query_library stops working on the project's primary mobile
// target.
//
// The sandbox must therefore live under the server's data dir: already
// resolved, already writable, and already holding the database we attach.
// The test points TMPDIR at a path that does not exist — the exact shape of
// the Termux failure — and requires the query to still work.
func TestAgentSandboxDoesNotDependOnTmpDir(t *testing.T) {
	st, cleanup := agentAnalyticsTestStore(t)
	defer cleanup()
	aliceID, aliceNovelID, _, _ := seedAnalyticsLibrary(t, st)

	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "does-not-exist"))

	out, err := st.RunAgentAnalyticsQuery(context.Background(), aliceID,
		"SELECT novel_id, chapters_total FROM "+AgentAnalyticsNovelView, 50)
	if err != nil {
		t.Fatalf("analytics must not depend on TMPDIR: %v", err)
	}
	rows := parseAgentAnalyticsRows(t, out)
	if len(rows) != 1 {
		t.Fatalf("expected alice's single novel, got %d rows: %s", len(rows), out)
	}
	if rows[0]["novel_id"] != aliceNovelID {
		t.Errorf("novel_id = %v, want %s", rows[0]["novel_id"], aliceNovelID)
	}
}

// TestAgentSandboxLivesUnderDataDir pins where the sandbox is actually created:
// inside the data dir, not in the system temp dir.
func TestAgentSandboxLivesUnderDataDir(t *testing.T) {
	st, cleanup := agentAnalyticsTestStore(t)
	defer cleanup()
	aliceID, _, _, _ := seedAnalyticsLibrary(t, st)

	path := st.buildAgentSandboxForTest(t, aliceID)
	dataDir, err := filepath.Abs(st.App.DataDir())
	if err != nil {
		t.Fatalf("abs data dir: %v", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("abs sandbox path: %v", err)
	}
	if !strings.HasPrefix(abs, dataDir+string(filepath.Separator)) {
		t.Errorf("sandbox at %s is outside the data dir %s; it must not use the system temp dir", abs, dataDir)
	}
	if _, err := os.Stat(filepath.Join(dataDir, agentSandboxDirName)); err != nil {
		t.Errorf("sandbox root dir missing: %v", err)
	}
}

// TestAgentSandboxLeavesNothingBehind pins the cleanup. An earlier version
// removed only the sandbox file, so every single query left an empty
// directory behind — 171 of them accumulated in tmpfs during a test run, which
// is RAM on most Linux systems.
func TestAgentSandboxLeavesNothingBehind(t *testing.T) {
	st, cleanup := agentAnalyticsTestStore(t)
	defer cleanup()
	aliceID, _, _, _ := seedAnalyticsLibrary(t, st)

	const queries = 5
	for i := 0; i < queries; i++ {
		if _, err := st.RunAgentAnalyticsQuery(context.Background(), aliceID,
			"SELECT novel_id, status, COUNT(*) FROM "+AgentAnalyticsChapterView+" GROUP BY novel_id, status", 50); err != nil {
			t.Fatalf("query %d: %v", i, err)
		}
	}
	root := filepath.Join(st.App.DataDir(), agentSandboxDirName)
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read sandbox root: %v", err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("%d sandbox directories left after %d queries (want 0): %v", len(entries), queries, names)
	}
}

// TestAgentSandboxFailureIsModelActionable pins the degradation path: when the
// sandbox cannot be created the tool must say analytics is unavailable and
// point at the alternatives, so the model stops retrying the same SQL instead
// of looping. The turn itself must not fail.
func TestAgentSandboxFailureIsModelActionable(t *testing.T) {
	st, cleanup := agentAnalyticsTestStore(t)
	defer cleanup()
	aliceID, _, _, _ := seedAnalyticsLibrary(t, st)

	// Make the sandbox root impossible to create: a regular file where the
	// directory needs to be.
	root := filepath.Join(st.App.DataDir(), agentSandboxDirName)
	if err := os.WriteFile(root, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("block sandbox root: %v", err)
	}
	t.Cleanup(func() { os.Remove(root) })

	_, err := st.RunAgentAnalyticsQuery(context.Background(), aliceID,
		"SELECT novel_id FROM "+AgentAnalyticsNovelView, 50)
	if err == nil {
		t.Fatal("expected an error when the sandbox cannot be created")
	}
	msg := err.Error()
	for _, want := range []string{"unavailable", agentSandboxDirName} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q should mention %q so the model can act on it", msg, want)
		}
	}
}

// TestAgentAnalyticsDescriptionFlags pins the semantics the model relies on
// when filtering by description presence: has_target_description covers the
// user-facing description, has_source_description the original-language one,
// and each is evaluated independently. The old single has_description column
// read as covering both sides, and the assistant answered the wrong question
// ("novels with no description at all") from target-only data.
func TestAgentAnalyticsDescriptionFlags(t *testing.T) {
	st, cleanup := agentAnalyticsTestStore(t)
	defer cleanup()
	aliceID, aliceNovelID, _, _ := seedAnalyticsLibrary(t, st)

	// SeedAnalyticsLibrary creates novels with no description on either side;
	// give alice's novel a target-only description: expect 1/0.
	if _, err := st.UpdateNovel(aliceID, aliceNovelID, map[string]any{"targetDescription": "Descripción de prueba"}); err != nil {
		t.Fatalf("set target description: %v", err)
	}

	out, err := st.RunAgentAnalyticsQuery(context.Background(), aliceID,
		"SELECT has_target_description, has_source_description FROM "+AgentAnalyticsNovelView+
			" WHERE novel_id = '"+aliceNovelID+"'", 50)
	if err != nil {
		t.Fatalf("description flags query: %v", err)
	}
	rows := parseAgentAnalyticsRows(t, out)
	if len(rows) != 1 {
		t.Fatalf("expected exactly alice's novel, got %s", out)
	}
	if rows[0]["has_target_description"] != float64(1) || rows[0]["has_source_description"] != float64(0) {
		t.Fatalf("target-only description must read 1/0, got %v", rows[0])
	}
}

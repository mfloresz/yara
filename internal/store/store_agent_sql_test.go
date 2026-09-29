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

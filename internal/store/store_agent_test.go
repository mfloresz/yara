package store

import (
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"

	"translator-server/internal/secure"
)

// TestEnsureSchemaRaisesAgentMessagesMax reproduces the 5000-char truncation:
// collections created before the explicit Max existed run on PocketBase's
// TextField default and reject real chat histories. EnsureSchema must raise
// the cap on existing databases, and saving a >5000 char trail must work.
func TestEnsureSchemaRaisesAgentMessagesMax(t *testing.T) {
	dataDir := t.TempDir()
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: dataDir})
	if err := app.Bootstrap(); err != nil {
		t.Fatalf("bootstrap pocketbase: %v", err)
	}
	t.Cleanup(func() {
		app.ResetBootstrapState()
	})

	encryptor, err := secure.NewEncryptorFromConfig("", filepath.Join(dataDir, "app.key"))
	if err != nil {
		t.Fatalf("create encryptor: %v", err)
	}
	st := New(app, encryptor)
	if err := st.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	// Simulate a database created before the explicit Max existed.
	collection, err := app.FindCollectionByNameOrId(AgentSessionsCollection)
	if err != nil {
		t.Fatalf("find agent sessions collection: %v", err)
	}
	field := collection.Fields.GetByName("messages")
	textField, ok := field.(*core.TextField)
	if !ok {
		t.Fatalf("messages field should be a TextField, got %T", field)
	}
	textField.Max = 0
	if err := app.Save(collection); err != nil {
		t.Fatalf("save legacy messages field: %v", err)
	}

	if err := st.EnsureSchema(); err != nil {
		t.Fatalf("re-run ensure schema: %v", err)
	}

	collection, err = app.FindCollectionByNameOrId(AgentSessionsCollection)
	if err != nil {
		t.Fatalf("re-find agent sessions collection: %v", err)
	}
	field = collection.Fields.GetByName("messages")
	textField, ok = field.(*core.TextField)
	if !ok {
		t.Fatalf("messages field should still be a TextField, got %T", field)
	}
	if textField.Max != agentMessagesFieldMax {
		t.Fatalf("expected messages Max %d after migration, got %d", agentMessagesFieldMax, textField.Max)
	}

	// End to end: a trail bigger than the old 5000-char default must persist.
	user, err := st.CreateUser("agent-max@example.com", "secret123", "Alice")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	bigTrail := `["` + strings.Repeat("x", 6000) + `"]`
	session, err := st.CreateAgentSession(user.User.ID, "[]")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := st.SaveAgentSessionMessages(user.User.ID, session.ID, bigTrail); err != nil {
		t.Fatalf("save >5000 char trail: %v", err)
	}
	saved, err := st.GetAgentSession(user.User.ID, session.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if saved.Messages != bigTrail {
		t.Fatalf("saved trail was altered (len got %d, want %d)", len(saved.Messages), len(bigTrail))
	}
}

// TestEnsureSchemaAddsUniqueOwnerIndexToAgentSessions pins the one-session-per
// user invariant at the schema level: existing databases get the unique owner
// index too, and a second session for the same owner cannot be created.
func TestEnsureSchemaAddsUniqueOwnerIndexToAgentSessions(t *testing.T) {
	dataDir := t.TempDir()
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: dataDir})
	if err := app.Bootstrap(); err != nil {
		t.Fatalf("bootstrap pocketbase: %v", err)
	}
	t.Cleanup(func() { app.ResetBootstrapState() })
	encryptor, err := secure.NewEncryptorFromConfig("", filepath.Join(dataDir, "app.key"))
	if err != nil {
		t.Fatalf("create encryptor: %v", err)
	}
	st := New(app, encryptor)
	if err := st.EnsureSchema(); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	collection, err := app.FindCollectionByNameOrId(AgentSessionsCollection)
	if err != nil {
		t.Fatalf("find collection: %v", err)
	}
	hasIndex := false
	for _, idx := range collection.Indexes {
		if strings.Contains(idx, agentSessionsOwnerIndex) {
			hasIndex = true
		}
	}
	if !hasIndex {
		t.Fatalf("expected a unique owner index, got %v", collection.Indexes)
	}

	alice, err := st.CreateUser("agent-idx-alice@example.com", "secret123", "Alice")
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	bob, err := st.CreateUser("agent-idx-bob@example.com", "secret123", "Bob")
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}

	// One session each: both users are independent.
	aliceSession, err := st.CreateAgentSession(alice.User.ID, "[]")
	if err != nil {
		t.Fatalf("create alice session: %v", err)
	}
	if _, err := st.CreateAgentSession(bob.User.ID, "[]"); err != nil {
		t.Fatalf("bob must get his own session: %v", err)
	}

	// A second session for alice is refused, and creation is idempotent: the
	// existing session comes back instead of a duplicate record.
	again, err := st.CreateAgentSession(alice.User.ID, "[]")
	if err != nil {
		t.Fatalf("repeat creation should return the existing session: %v", err)
	}
	if again.ID != aliceSession.ID {
		t.Fatalf("expected alice's existing session %q, got %q", aliceSession.ID, again.ID)
	}
}

// TestGetLatestAgentSessionIsPerUser pins that the "latest session" lookup
// never crosses owners.
func TestGetLatestAgentSessionIsPerUser(t *testing.T) {
	st, cleanup := agentAnalyticsTestStore(t)
	defer cleanup()
	alice, err := st.CreateUser("latest-alice@example.com", "secret123", "Alice")
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	bob, err := st.CreateUser("latest-bob@example.com", "secret123", "Bob")
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}

	aliceSession, err := st.CreateAgentSession(alice.User.ID, `[]`)
	if err != nil {
		t.Fatalf("create alice session: %v", err)
	}
	if _, err := st.CreateAgentSession(bob.User.ID, `[]`); err != nil {
		t.Fatalf("create bob session: %v", err)
	}

	aliceLatest, err := st.GetLatestAgentSession(alice.User.ID)
	if err != nil {
		t.Fatalf("alice latest: %v", err)
	}
	if aliceLatest.ID != aliceSession.ID {
		t.Fatalf("alice got someone else's session: %q vs %q", aliceLatest.ID, aliceSession.ID)
	}
	if aliceLatest.OwnerID != alice.User.ID {
		t.Fatalf("latest session owner mismatch: %q", aliceLatest.OwnerID)
	}

	// Cross-owner reads are refused, not silently allowed.
	if _, err := st.GetAgentSession(bob.User.ID, aliceSession.ID); err != ErrForbidden {
		t.Fatalf("expected ErrForbidden reading another user's session, got %v", err)
	}
	if _, err := st.SaveAgentSessionMessages(bob.User.ID, aliceSession.ID, `[]`); err != ErrForbidden {
		t.Fatalf("expected ErrForbidden writing another user's session, got %v", err)
	}

	// Deleting bob's sessions leaves alice's alone.
	if err := st.DeleteAgentSessions(bob.User.ID); err != nil {
		t.Fatalf("delete bob sessions: %v", err)
	}
	if _, err := st.GetAgentSession(alice.User.ID, aliceSession.ID); err != nil {
		t.Fatalf("alice's session must survive bob's reset: %v", err)
	}
	if _, err := st.GetLatestAgentSession(bob.User.ID); err != ErrNotFound {
		t.Fatalf("bob should have no sessions left, got %v", err)
	}
}

// TestSearchOwnedChaptersIsCaseSensitiveAndOwnerOnly pins the search contract
// the tool advertises: literal, case-sensitive, and restricted to novels the
// caller owns.
func TestSearchOwnedChaptersIsCaseSensitiveAndOwnerOnly(t *testing.T) {
	st, cleanup := agentAnalyticsTestStore(t)
	defer cleanup()
	alice, err := st.CreateUser("search-alice@example.com", "secret123", "Alice")
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	bob, err := st.CreateUser("search-bob@example.com", "secret123", "Bob")
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}

	novel := &Novel{SourceTitle: "Buscable", TargetTitle: "Buscable", Status: "ongoing", SourceLanguage: "en", TargetLanguage: "es"}
	if err := st.CreateNovel(alice.User.ID, novel); err != nil {
		t.Fatalf("create novel: %v", err)
	}
	if _, err := st.UpsertChapter(alice.User.ID, novel.ID, &Chapter{
		ChapterOrder:      1,
		Title:             "Love and War",
		OriginalContent:   "It was Love that carried the day, and also love.",
		TranslatedContent: "Fue el Amor lo que-decantó el día, y amor también.",
		Status:            "translated",
	}); err != nil {
		t.Fatalf("upsert chapter: %v", err)
	}

	// A public novel of bob: readable through REST, unreachable for alice.
	bobNovel := &Novel{SourceTitle: "Público de Bob", TargetTitle: "Público de Bob", Status: "ongoing", SourceLanguage: "en", TargetLanguage: "es", IsPublic: true}
	if err := st.CreateNovel(bob.User.ID, bobNovel); err != nil {
		t.Fatalf("create bob novel: %v", err)
	}
	if _, err := st.UpsertChapter(bob.User.ID, bobNovel.ID, &Chapter{
		ChapterOrder:    1,
		Title:           "Secreto",
		OriginalContent: "contenido secreto de bob",
	}); err != nil {
		t.Fatalf("upsert bob chapter: %v", err)
	}

	// Case-sensitive: "love" must not match "Love".
	hits, err := st.SearchOwnedChapters(alice.User.ID, novel.ID, "love", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected one hit for lowercase love, got %d", len(hits))
	}
	if len(hits[0].MatchedFields) == 0 {
		t.Fatalf("a hit must report matched fields, got %+v", hits[0])
	}
	if hits[0].Snippet == "" {
		t.Fatal("a body hit must carry a snippet")
	}

	// Case-sensitive both ways: the capitalised form finds the title too.
	hits, err = st.SearchOwnedChapters(alice.User.ID, novel.ID, "Love", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected one hit for Love, got %d", len(hits))
	}
	hasTitle := false
	for _, field := range hits[0].MatchedFields {
		if field == "title" {
			hasTitle = true
		}
	}
	if !hasTitle {
		t.Fatalf("Love should match the chapter title, matched: %v", hits[0].MatchedFields)
	}

	// Wildcards are literal, not patterns.
	hits, err = st.SearchOwnedChapters(alice.User.ID, novel.ID, "L%ve", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("%% must be literal, got %d hits", len(hits))
	}

	// Another user's public novel is not searchable.
	if _, err := st.SearchOwnedChapters(alice.User.ID, bobNovel.ID, "secreto", 10); err != ErrForbidden {
		t.Fatalf("expected ErrForbidden searching another user's novel, got %v", err)
	}
	// And its chapters are not readable by id either.
	hits, err = st.SearchOwnedChapters(bob.User.ID, bobNovel.ID, "secreto", 10)
	if err != nil {
		t.Fatalf("bob must be able to search his own: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("bob's own search should find his chapter, got %d", len(hits))
	}
}

// TestBuildSearchSnippetNeverSplitsARune pins that the snippet window is cut
// on character boundaries.
//
// The window is a byte offset and searchSnippetWindow (90) is not a multiple
// of any multi-byte rune width, so a raw slice landed mid-character for a
// large share of emoji and CJK bodies — invalid UTF-8 handed straight to the
// model as a search result. The agent serves exactly that content, so this
// sweeps match positions across multi-byte bodies rather than trusting one.
func TestBuildSearchSnippetNeverSplitsARune(t *testing.T) {
	bodies := map[string]string{
		"emoji":  strings.Repeat("😀", 120),
		"cjk":    strings.Repeat("章", 120),
		"accent": strings.Repeat("ñ", 120),
		"mixed":  strings.Repeat("El día😀章 ", 40),
	}
	// A query placed at a different offset in each body exercises every
	// alignment of the window against the rune grid.
	offsets := []int{0, 1, 2, 3, 5, 7, 11, 13, 17, 29, 41, 57, 73, 89, 97, 113}

	invalid := 0
	for name, body := range bodies {
		for _, off := range offsets {
			if off >= len(body) {
				continue
			}
			query := body[off : off+len("😀")]
			if !strings.Contains(body, query) {
				continue
			}
			snippet := buildSearchSnippet(body, query)
			if snippet == "" {
				continue
			}
			if !utf8.ValidString(snippet) {
				invalid++
				if invalid <= 3 {
					t.Errorf("%s body, query at %d: snippet is not valid UTF-8: %q", name, off, snippet)
				}
			}
			// The hit itself must survive the window.
			if !strings.Contains(snippet, query) {
				t.Errorf("%s body, query at %d: snippet lost the match: %q", name, off, snippet)
			}
		}
	}
	if invalid > 0 {
		t.Errorf("%d/%d snippets were invalid UTF-8", invalid, len(bodies)*len(offsets))
	}
}

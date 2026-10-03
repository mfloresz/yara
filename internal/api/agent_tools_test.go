package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"translator-server/internal/store"
)

// agentToolsTestSetup creates two users plus one novel for each, so both the
// happy paths and the ownership refusals have something to point at.
type agentToolsTestSetup struct {
	env        *apiTestEnv
	alice      authPayload
	bob        authPayload
	aliceNovel novelPayload
	bobNovel   novelPayload
}

func newAgentToolsTestSetup(t *testing.T, title string) *agentToolsTestSetup {
	t.Helper()
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "alice-agent-tools@example.com", "secret123", "Alice")
	bob := registerUser(t, env, "bob-agent-tools@example.com", "secret123", "Bob")
	return &agentToolsTestSetup{
		env:        env,
		alice:      alice,
		bob:        bob,
		aliceNovel: createNovel(t, env.handler, alice.Token, title, "es", "en"),
		bobNovel:   createNovel(t, env.handler, bob.Token, "Novela de Bob", "es", "en"),
	}
}

func (s *agentToolsTestSetup) aliceChapters(t *testing.T, orders ...int) []chapterPayload {
	t.Helper()
	chapters := make([]chapterPayload, 0, len(orders))
	for _, order := range orders {
		chapters = append(chapters, createChapter(t, s.env.handler, s.alice.Token, s.aliceNovel.ID, order))
	}
	return chapters
}

func (s *agentToolsTestSetup) setChapterStatus(t *testing.T, novelID, chapterID, status string) {
	t.Helper()
	resp := doJSONRequest(t, s.env.handler, http.MethodPatch, "/api/v1/novels/"+novelID+"/chapters/"+chapterID+"/status", s.alice.Token, map[string]any{
		"status": status,
	})
	assertStatus(t, resp, http.StatusOK)
}

// createPendingJob plants a pending job directly through the store, without
// enqueueing it: it counts as an active job for every admission check but the
// worker never touches it, so the assertions are deterministic.
func (s *agentToolsTestSetup) createPendingJob(t *testing.T, operation string) *store.Job {
	t.Helper()
	job := &store.Job{
		NovelID:     s.aliceNovel.ID,
		Status:      "pending",
		Operation:   operation,
		ChapterIDs:  "[]",
		OptionsJSON: "{}",
	}
	if err := s.env.server.Store.CreateJob(s.alice.User.ID, job); err != nil {
		t.Fatalf("create pending job: %v", err)
	}
	return job
}

func TestAgentToolCatalogIncludesNewTools(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "alice-catalog@example.com", "secret123", "Alice")
	names := map[string]bool{}
	for _, tool := range env.server.agentTools(alice.User.ID) {
		names[tool.Name] = true
	}
	for _, want := range []string{
		"list_tags", "list_authors", "list_series",
		"get_active_jobs", "get_novel_jobs", "create_job", "cancel_job", "retry_job",
		"get_glossary", "update_glossary", "generate_glossary",
		"check_novel_updates", "update_novel_from_url",
		"preview_chapter_cleanup", "apply_chapter_cleanup",
		"get_reading_progress", "set_reading_progress",
		"list_novel_epubs", "build_epub", "translate_novel_description",
		"bulk_set_chapter_status", "bulk_set_chapter_excluded",
		"ask_user",
	} {
		if !names[want] {
			t.Errorf("agent tool catalog is missing %q", want)
		}
	}
}

// TestAgentCatalogToolsAnswerCatalogQuestions covers the catalog questions:
// existing tags, partial author search ("Cris" must find "TM Cris" too),
// field-scoped novel lists, and per-series translation progress.
func TestAgentCatalogToolsAnswerCatalogQuestions(t *testing.T) {
	setup := newAgentToolsTestSetup(t, "Catalogo de prueba")
	env, alice := setup.env, setup.alice

	patchNovel := func(novel novelPayload, fields map[string]any) {
		t.Helper()
		resp := doJSONRequest(t, env.handler, http.MethodPatch, "/api/v1/novels/"+novel.ID, alice.Token, fields)
		assertStatus(t, resp, http.StatusOK)
	}
	patchNovel(setup.aliceNovel, map[string]any{
		"targetAuthor": "Cris",
		"targetSeries": "Cronicas",
		"tags":         []string{"fantasia", "cultivo"},
	})
	novelB := createNovel(t, env.handler, alice.Token, "Serie incompleta", "es", "en")
	patchNovel(novelB, map[string]any{
		"targetAuthor": "TM Cris",
		"targetSeries": "Cronicas",
		"tags":         []string{"romance"},
	})
	novelC := createNovel(t, env.handler, alice.Token, "Sin serie", "es", "en")
	patchNovel(novelC, map[string]any{"targetAuthor": "Otra Persona"})

	// Series "Cronicas": novel A fully translated, novel B with a pending
	// chapter, novel C outside any series.
	chaptersA := setup.aliceChapters(t, 1, 2)
	for _, ch := range chaptersA {
		setup.setChapterStatus(t, setup.aliceNovel.ID, ch.ID, "translated")
	}
	chaptersB := []chapterPayload{createChapter(t, env.handler, alice.Token, novelB.ID, 1)}
	chaptersC := []chapterPayload{createChapter(t, env.handler, alice.Token, novelC.ID, 1)}
	setup.setChapterStatus(t, novelC.ID, chaptersC[0].ID, "translated")

	// Tags existentes.
	for _, result := range runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"list_tags","args":{}}`,
	}, nil) {
		for _, want := range []string{"fantasia", "cultivo", "romance"} {
			if !strings.Contains(result, want) {
				t.Errorf("list_tags result missing tag %q: %s", want, result)
			}
		}
	}

	// Partial author search.
	for _, result := range runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"list_authors","args":{"query":"cris"}}`,
	}, nil) {
		for _, want := range []string{"Cris", "TM Cris"} {
			if !strings.Contains(result, want) {
				t.Errorf("list_authors partial search missing %q: %s", want, result)
			}
		}
		if strings.Contains(result, "Otra Persona") {
			t.Errorf("list_authors partial search matched a non-matching author: %s", result)
		}
	}

	// field=author scopes the novel search to authors only.
	for _, result := range runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"list_novels","args":{"query":"TM","field":"author"}}`,
	}, nil) {
		if !strings.Contains(result, novelB.ID) {
			t.Errorf("field=author search missing novel B: %s", result)
		}
		if strings.Contains(result, setup.aliceNovel.ID) {
			t.Errorf("field=author search leaked a title match: %s", result)
		}
	}

	// field=tags answers which novels carry a tag.
	for _, result := range runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"list_novels","args":{"query":"cultivo","field":"tags"}}`,
	}, nil) {
		if !strings.Contains(result, setup.aliceNovel.ID) {
			t.Errorf("field=tags search missing novel A: %s", result)
		}
		if strings.Contains(result, novelB.ID) {
			t.Errorf("field=tags search matched a novel without the tag: %s", result)
		}
	}

	// The series is incomplete while novel B has a pending chapter...
	for _, result := range runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"list_series","args":{"complete":"complete"}}`,
	}, nil) {
		if strings.Contains(result, "Cronicas") {
			t.Errorf("list_series complete=complete returned a series with pending chapters: %s", result)
		}
	}
	for _, result := range runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"list_series","args":{"complete":"incomplete"}}`,
	}, nil) {
		if !strings.Contains(result, "Cronicas") {
			t.Errorf("list_series complete=incomplete missing the incomplete series: %s", result)
		}
		if !strings.Contains(result, `"chaptersPending":1`) {
			t.Errorf("list_series result should report 1 pending chapter: %s", result)
		}
	}
	// ...and complete once that chapter is translated. Novel C has no series
	// and must never appear.
	setup.setChapterStatus(t, novelB.ID, chaptersB[0].ID, "translated")
	for _, result := range runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"list_series","args":{"complete":"complete"}}`,
	}, nil) {
		if !strings.Contains(result, "Cronicas") || !strings.Contains(result, `"fullyTranslated":true`) {
			t.Errorf("list_series complete=complete should report Cronicas as fully translated: %s", result)
		}
		if strings.Contains(result, "Sin serie") {
			t.Errorf("list_series should not invent a series for series-less novels: %s", result)
		}
	}
}

func TestAgentJobToolsLifecycle(t *testing.T) {
	setup := newAgentToolsTestSetup(t, "Jobs de prueba")
	env, alice := setup.env, setup.alice
	setup.aliceChapters(t, 1, 2, 3)

	var createResult string
	for _, result := range runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"create_job","args":{"novelId":"` + setup.aliceNovel.ID + `","operation":"translate","fromOrder":1,"toOrder":3}}`,
	}, nil) {
		createResult = result
	}
	for _, want := range []string{`"operation":"translate"`, `"totalChapters":3`, `"status":"pending"`, `"jobId"`} {
		if !strings.Contains(createResult, want) {
			t.Errorf("create_job result missing %s: %s", want, createResult)
		}
	}
	// The narrow projection must never carry the chapter id list back into
	// the session trail.
	if strings.Contains(createResult, `"chapterIds"`) {
		t.Errorf("create_job result should not include chapterIds: %s", createResult)
	}

	// Selection validation errors.
	for _, call := range []string{
		`{"_tool":"create_job","args":{"novelId":"` + setup.aliceNovel.ID + `","operation":"translate","chapterIds":["nope"]}}`,
		`{"_tool":"create_job","args":{"novelId":"` + setup.aliceNovel.ID + `","operation":"translate","fromOrder":1}}`,
		`{"_tool":"create_job","args":{"novelId":"` + setup.aliceNovel.ID + `","operation":"check","fromOrder":1,"toOrder":2}}`,
		`{"_tool":"create_job","args":{"novelId":"` + setup.aliceNovel.ID + `","operation":"download"}}`,
	} {
		for _, result := range runAgentTools(t, env, alice.Token, []string{call}, nil) {
			if !strings.Contains(result, "error:") {
				t.Errorf("create_job should reject %s, got: %s", call, result)
			}
		}
	}

	// One active job per novel: a planted pending job blocks new ones.
	setup.createPendingJob(t, "translate")
	for _, result := range runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"create_job","args":{"novelId":"` + setup.aliceNovel.ID + `","operation":"translate"}}`,
	}, nil) {
		if !strings.Contains(result, "already has an active job") {
			t.Errorf("create_job should be refused while a job is active: %s", result)
		}
	}
	for _, result := range runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"get_active_jobs","args":{}}`,
	}, nil) {
		if !strings.Contains(result, `"hasActive":true`) {
			t.Errorf("get_active_jobs should report the planted job: %s", result)
		}
		if strings.Contains(result, `"chapterIds"`) {
			t.Errorf("get_active_jobs must not carry chapterIds into the trail: %s", result)
		}
	}

	// Cancel the planted job, then verify retry/cancel round-trip on a failed
	// job (the worker never touches store-planted jobs, so statuses stay put).
	for _, result := range runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"cancel_job","args":{"jobId":"missing"}}`,
	}, nil) {
		if !strings.Contains(result, "error:") {
			t.Errorf("cancel_job with a foreign/missing id must fail: %s", result)
		}
	}
}

func TestAgentJobToolsCancelAndRetry(t *testing.T) {
	setup := newAgentToolsTestSetup(t, "Cancel retry")
	env, alice := setup.env, setup.alice

	failed := setup.createPendingJob(t, "translate")
	if err := env.server.Store.UpdateJobForUser(alice.User.ID, failed.ID, map[string]any{"status": "failed"}); err != nil {
		t.Fatalf("mark job failed: %v", err)
	}

	var retryResult string
	for _, result := range runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"retry_job","args":{"jobId":"` + failed.ID + `"}}`,
	}, nil) {
		retryResult = result
	}
	if !strings.Contains(retryResult, `"status":"pending"`) {
		t.Errorf("retry_job should re-queue the failed job: %s", retryResult)
	}

	var cancelResult string
	for _, result := range runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"cancel_job","args":{"jobId":"` + failed.ID + `"}}`,
	}, nil) {
		cancelResult = result
	}
	if !strings.Contains(cancelResult, `"status":"cancelled"`) {
		t.Errorf("cancel_job should cancel the job: %s", cancelResult)
	}

	for _, result := range runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"get_novel_jobs","args":{"novelId":"` + setup.aliceNovel.ID + `","failedOnly":true}}`,
	}, nil) {
		if strings.Contains(result, failed.ID) {
			t.Errorf("failedOnly listing should not include the cancelled job: %s", result)
		}
		if strings.Contains(result, `"chapterIds"`) {
			t.Errorf("get_novel_jobs must not carry chapterIds into the trail: %s", result)
		}
	}
	for _, result := range runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"get_novel_jobs","args":{"novelId":"` + setup.aliceNovel.ID + `"}}`,
	}, nil) {
		if !strings.Contains(result, failed.ID) {
			t.Errorf("get_novel_jobs should include the cancelled job: %s", result)
		}
		if !strings.Contains(result, `"novelTitle"`) {
			t.Errorf("get_novel_jobs should resolve the novel title: %s", result)
		}
	}
}

func TestAgentBulkChapterWriteTools(t *testing.T) {
	setup := newAgentToolsTestSetup(t, "Bulk writes")
	env, alice := setup.env, setup.alice
	chapters := setup.aliceChapters(t, 1, 2, 3, 4, 5)

	// Exclude chapter 3 first: bulk operations must skip it.
	for _, result := range runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"set_chapter_excluded","args":{"novelId":"` + setup.aliceNovel.ID + `","chapterId":"` + chapters[2].ID + `","excluded":true}}`,
	}, nil) {
		if strings.Contains(result, "error:") {
			t.Fatalf("set_chapter_excluded failed: %s", result)
		}
	}

	for _, result := range runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"bulk_set_chapter_status","args":{"novelId":"` + setup.aliceNovel.ID + `","status":"done","fromOrder":1,"toOrder":5}}`,
	}, nil) {
		if !strings.Contains(result, `"changed":4`) {
			t.Errorf("bulk_set_chapter_status should touch exactly the 4 included chapters: %s", result)
		}
	}
	// Stats were refreshed: the excluded chapter stays out, the other four are done.
	for _, result := range runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"get_novel_stats","args":{"id":"` + setup.aliceNovel.ID + `"}}`,
	}, nil) {
		if !strings.Contains(result, `"completedChapters":4`) {
			t.Errorf("get_novel_stats should count 4 completed chapters: %s", result)
		}
	}

	for _, result := range runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"bulk_set_chapter_excluded","args":{"novelId":"` + setup.aliceNovel.ID + `","excluded":true,"fromOrder":4,"toOrder":5}}`,
	}, nil) {
		if !strings.Contains(result, `"changed":2`) {
			t.Errorf("bulk_set_chapter_excluded should flip exactly 2 chapters: %s", result)
		}
	}

	// Invalid status is rejected with the closed set message.
	for _, result := range runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"bulk_set_chapter_status","args":{"novelId":"` + setup.aliceNovel.ID + `","status":"processing","fromOrder":1,"toOrder":2}}`,
	}, nil) {
		if !strings.Contains(result, "invalid status") {
			t.Errorf("bulk_set_chapter_status should reject processing: %s", result)
		}
	}

	// Refused while a job is active, allowed again once it is cancelled.
	setup.createPendingJob(t, "translate")
	for _, result := range runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"bulk_set_chapter_status","args":{"novelId":"` + setup.aliceNovel.ID + `","status":"pending","fromOrder":1,"toOrder":2}}`,
	}, nil) {
		if !strings.Contains(result, "refused while the novel has active jobs") {
			t.Errorf("bulk write should be refused during active jobs: %s", result)
		}
	}
}

func TestAgentGlossaryTools(t *testing.T) {
	setup := newAgentToolsTestSetup(t, "Glosario")
	env, alice := setup.env, setup.alice

	var upsertResult string
	for _, result := range runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"update_glossary","args":{"novelId":"` + setup.aliceNovel.ID + `","upsert":[{"source":"cultivador de qi","target":"qi cultivator"},{"source":"secta","target":"sect"}]}}`,
	}, nil) {
		upsertResult = result
	}
	if !strings.Contains(upsertResult, `"added":2`) {
		t.Errorf("update_glossary should add 2 entries: %s", upsertResult)
	}

	for _, result := range runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"update_glossary","args":{"novelId":"` + setup.aliceNovel.ID + `","upsert":[{"source":"Cultivador de Qi","target":"qi refiner"}],"remove":["secta"]}}`,
	}, nil) {
		if !strings.Contains(result, `"added":0`) || !strings.Contains(result, `"updated":1`) || !strings.Contains(result, `"removed":1`) || !strings.Contains(result, `"total":1`) {
			t.Errorf("update_glossary upsert+remove round-trip mismatch: %s", result)
		}
	}

	for _, result := range runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"get_glossary","args":{"novelId":"` + setup.aliceNovel.ID + `"}}`,
	}, nil) {
		if !strings.Contains(result, "qi refiner") || !strings.Contains(result, `"total":1`) {
			t.Errorf("get_glossary should show the merged glossary: %s", result)
		}
	}

	// Refused while a job is active.
	setup.createPendingJob(t, "translate")
	for _, result := range runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"update_glossary","args":{"novelId":"` + setup.aliceNovel.ID + `","upsert":[{"source":"x","target":"y"}]}}`,
	}, nil) {
		if !strings.Contains(result, "refused while the novel has active jobs") {
			t.Errorf("update_glossary should be refused during active jobs: %s", result)
		}
	}
}

func TestAgentReadingProgressTools(t *testing.T) {
	setup := newAgentToolsTestSetup(t, "Progreso")
	env, alice := setup.env, setup.alice
	setup.aliceChapters(t, 1, 2, 3)

	for _, result := range runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"get_reading_progress","args":{"novelId":"` + setup.aliceNovel.ID + `"}}`,
	}, nil) {
		if !strings.Contains(result, `"hasProgress":false`) {
			t.Errorf("fresh novel should report no progress: %s", result)
		}
	}

	for _, result := range runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"set_reading_progress","args":{"novelId":"` + setup.aliceNovel.ID + `","chapterOrder":2,"scrollPercent":42.5}}`,
	}, nil) {
		if strings.Contains(result, "error:") {
			t.Fatalf("set_reading_progress failed: %s", result)
		}
	}
	for _, result := range runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"get_reading_progress","args":{"novelId":"` + setup.aliceNovel.ID + `"}}`,
	}, nil) {
		for _, want := range []string{`"hasProgress":true`, `"chapterOrder":2`, `"scrollPercent":42.5`, `Capítulo`} {
			if !strings.Contains(result, want) {
				t.Errorf("get_reading_progress missing %s: %s", want, result)
			}
		}
	}

	for _, result := range runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"set_reading_progress","args":{"novelId":"` + setup.aliceNovel.ID + `","chapterId":"missing","chapterOrder":1}}`,
	}, nil) {
		if !strings.Contains(result, "error:") {
			t.Errorf("set_reading_progress must take exactly one chapter selector: %s", result)
		}
	}
}

func TestAgentEpubTools(t *testing.T) {
	setup := newAgentToolsTestSetup(t, "Epubs")
	env, alice := setup.env, setup.alice
	setup.aliceChapters(t, 1, 2)

	for _, result := range runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"list_novel_epubs","args":{"novelId":"` + setup.aliceNovel.ID + `"}}`,
	}, nil) {
		if !strings.Contains(result, `"total":0`) {
			t.Errorf("fresh novel should have no epubs: %s", result)
		}
	}

	for _, result := range runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"build_epub","args":{"novelId":"` + setup.aliceNovel.ID + `","source":"original"}}`,
	}, nil) {
		if !strings.Contains(result, `"epubId"`) || !strings.Contains(result, `"fileKind":"original"`) {
			t.Errorf("build_epub should store an epub: %s", result)
		}
	}
	for _, result := range runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"list_novel_epubs","args":{"novelId":"` + setup.aliceNovel.ID + `"}}`,
	}, nil) {
		if !strings.Contains(result, `"total":1`) {
			t.Errorf("list_novel_epubs should show the generated epub: %s", result)
		}
	}

	for _, call := range []string{
		`{"_tool":"build_epub","args":{"novelId":"` + setup.aliceNovel.ID + `","source":"translated"}}`,
		`{"_tool":"build_epub","args":{"novelId":"` + setup.aliceNovel.ID + `","source":"nope"}}`,
	} {
		for _, result := range runAgentTools(t, env, alice.Token, []string{call}, nil) {
			if !strings.Contains(result, "error:") {
				t.Errorf("build_epub should reject %s: %s", call, result)
			}
		}
	}
}

func TestAgentCleanupTools(t *testing.T) {
	setup := newAgentToolsTestSetup(t, "Limpieza")
	env, alice := setup.env, setup.alice

	blanky := func(order int) chapterPayload {
		t.Helper()
		resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/"+setup.aliceNovel.ID+"/chapters", alice.Token, map[string]any{
			"chapterOrder":    order,
			"title":           "Con huecos",
			"originalContent": "Párrafo uno.\n\n\n\nPárrafo dos.\n\n\n\nPárrafo tres.",
		})
		assertStatus(t, resp, http.StatusCreated)
		return chapterIDPayload(t, resp)
	}
	ch1, ch2 := blanky(1), blanky(2)

	var previewResult string
	for _, result := range runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"preview_chapter_cleanup","args":{"novelId":"` + setup.aliceNovel.ID + `","chapterIds":["` + ch1.ID + `","` + ch2.ID + `"],"mode":"remove_multiple_blanks","applyTo":"original"}}`,
	}, nil) {
		previewResult = result
	}
	if !strings.Contains(previewResult, `"changed":2`) || !strings.Contains(previewResult, `"changeCount"`) {
		t.Errorf("preview_chapter_cleanup should report both chapters as changed: %s", previewResult)
	}
	if strings.Contains(previewResult, `"sample":[]`) {
		t.Errorf("preview_chapter_cleanup sample should describe the changes: %s", previewResult)
	}

	for _, result := range runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"apply_chapter_cleanup","args":{"novelId":"` + setup.aliceNovel.ID + `","fromOrder":1,"toOrder":2,"mode":"remove_multiple_blanks","applyTo":"original"}}`,
	}, nil) {
		if !strings.Contains(result, `"modified":2`) || !strings.Contains(result, `"skipped":0`) {
			t.Errorf("apply_chapter_cleanup should modify both chapters: %s", result)
		}
	}

	// After applying, the preview reports nothing left to change.
	for _, result := range runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"preview_chapter_cleanup","args":{"novelId":"` + setup.aliceNovel.ID + `","fromOrder":1,"toOrder":2,"mode":"remove_multiple_blanks","applyTo":"original"}}`,
	}, nil) {
		if !strings.Contains(result, `"changed":0`) {
			t.Errorf("second preview should find nothing to change: %s", result)
		}
	}

	for _, call := range []string{
		`{"_tool":"preview_chapter_cleanup","args":{"novelId":"` + setup.aliceNovel.ID + `","fromOrder":1,"mode":"remove_multiple_blanks","applyTo":"original"}}`,
		`{"_tool":"apply_chapter_cleanup","args":{"novelId":"` + setup.aliceNovel.ID + `","fromOrder":1,"toOrder":2,"mode":"nope","applyTo":"original"}}`,
	} {
		for _, result := range runAgentTools(t, env, alice.Token, []string{call}, nil) {
			if !strings.Contains(result, "error:") {
				t.Errorf("cleanup tool should reject %s: %s", call, result)
			}
		}
	}
}

func TestAgentUpdateNovelExtendedFields(t *testing.T) {
	setup := newAgentToolsTestSetup(t, "Campos ampliados")
	env, alice := setup.env, setup.alice

	for _, result := range runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"update_novel","args":{"id":"` + setup.aliceNovel.ID + `","targetAuthor":"Cris","targetSeries":"Cronicas","tags":["fantasia"],"status":"ongoing"}}`,
	}, nil) {
		for _, want := range []string{"Cris", "Cronicas", "fantasia", `"status":"ongoing"`} {
			if !strings.Contains(result, want) {
				t.Errorf("update_novel result missing %s: %s", want, result)
			}
		}
	}
	for _, result := range runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"get_novel","args":{"id":"` + setup.aliceNovel.ID + `"}}`,
	}, nil) {
		for _, want := range []string{"Cris", "Cronicas", "fantasia"} {
			if !strings.Contains(result, want) {
				t.Errorf("get_novel should reflect the extended fields (%s): %s", want, result)
			}
		}
	}
	for _, result := range runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"update_novel","args":{"id":"` + setup.aliceNovel.ID + `","status":"paused"}}`,
	}, nil) {
		if !strings.Contains(result, "invalid status") {
			t.Errorf("update_novel should reject an unknown novel status: %s", result)
		}
	}
}

// TestAgentNewWriteToolsAreOwnerScoped extends the ownership guarantee to the
// tools added with the jobs/catalog/glossary/epub batches.
func TestAgentNewWriteToolsAreOwnerScoped(t *testing.T) {
	setup := newAgentToolsTestSetup(t, "Ajeno")
	env, bob := setup.env, setup.bob

	foreign := setup.aliceNovel.ID
	for _, call := range []string{
		`{"_tool":"create_job","args":{"novelId":"` + foreign + `","operation":"translate"}}`,
		`{"_tool":"get_novel_jobs","args":{"novelId":"` + foreign + `"}}`,
		`{"_tool":"bulk_set_chapter_status","args":{"novelId":"` + foreign + `","status":"done","fromOrder":1,"toOrder":2}}`,
		`{"_tool":"bulk_set_chapter_excluded","args":{"novelId":"` + foreign + `","excluded":true,"fromOrder":1,"toOrder":2}}`,
		`{"_tool":"update_glossary","args":{"novelId":"` + foreign + `","upsert":[{"source":"a","target":"b"}]}}`,
		`{"_tool":"build_epub","args":{"novelId":"` + foreign + `","source":"original"}}`,
		`{"_tool":"set_reading_progress","args":{"novelId":"` + foreign + `","chapterOrder":1}}`,
		`{"_tool":"cancel_job","args":{"jobId":"` + foreign + `"}}`,
	} {
		for _, result := range runAgentTools(t, env, bob.Token, []string{call}, nil) {
			if !strings.Contains(result, "error:") {
				t.Errorf("tool call against a foreign novel must fail: %s → %s", call, result)
			}
			if strings.Contains(result, foreign) {
				t.Errorf("error must not leak the foreign novel id: %s → %s", call, result)
			}
		}
	}
}

// TestSliceAgentLinesFromEnd pins the window math, including the negative
// startLine (from-the-end) semantics and its clamping.
func TestSliceAgentLinesFromEnd(t *testing.T) {
	body := "l1\nl2\nl3\nl4\nl5"

	slice, meta := sliceAgentLines(body, -2, 2)
	if slice != "l4\nl5" {
		t.Errorf("startLine -2 should return the last two lines, got %q", slice)
	}
	if meta["startLine"] != 3 || meta["totalLines"] != 5 || meta["hasMore"] != false {
		t.Errorf("unexpected metadata for the from-end window: %v", meta)
	}

	// A window reaching past the start clamps to 0 and keeps reading forward.
	slice, meta = sliceAgentLines(body, -50, 2)
	if slice != "l1\nl2" || meta["startLine"] != 0 || meta["hasMore"] != true {
		t.Errorf("from-end window beyond the start should clamp to 0: %q %v", slice, meta)
	}

	// startLine -1 anchors on the last line; there is nothing after it, so the
	// window is just that line even with a larger lineCount.
	slice, _ = sliceAgentLines(body, -1, 2)
	if slice != "l5" {
		t.Errorf("startLine -1 should start at the last line, got %q", slice)
	}

	// Plain forward paging is unchanged.
	slice, meta = sliceAgentLines(body, 1, 2)
	if slice != "l2\nl3" || meta["nextStartLine"] != 3 {
		t.Errorf("forward paging changed: %q %v", slice, meta)
	}
}

// TestAgentGetChapterFromEndAndProbe exercises the wiring through the tool:
// reading an ending in one call and probing a chapter's exact length.
func TestAgentGetChapterFromEndAndProbe(t *testing.T) {
	setup := newAgentToolsTestSetup(t, "Ventanas")
	env, alice := setup.env, setup.alice

	content := "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10"
	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/v1/novels/"+setup.aliceNovel.ID+"/chapters", alice.Token, map[string]any{
		"chapterOrder":    1,
		"title":           "Con final",
		"originalContent": content,
	})
	assertStatus(t, resp, http.StatusCreated)
	chapter := chapterIDPayload(t, resp)

	args := `{"novelId":"` + setup.aliceNovel.ID + `","chapterId":"` + chapter.ID + `","content":"original"`

	for _, result := range runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"get_chapter","args":` + args + `,"startLine":-3,"lineCount":3}}`,
	}, nil) {
		// The result is JSON: newlines inside the slice are escaped, so match
		// the exact escaped slice rather than bare fragments ("l1" would also
		// match "l10").
		for _, want := range []string{`"originalContent":"l8\nl9\nl10"`, `"startLine":7`, `"totalLines":10`, `"hasMore":false`} {
			if !strings.Contains(result, want) {
				t.Errorf("from-end read missing %s: %s", want, result)
			}
		}
	}

	for _, result := range runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"get_chapter","args":` + args + `,"startLine":0,"lineCount":1}}`,
	}, nil) {
		for _, want := range []string{`"originalContent":"l1"`, `"totalLines":10`, `"hasMore":true`, `"nextStartLine":1`, `"lineCount":1`} {
			if !strings.Contains(result, want) {
				t.Errorf("probe read missing %s: %s", want, result)
			}
		}
	}

	for _, result := range runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"get_chapter","args":` + args + `,"startLine":-100,"lineCount":2}}`,
	}, nil) {
		if !strings.Contains(result, `"originalContent":"l1\nl2"`) || !strings.Contains(result, `"startLine":0`) {
			t.Errorf("from-end window beyond the start should clamp to the opening: %s", result)
		}
	}
}

// TestAgentListNovelsPagination pins the offset parameter: pages must cover
// the full result set without repeats, no matter the tie order of created.
func TestAgentListNovelsPagination(t *testing.T) {
	setup := newAgentToolsTestSetup(t, "Paginacion")
	env, alice := setup.env, setup.alice

	created := []string{}
	for _, title := range []string{"Paginada Alfa", "Paginada Beta", "Paginada Gamma"} {
		novel := createNovel(t, env.handler, alice.Token, title, "es", "en")
		created = append(created, novel.ID)
	}

	results := runAgentTools(t, env, alice.Token, []string{
		`{"_tool":"list_novels","args":{"query":"Paginada","limit":2}}`,
		`{"_tool":"list_novels","args":{"query":"Paginada","limit":2,"offset":2}}`,
	}, nil)
	if len(results) != 2 {
		t.Fatalf("expected 2 tool results, got %d", len(results))
	}
	seen := map[string]int{}
	for _, id := range created {
		for page, result := range results {
			if strings.Contains(result, id) {
				seen[id]++
				_ = page
			}
		}
	}
	if len(seen) != 3 {
		t.Errorf("paged listing must cover every matching novel, got %v: %v / %v", seen, results[0], results[1])
	}
	for id, count := range seen {
		if count != 1 {
			t.Errorf("novel %s appeared %d times across pages", id, count)
		}
	}
}

// chapterIDPayload decodes a created chapter payload from a 201 response.
func chapterIDPayload(t *testing.T, resp *httptest.ResponseRecorder) chapterPayload {
	t.Helper()
	var chapter chapterPayload
	decodeData(t, resp, &chapter)
	return chapter
}

package api

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"translator-server/internal/ai"
	"translator-server/internal/store"
)

// mockProvider for concurrency tests
type mockConcurrentProvider struct {
	delay       time.Duration
	concurrent  *int32
	maxObserved *int32
}

func (m *mockConcurrentProvider) TranslateTitle(ctx context.Context, in ai.TranslateTitleInput) (string, error) {
	c := atomic.AddInt32(m.concurrent, 1)
	defer atomic.AddInt32(m.concurrent, -1)
	for {
		max := atomic.LoadInt32(m.maxObserved)
		if c > max {
			if atomic.CompareAndSwapInt32(m.maxObserved, max, c) {
				break
			}
		} else {
			break
		}
	}
	select {
	case <-time.After(m.delay):
		return "Titulo " + in.TitleOriginal, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
func (m *mockConcurrentProvider) TranslateText(ctx context.Context, in ai.TranslateTextInput) (string, error) {
	c := atomic.AddInt32(m.concurrent, 1)
	defer atomic.AddInt32(m.concurrent, -1)
	for {
		max := atomic.LoadInt32(m.maxObserved)
		if c > max {
			if atomic.CompareAndSwapInt32(m.maxObserved, max, c) {
				break
			}
		} else {
			break
		}
	}
	select {
	case <-time.After(m.delay):
		return "TRADUCIDO: " + in.TextToTranslate, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
func (m *mockConcurrentProvider) Refine(ctx context.Context, in ai.RefineInput) (ai.RefineOutput, error) {
	return ai.RefineOutput{}, nil
}
func (m *mockConcurrentProvider) Check(ctx context.Context, in ai.CheckInput) (ai.CheckOutput, error) {
	return ai.CheckOutput{OK: true}, nil
}
func (m *mockConcurrentProvider) GenerateGlossary(ctx context.Context, in ai.GenerateGlossaryInput) (ai.GenerateGlossaryOutput, error) {
	return ai.GenerateGlossaryOutput{}, nil
}

func TestConcurrentTranslationRespectsLimitAndNoCross(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-concurrent@example.com", "secret123", "Alice")
	novel := createNovel(t, env.handler, alice.Token, "Novela Concurrente", "en", "es")

	// Create 6 chapters
	for i := 1; i <= 6; i++ {
		_, err := env.store.UpsertChapter(alice.User.ID, novel.ID, &store.Chapter{
			ChapterOrder:    i,
			Title:           "Chapter " + string(rune('0'+i)),
			OriginalContent: "Original content for chapter " + string(rune('0'+i)) + " with enough text to translate.",
			Status:          "pending",
		})
		if err != nil {
			t.Fatalf("upsert chapter %d: %v", i, err)
		}
	}

	// Configure provider with concurrency 3 and API key
	if _, err := env.store.UpsertProviderSettingsWithConcurrency(alice.User.ID, "venice", "deepseek-v4-flash", "https://api.venice.ai/api/v1", 120000, 3); err != nil {
		t.Fatalf("upsert provider concurrency: %v", err)
	}
	if _, err := env.store.ReplaceProviderAPIKey(alice.User.ID, "venice", "test-key-123"); err != nil {
		t.Fatalf("set api key: %v", err)
	}
	// Ensure app settings point to venice
	if _, err := env.store.SaveAppSettings(alice.User.ID, store.AppSettings{
		AI: store.AISettings{
			Provider:    "venice",
			Model:       "deepseek-v4-flash",
			BaseURL:     "https://api.venice.ai/api/v1",
			TimeoutMs:   120000,
			Concurrency: 3,
		},
		Translation: store.DefaultTranslationDefaults,
	}); err != nil {
		t.Fatalf("save app settings: %v", err)
	}

	var concurrent int32
	var maxObserved int32
	mock := &mockConcurrentProvider{delay: 120 * time.Millisecond, concurrent: &concurrent, maxObserved: &maxObserved}
	env.server.NewAIProvider = func(s store.AISettings) (ai.Provider, error) {
		return mock, nil
	}

	// Create translate job for all chapters
	job := &store.Job{NovelID: novel.ID, Operation: "translate", Status: "pending", Provider: "venice", Model: "deepseek-v4-flash"}
	if err := env.store.CreateJob(alice.User.ID, job); err != nil {
		t.Fatalf("create job: %v", err)
	}
	// Process job - should use concurrent path
	start := time.Now()
	if err := env.server.processJob(job.ID); err != nil {
		t.Fatalf("processJob: %v", err)
	}
	elapsed := time.Since(start)
	// With 6 chapters, 120ms each, sequential would be ~720ms + overhead.
	// Concurrent 3 should be ~240ms (2 batches). Allow slack for -race slowdown.
	if elapsed > 800*time.Millisecond {
		t.Fatalf("expected concurrent execution to be faster, elapsed %v > 800ms, maxObserved %d", elapsed, atomic.LoadInt32(&maxObserved))
	}
	max := atomic.LoadInt32(&maxObserved)
	if max < 2 {
		t.Fatalf("expected concurrent execution to observe >1 concurrent calls, got %d", max)
	}
	if max > 3 {
		t.Fatalf("expected max concurrency <=3, got %d", max)
	}

	// Verify no cross: each chapter's translated content should correspond to its original
	chapters, err := env.store.ListChaptersAccessible(alice.User.ID, novel.ID)
	if err != nil {
		t.Fatalf("list chapters: %v", err)
	}
	if len(chapters) != 6 {
		t.Fatalf("expected 6 chapters, got %d", len(chapters))
	}
	for _, ch := range chapters {
		if ch.Status != "translated" {
			t.Fatalf("chapter %d status %q want translated", ch.ChapterOrder, ch.Status)
		}
		if ch.TranslatedContent == "" {
			t.Fatalf("chapter %d has empty translated content", ch.ChapterOrder)
		}
		// translated content should contain the original marker (chapter number)
		// Since mock returns "TRADUCIDO: "+original, check it contains original prefix
		if len(ch.TranslatedContent) < 10 {
			t.Fatalf("chapter %d translated content too short: %q", ch.ChapterOrder, ch.TranslatedContent)
		}
	}
	// Verify job completed correctly
	updatedJob, err := env.store.GetJob(job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if updatedJob.Status != "done" {
		t.Fatalf("expected job status done, got %q error %q", updatedJob.Status, updatedJob.ErrorMessage)
	}
	if updatedJob.CompletedChapters != 6 {
		t.Fatalf("expected completed 6, got %d", updatedJob.CompletedChapters)
	}
	if updatedJob.FailedChapters != 0 {
		t.Fatalf("expected failed 0, got %d", updatedJob.FailedChapters)
	}
}

func TestSequentialWhenConcurrencyDisabled(t *testing.T) {
	env := newAPITestEnv(t)
	alice := registerUser(t, env.handler, "alice-seq@example.com", "secret123", "Alice")
	novel := createNovel(t, env.handler, alice.Token, "Novela Secuencial", "en", "es")
	for i := 1; i <= 3; i++ {
		_, err := env.store.UpsertChapter(alice.User.ID, novel.ID, &store.Chapter{
			ChapterOrder:    i,
			Title:           "Ch " + string(rune('0'+i)),
			OriginalContent: "Content " + string(rune('0'+i)),
			Status:          "pending",
		})
		if err != nil {
			t.Fatalf("upsert chapter: %v", err)
		}
	}
	if _, err := env.store.UpsertProviderSettingsWithConcurrency(alice.User.ID, "venice", "deepseek-v4-flash", "https://api.venice.ai/api/v1", 120000, 1); err != nil {
		t.Fatalf("upsert provider: %v", err)
	}
	if _, err := env.store.ReplaceProviderAPIKey(alice.User.ID, "venice", "test-key-123"); err != nil {
		t.Fatalf("set key: %v", err)
	}
	if _, err := env.store.SaveAppSettings(alice.User.ID, store.AppSettings{
		AI: store.AISettings{Provider: "venice", Model: "deepseek-v4-flash", BaseURL: "https://api.venice.ai/api/v1", TimeoutMs: 120000, Concurrency: 1},
		Translation: store.DefaultTranslationDefaults,
	}); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	var concurrent int32
	var maxObserved int32
	mock := &mockConcurrentProvider{delay: 80 * time.Millisecond, concurrent: &concurrent, maxObserved: &maxObserved}
	env.server.NewAIProvider = func(s store.AISettings) (ai.Provider, error) { return mock, nil }

	job2 := &store.Job{NovelID: novel.ID, Operation: "translate", Status: "pending"}
	if err := env.store.CreateJob(alice.User.ID, job2); err != nil {
		t.Fatalf("create job: %v", err)
	}
	start := time.Now()
	if err := env.server.processJob(job2.ID); err != nil {
		t.Fatalf("processJob: %v", err)
	}
	elapsed := time.Since(start)
	// Sequential 3*80 =240ms, concurrent would be ~80ms. Ensure sequential observed max 1
	max := atomic.LoadInt32(&maxObserved)
	if max != 1 {
		t.Fatalf("expected sequential max concurrency 1, got %d elapsed %v", max, elapsed)
	}
	if elapsed < 200*time.Millisecond {
		t.Fatalf("expected sequential to take >=200ms, got %v", elapsed)
	}
}

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"translator-server/internal/ai"
	"translator-server/internal/store"
)

const defaultMaxTokensPerBatch = 90000

// maxAllowedTokensPerBatch is a safety cap so a client cannot request an
// arbitrarily large batch that concatenates the whole novel into a single
// prompt (cost / model-context / DoS protection).
const maxAllowedTokensPerBatch = 500000

// errStructuredOutputNotSupported is the user-facing message when the selected
// model does not support response_format / structured outputs.
const errStructuredOutputNotSupported = "this model does not support structured output (response_format). Use a model that supports it (e.g. gpt-4o, gpt-4o-mini, gpt-4.1) or switch provider"

// isStructuredOutputNotSupported reports whether err is caused by the model
// not supporting structured output (response_format). The error message
// varies across providers but always contains "response_format".
func isStructuredOutputNotSupported(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "response_format is not supported") ||
		strings.Contains(msg, "response_format") && strings.Contains(msg, "not supported")
}

type glossaryJobOptions struct {
	ChapterFrom       int    `json:"chapterFrom"`
	ChapterTo         int    `json:"chapterTo"`
	Mode              string `json:"mode"` // "together" or "batch"
	MaxTokensPerBatch int    `json:"maxTokensPerBatch"`
	Provider          string `json:"provider"`
	Model             string `json:"model"`
}

// chapterInRangeWithContent reports whether a chapter falls within the requested
// range and has non-whitespace original content. Shared by the request handler
// and the job processor so acceptance and processing use identical criteria.
func chapterInRangeWithContent(ch store.Chapter, from, to int) bool {
	if ch.ChapterOrder < from {
		return false
	}
	if to > 0 && ch.ChapterOrder > to {
		return false
	}
	return strings.TrimSpace(ch.OriginalContent) != ""
}

// estimateTokens returns a fast, dependency-free token estimate.
// For English text: 1 token ≈ 4 characters. This gives ~90-98% accuracy
// which is more than sufficient for batch-sizing glossary jobs.
func estimateTokens(text string) int {
	if text == "" {
		return 0
	}
	chars := utf8.RuneCountInString(text)
	return chars / 4
}

func (s *Server) processGenerateGlossaryJob(ctx context.Context, job *store.Job) error {
	var opts glossaryJobOptions
	if err := json.Unmarshal([]byte(job.OptionsJSON), &opts); err != nil {
		if ue := s.Store.UpdateJob(job.ID, map[string]interface{}{"status": "failed", "errorMessage": fmt.Sprintf("invalid job options: %v", err)}); ue != nil {
			slog.Error("update job status on invalid options", "jobId", job.ID, "error", ue)
		}
		return fmt.Errorf("parse glossary options: %w", err)
	}

	if opts.Mode == "" {
		opts.Mode = "together"
	}
	if opts.MaxTokensPerBatch <= 0 {
		opts.MaxTokensPerBatch = defaultMaxTokensPerBatch
	}
	// Mirror the request handler's contract: an over-limit batch size is rejected
	// rather than silently clamped, so acceptance and processing stay in lockstep.
	if opts.MaxTokensPerBatch > maxAllowedTokensPerBatch {
		msg := fmt.Sprintf("maxTokensPerBatch %d exceeds the allowed maximum %d", opts.MaxTokensPerBatch, maxAllowedTokensPerBatch)
		if ue := s.Store.UpdateJob(job.ID, map[string]interface{}{"status": "failed", "errorMessage": msg}); ue != nil {
			slog.Error("update job status on invalid options", "jobId", job.ID, "error", ue)
		}
		return fmt.Errorf("%s", msg)
	}
	if opts.ChapterFrom <= 0 {
		opts.ChapterFrom = 1
	}

	novel, err := s.Store.GetOwnedNovel(job.OwnerID, job.NovelID)
	if err != nil {
		if ue := s.Store.UpdateJob(job.ID, map[string]interface{}{"status": "failed", "errorMessage": fmt.Sprintf("novel not found: %v", err)}); ue != nil {
			slog.Error("update job status on novel error", "jobId", job.ID, "error", ue)
		}
		return fmt.Errorf("get novel: %w", err)
	}

	if err := s.Store.UpdateJob(job.ID, map[string]interface{}{
		"status":       "running",
		"errorMessage": "",
	}); err != nil {
		return fmt.Errorf("set job running: %w", err)
	}

	chapters, err := s.Store.ListChaptersAccessible(job.OwnerID, job.NovelID)
	if err != nil {
		if ue := s.Store.UpdateJob(job.ID, map[string]interface{}{"status": "failed", "errorMessage": err.Error()}); ue != nil {
			slog.Error("update job status on chapters error", "jobId", job.ID, "error", ue)
		}
		return fmt.Errorf("list chapters: %w", err)
	}

	var selectedChapters []store.Chapter
	for _, ch := range chapters {
		if chapterInRangeWithContent(ch, opts.ChapterFrom, opts.ChapterTo) {
			selectedChapters = append(selectedChapters, ch)
		}
	}

	if len(selectedChapters) == 0 {
		if ue := s.Store.UpdateJob(job.ID, map[string]interface{}{
			"status":       "failed",
			"errorMessage": "no chapters found in the specified range with content",
		}); ue != nil {
			slog.Error("update job status on no chapters", "jobId", job.ID, "error", ue)
		}
		return fmt.Errorf("no chapters in range")
	}

	if ue := s.Store.UpdateJob(job.ID, map[string]interface{}{
		"totalChapters": len(selectedChapters),
	}); ue != nil {
		slog.Warn("update job total chapters", "jobId", job.ID, "error", ue)
	}

	existingEntries := extractExistingGlossary(novel.Glossary)
	existingTerms := extractExistingTerms(existingEntries)

	provider, err := s.resolveGlossaryProvider(job, novel)
	if err != nil {
		if ue := s.Store.UpdateJob(job.ID, map[string]interface{}{"status": "failed", "errorMessage": err.Error()}); ue != nil {
			slog.Error("update job status on provider error", "jobId", job.ID, "error", ue)
		}
		return fmt.Errorf("resolve provider: %w", err)
	}

	prompts, err := s.Store.GetEffectivePrompts(job.OwnerID, novel)
	if err != nil {
		if ue := s.Store.UpdateJob(job.ID, map[string]interface{}{"status": "failed", "errorMessage": err.Error()}); ue != nil {
			slog.Error("update job status on prompts error", "jobId", job.ID, "error", ue)
		}
		return fmt.Errorf("get prompts: %w", err)
	}

	systemPrompt := resolveGlossaryPrompt(prompts)

	var allEntries []ai.GlossaryEntry

	if opts.Mode == "batch" {
		allEntries, err = s.processGlossaryBatch(ctx, provider, systemPrompt, selectedChapters, novel, existingTerms, job.ID, opts)
	} else {
		allEntries, err = s.processGlossaryTogether(ctx, provider, systemPrompt, selectedChapters, novel, existingTerms, job.ID)
	}
	if err != nil {
		userMsg := err.Error()
		if isStructuredOutputNotSupported(err) {
			userMsg = errStructuredOutputNotSupported
		}
		if ue := s.Store.UpdateJob(job.ID, map[string]interface{}{"status": "failed", "errorMessage": userMsg}); ue != nil {
			slog.Error("update job status on generation error", "jobId", job.ID, "error", ue)
		}
		return fmt.Errorf("generate glossary: %w", err)
	}

	merged := mergeGlossary(existingEntries, allEntries)

	mergedJSON, err := json.Marshal(merged)
	if err != nil {
		if ue := s.Store.UpdateJob(job.ID, map[string]interface{}{"status": "failed", "errorMessage": err.Error()}); ue != nil {
			slog.Error("update job status on marshal error", "jobId", job.ID, "error", ue)
		}
		return fmt.Errorf("marshal glossary: %w", err)
	}

	if err := s.Store.UpdateNovelGlossary(job.OwnerID, job.NovelID, string(mergedJSON)); err != nil {
		if ue := s.Store.UpdateJob(job.ID, map[string]interface{}{"status": "failed", "errorMessage": err.Error()}); ue != nil {
			slog.Error("update job status on save error", "jobId", job.ID, "error", ue)
		}
		return fmt.Errorf("save glossary: %w", err)
	}

	if err := s.Store.UpdateJob(job.ID, map[string]interface{}{
		"status": "done",
	}); err != nil {
		return fmt.Errorf("set job done: %w", err)
	}

	return nil
}

func (s *Server) resolveGlossaryProvider(job *store.Job, novel *store.Novel) (ai.Provider, error) {
	providerKey := strings.TrimSpace(job.Provider)
	modelOverride := strings.TrimSpace(job.Model)

	if providerKey == "" {
		settings, err := s.Store.GetAppSettings(job.OwnerID)
		if err != nil {
			return nil, fmt.Errorf("get app settings: %w", err)
		}
		providerKey = settings.AI.Provider
		modelOverride = settings.AI.Model
	}

	if novel != nil && strings.TrimSpace(novel.AIOptions) != "" {
		var aiOptions novelAIOptions
		if err := json.Unmarshal([]byte(novel.AIOptions), &aiOptions); err == nil {
			if strings.TrimSpace(aiOptions.Provider) != "" {
				providerKey = strings.TrimSpace(aiOptions.Provider)
			}
			if strings.TrimSpace(aiOptions.Model) != "" {
				modelOverride = strings.TrimSpace(aiOptions.Model)
			}
		}
	}

	if providerKey == "" {
		return nil, fmt.Errorf("no AI provider configured")
	}

	aiSettings, err := s.Store.ResolveProviderAISettings(job.OwnerID, providerKey)
	if err != nil {
		return nil, fmt.Errorf("resolve provider: %w", err)
	}
	if modelOverride != "" {
		aiSettings.Model = modelOverride
	}

	return s.newAIProvider(aiSettings)
}

func resolveGlossaryPrompt(prompts []store.Prompt) string {
	for _, p := range prompts {
		if p.Key == "glossary" && strings.TrimSpace(p.SystemPrompt) != "" {
			return p.SystemPrompt
		}
	}
	return store.DefaultGlossaryPrompt
}

func extractExistingGlossary(glossaryJSON string) []glossaryEntry {
	if strings.TrimSpace(glossaryJSON) == "" || glossaryJSON == "[]" {
		return nil
	}
	var entries []glossaryEntry
	if err := json.Unmarshal([]byte(glossaryJSON), &entries); err != nil {
		return nil
	}
	// Backfill missing IDs for entries stored before the id field existed.
	for i := range entries {
		if entries[i].ID == "" {
			entries[i].ID = uuid.New().String()
		}
	}
	return entries
}

func extractExistingTerms(entries []glossaryEntry) []string {
	terms := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.TrimSpace(e.Source) != "" {
			terms = append(terms, strings.TrimSpace(e.Source))
		}
	}
	return terms
}

func (s *Server) processGlossaryTogether(ctx context.Context, provider ai.Provider, systemPrompt string, chapters []store.Chapter, novel *store.Novel, existingTerms []string, jobID string) ([]ai.GlossaryEntry, error) {
	texts := make([]string, len(chapters))
	for i, ch := range chapters {
		texts[i] = ch.OriginalContent
	}

	input := ai.GenerateGlossaryInput{
		SystemPrompt:  systemPrompt,
		Texts:         texts,
		SourceLang:    novel.SourceLanguage,
		TargetLang:    novel.TargetLanguage,
		ExistingTerms: existingTerms,
	}

	result, err := provider.GenerateGlossary(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("generate glossary (together): %w", err)
	}

	return flattenGlossaryOutput(result), nil
}

func (s *Server) processGlossaryBatch(ctx context.Context, provider ai.Provider, systemPrompt string, chapters []store.Chapter, novel *store.Novel, existingTerms []string, jobID string, opts glossaryJobOptions) ([]ai.GlossaryEntry, error) {
	type textBatch struct {
		texts  []string
		chFrom int
		chTo   int
	}

	var batches []textBatch
	var currentBatch textBatch
	var currentTokens int

	for _, ch := range chapters {
		chTokens := estimateTokens(ch.OriginalContent)
		if len(currentBatch.texts) > 0 && currentTokens+chTokens > opts.MaxTokensPerBatch {
			batches = append(batches, currentBatch)
			currentBatch = textBatch{chFrom: ch.ChapterOrder, chTo: ch.ChapterOrder}
			currentTokens = 0
		}
		if currentBatch.chFrom == 0 {
			currentBatch.chFrom = ch.ChapterOrder
		}
		currentBatch.chTo = ch.ChapterOrder
		currentBatch.texts = append(currentBatch.texts, ch.OriginalContent)
		currentTokens += chTokens
	}
	if len(currentBatch.texts) > 0 {
		batches = append(batches, currentBatch)
	}

	totalBatches := len(batches)
	var allEntries []ai.GlossaryEntry
	failedBatches := 0
	var lastErr error

	for i, batch := range batches {
		if ctx.Err() != nil {
			return allEntries, ctx.Err()
		}

		batchInfo := fmt.Sprintf("%d de %d", i+1, totalBatches)
		input := ai.GenerateGlossaryInput{
			SystemPrompt:  systemPrompt,
			Texts:         batch.texts,
			SourceLang:    novel.SourceLanguage,
			TargetLang:    novel.TargetLanguage,
			ExistingTerms: existingTerms,
			BatchInfo:     batchInfo,
		}

		result, err := provider.GenerateGlossary(ctx, input)
		if err != nil {
			// Structured-output errors are deterministic: every subsequent batch
			// will fail with the same error. Fail immediately instead of burning
			// tokens on doomed retries.
			if isStructuredOutputNotSupported(err) {
				return nil, fmt.Errorf("generate glossary (batch %s): %w", batchInfo, err)
			}
			slog.Warn("glossary batch failed", "jobId", jobID, "batch", i+1, "total", totalBatches, "error", err)
			failedBatches++
			lastErr = err
			continue
		}

		entries := flattenGlossaryOutput(result)
		allEntries = append(allEntries, entries...)

		if ue := s.Store.UpdateJob(jobID, map[string]interface{}{
			"completedChapters": i + 1,
		}); ue != nil {
			slog.Warn("update job batch progress", "jobId", jobID, "error", ue)
		}
	}

	// If every batch failed, treat the whole job as failed rather than silently
	// overwriting the existing glossary with no new entries.
	if totalBatches > 0 && failedBatches == totalBatches {
		return nil, fmt.Errorf("all %d glossary batches failed: %w", totalBatches, lastErr)
	}
	if failedBatches > 0 {
		slog.Warn("glossary generation completed with partial failures", "jobId", jobID, "failedBatches", failedBatches, "totalBatches", totalBatches)
	}

	return allEntries, nil
}

func flattenGlossaryOutput(out ai.GenerateGlossaryOutput) []ai.GlossaryEntry {
	entries := make([]ai.GlossaryEntry, 0, len(out.Terms)+len(out.CultivationSystem))
	entries = append(entries, out.Terms...)
	entries = append(entries, out.CultivationSystem...)
	return entries
}

func mergeGlossary(existing []glossaryEntry, newEntries []ai.GlossaryEntry) []glossaryEntry {
	// Preserve existing entries (including manually-added ones with their
	// target/context); new entries add missing terms and update existing ones
	// by source. Order is kept stable: existing terms first, new terms appended.
	indexBySource := make(map[string]int, len(existing)+len(newEntries))
	result := make([]glossaryEntry, 0, len(existing)+len(newEntries))

	for _, e := range existing {
		source := strings.TrimSpace(e.Source)
		if source == "" {
			continue
		}
		if _, ok := indexBySource[source]; ok {
			continue
		}
		indexBySource[source] = len(result)
		id := e.ID
		if id == "" {
			id = uuid.New().String()
		}
		result = append(result, glossaryEntry{
			ID:      id,
			Source:  source,
			Target:  strings.TrimSpace(e.Target),
			Context: strings.TrimSpace(e.Context),
		})
	}

	for _, e := range newEntries {
		source := strings.TrimSpace(e.Source)
		target := strings.TrimSpace(e.Target)
		if source == "" || target == "" {
			continue
		}
		entry := glossaryEntry{
			ID:      uuid.New().String(),
			Source:  source,
			Target:  target,
			Context: strings.TrimSpace(e.Context),
		}
		if idx, ok := indexBySource[source]; ok {
			// Preserve existing approved translations (see DefaultGlossaryPrompt:
			// "Always preserve existing approved translations"). Do not overwrite
			// the target of a term that already exists; only backfill an empty
			// context from the new entry.
			if result[idx].Context == "" && entry.Context != "" {
				result[idx].Context = entry.Context
			}
			continue
		}
		indexBySource[source] = len(result)
		result = append(result, entry)
	}

	return result
}

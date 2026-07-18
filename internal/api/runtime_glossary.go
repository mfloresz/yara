package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"

	tokenizer "github.com/pandodao/tokenizer-go"
	"translator-server/internal/ai"
	"translator-server/internal/store"
)

const defaultMaxTokensPerBatch = 90000

type glossaryJobOptions struct {
	ChapterFrom       int    `json:"chapterFrom"`
	ChapterTo         int    `json:"chapterTo"`
	Mode              string `json:"mode"` // "together" or "batch"
	MaxTokensPerBatch int    `json:"maxTokensPerBatch"`
	Provider          string `json:"provider"`
	Model             string `json:"model"`
}

func estimateTokens(text string) int {
	count, err := tokenizer.CalToken(text)
	if err != nil {
		// Fallback: rough estimate based on character count
		return len(text) / 4
	}
	return count
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
		if ch.ChapterOrder >= opts.ChapterFrom && (opts.ChapterTo <= 0 || ch.ChapterOrder <= opts.ChapterTo) {
			if strings.TrimSpace(ch.OriginalContent) != "" {
				selectedChapters = append(selectedChapters, ch)
			}
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

	existingTerms := extractExistingTerms(novel.Glossary)

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

	systemPrompt := resolveGlossaryPrompt(prompts, novel)

	var allEntries []ai.GlossaryEntry

	if opts.Mode == "batch" {
		allEntries, err = s.processGlossaryBatch(ctx, provider, systemPrompt, selectedChapters, novel, existingTerms, job.ID, opts)
	} else {
		allEntries, err = s.processGlossaryTogether(ctx, provider, systemPrompt, selectedChapters, novel, existingTerms, job.ID)
	}
	if err != nil {
		if ue := s.Store.UpdateJob(job.ID, map[string]interface{}{"status": "failed", "errorMessage": err.Error()}); ue != nil {
			slog.Error("update job status on generation error", "jobId", job.ID, "error", ue)
		}
		return fmt.Errorf("generate glossary: %w", err)
	}

	merged := mergeGlossary(existingTerms, allEntries)

	mergedJSON, err := json.Marshal(merged)
	if err != nil {
		if ue := s.Store.UpdateJob(job.ID, map[string]interface{}{"status": "failed", "errorMessage": err.Error()}); ue != nil {
			slog.Error("update job status on marshal error", "jobId", job.ID, "error", ue)
		}
		return fmt.Errorf("marshal glossary: %w", err)
	}

	if err := s.Store.UpdateNovelGlossary(job.NovelID, string(mergedJSON)); err != nil {
		if ue := s.Store.UpdateJob(job.ID, map[string]interface{}{"status": "failed", "errorMessage": err.Error()}); ue != nil {
			slog.Error("update job status on save error", "jobId", job.ID, "error", ue)
		}
		return fmt.Errorf("save glossary: %w", err)
	}

	return s.Store.UpdateJob(job.ID, map[string]interface{}{
		"status": "done",
	})
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

func resolveGlossaryPrompt(prompts []store.Prompt, novel *store.Novel) string {
	for _, p := range prompts {
		if p.Key == "glossary" && strings.TrimSpace(p.SystemPrompt) != "" {
			return p.SystemPrompt
		}
	}
	return store.DefaultGlossaryPrompt
}

func extractExistingTerms(glossaryJSON string) []string {
	if strings.TrimSpace(glossaryJSON) == "" || glossaryJSON == "[]" {
		return nil
	}
	var entries []glossaryEntry
	if err := json.Unmarshal([]byte(glossaryJSON), &entries); err != nil {
		return nil
	}
	terms := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.TrimSpace(e.Source) != "" {
			terms = append(terms, e.Source)
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
			slog.Warn("glossary batch failed", "jobId", jobID, "batch", i+1, "total", totalBatches, "error", err)
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

	return allEntries, nil
}

func flattenGlossaryOutput(out ai.GenerateGlossaryOutput) []ai.GlossaryEntry {
	entries := make([]ai.GlossaryEntry, 0, len(out.Terms)+len(out.CultivationSystem))
	entries = append(entries, out.Terms...)
	entries = append(entries, out.CultivationSystem...)
	return entries
}

func mergeGlossary(existingTerms []string, newEntries []ai.GlossaryEntry) []glossaryEntry {
	existingSet := make(map[string]struct{}, len(existingTerms))
	for _, t := range existingTerms {
		existingSet[strings.TrimSpace(t)] = struct{}{}
	}

	result := make([]glossaryEntry, 0, len(existingTerms)+len(newEntries))

	for _, e := range newEntries {
		source := strings.TrimSpace(e.Source)
		target := strings.TrimSpace(e.Target)
		if source == "" || target == "" {
			continue
		}
		result = append(result, glossaryEntry{
			Source:  source,
			Target:  target,
			Context: strings.TrimSpace(e.Context),
		})
		existingSet[source] = struct{}{}
	}

	_ = math.MaxInt32
	return result
}

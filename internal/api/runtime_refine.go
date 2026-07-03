package api

import (
	"fmt"
	"log/slog"
	"strings"

	"translator-server/internal/ai"
	"translator-server/internal/store"
)

func (s *Server) runRefineChapter(jc *jobContext, idx int, chapter *store.Chapter) error {
	if strings.TrimSpace(chapter.TranslatedContent) == "" {
		return fmt.Errorf("chapter %s has no translated content", chapter.ID)
	}
	baseline := chapter.TranslatedContent
	current := chapter.TranslatedContent

	attempts := 1
	if jc.cfg.Translation.EnableCheck {
		attempts = max(1, jc.cfg.Translation.MaxRetries)
	}

	for attempt := 1; attempt <= attempts; attempt++ {
		if jc.cfg.Translation.EnableCheck {
			checkPrompt := buildCheckPrompt(jc.cfg, jc.novel, chapter, current)
			checkOut, err := jc.provider.Check(jc.runCtx, checkPrompt)
			if err == nil && checkOut.OK {
				return s.saveRefinedChapter(jc, chapter, baseline, current)
			}
		}
		if err := jc.runCtx.Err(); err != nil {
			return fmt.Errorf("refine context cancelled: %w", err)
		}

		systemPrompt, userPrompt := buildRefinePrompt(jc.cfg, jc.novel, chapter, current)
		summary, err := jc.provider.Refine(jc.runCtx, ai.RefineInput{
			SystemPrompt:   systemPrompt,
			UserPrompt:     userPrompt,
			SourceLanguage: jc.novel.SourceLanguage,
			TargetLanguage: jc.novel.TargetLanguage,
			ApplyEdits:     newApplyEditsFunc(&current, chapter.ID),
			CurrentText:    newCurrentTextFunc(&current),
		})
		if err != nil {
			return fmt.Errorf("refine chapter: %w", err)
		}
		if len(summary.Unresolved) > 0 {
			slog.Warn("refine finished with unresolved edits", "chapterId", chapter.ID,
				"proposed", summary.TotalProposed, "applied", summary.TotalApplied, "unresolved", len(summary.Unresolved))
		}
	}

	return s.saveRefinedChapter(jc, chapter, baseline, current)
}

func (s *Server) saveRefinedChapter(jc *jobContext, chapter *store.Chapter, baseline, refined string) error {
	chapter.RefinedContent = refined
	applied, err := s.Store.SaveRefinedContentIfUnchanged(chapter.ID, baseline, chapter.RefinedContent, "refined")
	if err != nil {
		return fmt.Errorf("save chapter refinement: %w", err)
	}
	if !applied {
		return fmt.Errorf("chapter %s was edited while refinement was running; refinement discarded, retry the refine job", chapter.ID)
	}
	jc.statsDirty = true
	return nil
}

func buildRefinePrompt(cfg resolvedJobConfig, novel *store.Novel, chapter *store.Chapter, current string) (systemPrompt, userPrompt string) {
	glossaryText := formatGlossary(cfg.Glossary)
	values := map[string]string{
		"{SOURCE_LANG}": novel.SourceLanguage,
		"{TARGET_LANG}": novel.TargetLanguage,
		"{GLOSSARY}":    glossaryText,
		"{ORIGINAL}":      chapter.OriginalContent,
		"{ORIGINAL_TEXT}":  chapter.OriginalContent,
		"{TRANSLATION}":   current,
		"{TRANSLATION_TEXT}": current,
	}
	systemPrompt = strings.TrimSpace(fillPrompt(cfg.Prompts.Refine.SystemPrompt, values))
	userPrompt = strings.TrimSpace(fillPrompt(cfg.Prompts.Refine.UserPrompt, values))
	return systemPrompt, userPrompt
}

func buildCheckPrompt(cfg resolvedJobConfig, novel *store.Novel, chapter *store.Chapter, current string) ai.CheckInput {
	glossaryText := formatGlossary(cfg.Glossary)
	systemPrompt := fillPrompt(cfg.Prompts.Check.SystemPrompt, map[string]string{
		"{SOURCE_LANG}": novel.SourceLanguage,
		"{TARGET_LANG}": novel.TargetLanguage,
		"{GLOSSARY}":    glossaryText,
	})
	userPrompt := fillPrompt(cfg.Prompts.Check.UserPrompt, map[string]string{
		"{SOURCE_LANG}": novel.SourceLanguage,
		"{TARGET_LANG}": novel.TargetLanguage,
		"{ORIGINAL}":    chapter.OriginalContent,
		"{TRANSLATION}": current,
	})
	return ai.CheckInput{
		SystemPrompt: systemPrompt,
		UserPrompt:   userPrompt,
	}
}

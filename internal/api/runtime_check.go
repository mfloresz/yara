package api

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"translator-server/internal/ai"
	"translator-server/internal/store"
)

// checkDecisionThreshold is the probability above which a decision-model
// signal is treated as true. The decision model is deterministic per input, so
// a fixed threshold is stable; see the action guard in resolveCheckAction.
const checkDecisionThreshold = 0.5

const defaultTranslationBrief = `Produce a natural target-language localization, NOT a literal translation. Priority order when rules conflict: (1) terminology and glossary, (2) dialogue formatting, (3) natural syntax and register, (4) literal fidelity.

Non-literal wording, idiom substitution, sentence reordering, merging or splitting sentences, and cultural naturalization are acceptable when they preserve the scene's meaning, intent, tone, and emotional effect. Do not penalize a translation for being adaptive rather than literal. Do penalize meaning drift: changed facts, negation, numbers, relationships, character intent, contradictions with the original, additions or omissions that change the story, untranslated content, or truncation.`

const refineCapabilities = `A refine pass makes surgical edits on the existing translated text. Each edit replaces one exact sentence or line that occurs once. It can fix untranslated words or phrases, terminology and glossary mismatches, grammar, register, dialogue punctuation, and localized meaning or intent errors inside an existing sentence.

A refine pass cannot reliably fix a truncated ending, missing sentences, missing paragraphs, missing scenes, or text cut off mid-sentence. Those require translating the missing content again from the original source.`

var decisionQuestions = map[string]ai.DecisionQuestion{
	"recommended_action": {
		Type:         "choice",
		Instructions: "What is the smallest remediation that will make this translation correct, complete, and consistent with the translation brief?",
		Criteria: map[string]string{
			ai.CheckDecisionAccept:      "The translation is complete, fully in the target language, and preserves the original meaning, intent, and tone. Only stylistic preferences differ; there is no real defect that needs correction.",
			ai.CheckDecisionRefine:      "The translation is complete and structurally intact, but it has localized defects that a surgical edit pass can fix: untranslated words or phrases, terminology, grammar, register, dialogue punctuation, or a localized meaning or intent error. Missing content is NOT the problem.",
			ai.CheckDecisionRetranslate: "The translation is truncated, missing sentences, paragraphs, or scenes, cut off mid-sentence, or otherwise structurally broken. Surgical edits cannot reliably restore what is missing; the chapter must be translated again from the source.",
		},
	},
	"structurally_intact": {
		Type:         "noul",
		Instructions: "Does the translation cover every part of the original? Count only whether each sentence, paragraph, and scene has a counterpart in the translation, regardless of the language that counterpart is written in.",
		Criteria: map[string]string{
			"true":  "Every sentence, paragraph, and scene of the original has a counterpart in the translation, and the translation ends where the original ends. Content that is present but still untranslated still counts as present.",
			"false": "At least one sentence, paragraph, or scene has no counterpart at all, or the translation stops mid-sentence or before the end of the original.",
		},
	},
	"untranslated_content_present": {
		Type:         "noul",
		Instructions: "Does the translation leave any source word, phrase, or sentence untranslated that should have been rendered in the target language?",
		Criteria: map[string]string{
			"true":  "At least one word, phrase, or sentence that should be in the target language is still in the source language. This includes a single common noun left in the source language, even if it is a term the story uses often.",
			"false": "Every word, phrase, and sentence that should be in the target language is translated. Only proper names and terms that must stay in the source language remain in the source language.",
		},
	},
	"has_meaning_error": {
		Type:         "noul",
		Instructions: "Does the translation contain a meaning error: a changed fact, negation, number, relationship, character intent, or a contradiction with the original?",
		Criteria: map[string]string{
			"true":  "At least one meaning error is present.",
			"false": "No meaning error is present; differences are only stylistic or adaptive.",
		},
	},
}

// checkChapter runs the configured check. A decision model is preferred when
// it is configured; any decision failure falls back to the chat-model check so
// a provider outage degrades to the previous behavior instead of failing the
// job.
func (s *Server) checkChapter(ctx context.Context, jc *jobContext, chapter *store.Chapter, current string) (ai.CheckOutput, error) {
	if jc.decision != nil {
		out, err := checkChapterWithDecision(ctx, jc, chapter, current)
		if err == nil {
			return out, nil
		}
		slog.Warn("decision check failed, falling back to content provider check", "chapterId", chapter.ID, "err", err)
	}
	out, err := jc.provider.Check(ctx, buildCheckPrompt(jc.cfg, jc.novel, chapter, current))
	if err != nil {
		return out, err
	}
	// The default check prompt asks for the same three-way decision; normalize
	// it. An empty or unknown value is left for resolveCheckAction to fail
	// closed on.
	out.Decision = strings.ToLower(strings.TrimSpace(out.Decision))
	return out, nil
}

func checkChapterWithDecision(ctx context.Context, jc *jobContext, chapter *store.Chapter, current string) (ai.CheckOutput, error) {
	resp, err := jc.decision.Decide(ctx, buildDecisionRequest(jc, chapter, current))
	if err != nil {
		return ai.CheckOutput{}, err
	}
	action, ok := resp.Answers["recommended_action"]
	if !ok {
		return ai.CheckOutput{}, fmt.Errorf("decision response missing recommended_action")
	}
	decision := strings.ToLower(strings.TrimSpace(action.Choice))
	switch decision {
	case ai.CheckDecisionAccept, ai.CheckDecisionRefine, ai.CheckDecisionRetranslate:
	default:
		return ai.CheckOutput{}, fmt.Errorf("decision response has invalid action %q", action.Choice)
	}
	signals := make(map[string]float64, 3)
	for _, name := range []string{"structurally_intact", "untranslated_content_present", "has_meaning_error"} {
		answer, ok := resp.Answers[name]
		if !ok {
			return ai.CheckOutput{}, fmt.Errorf("decision response missing %s", name)
		}
		signals[name] = answer.Noul
	}
	return ai.CheckOutput{
		Decision:   decision,
		Confidence: action.Confidence,
		Signals:    signals,
	}, nil
}

func buildDecisionRequest(jc *jobContext, chapter *store.Chapter, current string) ai.DecisionRequest {
	brief := strings.TrimSpace(jc.baseSystemPrompt)
	if brief == "" {
		brief = defaultTranslationBrief
	}
	state := map[string]any{
		"task": fmt.Sprintf(
			"Evaluate a %s translation of a %s chapter and choose the smallest remediation needed.",
			jc.novel.TargetLanguage, jc.novel.SourceLanguage,
		),
		"source_language":     jc.novel.SourceLanguage,
		"target_language":     jc.novel.TargetLanguage,
		"translation_brief":   brief,
		"refine_capabilities": refineCapabilities,
		"original":            chapter.OriginalContent,
		"translation":         current,
		"note":                "Judge the translation against translation_brief, not against a literal word-for-word ideal.",
	}
	model := ""
	if jc.cfg.CheckAI != nil {
		model = jc.cfg.CheckAI.Model
	}
	return ai.DecisionRequest{
		Model:     model,
		State:     state,
		Questions: decisionQuestions,
	}
}

// resolveCheckAction maps a check result to the remediation to run. An
// explicit choice (decision model, or a chat model following the default check
// prompt) is authoritative except at three edges:
//   - missing content (structurally_intact below the threshold) always wins
//     over a refine, because surgical edits cannot restore it;
//   - a retranslate choice is downgraded to refine when the structure signal
//     says the text is intact;
//   - an accept is escalated to refine when untranslated content or a meaning
//     error is clearly present.
//
// A check without signals (chat fallback) keeps its choice; one without a
// choice fails closed to refine.
func resolveCheckAction(out ai.CheckOutput) string {
	decision := strings.ToLower(strings.TrimSpace(out.Decision))
	if decision == "" {
		return ai.CheckDecisionRefine
	}
	if intact, ok := out.Signals["structurally_intact"]; ok {
		if intact < checkDecisionThreshold {
			return ai.CheckDecisionRetranslate
		}
		if decision == ai.CheckDecisionRetranslate {
			return ai.CheckDecisionRefine
		}
	}
	switch decision {
	case ai.CheckDecisionAccept:
		if out.Signals["untranslated_content_present"] >= checkDecisionThreshold ||
			out.Signals["has_meaning_error"] >= checkDecisionThreshold {
			return ai.CheckDecisionRefine
		}
		return ai.CheckDecisionAccept
	case ai.CheckDecisionRefine:
		return ai.CheckDecisionRefine
	case ai.CheckDecisionRetranslate:
		return ai.CheckDecisionRetranslate
	default:
		return ai.CheckDecisionRefine
	}
}

package api

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"translator-server/internal/ai"
	"translator-server/internal/store"
)

// scriptedDecisionChecker answers with a fixed sequence so a test can drive
// every check in a refine loop.
type scriptedDecisionChecker struct {
	responses []ai.DecisionResponse
	errors    []error
	calls     int
	requests  []ai.DecisionRequest
}

func (d *scriptedDecisionChecker) Decide(_ context.Context, req ai.DecisionRequest) (ai.DecisionResponse, error) {
	d.requests = append(d.requests, req)
	i := d.calls
	d.calls++
	if i < len(d.errors) && d.errors[i] != nil {
		return ai.DecisionResponse{}, d.errors[i]
	}
	if i >= len(d.responses) {
		return ai.DecisionResponse{}, fmt.Errorf("unexpected decision call %d", i)
	}
	return d.responses[i], nil
}

type refineFlowProvider struct {
	translation    string
	translateCalls int
	refineEdits    []ai.RefineEdit
	refineCalls    int
	checks         []ai.CheckOutput
	checkCalls     int
}

func (p *refineFlowProvider) TranslateTitle(context.Context, ai.TranslateTitleInput) (string, error) {
	return "Título", nil
}

func (p *refineFlowProvider) TranslateText(_ context.Context, in ai.TranslateTextInput) (string, error) {
	p.translateCalls++
	if p.translation == "" {
		return "TRADUCIDO: " + in.TextToTranslate, nil
	}
	return p.translation, nil
}

func (p *refineFlowProvider) Refine(_ context.Context, in ai.RefineInput) (ai.RefineOutput, error) {
	p.refineCalls++
	if len(p.refineEdits) == 0 {
		return ai.RefineOutput{}, nil
	}
	results := in.ApplyEdits(p.refineEdits)
	applied := 0
	for _, result := range results {
		if result.Applied {
			applied++
		}
	}
	return ai.RefineOutput{TotalProposed: len(results), TotalApplied: applied}, nil
}

func (p *refineFlowProvider) Check(context.Context, ai.CheckInput) (ai.CheckOutput, error) {
	p.checkCalls++
	if len(p.checks) == 0 {
		return ai.CheckOutput{Decision: ai.CheckDecisionAccept}, nil
	}
	out := p.checks[0]
	p.checks = p.checks[1:]
	return out, nil
}

func (p *refineFlowProvider) GenerateGlossary(context.Context, ai.GenerateGlossaryInput) (ai.GenerateGlossaryOutput, error) {
	return ai.GenerateGlossaryOutput{}, nil
}

func newRefineFlowFixture(t *testing.T) (*apiTestEnv, authPayload, novelPayload, *store.Chapter) {
	t.Helper()
	env := newAPITestEnv(t)
	alice := registerUser(t, env, "refine-flow@example.com", "secret123", "Alice")
	novel := createNovel(t, env.handler, alice.Token, "Libro", "en", "es")
	chapter, err := env.store.UpsertChapterWithoutStats(alice.User.ID, novel.ID, &store.Chapter{
		ChapterOrder:      1,
		Title:             "One",
		TranslatedTitle:   "Uno",
		OriginalContent:   "The house is red.\nThe sky is blue.",
		TranslatedContent: "La casa es roja.\nEl cielo es azul.",
		Status:            "translated",
	})
	if err != nil {
		t.Fatalf("create chapter: %v", err)
	}
	return env, alice, novel, chapter
}

func newRefineFlowContext(novel novelPayload, provider ai.Provider, decision ai.DecisionChecker) *jobContext {
	return &jobContext{
		jobID:    "job-refine-flow",
		runCtx:   context.Background(),
		novel:    &store.Novel{ID: novel.ID, SourceLanguage: "en", TargetLanguage: "es"},
		provider: provider,
		decision: decision,
		cfg: resolvedJobConfig{
			Translation: store.TranslationDefaults{EnableCheck: true, MaxRetries: 2},
			Prompts: promptSettings{
				Refine: promptTemplate{
					SystemPrompt: "refine",
					UserPrompt:   "{ORIGINAL}\n---\n{TRANSLATION}",
				},
			},
		},
	}
}

func TestRunRefineChapterRetranslatesWhenStructureIsBroken(t *testing.T) {
	env, alice, novel, chapter := newRefineFlowFixture(t)
	provider := &refineFlowProvider{translation: "La casa nueva es roja.\nEl cielo nuevo es azul."}
	decision := &scriptedDecisionChecker{responses: []ai.DecisionResponse{
		decisionAnswers(ai.CheckDecisionRetranslate, 0.02, 0.1, 0.1),
		decisionAnswers(ai.CheckDecisionAccept, 0.97, 0.05, 0.05),
	}}
	jc := newRefineFlowContext(novel, provider, decision)

	if err := env.server.runRefineChapter(jc, 0, chapter); err != nil {
		t.Fatalf("runRefineChapter: %v", err)
	}

	if decision.calls != 2 {
		t.Fatalf("expected a check after retranslation, got %d checks", decision.calls)
	}
	if provider.translateCalls != 1 {
		t.Fatalf("expected one retranslation, got %d", provider.translateCalls)
	}
	if provider.refineCalls != 0 {
		t.Fatalf("expected no refine pass after retranslation, got %d", provider.refineCalls)
	}
	if got := decision.requests[1].State["translation"]; got != provider.translation {
		t.Fatalf("second check should see the retranslated text, got %v", got)
	}

	updated, err := env.store.GetOwnedChapter(alice.User.ID, novel.ID, chapter.ID)
	if err != nil {
		t.Fatalf("load chapter: %v", err)
	}
	if updated.TranslatedContent != provider.translation {
		t.Fatalf("translated content = %q, want %q", updated.TranslatedContent, provider.translation)
	}
	if updated.RefinedContent != provider.translation {
		t.Fatalf("refined content = %q, want the accepted retranslation", updated.RefinedContent)
	}
}

func TestRunRefineChapterEscalatesAcceptToRefineOnUntranslatedContent(t *testing.T) {
	env, alice, novel, chapter := newRefineFlowFixture(t)
	provider := &refineFlowProvider{refineEdits: []ai.RefineEdit{{
		Original:    "El cielo es azul.",
		Replacement: "El cielo está azul.",
	}}}
	decision := &scriptedDecisionChecker{responses: []ai.DecisionResponse{
		decisionAnswers(ai.CheckDecisionAccept, 0.98, 0.94, 0.05),
		decisionAnswers(ai.CheckDecisionAccept, 0.98, 0.05, 0.05),
	}}
	jc := newRefineFlowContext(novel, provider, decision)

	if err := env.server.runRefineChapter(jc, 0, chapter); err != nil {
		t.Fatalf("runRefineChapter: %v", err)
	}

	if provider.refineCalls != 1 {
		t.Fatalf("expected the accept to be escalated into one refine, got %d", provider.refineCalls)
	}
	if decision.calls != 2 {
		t.Fatalf("expected a re-check after refining, got %d", decision.calls)
	}

	updated, err := env.store.GetOwnedChapter(alice.User.ID, novel.ID, chapter.ID)
	if err != nil {
		t.Fatalf("load chapter: %v", err)
	}
	if updated.TranslatedContent != "La casa es roja.\nEl cielo es azul." {
		t.Fatalf("refine must not rewrite the translation, got %q", updated.TranslatedContent)
	}
	if updated.RefinedContent != "La casa es roja.\nEl cielo está azul." {
		t.Fatalf("refined content = %q", updated.RefinedContent)
	}
}

func TestRunRefineChapterAcceptsWithoutRefining(t *testing.T) {
	env, alice, novel, chapter := newRefineFlowFixture(t)
	provider := &refineFlowProvider{}
	decision := &scriptedDecisionChecker{responses: []ai.DecisionResponse{
		decisionAnswers(ai.CheckDecisionAccept, 0.97, 0.05, 0.05),
	}}
	jc := newRefineFlowContext(novel, provider, decision)

	if err := env.server.runRefineChapter(jc, 0, chapter); err != nil {
		t.Fatalf("runRefineChapter: %v", err)
	}

	if provider.refineCalls != 0 {
		t.Fatalf("expected no refine pass, got %d", provider.refineCalls)
	}
	updated, err := env.store.GetOwnedChapter(alice.User.ID, novel.ID, chapter.ID)
	if err != nil {
		t.Fatalf("load chapter: %v", err)
	}
	if updated.RefinedContent != "La casa es roja.\nEl cielo es azul." {
		t.Fatalf("refined content = %q", updated.RefinedContent)
	}
}

func TestRunRefineChapterUsesChatCheckWhenDecisionFails(t *testing.T) {
	env, alice, novel, chapter := newRefineFlowFixture(t)
	provider := &refineFlowProvider{
		refineEdits: []ai.RefineEdit{{
			Original:    "El cielo es azul.",
			Replacement: "El cielo está azul.",
		}},
		checks: []ai.CheckOutput{{Decision: ai.CheckDecisionRefine}, {Decision: ai.CheckDecisionAccept}},
	}
	decision := &scriptedDecisionChecker{errors: []error{errors.New("down"), errors.New("down")}}
	jc := newRefineFlowContext(novel, provider, decision)

	if err := env.server.runRefineChapter(jc, 0, chapter); err != nil {
		t.Fatalf("runRefineChapter: %v", err)
	}

	if decision.calls != 2 {
		t.Fatalf("expected two failed decision checks, got %d", decision.calls)
	}
	if provider.checkCalls != 2 {
		t.Fatalf("expected the chat provider check to run twice, got %d", provider.checkCalls)
	}
	if provider.refineCalls != 1 {
		t.Fatalf("expected one refine pass, got %d", provider.refineCalls)
	}
	updated, err := env.store.GetOwnedChapter(alice.User.ID, novel.ID, chapter.ID)
	if err != nil {
		t.Fatalf("load chapter: %v", err)
	}
	if updated.RefinedContent != "La casa es roja.\nEl cielo está azul." {
		t.Fatalf("refined content = %q", updated.RefinedContent)
	}
}

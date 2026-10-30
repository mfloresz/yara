package api

import (
	"context"
	"errors"
	"strings"
	"testing"

	"translator-server/internal/ai"
	"translator-server/internal/store"
)

type stubDecisionChecker struct {
	response ai.DecisionResponse
	err      error
	calls    int
	lastReq  ai.DecisionRequest
}

func (d *stubDecisionChecker) Decide(_ context.Context, req ai.DecisionRequest) (ai.DecisionResponse, error) {
	d.calls++
	d.lastReq = req
	return d.response, d.err
}

// checkStubProvider is a minimal ai.Provider for check/refine flow tests.
type checkStubProvider struct {
	checkOutput ai.CheckOutput
	checkErr    error
	checkCalls  int
	refineCalls int
}

func (p *checkStubProvider) TranslateTitle(context.Context, ai.TranslateTitleInput) (string, error) {
	return "Título", nil
}

func (p *checkStubProvider) TranslateText(_ context.Context, in ai.TranslateTextInput) (string, error) {
	return "TRADUCIDO: " + in.TextToTranslate, nil
}

func (p *checkStubProvider) Refine(context.Context, ai.RefineInput) (ai.RefineOutput, error) {
	p.refineCalls++
	return ai.RefineOutput{}, nil
}

func (p *checkStubProvider) Check(context.Context, ai.CheckInput) (ai.CheckOutput, error) {
	p.checkCalls++
	return p.checkOutput, p.checkErr
}

func (p *checkStubProvider) GenerateGlossary(context.Context, ai.GenerateGlossaryInput) (ai.GenerateGlossaryOutput, error) {
	return ai.GenerateGlossaryOutput{}, nil
}

func decisionAnswers(action string, intact, untranslated, meaning float64) ai.DecisionResponse {
	return ai.DecisionResponse{Answers: map[string]ai.DecisionAnswer{
		"recommended_action":           {Type: "choice", Choice: action, Confidence: 0.9},
		"structurally_intact":          {Type: "noul", Noul: intact},
		"untranslated_content_present": {Type: "noul", Noul: untranslated},
		"has_meaning_error":            {Type: "noul", Noul: meaning},
	}}
}

func TestResolveCheckAction(t *testing.T) {
	cases := []struct {
		name string
		out  ai.CheckOutput
		want string
	}{
		{
			name: "missing decision fails closed to refine",
			out:  ai.CheckOutput{},
			want: ai.CheckDecisionRefine,
		},
		{
			name: "clean accept stays accept",
			out:  ai.CheckOutput{Decision: ai.CheckDecisionAccept, Signals: map[string]float64{"structurally_intact": 0.97}},
			want: ai.CheckDecisionAccept,
		},
		{
			name: "accept is escalated when structure is broken",
			out:  ai.CheckOutput{Decision: ai.CheckDecisionAccept, Signals: map[string]float64{"structurally_intact": 0.02}},
			want: ai.CheckDecisionRetranslate,
		},
		{
			name: "accept is escalated when untranslated content remains",
			out: ai.CheckOutput{Decision: ai.CheckDecisionAccept, Signals: map[string]float64{
				"structurally_intact": 0.98, "untranslated_content_present": 0.94,
			}},
			want: ai.CheckDecisionRefine,
		},
		{
			name: "accept is escalated on a meaning error",
			out: ai.CheckOutput{Decision: ai.CheckDecisionAccept, Signals: map[string]float64{
				"structurally_intact": 0.98, "has_meaning_error": 0.93,
			}},
			want: ai.CheckDecisionRefine,
		},
		{
			name: "refine choice with intact structure stays refine",
			out: ai.CheckOutput{Decision: ai.CheckDecisionRefine, Signals: map[string]float64{
				"structurally_intact": 0.98, "untranslated_content_present": 0.9,
			}},
			want: ai.CheckDecisionRefine,
		},
		{
			name: "retranslate choice with intact structure downgrades to refine",
			out:  ai.CheckOutput{Decision: ai.CheckDecisionRetranslate, Signals: map[string]float64{"structurally_intact": 0.97}},
			want: ai.CheckDecisionRefine,
		},
		{
			name: "retranslate choice without signals is kept",
			out:  ai.CheckOutput{Decision: ai.CheckDecisionRetranslate},
			want: ai.CheckDecisionRetranslate,
		},
		{
			name: "retranslate choice with broken structure is kept",
			out:  ai.CheckOutput{Decision: ai.CheckDecisionRetranslate, Signals: map[string]float64{"structurally_intact": 0.01}},
			want: ai.CheckDecisionRetranslate,
		},
		{
			name: "unknown action fails closed to refine",
			out:  ai.CheckOutput{Decision: "rewrite", Signals: map[string]float64{"structurally_intact": 0.99}},
			want: ai.CheckDecisionRefine,
		},
		{
			name: "action is trimmed and lowercased",
			out:  ai.CheckOutput{Decision: "  ACCEPT ", Signals: map[string]float64{"structurally_intact": 0.99}},
			want: ai.CheckDecisionAccept,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveCheckAction(tc.out); got != tc.want {
				t.Fatalf("resolveCheckAction = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildDecisionRequestCarriesBriefAndChapter(t *testing.T) {
	jc := &jobContext{
		novel:            &store.Novel{SourceLanguage: "en", TargetLanguage: "es"},
		baseSystemPrompt: "Adapt, do not translate literally.",
		cfg: resolvedJobConfig{
			CheckAI: &store.AISettings{Model: "perplexity/pplx-decider-v1.2"},
		},
	}
	chapter := &store.Chapter{OriginalContent: "Hello world."}

	req := buildDecisionRequest(jc, chapter, "Hola mundo.")

	if req.Model != "perplexity/pplx-decider-v1.2" {
		t.Fatalf("model = %q", req.Model)
	}
	if req.State["translation_brief"] != "Adapt, do not translate literally." {
		t.Fatalf("translation_brief = %v", req.State["translation_brief"])
	}
	if req.State["original"] != "Hello world." || req.State["translation"] != "Hola mundo." {
		t.Fatalf("chapter text missing from state: %+v", req.State)
	}
	if req.State["source_language"] != "en" || req.State["target_language"] != "es" {
		t.Fatalf("languages missing from state: %+v", req.State)
	}
	if got := req.State["refine_capabilities"]; got != refineCapabilities {
		t.Fatalf("refine_capabilities = %v", got)
	}

	action, ok := req.Questions["recommended_action"]
	if !ok || action.Type != "choice" {
		t.Fatalf("recommended_action question missing or wrong type: %+v", action)
	}
	for _, label := range []string{"accept", "refine", "retranslate"} {
		if action.Criteria[label] == "" {
			t.Fatalf("missing criteria for %q", label)
		}
	}
	for _, name := range []string{"structurally_intact", "untranslated_content_present", "has_meaning_error"} {
		q, ok := req.Questions[name]
		if !ok || q.Type != "noul" {
			t.Fatalf("%s question missing or wrong type: %+v", name, q)
		}
		if q.Criteria["true"] == "" || q.Criteria["false"] == "" {
			t.Fatalf("%s missing true/false criteria", name)
		}
	}
}

func TestBuildDecisionRequestFallsBackToDefaultBrief(t *testing.T) {
	jc := &jobContext{novel: &store.Novel{SourceLanguage: "en", TargetLanguage: "es"}}
	req := buildDecisionRequest(jc, &store.Chapter{}, "")
	if req.State["translation_brief"] != defaultTranslationBrief {
		t.Fatalf("expected default brief, got %v", req.State["translation_brief"])
	}
	if req.Model != "" {
		t.Fatalf("expected empty model without CheckAI, got %q", req.Model)
	}
}

func TestCheckChapterWithDecisionMapsAnswers(t *testing.T) {
	checker := &stubDecisionChecker{response: decisionAnswers(ai.CheckDecisionRefine, 0.98, 0.95, 0.1)}
	jc := &jobContext{
		novel:    &store.Novel{SourceLanguage: "en", TargetLanguage: "es"},
		decision: checker,
	}
	out, err := checkChapterWithDecision(context.Background(), jc, &store.Chapter{OriginalContent: "Hello"}, "Hola")
	if err != nil {
		t.Fatalf("checkChapterWithDecision: %v", err)
	}
	if checker.calls != 1 {
		t.Fatalf("expected one Decide call, got %d", checker.calls)
	}
	if out.Decision != ai.CheckDecisionRefine {
		t.Fatalf("unexpected output: %+v", out)
	}
	if out.Confidence != 0.9 {
		t.Fatalf("confidence = %v", out.Confidence)
	}
	if out.Signals["untranslated_content_present"] != 0.95 {
		t.Fatalf("signals = %+v", out.Signals)
	}
}

func TestCheckChapterWithDecisionRejectsIncompleteAnswers(t *testing.T) {
	cases := map[string]ai.DecisionResponse{
		"missing action": {
			Answers: map[string]ai.DecisionAnswer{
				"structurally_intact":          {Noul: 0.9},
				"untranslated_content_present": {Noul: 0.1},
				"has_meaning_error":            {Noul: 0.1},
			},
		},
		"invalid action": decisionAnswers("rewrite", 0.9, 0.1, 0.1),
		"missing signal": {
			Answers: map[string]ai.DecisionAnswer{
				"recommended_action":  {Choice: ai.CheckDecisionAccept},
				"structurally_intact": {Noul: 0.9},
			},
		},
	}
	for name, response := range cases {
		t.Run(name, func(t *testing.T) {
			jc := &jobContext{
				novel:    &store.Novel{SourceLanguage: "en", TargetLanguage: "es"},
				decision: &stubDecisionChecker{response: response},
			}
			if _, err := checkChapterWithDecision(context.Background(), jc, &store.Chapter{}, ""); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestCheckChapterFallsBackToProviderOnDecisionFailure(t *testing.T) {
	provider := &checkStubProvider{checkOutput: ai.CheckOutput{Decision: ai.CheckDecisionAccept}}
	jc := &jobContext{
		runCtx:   context.Background(),
		novel:    &store.Novel{SourceLanguage: "en", TargetLanguage: "es"},
		provider: provider,
		decision: &stubDecisionChecker{err: errors.New("decision unavailable")},
	}

	out, err := (&Server{}).checkChapter(jc.runCtx, jc, &store.Chapter{OriginalContent: "Hello"}, "Hola")
	if err != nil {
		t.Fatalf("checkChapter: %v", err)
	}
	if out.Decision != ai.CheckDecisionAccept {
		t.Fatalf("expected fallback provider output, got %+v", out)
	}
	if provider.checkCalls != 1 {
		t.Fatalf("expected provider check to run once, got %d", provider.checkCalls)
	}
}

func TestCheckChapterUsesDecisionWhenAvailable(t *testing.T) {
	provider := &checkStubProvider{checkOutput: ai.CheckOutput{Decision: ai.CheckDecisionRefine}}
	jc := &jobContext{
		runCtx:   context.Background(),
		novel:    &store.Novel{SourceLanguage: "en", TargetLanguage: "es"},
		provider: provider,
		decision: &stubDecisionChecker{response: decisionAnswers(ai.CheckDecisionAccept, 0.98, 0.05, 0.05)},
	}

	out, err := (&Server{}).checkChapter(jc.runCtx, jc, &store.Chapter{OriginalContent: "Hello"}, "Hola")
	if err != nil {
		t.Fatalf("checkChapter: %v", err)
	}
	if provider.checkCalls != 0 {
		t.Fatalf("expected the chat provider not to be called, got %d calls", provider.checkCalls)
	}
	if out.Decision != ai.CheckDecisionAccept {
		t.Fatalf("expected decision output, got %+v", out)
	}
}

func TestCheckChapterNormalizesChatDecision(t *testing.T) {
	cases := []struct {
		name         string
		decision     string
		wantDecision string
	}{
		{
			name:         "accept is trimmed and lowercased",
			decision:     " ACCEPT ",
			wantDecision: ai.CheckDecisionAccept,
		},
		{
			name:         "refine is trimmed and lowercased",
			decision:     "Refine",
			wantDecision: ai.CheckDecisionRefine,
		},
		{
			name:         "retranslate is kept",
			decision:     "retranslate",
			wantDecision: ai.CheckDecisionRetranslate,
		},
		{
			name:         "missing decision is left for resolveCheckAction",
			decision:     "",
			wantDecision: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provider := &checkStubProvider{checkOutput: ai.CheckOutput{Decision: tc.decision}}
			jc := &jobContext{
				runCtx:   context.Background(),
				novel:    &store.Novel{SourceLanguage: "en", TargetLanguage: "es"},
				provider: provider,
			}
			out, err := (&Server{}).checkChapter(jc.runCtx, jc, &store.Chapter{OriginalContent: "Hello"}, "Hola")
			if err != nil {
				t.Fatalf("checkChapter: %v", err)
			}
			if out.Decision != tc.wantDecision {
				t.Fatalf("Decision = %q, want %q", out.Decision, tc.wantDecision)
			}
		})
	}
}

func TestDecisionQuestionsCoverAllSignals(t *testing.T) {
	for _, name := range []string{"structurally_intact", "untranslated_content_present", "has_meaning_error"} {
		if _, ok := decisionQuestions[name]; !ok {
			t.Fatalf("decisionQuestions is missing %s", name)
		}
	}
	if strings.TrimSpace(decisionQuestions["structurally_intact"].Instructions) == "" {
		t.Fatal("structurally_intact needs instructions")
	}
}

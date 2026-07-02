# Refine Tool-Calling Implementation Plan

## 0. Audience and how to use this document

This plan is written for an engineer (human or AI agent) with no prior
context on the discussion that produced it. Every step names exact files,
exact function names/signatures, and exact verification commands. Follow the
steps in order and run the verification command after each step before
moving on.

This document **replaces** any earlier plan about refine robustness. The
previous draft assumed the current chunked, structured-output design would
stay in place and only patched save-safety. That assumption is wrong per
product decision (section 1) — refine must never chunk the chapter text.
This plan supersedes it entirely, but keeps the one piece of that draft that
is still correct and needed: the optimistic-concurrency-controlled save
(section 5, Step 6).

## 1. Confirmed product decisions (do not re-litigate these)

1. **No segmentation/chunking for refine, ever.** Translation may segment
   chapters; refine must always see the whole chapter's original text and
   the whole current translation in a single call. This is different from
   today's implementation, which splits the translation into 15-line windows
   with 2-line overlap (`buildRefineChunks`) — that machinery is being
   removed entirely for refine.
2. **The LLM proposes edits via a tool call**, not via structured JSON
   output in the response body. The tool receives a batch of
   `{original, replacement}` pairs (no `reason` field — it's never surfaced
   to a human, so it's wasted output tokens).
3. **Apply-first-then-retry-only-failures.** When a tool call arrives with N
   edits, every edit is attempted immediately and independently. Edits that
   succeed are committed right away. Only the edits that failed (text not
   found, or found more than once) are reported back to the model as
   feedback, so it can resend corrected versions — without blocking or
   re-doing the edits that already succeeded.
4. **Hard cap of 5 tool-loop steps** (`MaxSteps = 5`). This is a fixed
   constant, not user-configurable, for now.
5. **Refine must always save, never lose text.** If the model exhausts the
   5 steps with edits still unresolved, or produces no edits at all, the
   chapter is still saved to `refined_content`. Because edits are applied as
   exact string replacements against the current translation, any edit that
   never succeeds simply leaves that span of text as the (already
   correct-shaped) translation — nothing is deleted or blanked out. There is
   no separate "rollback" or "discard" path for unresolved edits.
6. **All configured providers/models are assumed to support tool calling.**
   No fallback to the old structured-output path is needed.
7. **Split responsibilities into dedicated files** for maintainability:
   - `internal/ai/refine.go` — the provider-side tool-calling implementation
     (currently inlined in `internal/ai/openai.go`).
   - `internal/api/refine_apply.go` — the exact-match edit-application logic
     and the closure that bridges it to the AI layer (currently inlined in
     `internal/api/runtime_refine.go`).
   - `internal/api/runtime_refine.go` stays, but shrinks to just job/chapter
     orchestration (the attempts loop, prompt building, saving).

## 2. Non-goals

- No fuzzy/approximate text matching.
- No diff/AST/patch-based editing.
- No undo/snapshot system.
- No changes to the **translate** job path
  (`runTranslateChapterDetailed`, segmentation, `AISettings.Concurrency`).
- No new HTTP endpoints, no frontend changes. Failures still surface via the
  existing `chapter.status = "failed"` + `chapter.errorMessage` fields.
- No per-provider fallback logic for providers that might not support tools.

If you find yourself touching `runTranslateChapterDetailed`, segmentation
code, or anything under `frontend/`, stop — out of scope.

## 3. State/feedback/blast-radius/timing summary (read before coding)

- **State ownership:** `translated_content` in the `chapters` collection is
  the source of truth a refine pass starts from. The in-memory copy taken at
  job start (`chapter.TranslatedContent`) is a snapshot that can go stale if
  a user edits the chapter through the API while the job is still working
  through earlier chapters in the queue (jobs process chapters sequentially,
  see `AGENTS.md`: concurrency is not wired in). `refined_content` is a
  derived field; it is safe to keep overwriting it as long as it's derived
  from a `translated_content` that is still current at write time (this is
  exactly what the OCC guard in Step 6 checks).
- **Feedback:** chapter failures surface today via
  `chapter.status = "failed"` + `chapter.errorMessage`, set by
  `internal/api/runtime_worker.go`'s `processJob` when `runRefineChapter`
  returns a non-nil error. Reuse this channel; do not add a new status value.
  Unresolved edits (decision 5) are **not** a failure — they must not cause
  `runRefineChapter` to return an error. Only log them (`slog.Warn`) for
  observability.
- **Blast radius:** this plan changes the `ai.Provider` interface's `Refine`
  method contract and the `RefineEdit`/`RefineOutput` types. `Provider` has
  exactly one implementation (`OpenAIProvider`) and `Refine` has exactly one
  caller (`runRefineChapter`) — confirmed by repo-wide search. Safe to change
  freely.
- **Timing:** the entire refine attempt for one chapter is now a single
  `Provider.Refine` call (internally, up to 5 tool-loop turns handled by
  `goai`). The check-then-refine retry loop (`jc.cfg.Translation.EnableCheck`,
  `MaxRetries`) is unchanged and sits one level above this — it can call
  `Refine` again with the already-partially-refined text if the check fails.

## 4. Prompt/backward-compatibility note

`internal/store/prompt_defaults.go` ships default system/user prompts for
refine, but users can override them per-novel or globally (see
`router_novels.go`, `RefineSystemPrompt`/`RefineUserPrompt`). Existing
overrides may reference chunk-only placeholders that are going away
conceptually (`{TRANSLATION_CHUNK}`, `{ORIGINAL_CHUNK}`, `{TRANSLATION_CONTEXT}`,
`{START_LINE}`, `{END_LINE}`). To avoid silently breaking a user's saved
custom prompt (turning those placeholders into blank text), **keep filling
them as aliases** that resolve to the whole chapter, in addition to
introducing `{TRANSLATION}` as the primary placeholder going forward. See
Step 5 for the exact mapping.

## 5. Implementation steps

### Step 1 — Update `internal/ai/provider.go` types

Replace the `RefineInput`, `RefineEdit`, `RefineOutput` types and leave the
`Provider` interface line for `Refine` unchanged in shape:

```go
type Provider interface {
	TranslateTitle(ctx context.Context, input TranslateTitleInput) (string, error)
	TranslateText(ctx context.Context, input TranslateTextInput) (string, error)
	Refine(ctx context.Context, input RefineInput) (RefineOutput, error)
	Check(ctx context.Context, input CheckInput) (CheckOutput, error)
}

// RefineInput carries a fully-rendered system/user prompt pair (built by
// internal/api from the novel's configured refine prompt template) plus a
// callback the provider must invoke whenever the model calls the apply_edits
// tool. ApplyEdits must be called with every edit from a single tool call in
// one invocation; the caller (internal/api) applies them immediately and
// returns per-edit results so the provider can report only the failures back
// to the model.
type RefineInput struct {
	SystemPrompt   string
	UserPrompt     string
	SourceLanguage string
	TargetLanguage string
	ApplyEdits     func(edits []RefineEdit) []RefineEditResult
}

// RefineEdit is what the model submits via the apply_edits tool. There is no
// "reason" field: it is never shown to a user, so asking the model to
// produce it only spends output tokens for no benefit.
type RefineEdit struct {
	Original    string `json:"original"`
	Replacement string `json:"replacement"`
}

// RefineEditResult is the outcome of attempting one RefineEdit. Reason is
// for internal logging and for building the model-facing feedback message;
// it is never sent back to the model as structured data, only folded into a
// free-text tool result string.
type RefineEditResult struct {
	Edit    RefineEdit
	Applied bool
	Reason  string // one of: not_found, multiple_matches, no_op, empty_original ("" when Applied)
}

// RefineOutput summarizes one Refine call across however many tool-loop
// turns it took. Unresolved holds whatever edits were still failing the last
// time the tool was called (empty if everything succeeded or the model never
// called the tool).
type RefineOutput struct {
	TotalProposed int
	TotalApplied  int
	Unresolved    []RefineEdit
}
```

**Verification:** `cd yara && go build ./internal/ai/...` will fail at this
point because `openai.go` still references the old fields — that's expected,
fix it in Step 2.

### Step 2 — Remove the old `Refine` method from `internal/ai/openai.go`

Delete this whole method from `openai.go` (it moves to the new file in Step 3):

```go
func (p *OpenAIProvider) Refine(ctx context.Context, in RefineInput) (RefineOutput, error) {
	model, err := p.model()
	if err != nil {
		return RefineOutput{}, err
	}
	opts := append(p.opts(),
		goai.WithPrompt(in.TranslatedText),
		goai.WithTimeout(p.resolveTimeout()),
	)
	result, err := goai.GenerateObject[RefineOutput](ctx, model, opts...)
	if err != nil {
		return RefineOutput{}, fmt.Errorf("openai refine: %w", err)
	}
	return result.Object, nil
}
```

Leave `TranslateTitle`, `TranslateText`, `Check`, `model()`, `opts()`,
`textOpts()`, `resolveTimeout()` untouched in this file.

### Step 3 — Create `internal/ai/refine.go`

New file, same package (`ai`):

```go
package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zendev-sh/goai"
)

// refineMaxSteps caps how many apply_edits tool-loop turns a single Refine
// call can take. This is a product decision, not user-configurable.
const refineMaxSteps = 5

const refineApplyEditsSchema = `{
  "type": "object",
  "properties": {
    "edits": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "original": {
            "type": "string",
            "description": "Text copied exactly, character for character, from the current translation. Must occur exactly once."
          },
          "replacement": {
            "type": "string",
            "description": "The corrected replacement text."
          }
        },
        "required": ["original", "replacement"]
      }
    }
  },
  "required": ["edits"]
}`

// Refine runs a single tool-calling pass over the whole chapter. The model
// is given the apply_edits tool and up to refineMaxSteps turns to converge:
// each call applies every edit it can and reports back only the ones that
// failed, so the model can retry just those without redoing work that
// already succeeded.
func (p *OpenAIProvider) Refine(ctx context.Context, in RefineInput) (RefineOutput, error) {
	model, err := p.model()
	if err != nil {
		return RefineOutput{}, err
	}

	var summary RefineOutput
	applyEditsTool := goai.Tool{
		Name: "apply_edits",
		Description: "Apply a batch of exact-text replacements to the current translation. " +
			"Every edit is attempted independently in the order given — one failing edit never " +
			"blocks the others from being applied. If some edits fail, call this tool again with " +
			"corrected versions of only the failed edits.",
		InputSchema: json.RawMessage(refineApplyEditsSchema),
		Execute: func(_ context.Context, input json.RawMessage) (string, error) {
			var args struct {
				Edits []RefineEdit `json:"edits"`
			}
			if err := json.Unmarshal(input, &args); err != nil {
				return "", fmt.Errorf("invalid apply_edits payload: %w", err)
			}
			results := in.ApplyEdits(args.Edits)
			summary.TotalProposed += len(args.Edits)

			var unresolved []RefineEdit
			var feedback strings.Builder
			appliedNow := 0
			for _, r := range results {
				if r.Applied {
					appliedNow++
					summary.TotalApplied++
					continue
				}
				unresolved = append(unresolved, r.Edit)
				fmt.Fprintf(&feedback, "- FAILED (%s): %q\n", r.Reason, truncateForFeedback(r.Edit.Original))
			}
			summary.Unresolved = unresolved

			if len(unresolved) == 0 {
				return fmt.Sprintf("Applied %d/%d edits. All edits in this batch succeeded.", appliedNow, len(results)), nil
			}
			return fmt.Sprintf(
				"Applied %d/%d edits. %d failed and were NOT applied:\n%sResend corrected versions of only the failed edits, copied exactly from the current translation.",
				appliedNow, len(results), len(unresolved), feedback.String(),
			), nil
		},
	}

	// Use textOpts (not opts): this is a GenerateText + tool-loop call, not
	// GenerateObject, so provider options like strictJsonSchema don't apply
	// (see TranslateText for the same distinction).
	opts := append(p.textOpts(),
		goai.WithSystem(in.SystemPrompt),
		goai.WithPrompt(in.UserPrompt),
		goai.WithTools(applyEditsTool),
		goai.WithMaxSteps(refineMaxSteps),
		goai.WithTimeout(p.resolveTimeout()),
	)
	if _, err := goai.GenerateText(ctx, model, opts...); err != nil {
		return summary, fmt.Errorf("openai refine: %w", err)
	}
	return summary, nil
}

func truncateForFeedback(s string) string {
	const maxLen = 200
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "…"
}
```

**Verification:** `cd yara && go build ./internal/ai/...` must succeed.

### Step 4 — Update `internal/store/prompt_defaults.go`

Replace `DefaultRefineSystemPrompt` and `DefaultRefineUserPrompt`:

```go
const DefaultRefineSystemPrompt = `You are a translation quality reviewer and literary editor refining a machine translation.

You do not rewrite the whole chapter. You call the apply_edits tool with precise, surgical corrections.

Guidelines:
- Fix translation errors, missing meaning, incorrect terminology, grammar issues, and clearly awkward phrasing.
- Preserve meaning, narrative voice, tone, paragraph structure, and character names.
- Respect required terminology from the glossary.
- Do not over-normalize valid stylistic choices. If a sentence is accurate and natural enough, leave it unchanged.
- Each edit's "original" must be a complete sentence or complete line copied exactly, character for character, from the current translation. It must occur exactly once.
- If you cannot find a complete sentence or line that matches exactly, do not propose that edit.
- Call apply_edits with all the edits you have ready. If some are reported as failed, resend corrected versions of only those — do not resend edits that already succeeded.
- When you have no more corrections to make, stop calling the tool.

Source language: {SOURCE_LANG}
Target language: {TARGET_LANG}

Glossary:
{GLOSSARY}`

const DefaultRefineUserPrompt = `Original ({SOURCE_LANG}):
{ORIGINAL}

Current translation ({TARGET_LANG}):
{TRANSLATION}

Review the current translation against the original and call apply_edits with any corrections needed. If no corrections are needed, do not call the tool.`
```

Do not touch `DefaultTranslationSystemPrompt`, `DefaultTranslationUserPrompt`,
`DefaultCheckSystemPrompt`, `DefaultCheckUserPrompt`, or anything else in
this file.

**Verification:** `cd yara && go build ./internal/store/...` must succeed.

### Step 5 — Create `internal/api/refine_apply.go`

New file, package `api`. This holds the exact-match edit-application core
(same matching rules as before: empty/no-op edits are skipped, an edit is
only applied if its `original` occurs exactly once in the current text) plus
the closure that bridges it to `ai.RefineInput.ApplyEdits`:

```go
package api

import (
	"log/slog"
	"strings"

	"translator-server/internal/ai"
)

const (
	reasonEmptyOriginal   = "empty_original"
	reasonNoOp            = "no_op"
	reasonNotFound        = "not_found"
	reasonMultipleMatches = "multiple_matches"
)

// applyRefineEdits attempts every edit against text, in order. Each
// successful replacement commits immediately, so a later failing edit never
// blocks or rolls back edits that already succeeded. It returns the updated
// text and one result per input edit (same order), so the caller can build
// feedback for only the failures.
func applyRefineEdits(text string, edits []ai.RefineEdit) (string, []ai.RefineEditResult) {
	results := make([]ai.RefineEditResult, 0, len(edits))
	for _, edit := range edits {
		original := trimEditBoundaryNewlines(edit.Original)
		replacement := trimEditBoundaryNewlines(edit.Replacement)

		if original == "" {
			results = append(results, ai.RefineEditResult{Edit: edit, Reason: reasonEmptyOriginal})
			continue
		}
		if original == replacement {
			results = append(results, ai.RefineEditResult{Edit: edit, Reason: reasonNoOp})
			continue
		}

		count := strings.Count(text, original)
		if count == 0 {
			results = append(results, ai.RefineEditResult{Edit: edit, Reason: reasonNotFound})
			continue
		}
		if count > 1 {
			results = append(results, ai.RefineEditResult{Edit: edit, Reason: reasonMultipleMatches})
			continue
		}

		text = strings.Replace(text, original, replacement, 1)
		results = append(results, ai.RefineEditResult{Edit: edit, Applied: true})
	}
	return text, results
}

func trimEditBoundaryNewlines(text string) string {
	return strings.Trim(text, "\r\n")
}

// newApplyEditsFunc returns a closure suitable for ai.RefineInput.ApplyEdits.
// It mutates *buffer in place on every call, so edits applied by an earlier
// tool call in the same Refine attempt are visible to later ones (e.g. an
// edit whose "original" text only exists after a previous edit already
// applied). chapterID is only used for log correlation.
func newApplyEditsFunc(buffer *string, chapterID string) func(edits []ai.RefineEdit) []ai.RefineEditResult {
	return func(edits []ai.RefineEdit) []ai.RefineEditResult {
		updated, results := applyRefineEdits(*buffer, edits)
		*buffer = updated

		failed := 0
		for _, r := range results {
			if !r.Applied {
				failed++
			}
		}
		if failed > 0 {
			slog.Warn("refine edits skipped", "chapterId", chapterID, "failed", failed, "total", len(results))
		}
		return results
	}
}
```

**Verification:** `cd yara && go build ./internal/api/...` will still fail
at this point because `runtime_refine.go` hasn't been updated yet — expected,
fixed in Step 7.

### Step 6 — Add the optimistic-concurrency-guarded save to the store

File: `internal/store/store_chapters.go`. Add next to
`SaveChapterTranslationFast` (do not modify `SaveChapterTranslation`,
`SaveChapterTranslationFast`, or `saveChapterTranslation` — they're still
used by the translate path):

```go
// SaveRefinedContentIfUnchanged writes refinedContent (and status) only if
// the chapter's translated_content still equals expectedTranslatedContent.
// It returns applied=false, nil (no write, no error) if translated_content
// has changed since the caller last read it — e.g. a user edited the
// chapter through the API while a refine job was working from a stale
// in-memory copy. This prevents a slow-running refine job from clobbering a
// newer edit.
func (s *Store) SaveRefinedContentIfUnchanged(chapterID, expectedTranslatedContent, refinedContent, status string) (applied bool, err error) {
	record, err := s.App.FindRecordById(ChaptersCollection, chapterID)
	if err != nil {
		return false, ErrNotFound
	}
	if record.GetString("translated_content") != expectedTranslatedContent {
		return false, nil
	}
	if refinedContent != "" {
		record.Set("refined_content", refinedContent)
	}
	if status != "" {
		record.Set("status", status)
	}
	record.Set("error_message", "")
	setCharCounts(record, record.GetString("original_content"), record.GetString("translated_content"), record.GetString("refined_content"))
	if err := s.App.Save(record); err != nil {
		return false, err
	}
	if err := s.RecalculateNovelStats(record.GetString("novel")); err != nil {
		return false, err
	}
	return true, nil
}
```

**Verification:** `cd yara && go build ./internal/store/...` must succeed.

### Step 7 — Rewrite `internal/api/runtime_refine.go`

This file shrinks: chunking (`buildRefineChunks`, `refineChunk` usage,
`splitLines`, `applyRefineEdits`) moves out or is deleted entirely. What
remains is job/chapter orchestration:

1. Delete `buildRefineChunks` and `splitLines` from this file entirely (they
   are not used anywhere else — confirmed by repo-wide search before writing
   this plan).
2. Delete the `refineChunk` type from `internal/api/runtime_types.go`
   entirely (also not used anywhere else).
3. Delete `applyRefineEdits` and `trimEditBoundaryNewlines` from this file —
   they now live in `internal/api/refine_apply.go` (Step 5).
4. Rewrite `buildRefinePrompt` to work on the whole chapter instead of a
   chunk, and to return the system and user prompt separately (matching how
   `buildCheckPrompt` already works):

```go
func buildRefinePrompt(cfg resolvedJobConfig, novel *store.Novel, chapter *store.Chapter, current string) (systemPrompt, userPrompt string) {
	glossaryText := formatGlossary(cfg.Glossary)
	lineCount := strings.Count(current, "\n") + 1
	values := map[string]string{
		"{SOURCE_LANG}": novel.SourceLanguage,
		"{TARGET_LANG}": novel.TargetLanguage,
		"{GLOSSARY}":    glossaryText,
		"{ORIGINAL}":    chapter.OriginalContent,
		"{TRANSLATION}": current,
		// Legacy placeholders from the removed chunked implementation.
		// Refine no longer chunks text, so these resolve to the whole
		// chapter for backward compatibility with prompts a user may have
		// customized before this change.
		"{ORIGINAL_CHUNK}":      chapter.OriginalContent,
		"{TRANSLATION_CHUNK}":   current,
		"{TRANSLATION_CONTEXT}": current,
		"{START_LINE}":          "1",
		"{END_LINE}":            strconv.Itoa(lineCount),
	}
	systemPrompt = strings.TrimSpace(fillPrompt(cfg.Prompts.Refine.SystemPrompt, values))
	userPrompt = strings.TrimSpace(fillPrompt(cfg.Prompts.Refine.UserPrompt, values))
	return systemPrompt, userPrompt
}
```

Add `"strconv"` to this file's imports.

5. Rewrite `runRefineChapter`:

```go
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
	prevOrig, prevTranslated := "", ""
	if jc.cfg.IncludePrevTitle && idx > 0 {
		prevOrig = jc.chapters[idx-1].Title
		prevTranslated = jc.chapters[idx-1].TranslatedTitle
	}

	for attempt := 1; attempt <= attempts; attempt++ {
		if jc.cfg.Translation.EnableCheck {
			checkPrompt := buildCheckPrompt(jc.cfg, jc.novel, chapter, prevOrig, prevTranslated, current)
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

// saveRefinedChapter persists the refined text. It always saves — unresolved
// edits are not a failure, they simply leave that span of text as the
// existing translation (decision: refine must never lose text). The only
// case this returns an error is the OCC guard: translated_content changed
// under the job (e.g. edited via the API) since baseline was captured.
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
```

`buildCheckPrompt` is unchanged — leave it exactly as is.

**Verification:** `cd yara && go build ./...` must succeed with no errors.

### Step 8 — Update `internal/api/refine_test.go`

Remove `TestBuildRefineChunksUsesEditableLinesWithOverlap` entirely (the
function it tests no longer exists). Rewrite
`TestApplyRefineEditsOnlyAppliesExactUniqueMatches` for the new
`applyRefineEdits` signature (now in `refine_apply.go`, same package) and add
coverage for the "apply first, only report failures" contract:

```go
package api

import (
	"testing"

	"translator-server/internal/ai"
)

func TestApplyRefineEditsCommitsSuccessesAndReportsOnlyFailures(t *testing.T) {
	text := "La casa era roja.\nLa casa era roja.\nEl cielo era azul."
	edits := []ai.RefineEdit{
		{Original: "El cielo era azul.", Replacement: "El cielo estaba azul."},
		{Original: "La casa era roja.", Replacement: "La casa estaba roja."}, // matches twice: must fail
		{Original: "No existe.", Replacement: "No existía."},                // not found: must fail
	}

	updated, results := applyRefineEdits(text, edits)

	want := "La casa era roja.\nLa casa era roja.\nEl cielo estaba azul."
	if updated != want {
		t.Fatalf("unexpected text:\nwant: %q\n got: %q", want, updated)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if !results[0].Applied {
		t.Fatalf("expected first edit to be applied, got reason %q", results[0].Reason)
	}
	if results[1].Applied || results[1].Reason != reasonMultipleMatches {
		t.Fatalf("expected second edit to fail as multiple_matches, got %+v", results[1])
	}
	if results[2].Applied || results[2].Reason != reasonNotFound {
		t.Fatalf("expected third edit to fail as not_found, got %+v", results[2])
	}
}

func TestNewApplyEditsFuncMutatesBufferAcrossCalls(t *testing.T) {
	buffer := "one\ntwo\nthree"
	apply := newApplyEditsFunc(&buffer, "chapter-1")

	results := apply([]ai.RefineEdit{{Original: "two", Replacement: "TWO"}})
	if !results[0].Applied {
		t.Fatalf("expected edit to apply, got %+v", results[0])
	}
	if buffer != "one\nTWO\nthree" {
		t.Fatalf("expected buffer to be mutated, got %q", buffer)
	}

	// A second call must see the mutation from the first call.
	results = apply([]ai.RefineEdit{{Original: "TWO", Replacement: "2"}})
	if !results[0].Applied {
		t.Fatalf("expected second edit to apply against mutated buffer, got %+v", results[0])
	}
	if buffer != "one\n2\nthree" {
		t.Fatalf("expected buffer to reflect both edits, got %q", buffer)
	}
}
```

**Verification:** `cd yara && go test ./internal/api/... -run TestApplyRefineEdits -v && go test ./internal/api/... -run TestNewApplyEditsFunc -v`
must pass.

### Step 9 — Add a store-level test for the OCC guard

File: `internal/store/store_test.go` — reuse whatever test-store bootstrap
helper this file already uses (check its top before writing new setup code).

Add a test that:

1. Creates a novel and a chapter with `translated_content = "original translation"`.
2. Calls `SaveRefinedContentIfUnchanged(chapterID, "original translation", "refined text", "refined")`, asserts `applied == true`, `err == nil`, and that re-fetching the chapter shows `refined_content == "refined text"` and `status == "refined"`.
3. Changes the same chapter's `translated_content` (simulating a concurrent user edit) to `"edited by user"` via whatever store method already exists for that (check `UpsertChapter` or the chapter update path used by the HTTP layer).
4. Calls `SaveRefinedContentIfUnchanged(chapterID, "original translation", "should not be saved", "refined")` (stale baseline) and asserts `applied == false`, `err == nil`, and that `refined_content` is unchanged from step 2 (i.e. `"refined text"`, not `"should not be saved"`).

**Verification:** `cd yara && go test ./internal/store/... -run TestSaveRefinedContentIfUnchanged -v` passes.

### Step 10 — (Recommended) Add a fake `ai.Provider` to test `runRefineChapter` end-to-end without a real LLM

`ai.Provider` is already an interface with exactly one production
implementation, which makes this cheap. Add to `internal/api/refine_test.go`
(or a new `runtime_refine_test.go` next to `runtime_refine.go` if building a
`*Server`/`jobContext` needs imports not already present in `refine_test.go`
— check `router_integration_test.go` for the `newAPITestEnv` helper and
follow its existing pattern for constructing a real `*Server` backed by
PocketBase in `t.TempDir()`):

```go
type fakeRefineProvider struct {
	ai.Provider // embed to satisfy the interface; only Refine/Check need real behavior
	refineFunc  func(ai.RefineInput) (ai.RefineOutput, error)
	checkFunc   func(ai.CheckInput) (ai.CheckOutput, error)
}

func (f *fakeRefineProvider) Refine(_ context.Context, in ai.RefineInput) (ai.RefineOutput, error) {
	return f.refineFunc(in)
}

func (f *fakeRefineProvider) Check(_ context.Context, in ai.CheckInput) (ai.CheckOutput, error) {
	if f.checkFunc == nil {
		return ai.CheckOutput{OK: true}, nil
	}
	return f.checkFunc(in)
}
```

Use it to verify, without any network call:

- A `Refine` call that invokes `in.ApplyEdits` with a mix of matching and
  non-matching edits results in the matching ones being persisted to
  `refined_content` and the chapter status ending as `"refined"` (not
  `"failed"`) even when some edits are unresolved.
- A `Refine` call whose `ApplyEdits` is never called (model produced no
  edits) still results in the chapter being saved with
  `refined_content == translated_content`.
- If the chapter's `translated_content` is changed in the store between
  loading the chapter and calling `runRefineChapter`, the chapter ends with
  `status == "failed"` and `refined_content` is not written.

If wiring a full `jobContext` requires substantial unrelated scaffolding
(AI settings resolution, prompts, etc.) beyond what `router_integration_test.go`
already provides as reusable helpers, it's acceptable to skip this step and
rely on Steps 8-9 alone — note that trade-off explicitly instead of building
a large new test harness just for this.

## 6. Explicitly out of scope

- Fuzzy matching, regex, or AST-based edits — rejected for the same reason
  as before: it would make "which text gets replaced" unpredictable, which
  directly undermines the "exact and safe" guarantee this design relies on.
- Undo/snapshot history for refined content — no product requirement exists;
  would need new schema and endpoints.
- Applying the OCC guard to the translate path — different race shape
  (nobody is expected to hand-edit `translated_content` while it's still
  being generated for the first time); treat as a separate follow-up if ever
  needed.
- Making `refineMaxSteps` user-configurable — ship as a constant; revisit
  only if there's a concrete product need.
- Per-provider capability detection / fallback to structured-output refine —
  not needed per decision 6.

## 7. Rollout notes

- No schema migration needed — `SaveRefinedContentIfUnchanged` reads/writes
  fields that already exist on the `chapters` collection.
- No API contract change — job/chapter HTTP responses are unaffected.
- Existing per-novel or global custom refine prompts keep working (Step 7's
  legacy placeholder aliases), though they will now receive the whole
  chapter instead of a 15-line window — if a user wrote a prompt that
  explicitly says "only edit the shown chunk," that instruction becomes
  moot/no-op since there's no longer a chunk boundary; it will not break
  anything, just no longer be operative.
- After this change, a refine job report of "chapter X was edited while
  refinement was running" means: re-run the refine job for that novel: no
  manual data repair needed, nothing was overwritten.
- A refine job that completes with `slog.Warn "refine finished with
  unresolved edits"` in the logs is expected and not a failure — the chapter
  still saves and reaches `status = "refined"`; those specific edits just
  didn't get applied (the corresponding text remains as the existing
  translation).

## 8. Final verification checklist

Run from the repository root (`yara/`) after completing all steps:

```
go build ./...
go vet ./...
go test ./internal/ai/... -v
go test ./internal/store/... -run TestSaveRefinedContentIfUnchanged -v
go test ./internal/api/... -run TestApplyRefineEdits -v
go test ./internal/api/... -run TestNewApplyEditsFunc -v
go test -short ./...
```

All must pass before considering this plan complete.

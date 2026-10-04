package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"translator-server/internal/ai"
	"translator-server/internal/store"
)

// Limits bounding one agent chat turn. Tool results are additionally
// truncated inside internal/ai before they reach the model.
const (
	agentMaxMessageChars    = 8000
	agentMaxHistoryMessages = 60
	// agentMaxHistoryBytes bounds the JSON-ENCODED trail, which is what the
	// agent_sessions.messages field cap measures. It is deliberately under
	// store's field max (1M) so a trail always saves.
	agentMaxHistoryBytes        = 800_000
	agentListNovelsDefaultLimit = 20
	agentListNovelsMaxLimit     = 50
	agentChaptersDefaultLimit   = 50
	agentChaptersMaxLimit       = 200
	agentChapterContentMaxChars = 8000
)

var errAgentUnsupportedProvider = errors.New("the configured AI provider does not support agent chat")

// agentSystemPrompt anchors the assistant: what it can see (its tools), when
// it may write, and how to behave. The selected-novel block is appended per
// turn when the chat has one picked, so history stays free of context noise.
const agentSystemPrompt = `You are the library assistant of Yara, a self-hosted app for reading and translating literary novels. You help the user inspect and maintain their novel library and its chapters.

Rules:
- Reply in the same language the user writes in. Be concise and concrete.
- Format replies as plain markdown for a renderer with no math support: never use LaTeX math notation ($...$, $\rightarrow$, \times); write Unicode symbols instead (→, ×, ≈).
- Every tool works ONLY on novels the user owns. Other users' novels, chapters, sessions, accounts and settings are not reachable by any tool, and no SQL query can read them. If asked about them, say the assistant can only see the user's own library.
- Never invent library data. Use the tools to look up novels, chapters and stats; cite novel ids and chapter orders when reporting results.
- list_novels returns hasDescription for the target (user-facing) description only, plus hasSourceDescription for the original-language one. Its optional field scopes the query to one kind of match: title, author, series or tags (e.g. field=tags answers "which novels carry tag X"). Page with offset when more novels match than fit on one page — do not report one page as the whole list. For a filter over BOTH descriptions at once, query_library is cheaper.
- Catalog questions: list_tags lists the user's tags; list_authors lists distinct authors matching a PARTIAL name (use it before listing novels when the user half-remembers a name — "Cris" also finds "TM Cris"); list_series aggregates chapter progress per series (complete=complete answers "which series are fully translated"). Then point at the novels with list_novels and the matching field.
- "Being translated" can mean two things: novels with active translate/refine jobs (get_active_jobs) or novels with unfinished chapters (query_library: chapters_pending > 0). Answer with the one the user means, or both when unclear.
- Jobs run in the BACKGROUND: create_job (translate/refine/check) and update_novel_from_url enqueue work and return a jobId; report it and do not poll in loops — check get_active_jobs or get_novel_jobs once per user message. cancel_job and retry_job manage them. Downloads never go through create_job: update_novel_from_url diffs the source TOC (check_novel_updates is its read-only dry run) and enqueues the download. To re-translate chapters flagged by a query_library analysis (e.g. translations 40% shorter than the original), pass their chapter_ids to create_job — the job overwrites the previous translation.
- Excluded chapters are logically DELETED: omit them from every analysis, count and chapter listing by default (in query_library that means is_excluded = 0). At most mention how many exist; surface the chapters themselves only when the user explicitly asks about excluded ones. set_chapter_excluded and bulk_set_chapter_excluded control the flag.
- Reading a chapter body requires get_chapter with content set to original, translated or refined; summaries from get_novel_chapters never include the body. A negative startLine counts from the end (startLine -10, lineCount 10 returns the last 10 lines), and lineCount 1 is a cheap probe that reveals a chapter's exact length (totalLines) without pulling its text.
- search_chapters looks inside chapter titles AND bodies of one novel (literal text match, returns snippets); use it to locate where something is said before reading a whole chapter.
- query_library runs ONE read-only analytics SELECT over the library progress views — the cheapest way to answer aggregate questions ("which novels are missing fewer than 10 chapters to be complete", counts, filters, rankings). Chapter bodies are not in SQL; use get_chapter for those. For whole-library sweeps, run a COUNT(*) first and then SELECT ... LIMIT 200 OFFSET k, raising k each call until nothing is left — never present a truncated page as the complete answer.
- Writing tools (update_novel, update_chapter, set_chapter_status, set_chapter_excluded, bulk_set_chapter_status, bulk_set_chapter_excluded, update_glossary, apply_chapter_cleanup, set_reading_progress) apply immediately to novels the user owns. Only call them when the user clearly asked for a change, never for exploratory suggestions; after applying, tell the user exactly what changed. update_chapter replaces the whole content field, so read the current text with get_chapter first when the user asks for modifications. update_novel can also edit targetAuthor, targetSeries, tags and the novel status (ongoing, completed, hiatus, cancelled); source-side metadata stays untouched. While a novel has active download/translation jobs, chapter and glossary writes are refused — say so instead of retrying.
- bulk_set_chapter_status and bulk_set_chapter_excluded take fromOrder AND toOrder (inclusive) and change whole ranges in one call; they skip excluded and processing chapters.
- The glossary tools read (get_glossary), edit (update_glossary: upsert/remove by source term) and regenerate (generate_glossary enqueues a job) a novel's term pairs.
- For cleanups ALWAYS run preview_chapter_cleanup first, show the user the sample changes, and only call apply_chapter_cleanup after they agreed; both process at most 25/100 chapters per call.
- get_reading_progress and set_reading_progress report and update where the user is in a novel. list_novel_epubs and build_epub manage the stored EPUB exports; build_epub refuses very large novels — suggest the export UI instead.
- translate_novel_description returns a translated synopsis WITHOUT saving it; apply it with update_novel (targetDescription) when the user wants it stored.
- Valid chapter statuses: pending, translated, refined, done, failed.
- When the user's request is ambiguous because several novels or chapters match (e.g. two novels share a title), call ask_user with the candidates as clickable options instead of asking in prose: label is the human-readable choice (e.g. "The Guardian (de Evil_Warlord)"), value is the exact text their click will send (the id). ask_user must be the only tool call in that step; the picked value arrives as the user's next message.
- The user's selected novel (when present in this prompt) is the default subject of their questions; still use its id with the tools.

Tool args are JSON objects. When several small lookups would answer the question, you may call several tools across steps before answering.`

// agentSelectedNovelContext renders the per-turn selected novel block for the
// system prompt.
func agentSelectedNovelContext(novel *store.Novel) string {
	title := novel.TargetTitle
	if strings.TrimSpace(title) == "" {
		title = novel.SourceTitle
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Selected novel: %q (id: %s)", title, novel.ID)
	if author := novel.TargetAuthor; strings.TrimSpace(author) != "" {
		fmt.Fprintf(&b, ", author: %s", author)
	}
	if status := novel.Status; strings.TrimSpace(status) != "" {
		fmt.Fprintf(&b, ", status: %s", status)
	}
	b.WriteString(". Use this id with the tools unless the user names another novel.")
	return b.String()
}

// resolveAgentRunner builds the user's configured AI provider and returns it
// as an ai.AgentProvider. Only OpenAI-compatible providers implement the tool
// loop (the Google provider does not), so the type assertion is the
// capability gate.
func (s *Server) resolveAgentRunner(userID string) (ai.AgentProvider, error) {
	appSettings, err := s.Store.GetAppSettings(userID)
	if err != nil {
		return nil, fmt.Errorf("get app settings: %w", err)
	}
	providerKey := strings.TrimSpace(appSettings.AI.Provider)
	if providerKey == "" {
		providerKey = store.DefaultAISettings.Provider
	}
	settings, err := s.Store.ResolveProviderAISettings(userID, providerKey)
	if err != nil {
		return nil, fmt.Errorf("resolve provider AI settings: %w", err)
	}
	if strings.TrimSpace(settings.Model) == "" {
		if info, ok := ai.ProviderByID(settings.Provider); ok {
			settings.Model = info.DefaultModel
		}
	}
	provider, err := s.newAIProvider(settings, "")
	if err != nil {
		return nil, err
	}
	agent, ok := provider.(ai.AgentProvider)
	if !ok {
		return nil, errAgentUnsupportedProvider
	}
	return agent, nil
}

// agentTools builds the model-callable tool catalog for one user. Every tool
// is a thin wrapper over the store scoped to userID, so the model can never
// see or touch another user's library.
func (s *Server) agentTools(userID string) []ai.AgentTool {
	return []ai.AgentTool{
		// Catalog questions
		s.agentToolListNovels(userID),
		s.agentToolListTags(userID),
		s.agentToolListAuthors(userID),
		s.agentToolListSeries(userID),
		s.agentToolGetNovel(userID),
		s.agentToolGetNovelStats(userID),
		s.agentToolGetNovelChapters(userID),
		s.agentToolGetChapter(userID),
		s.agentToolSearchChapters(userID),
		s.agentToolQueryLibrary(userID),
		// Jobs
		s.agentToolGetActiveJobs(userID),
		s.agentToolGetNovelJobs(userID),
		s.agentToolCreateJob(userID),
		s.agentToolCancelJob(userID),
		s.agentToolRetryJob(userID),
		// Glossary
		s.agentToolGetGlossary(userID),
		s.agentToolUpdateGlossary(userID),
		s.agentToolGenerateGlossary(userID),
		// Source updates
		s.agentToolCheckNovelUpdates(userID),
		s.agentToolUpdateNovelFromURL(userID),
		// Cleanup
		s.agentToolPreviewChapterCleanup(userID),
		s.agentToolApplyChapterCleanup(userID),
		// Reading progress
		s.agentToolGetReadingProgress(userID),
		s.agentToolSetReadingProgress(userID),
		// EPUB exports
		s.agentToolListNovelEpubs(userID),
		s.agentToolBuildEpub(userID),
		// Description translation
		s.agentToolTranslateNovelDescription(userID),
		// Writes
		s.agentToolUpdateNovel(userID),
		s.agentToolUpdateChapter(userID),
		s.agentToolSetChapterStatus(userID),
		s.agentToolSetChapterExcluded(userID),
		s.agentToolBulkSetChapterStatus(userID),
		s.agentToolBulkSetChapterExcluded(userID),
		agentToolAskUser(),
	}
}

// agentAskUserToolName is the terminal tool the model calls to surface a
// multiple-choice question. The router maps its tool_call event to a
// "question" event the frontend renders as clickable options; the picked
// option's value arrives as the user's next message.
const agentAskUserToolName = "ask_user"

// agentToolAskUser touches no store data, so it needs no userID. Terminal:
// AgentChat ends the turn on this call; Execute only produces the tool result
// the model reads on the next turn, next to the user's picked answer.
func agentToolAskUser() ai.AgentTool {
	return ai.AgentTool{
		Name:        agentAskUserToolName,
		Description: "Ask the user to pick between options when their request is ambiguous (e.g. several novels match a title). The question renders as clickable buttons and the clicked option's value arrives as the user's next message. Call it alone, without any other tool in the same step, and only when the difference matters to answer.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "question": {"type": "string", "description": "Short question shown above the options."},
    "options": {
      "type": "array",
      "minItems": 2,
      "maxItems": 8,
      "items": {
        "type": "object",
        "properties": {
          "label": {"type": "string", "description": "Human-readable choice, e.g. \"The Guardian (de Evil_Warlord)\"."},
          "value": {"type": "string", "description": "Exact message sent when the option is clicked, e.g. the novel id."}
        },
        "required": ["label", "value"]
      }
    }
  },
  "required": ["question", "options"]
}`),
		Terminal: true,
		Execute: func(_ context.Context, _ json.RawMessage) (string, error) {
			return `{"awaitingUser":true,"note":"the user's picked answer arrives in their next message"}`, nil
		},
	}
}

func marshalToolResult(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (s *Server) agentToolListNovels(userID string) ai.AgentTool {
	return ai.AgentTool{
		Name:        "list_novels",
		Description: "List the user's OWN novels, newest first. Set query to search, and field to scope that search to one kind of match: title, author, series or tags (partial matching, e.g. field=author with \"Cris\" finds \"TM Cris\" too; field=tags answers which novels carry a tag). Returns light records: hasDescription tells you whether the target (user-facing) description is empty, and hasSourceDescription whether the original-language description is. Page with offset when more novels match than fit on one page; for a filter over BOTH descriptions at once, query_library is cheaper. The assistant only ever sees novels the user owns, never other users' public novels.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "description": "Optional search text."},
    "field": {"type": "string", "enum": ["all", "title", "author", "series", "tags"], "description": "Scope the query to one field (default all)."},
    "limit": {"type": "integer", "minimum": 1, "maximum": 50, "description": "Novels per page (default 20)."},
    "offset": {"type": "integer", "minimum": 0, "description": "Novels to skip before the page starts (default 0). Combine with limit to page through long result sets."}
  }
}`),
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var a struct {
				Query  string `json:"query"`
				Field  string `json:"field"`
				Limit  int    `json:"limit"`
				Offset int    `json:"offset"`
			}
			_ = json.Unmarshal(args, &a)
			limit := a.Limit
			if limit <= 0 {
				limit = agentListNovelsDefaultLimit
			}
			if limit > agentListNovelsMaxLimit {
				limit = agentListNovelsMaxLimit
			}
			offset := a.Offset
			if offset < 0 {
				offset = 0
			}
			// Shared: "own" keeps the assistant inside the caller's library:
			// with the default "all" scope, public novels belonging to other
			// users would be listed and then readable through get_novel.
			novels, _, err := s.Store.SearchNovels(userID, strings.TrimSpace(a.Query), limit, offset, "", "", store.ListNovelOptions{Shared: "own", SearchField: a.Field})
			if err != nil {
				return "", err
			}
			out := make([]map[string]any, 0, len(novels))
			for _, n := range novels {
				out = append(out, map[string]any{
					"id":                   n.ID,
					"sourceTitle":          n.SourceTitle,
					"targetTitle":          n.TargetTitle,
					"author":               firstNonEmpty(n.TargetAuthor, n.SourceAuthor),
					"series":               firstNonEmpty(n.TargetSeries, n.SourceSeries),
					"status":               n.Status,
					"tags":                 n.Tags,
					"hasDescription":       strings.TrimSpace(n.TargetDescription) != "",
					"hasSourceDescription": strings.TrimSpace(n.SourceDescription) != "",
				})
			}
			return marshalToolResult(out)
		},
	}
}

func (s *Server) agentToolGetNovel(userID string) ai.AgentTool {
	return ai.AgentTool{
		Name:        "get_novel",
		Description: "Get one of the user's OWN novels by id: titles, authors, series, source and target descriptions, status, tags, source URL and the user's notes. Ids of novels the user does not own are not accessible.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "id": {"type": "string", "description": "Novel id."}
  },
  "required": ["id"]
}`),
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var a struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.ID) == "" {
				return "", fmt.Errorf("invalid id")
			}
			novel, err := s.Store.GetOwnedNovel(userID, strings.TrimSpace(a.ID))
			if err != nil {
				return "", err
			}
			return marshalToolResult(map[string]any{
				"id":                novel.ID,
				"sourceLanguage":    novel.SourceLanguage,
				"targetLanguage":    novel.TargetLanguage,
				"sourceTitle":       novel.SourceTitle,
				"targetTitle":       novel.TargetTitle,
				"sourceAuthor":      novel.SourceAuthor,
				"targetAuthor":      novel.TargetAuthor,
				"sourceSeries":      novel.SourceSeries,
				"targetSeries":      novel.TargetSeries,
				"sourceNumber":      novel.SourceNumber,
				"targetNumber":      novel.TargetNumber,
				"sourceDescription": novel.SourceDescription,
				"targetDescription": novel.TargetDescription,
				"status":            novel.Status,
				"tags":              novel.Tags,
				"url":               novel.URL,
				"notes":             novel.Notes,
			})
		},
	}
}

func (s *Server) agentToolGetNovelStats(userID string) ai.AgentTool {
	return ai.AgentTool{
		Name:        "get_novel_stats",
		Description: "Get translation progress stats for one of the user's OWN novels: total, completed and translated chapter counts plus character totals.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "id": {"type": "string", "description": "Novel id."}
  },
  "required": ["id"]
}`),
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var a struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.ID) == "" {
				return "", fmt.Errorf("invalid id")
			}
			stats, err := s.Store.GetOwnedChapterStats(userID, strings.TrimSpace(a.ID))
			if err != nil {
				return "", err
			}
			return marshalToolResult(stats)
		},
	}
}

func (s *Server) agentToolGetNovelChapters(userID string) ai.AgentTool {
	return ai.AgentTool{
		Name: "get_novel_chapters",
		Description: "List chapter summaries for one of the user's OWN novels, ordered: titles, translation status, exclusion flag and character counts. " +
			"Page with offset/limit, or select a contiguous block of chapters with fromOrder/toOrder (inclusive chapter orders, e.g. fromOrder 5, toOrder 20). " +
			"Summaries never include chapter bodies; use get_chapter for those. Use query_library for aggregate questions over many chapters at once.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "id": {"type": "string", "description": "Novel id."},
    "limit": {"type": "integer", "minimum": 1, "maximum": 200, "description": "Summaries per page (default 50)."},
    "offset": {"type": "integer", "minimum": 0, "description": "Offset for paging through long chapter lists."},
    "fromOrder": {"type": "integer", "minimum": 0, "description": "Return chapters from this chapter order (inclusive)."},
    "toOrder": {"type": "integer", "minimum": 0, "description": "Return chapters up to this chapter order (inclusive)."}
  },
  "required": ["id"]
}`),
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var a struct {
				ID        string `json:"id"`
				Limit     int    `json:"limit"`
				Offset    int    `json:"offset"`
				FromOrder *int   `json:"fromOrder"`
				ToOrder   *int   `json:"toOrder"`
			}
			if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.ID) == "" {
				return "", fmt.Errorf("invalid id")
			}
			limit := a.Limit
			if limit <= 0 {
				limit = agentChaptersDefaultLimit
			}
			if limit > agentChaptersMaxLimit {
				limit = agentChaptersMaxLimit
			}
			if a.Offset < 0 {
				a.Offset = 0
			}
			var summaries []store.ChapterSummary
			var total int
			var err error
			bounds := orderRangeBounds(a.FromOrder, a.ToOrder)
			if bounds.set {
				// Range mode addresses a contiguous block of chapters directly,
				// which is how the model inspects a slice of a long novel
				// without walking it in fixed pages.
				minOrder, maxOrder := bounds.min, bounds.max
				if maxOrder == 0 {
					maxOrder = -1
				}
				summaries, total, err = s.Store.GetOwnedChapterSummariesInOrderRange(
					userID, strings.TrimSpace(a.ID), minOrder, maxOrder, limit)
			} else {
				summaries, total, err = s.Store.GetOwnedChapterSummaries(userID, strings.TrimSpace(a.ID), limit, a.Offset)
			}
			if err != nil {
				return "", err
			}
			out := make([]map[string]any, 0, len(summaries))
			for _, c := range summaries {
				out = append(out, map[string]any{
					"id":                   c.ID,
					"chapterOrder":         c.ChapterOrder,
					"title":                c.Title,
					"translatedTitle":      c.TranslatedTitle,
					"status":               c.Status,
					"excluded":             c.Excluded,
					"hasOriginalContent":   c.HasOriginalContent,
					"hasTranslatedContent": c.HasTranslatedContent,
					"hasRefinedContent":    c.HasRefinedContent,
					"originalChars":        c.OriginalChars,
					"translatedChars":      c.TranslatedChars,
				})
			}
			result := map[string]any{
				"total":    total,
				"offset":   a.Offset,
				"returned": len(out),
				"chapters": out,
			}
			if bounds.set {
				result["mode"] = "orderRange"
				if bounds.min > 0 || a.FromOrder != nil {
					result["fromOrder"] = bounds.min
				}
				if a.ToOrder != nil {
					result["toOrder"] = bounds.max
				}
			}
			return marshalToolResult(result)
		},
	}
}

// orderRange normalizes the fromOrder/toOrder pair, accepting either bound
// independently so the model can ask for "everything from 40 onwards".
type orderRange struct {
	min int
	max int
	set bool
}

func orderRangeBounds(from, to *int) orderRange {
	r := orderRange{}
	if from != nil && *from >= 0 {
		r.min = *from
		r.set = true
	}
	if to != nil && *to >= 0 {
		r.max = *to
		r.set = true
	}
	if r.set && r.max > 0 && r.min > r.max {
		r.min, r.max = r.max, r.min
	}
	return r
}

func (s *Server) agentToolGetChapter(userID string) ai.AgentTool {
	return ai.AgentTool{
		Name: "get_chapter",
		Description: "Read one chapter of the user's OWN novel by novel id + chapter id. Without content it returns metadata only. " +
			"Set content to original, translated or refined to include that text, and control how much comes back with startLine (0-based, default 0) and lineCount (default " + strconv.Itoa(agentChapterDefaultLineCount) + ", max " + strconv.Itoa(agentChapterMaxLineCount) + "). " +
			"A NEGATIVE startLine counts from the end: startLine -10 with lineCount 10 returns the last 10 lines, so reading an ending is one call. " +
			"The response reports totalLines and nextStartLine, so you can page through a long chapter in slices instead of loading it whole; " +
			"requesting lineCount 1 is a cheap probe that reveals the chapter's exact length without pulling its text. " +
			"Omitting both returns the opening slice of the chapter, never a silently cut text.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "novelId": {"type": "string", "description": "Novel id."},
    "chapterId": {"type": "string", "description": "Chapter id (from get_novel_chapters)."},
    "content": {"type": "string", "enum": ["", "original", "translated", "refined"], "description": "Which body text to include, if any."},
    "startLine": {"type": "integer", "description": "0-based line to start at (default 0). Negative values count from the end: -10 starts 10 lines before the last."},
    "lineCount": {"type": "integer", "minimum": 1, "maximum": 400, "description": "How many lines to return (default 60). Use 1 as a cheap probe to learn totalLines."}
  },
  "required": ["novelId", "chapterId"]
}`),
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var a struct {
				NovelID   string `json:"novelId"`
				ChapterID string `json:"chapterId"`
				Content   string `json:"content"`
				StartLine int    `json:"startLine"`
				LineCount int    `json:"lineCount"`
			}
			if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.NovelID) == "" || strings.TrimSpace(a.ChapterID) == "" {
				return "", fmt.Errorf("invalid novelId/chapterId")
			}
			chapter, err := s.Store.GetOwnedChapter(userID, strings.TrimSpace(a.NovelID), strings.TrimSpace(a.ChapterID))
			if err != nil {
				return "", err
			}
			out := map[string]any{
				"id":              chapter.ID,
				"novelId":         chapter.NovelID,
				"chapterOrder":    chapter.ChapterOrder,
				"title":           chapter.Title,
				"translatedTitle": chapter.TranslatedTitle,
				"status":          chapter.Status,
			}
			var body string
			field := ""
			switch a.Content {
			case "original":
				body, field = chapter.OriginalContent, "originalContent"
			case "translated":
				body, field = chapter.TranslatedContent, "translatedContent"
			case "refined":
				body, field = chapter.RefinedContent, "refinedContent"
			}
			if field == "" {
				return marshalToolResult(out)
			}
			slice, meta := sliceAgentLines(body, a.StartLine, a.LineCount)
			out[field] = slice
			out["contentWindow"] = meta
			return marshalToolResult(out)
		},
	}
}

func (s *Server) agentToolQueryLibrary(userID string) ai.AgentTool {
	return ai.AgentTool{
		Name: "query_library",
		Description: "Run ONE read-only analytics SELECT over the library progress views (SQLite dialect). The cheapest way to answer whole-library questions in one call: counts, filters, rankings, ratios. Owner filtering is automatic (there is no owner column to filter by) and these two views are the only relations that exist here:\n" +
			"- v_agent_novel_progress, one row per novel: novel_id, title (target title falling back to source title), author (target author falling back to source), status (novel status), source_language, target_language, is_public, has_target_description (1 when the user-facing target description is non-empty), has_source_description (1 when the original-language description is non-empty), chapters_total (non-excluded chapters), chapters_translated (chapter status translated/refined/done), chapters_completed (chapter status refined/done), chapters_pending (chapters_total minus chapters_translated), original_chars, translated_chars, refined_chars (sums of the chapters' char counts, non-excluded), max_chapter_order (largest chapter_order, excluded included), updated\n" +
			"- v_agent_chapter_overview, one row per chapter: novel_id, chapter_id, chapter_order, title, translated_title, status (pending/processing/translated/refined/done/failed), is_excluded (1 when the user excluded the chapter), original_chars, translated_chars, refined_chars (that chapter's char counts), error_message (last download/translation error, empty when none), updated\n" +
			"Rules: exactly one SELECT (or WITH ... SELECT) reading at least one of these views; no ';' and no comments; any other table is absent here and errors; chapter bodies and description texts are not in the views (use get_chapter / get_novel).\n" +
			"Default: filter is_excluded = 0 in every chapter analysis — excluded chapters are logically deleted and are only surfaced when the user explicitly asks about excluded ones (the novel view's counters already exclude them).\n" +
			"Sweeps: your own LIMIT ... OFFSET ... survives inside the query, so page through long results by raising OFFSET (the response reports truncated when more rows exist) — never present a truncated page as the complete answer. Answer how-many questions with COUNT(*) instead of listing rows.\n" +
			"Examples:\n" +
			"- Chapters whose translation is 40%+ shorter than the original: SELECT novel_id, chapter_id, chapter_order, title FROM v_agent_chapter_overview WHERE is_excluded = 0 AND status = 'translated' AND translated_chars <= original_chars * 0.6\n" +
			"- Novels missing BOTH descriptions: SELECT novel_id, title FROM v_agent_novel_progress WHERE has_target_description = 0 AND has_source_description = 0",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "sql": {"type": "string", "description": "One SELECT over the two v_agent_* views, e.g. SELECT novel_id, title, chapters_pending FROM v_agent_novel_progress WHERE chapters_pending < 10 ORDER BY chapters_pending."},
    "limit": {"type": "integer", "minimum": 1, "maximum": 200, "description": "Max rows per call (default 50)."}
  },
  "required": ["sql"]
}`),
		Execute: func(ctx context.Context, args json.RawMessage) (string, error) {
			var a struct {
				SQL   string `json:"sql"`
				Limit int    `json:"limit"`
			}
			if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.SQL) == "" {
				return "", fmt.Errorf("missing sql")
			}
			return s.Store.RunAgentAnalyticsQuery(ctx, userID, a.SQL, a.Limit)
		},
	}
}

func (s *Server) agentToolUpdateNovel(userID string) ai.AgentTool {
	return ai.AgentTool{
		Name:        "update_novel",
		Description: "Apply edits to one of the user's OWN novels: target (user-facing) title, author, series, tags, novel status, description and/or the user's notes. Source-side metadata (original title/author/series, URL) is never editable here. Writes immediately; empty strings clear a text field. Only use on explicit user request.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "id": {"type": "string", "description": "Novel id."},
    "targetTitle": {"type": "string", "description": "New user-facing title. Omit to leave unchanged; empty string clears it."},
    "targetAuthor": {"type": "string", "description": "New user-facing author. Omit to leave unchanged; empty string clears it."},
    "targetSeries": {"type": "string", "description": "New user-facing series. Omit to leave unchanged; empty string clears it."},
    "tags": {"type": "array", "items": {"type": "string"}, "description": "Replaces the whole tag list; empty array clears it."},
    "status": {"type": "string", "enum": ["ongoing", "completed", "hiatus", "cancelled"], "description": "Novel reading/translation status."},
    "targetDescription": {"type": "string", "description": "New user-facing description. Omit to leave unchanged; empty string clears it."},
    "notes": {"type": "string", "description": "New private notes. Omit to leave unchanged; empty string clears them."}
  },
  "required": ["id"]
}`),
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var a struct {
				ID                string   `json:"id"`
				TargetTitle       *string  `json:"targetTitle"`
				TargetAuthor      *string  `json:"targetAuthor"`
				TargetSeries      *string  `json:"targetSeries"`
				Tags              []string `json:"tags"`
				Status            string   `json:"status"`
				TargetDescription *string  `json:"targetDescription"`
				Notes             *string  `json:"notes"`
			}
			if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.ID) == "" {
				return "", fmt.Errorf("invalid id")
			}
			patch := map[string]any{}
			if a.TargetTitle != nil {
				patch["targetTitle"] = *a.TargetTitle
			}
			if a.TargetAuthor != nil {
				patch["targetAuthor"] = *a.TargetAuthor
			}
			if a.TargetSeries != nil {
				patch["targetSeries"] = *a.TargetSeries
			}
			if a.Tags != nil {
				patch["tags"] = a.Tags
			}
			if a.Status != "" {
				switch a.Status {
				case "ongoing", "completed", "hiatus", "cancelled":
				default:
					return "", fmt.Errorf("invalid status %q: use ongoing, completed, hiatus or cancelled", a.Status)
				}
				patch["status"] = a.Status
			}
			if a.TargetDescription != nil {
				patch["targetDescription"] = *a.TargetDescription
			}
			if a.Notes != nil {
				patch["notes"] = *a.Notes
			}
			if len(patch) == 0 {
				return "", fmt.Errorf("nothing to update: provide targetTitle, targetAuthor, targetSeries, tags, status, targetDescription or notes")
			}
			novel, err := s.Store.UpdateNovel(userID, strings.TrimSpace(a.ID), patch)
			if err != nil {
				return "", err
			}
			return marshalToolResult(map[string]any{
				"ok":    true,
				"novel": agentNovelAfterUpdate(novel),
			})
		},
	}
}

func agentNovelAfterUpdate(novel *store.Novel) map[string]any {
	return map[string]any{
		"id":                novel.ID,
		"targetTitle":       novel.TargetTitle,
		"targetAuthor":      novel.TargetAuthor,
		"targetSeries":      novel.TargetSeries,
		"tags":              novel.Tags,
		"status":            novel.Status,
		"targetDescription": novel.TargetDescription,
		"notes":             novel.Notes,
	}
}

func (s *Server) agentToolSearchChapters(userID string) ai.AgentTool {
	return ai.AgentTool{
		Name:        "search_chapters",
		Description: "Search chapter titles AND bodies of one novel for a literal text match (case-sensitive substring; wildcards are literal). Returns chapter hits with matched field names and a snippet when a body matched. Use it to locate where something is said before reading whole chapters.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "novelId": {"type": "string", "description": "Novel id."},
    "query": {"type": "string", "description": "Literal text to search for."},
    "limit": {"type": "integer", "minimum": 1, "maximum": 25, "description": "Max hits (default 10)."}
  },
  "required": ["novelId", "query"]
}`),
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var a struct {
				NovelID string `json:"novelId"`
				Query   string `json:"query"`
				Limit   int    `json:"limit"`
			}
			if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.NovelID) == "" {
				return "", fmt.Errorf("invalid novelId")
			}
			hits, err := s.Store.SearchOwnedChapters(userID, strings.TrimSpace(a.NovelID), a.Query, a.Limit)
			if err != nil {
				return "", err
			}
			return marshalToolResult(map[string]any{"hits": hits})
		},
	}
}

func (s *Server) agentToolUpdateChapter(userID string) ai.AgentTool {
	return ai.AgentTool{
		Name:        "update_chapter",
		Description: "Edit one chapter of the user's OWN novel: title (original), translatedTitle, translatedContent, refinedContent. Content values replace the WHOLE field, so read the current text with get_chapter first when editing. Omitted fields stay unchanged; empty strings clear them. Refused while the novel has active jobs.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "novelId": {"type": "string", "description": "Novel id."},
    "chapterId": {"type": "string", "description": "Chapter id (from get_novel_chapters or search_chapters)."},
    "title": {"type": "string", "description": "New original title."},
    "translatedTitle": {"type": "string", "description": "New translated title."},
    "translatedContent": {"type": "string", "description": "New translated body; replaces the whole text."},
    "refinedContent": {"type": "string", "description": "New refined body; replaces the whole text."}
  },
  "required": ["novelId", "chapterId"]
}`),
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var a struct {
				NovelID           string  `json:"novelId"`
				ChapterID         string  `json:"chapterId"`
				Title             *string `json:"title"`
				TranslatedTitle   *string `json:"translatedTitle"`
				TranslatedContent *string `json:"translatedContent"`
				RefinedContent    *string `json:"refinedContent"`
			}
			if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.NovelID) == "" || strings.TrimSpace(a.ChapterID) == "" {
				return "", fmt.Errorf("invalid novelId/chapterId")
			}
			chapter, err := s.Store.UpdateChapterEdits(userID, strings.TrimSpace(a.NovelID), strings.TrimSpace(a.ChapterID), store.ChapterEdits{
				Title:             a.Title,
				TranslatedTitle:   a.TranslatedTitle,
				TranslatedContent: a.TranslatedContent,
				RefinedContent:    a.RefinedContent,
			})
			if err != nil {
				return "", err
			}
			return marshalToolResult(map[string]any{
				"ok": true,
				"chapter": map[string]any{
					"id":              chapter.ID,
					"chapterOrder":    chapter.ChapterOrder,
					"title":           chapter.Title,
					"translatedTitle": chapter.TranslatedTitle,
					"status":          chapter.Status,
					"originalChars":   len(chapter.OriginalContent),
					"translatedChars": len(chapter.TranslatedContent),
					"refinedChars":    len(chapter.RefinedContent),
				},
			})
		},
	}
}

// agentChapterStatuses is the closed set the model may set via
// set_chapter_status. It mirrors the chapters collection SelectField values
// (store_schema.go) minus "processing", which is a transient state the
// translation worker owns. Anything else returns a tool error the model can
// read — a value outside the schema would fail PocketBase validation.
var agentChapterStatuses = map[string]bool{
	"pending":    true,
	"translated": true,
	"refined":    true,
	"done":       true,
	"failed":     true,
}

func (s *Server) agentToolSetChapterStatus(userID string) ai.AgentTool {
	return ai.AgentTool{
		Name:        "set_chapter_status",
		Description: "Change one chapter of the user's OWN novel: flip its translation status (pending, translated, refined, done, failed) and optionally set an error note. This does not create, translate or delete content; it only flips the status flag and refreshes novel stats.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "novelId": {"type": "string", "description": "Novel id."},
    "chapterId": {"type": "string", "description": "Chapter id."},
    "status": {"type": "string", "enum": ["pending", "translated", "refined", "done", "failed"]},
    "errorMessage": {"type": "string", "description": "Optional note stored with the chapter (cleared when omitted)."}
  },
  "required": ["novelId", "chapterId", "status"]
}`),
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var a struct {
				NovelID      string `json:"novelId"`
				ChapterID    string `json:"chapterId"`
				Status       string `json:"status"`
				ErrorMessage string `json:"errorMessage"`
			}
			if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.NovelID) == "" || strings.TrimSpace(a.ChapterID) == "" {
				return "", fmt.Errorf("invalid novelId/chapterId")
			}
			if !agentChapterStatuses[a.Status] {
				return "", fmt.Errorf("invalid status %q: use pending, translated, refined, done or failed", a.Status)
			}
			if err := s.Store.UpdateChapterStatusForUser(userID, strings.TrimSpace(a.NovelID), strings.TrimSpace(a.ChapterID), a.Status, a.ErrorMessage); err != nil {
				return "", err
			}
			return marshalToolResult(map[string]any{"ok": true, "chapterId": strings.TrimSpace(a.ChapterID), "status": a.Status})
		},
	}
}

func (s *Server) agentToolSetChapterExcluded(userID string) ai.AgentTool {
	return ai.AgentTool{
		Name:        "set_chapter_excluded",
		Description: "Include or exclude one chapter of the user's OWN novel (excluded chapters are hidden from readers and skipped by stats and translation jobs). Refused while the novel has active jobs.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "novelId": {"type": "string", "description": "Novel id."},
    "chapterId": {"type": "string", "description": "Chapter id."},
    "excluded": {"type": "boolean", "description": "true to exclude (hide), false to include."}
  },
  "required": ["novelId", "chapterId", "excluded"]
}`),
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var a struct {
				NovelID   string `json:"novelId"`
				ChapterID string `json:"chapterId"`
				Excluded  bool   `json:"excluded"`
			}
			if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.NovelID) == "" || strings.TrimSpace(a.ChapterID) == "" {
				return "", fmt.Errorf("invalid novelId/chapterId")
			}
			if err := s.Store.SetChapterExcluded(userID, strings.TrimSpace(a.NovelID), strings.TrimSpace(a.ChapterID), a.Excluded); err != nil {
				return "", err
			}
			return marshalToolResult(map[string]any{"ok": true, "chapterId": strings.TrimSpace(a.ChapterID), "excluded": a.Excluded})
		},
	}
}

// truncateAgentContent caps a chapter body for the model context. The cut is
// rune-safe: a byte-wise cut would land inside a multi-byte character and
// marshal as U+FFFD, which matters because the library serves accented Spanish
// and CJK text.
func truncateAgentContent(s string) string {
	if utf8.RuneCountInString(s) <= agentChapterContentMaxChars {
		return s
	}
	return store.TruncateRunes(s, agentChapterContentMaxChars) + "\n…[truncated]"
}

// Chapter bodies are returned as explicit line windows rather than a fixed
// character cut. A silent truncation destroys the tail of a chapter with no
// way for the model to notice or page past it; a window is self-limiting (the
// model controls the size) and resumable (it is told where the next slice
// starts). The default slice is deliberately small so the first read of an
// unknown chapter stays cheap.
const (
	agentChapterDefaultLineCount = 60
	agentChapterMaxLineCount     = 400
)

// sliceAgentLines returns the requested window of body plus a descriptor
// telling the model how to continue. A negative startLine counts from the end
// (-1 is the last line), so "show me the ending" is one call; the resolved
// startLine is reported back so the model knows where the window landed.
// Out-of-range requests clamp instead of erroring: the model paging near the
// end of a chapter should get an empty or partial slice and a null
// nextStartLine, not a tool failure.
func sliceAgentLines(body string, startLine, lineCount int) (string, map[string]any) {
	lines := strings.Split(body, "\n")
	total := len(lines)
	if lineCount <= 0 {
		lineCount = agentChapterDefaultLineCount
	}
	if lineCount > agentChapterMaxLineCount {
		lineCount = agentChapterMaxLineCount
	}
	if startLine < 0 {
		startLine = total + startLine
		if startLine < 0 {
			startLine = 0
		}
	}
	if startLine > total {
		startLine = total
	}
	end := startLine + lineCount
	if end > total {
		end = total
	}
	slice := strings.Join(lines[startLine:end], "\n")
	meta := map[string]any{
		"startLine":  startLine,
		"lineCount":  end - startLine,
		"totalLines": total,
	}
	// Only advertise a next window when one exists, so an exhausted chapter is
	// unambiguous rather than sending the model back to an empty slice.
	if end < total {
		meta["nextStartLine"] = end
		meta["hasMore"] = true
	} else {
		meta["hasMore"] = false
	}
	return slice, meta
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// agentHistoryFromSession decodes the persisted message trail; corrupt or
// empty histories degrade to a fresh conversation instead of failing the
// turn.
func agentHistoryFromSession(session *store.AgentSession) []ai.AgentMessage {
	history := []ai.AgentMessage{}
	if strings.TrimSpace(session.Messages) == "" {
		return history
	}
	if err := json.Unmarshal([]byte(session.Messages), &history); err != nil {
		return []ai.AgentMessage{}
	}
	return history
}

// trimAgentHistory bounds the persisted trail by both message count and
// encoded size, cutting only at a turn boundary.
//
// Cutting at an arbitrary offset can drop an assistant(toolCalls) message
// while keeping its `tool` replies, or the reverse. The trail then replays as
// a conversation whose first message is a tool result, which every
// OpenAI-compatible API rejects with a 400 — and because the bad trail is
// already persisted, every later turn fails the same way until the session is
// reset. So the cut walks back to the newest `user` message: a turn boundary
// where everything after it is a self-contained assistant/tool exchange.
func trimAgentHistory(messages []ai.AgentMessage) []ai.AgentMessage {
	if len(messages) <= agentMaxHistoryMessages {
		return messages
	}
	start := len(messages) - agentMaxHistoryMessages
	for start < len(messages) && messages[start].Role != "user" {
		start++
	}
	if start >= len(messages) {
		// No user message in the kept window (a malformed trail): fall back to
		// dropping the whole window rather than persisting a broken one.
		return nil
	}
	return messages[start:]
}

// agentHistoryEncodedSize reports the exact byte length of a trail once
// JSON-encoded, which is what the collection's field cap actually measures.
//
// An earlier version summed raw string lengths, which undercounts: JSON escapes
// expand, and literary text is full of quotes and newlines. A quote, newline
// and tab (9 bytes) encode to 15, so a raw-length budget of 800k could encode to
// well over the 1M field cap — and the save would then fail on every turn
// until the session was reset, because the oversized trail is what never
// persists. Measuring the encoding removes the guesswork.
func agentHistoryEncodedSize(messages []ai.AgentMessage) (int, error) {
	encoded, err := json.Marshal(messages)
	if err != nil {
		return 0, err
	}
	return len(encoded), nil
}

// runAgentTurn executes one chat turn: replay history, stream events through
// onEvent, then persist the extended trail. Callers must serialize turns per
// user (agentTurnLocks) and pass an already-resolved runner.
func (s *Server) runAgentTurn(ctx context.Context, runner ai.AgentProvider, userID string, session *store.AgentSession, selectedNovel *store.Novel, message string, onEvent func(ai.AgentEvent)) (ai.AgentChatOutput, *store.AgentSession, error) {
	history := agentHistoryFromSession(session)
	history = append(history, ai.AgentMessage{Role: "user", Content: message})

	system := agentSystemPrompt
	if selectedNovel != nil {
		system += "\n\n" + agentSelectedNovelContext(selectedNovel)
	}

	output, err := runner.AgentChat(ctx, ai.AgentChatInput{
		System:   system,
		Messages: history,
		Tools:    s.agentTools(userID),
		OnEvent:  onEvent,
	})
	if err != nil {
		return output, session, err
	}

	full := trimAgentHistory(output.Messages)
	// Tool call arguments are unbounded (update_chapter carries whole
	// chapters), so a count-bounded trail can still blow past the collection's
	// field cap. Drop whole turns from the front until the encoded trail fits.
	var encoded []byte
	trims := 0
	for {
		encoded, err = json.Marshal(full)
		if err != nil {
			return output, session, fmt.Errorf("encode agent history: %w", err)
		}
		if len(full) == 0 || len(encoded) <= agentMaxHistoryBytes {
			break
		}
		// Each pass must strictly shorten the trail, so it cannot exceed the
		// message count; the guard turns a future regression here into a
		// failed save rather than a hung request.
		if trims >= len(full) {
			return output, session, fmt.Errorf("agent history did not converge below %d bytes", agentMaxHistoryBytes)
		}
		trims++
		// The first message of a well-formed trail is a user turn, which would
		// make the cut a no-op and spin forever. Advance past it so every
		// iteration strictly shortens the trail.
		cut := 1
		for cut < len(full) && full[cut].Role != "user" {
			cut++
		}
		if cut >= len(full) {
			full = nil
			continue
		}
		full = full[cut:]
	}

	saved, err := s.Store.SaveAgentSessionMessages(userID, session.ID, string(encoded))
	if err != nil {
		return output, session, fmt.Errorf("save agent session: %w", err)
	}
	return output, saved, nil
}

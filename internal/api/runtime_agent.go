package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
- Every tool works ONLY on novels the user owns. Other users' novels, chapters, sessions, accounts and settings are not reachable by any tool, and no SQL query can read them. If asked about them, say the assistant can only see the user's own library.
- Never invent library data. Use the tools to look up novels, chapters and stats; cite novel ids and chapter orders when reporting results.
- list_novels returns hasDescription so you can answer questions like "which novels are missing a description". Request a generous limit when the user asks for a full sweep.
- Reading a chapter body requires get_chapter with content set to original, translated or refined; summaries from get_novel_chapters never include the body.
- search_chapters looks inside chapter titles AND bodies of one novel (literal text match, returns snippets); use it to locate where something is said before reading a whole chapter.
- query_library runs ONE read-only analytics SELECT over the library progress views — the cheapest way to answer aggregate questions ("which novels are missing fewer than 10 chapters to be complete", counts, filters, rankings). Chapter bodies are not in SQL; use get_chapter for those.
- Writing tools (update_novel, update_chapter, set_chapter_status, set_chapter_excluded) apply immediately to novels the user owns. Only call them when the user clearly asked for a change, never for exploratory suggestions; after applying, tell the user exactly what changed. update_chapter replaces the whole content field, so read the current text with get_chapter first when the user asks for modifications. While a novel has active download/translation jobs, chapter writes are refused — say so instead of retrying.
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
		s.agentToolListNovels(userID),
		s.agentToolGetNovel(userID),
		s.agentToolGetNovelStats(userID),
		s.agentToolGetNovelChapters(userID),
		s.agentToolGetChapter(userID),
		s.agentToolSearchChapters(userID),
		s.agentToolQueryLibrary(userID),
		s.agentToolUpdateNovel(userID),
		s.agentToolUpdateChapter(userID),
		s.agentToolSetChapterStatus(userID),
		s.agentToolSetChapterExcluded(userID),
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
		Description: "List the user's OWN novels, newest first. Set query to search by title, author, series or tags. Returns light records: hasDescription tells you whether the target (user-facing) description is empty. The assistant only ever sees novels the user owns, never other users' public novels.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "description": "Optional search text across title, author, series and tags."},
    "limit": {"type": "integer", "minimum": 1, "maximum": 50, "description": "How many novels to return (default 20). Use the max when asked for a full sweep."}
  }
}`),
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var a struct {
				Query string `json:"query"`
				Limit int    `json:"limit"`
			}
			_ = json.Unmarshal(args, &a)
			limit := a.Limit
			if limit <= 0 {
				limit = agentListNovelsDefaultLimit
			}
			if limit > agentListNovelsMaxLimit {
				limit = agentListNovelsMaxLimit
			}
			// Shared: "own" keeps the assistant inside the caller's library:
			// with the default "all" scope, public novels belonging to other
			// users would be listed and then readable through get_novel.
			novels, _, err := s.Store.SearchNovels(userID, strings.TrimSpace(a.Query), limit, 0, "", "", store.ListNovelOptions{Shared: "own"})
			if err != nil {
				return "", err
			}
			out := make([]map[string]any, 0, len(novels))
			for _, n := range novels {
				out = append(out, map[string]any{
					"id":             n.ID,
					"sourceTitle":    n.SourceTitle,
					"targetTitle":    n.TargetTitle,
					"author":         firstNonEmpty(n.TargetAuthor, n.SourceAuthor),
					"series":         firstNonEmpty(n.TargetSeries, n.SourceSeries),
					"status":         n.Status,
					"tags":           n.Tags,
					"hasDescription": strings.TrimSpace(n.TargetDescription) != "",
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
		Name:        "get_novel_chapters",
		Description: "List chapter summaries for one of the user's OWN novels (ordered): titles, translation status, exclusion flag and character counts. Summaries never include chapter bodies; use get_chapter for those.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "id": {"type": "string", "description": "Novel id."},
    "limit": {"type": "integer", "minimum": 1, "maximum": 200, "description": "Summaries per page (default 50)."},
    "offset": {"type": "integer", "minimum": 0, "description": "Offset for paging through long chapter lists."}
  },
  "required": ["id"]
}`),
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var a struct {
				ID     string `json:"id"`
				Limit  int    `json:"limit"`
				Offset int    `json:"offset"`
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
			summaries, total, err := s.Store.GetOwnedChapterSummaries(userID, strings.TrimSpace(a.ID), limit, a.Offset)
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
			return marshalToolResult(map[string]any{
				"total":    total,
				"offset":   a.Offset,
				"chapters": out,
			})
		},
	}
}

func (s *Server) agentToolGetChapter(userID string) ai.AgentTool {
	return ai.AgentTool{
		Name:        "get_chapter",
		Description: "Read one chapter of the user's OWN novel by novel id + chapter id. Without content it returns metadata only; set content to original, translated or refined to include that text (truncated for context safety).",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "novelId": {"type": "string", "description": "Novel id."},
    "chapterId": {"type": "string", "description": "Chapter id (from get_novel_chapters)."},
    "content": {"type": "string", "enum": ["", "original", "translated", "refined"], "description": "Which body text to include, if any."}
  },
  "required": ["novelId", "chapterId"]
}`),
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var a struct {
				NovelID   string `json:"novelId"`
				ChapterID string `json:"chapterId"`
				Content   string `json:"content"`
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
			switch a.Content {
			case "original":
				out["originalContent"] = truncateAgentContent(chapter.OriginalContent)
			case "translated":
				out["translatedContent"] = truncateAgentContent(chapter.TranslatedContent)
			case "refined":
				out["refinedContent"] = truncateAgentContent(chapter.RefinedContent)
			}
			return marshalToolResult(out)
		},
	}
}

func (s *Server) agentToolQueryLibrary(userID string) ai.AgentTool {
	return ai.AgentTool{
		Name: "query_library",
		Description: "Run ONE read-only analytics SELECT over the library progress views (SQLite dialect). Cheapest way to answer aggregate questions like 'novels missing fewer than 10 chapters to be complete', counts, filters, rankings. Only these two views exist here (owner filtering is automatic, there is no owner column to filter by):\n" +
			"- v_agent_novel_progress: novel_id, title, author, status, source_language, target_language, is_public, has_description, total, translated, completed, pending, original_chars, translated_chars, refined_chars, max_chapter_order, updated\n" +
			"- v_agent_chapter_overview: novel_id, chapter_id, chapter_order, title, translated_title, status, excluded, original_chars, translated_chars, refined_chars, error_message, updated\n" +
			"Rules: one SELECT (or WITH ... SELECT) that reads at least one of these views, no ';' and no comments, any other table is absent here and will error, chapter bodies are not in the views (use get_chapter).",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "sql": {"type": "string", "description": "One SELECT over the two v_agent_* views, e.g. SELECT novel_id, title, pending FROM v_agent_novel_progress WHERE pending < 10 ORDER BY pending."},
    "limit": {"type": "integer", "minimum": 1, "maximum": 200, "description": "Max rows (default 50)."}
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
		Description: "Apply edits to one of the user's OWN novels: target (user-facing) title, target description and/or the user's notes. Writes immediately; empty strings clear a field. Only use on explicit user request.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "id": {"type": "string", "description": "Novel id."},
    "targetTitle": {"type": "string", "description": "New user-facing title. Omit to leave unchanged; empty string clears it."},
    "targetDescription": {"type": "string", "description": "New user-facing description. Omit to leave unchanged; empty string clears it."},
    "notes": {"type": "string", "description": "New private notes. Omit to leave unchanged; empty string clears them."}
  },
  "required": ["id"]
}`),
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var a struct {
				ID                string  `json:"id"`
				TargetTitle       *string `json:"targetTitle"`
				TargetDescription *string `json:"targetDescription"`
				Notes             *string `json:"notes"`
			}
			if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.ID) == "" {
				return "", fmt.Errorf("invalid id")
			}
			patch := map[string]any{}
			if a.TargetTitle != nil {
				patch["targetTitle"] = *a.TargetTitle
			}
			if a.TargetDescription != nil {
				patch["targetDescription"] = *a.TargetDescription
			}
			if a.Notes != nil {
				patch["notes"] = *a.Notes
			}
			if len(patch) == 0 {
				return "", fmt.Errorf("nothing to update: provide targetTitle, targetDescription or notes")
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

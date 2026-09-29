package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"translator-server/internal/ai"
	"translator-server/internal/store"
)

// Limits bounding one agent chat turn. Tool results are additionally
// truncated inside internal/ai before they reach the model.
const (
	agentMaxMessageChars        = 8000
	agentMaxHistoryMessages     = 60
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
- Never invent library data. Use the tools to look up novels, chapters and stats; cite novel ids and chapter orders when reporting results.
- list_novels returns hasDescription so you can answer questions like "which novels are missing a description". Request a generous limit when the user asks for a full sweep.
- Reading a chapter body requires get_chapter with content set to original, translated or refined; summaries from get_novel_chapters never include the body.
- search_chapters looks inside chapter titles AND bodies of one novel (literal text match, returns snippets); use it to locate where something is said before reading a whole chapter.
- Writing tools (update_novel, update_chapter, set_chapter_status, set_chapter_excluded) apply immediately. Only call them when the user clearly asked for a change, never for exploratory suggestions; after applying, tell the user exactly what changed. update_chapter replaces the whole content field, so read the current text with get_chapter first when the user asks for modifications. While a novel has active download/translation jobs, chapter writes are refused — say so instead of retrying.
- Valid chapter statuses: pending, translated, refined, done, error.
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
		s.agentToolUpdateNovel(userID),
		s.agentToolUpdateChapter(userID),
		s.agentToolSetChapterStatus(userID),
		s.agentToolSetChapterExcluded(userID),
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
		Description: "List the user's novels, newest first. Set query to search by title, author, series or tags. Returns light records: hasDescription tells you whether the target (user-facing) description is empty.",
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
			novels, _, err := s.Store.SearchNovels(userID, strings.TrimSpace(a.Query), limit, 0, "", "", store.ListNovelOptions{Shared: "all"})
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
		Description: "Get one novel's full metadata by id: titles, authors, series, source and target descriptions, status, tags, source URL and the user's notes.",
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
			novel, err := s.Store.GetNovelAccessible(userID, strings.TrimSpace(a.ID))
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
		Description: "Get translation progress stats for one novel: total, completed and translated chapter counts plus character totals.",
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
			if _, err := s.Store.GetNovelAccessible(userID, strings.TrimSpace(a.ID)); err != nil {
				return "", err
			}
			stats, err := s.Store.GetChapterStatsAccessible(userID, strings.TrimSpace(a.ID))
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
		Description: "List chapter summaries for one novel (ordered): titles, translation status, exclusion flag and character counts. Summaries never include chapter bodies; use get_chapter for those.",
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
			summaries, total, err := s.Store.ListChapterSummariesAccessible(userID, strings.TrimSpace(a.ID), limit, a.Offset)
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
		Description: "Read one chapter by novel id + chapter id. Without content it returns metadata only; set content to original, translated or refined to include that text (truncated for context safety).",
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
			chapter, err := s.Store.GetChapterAccessible(userID, strings.TrimSpace(a.NovelID), strings.TrimSpace(a.ChapterID))
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

func (s *Server) agentToolUpdateNovel(userID string) ai.AgentTool {
	return ai.AgentTool{
		Name:        "update_novel",
		Description: "Apply edits to one novel's metadata: target (user-facing) title, target description and/or the user's notes. Writes immediately; empty strings clear a field. Only use on explicit user request.",
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
			hits, err := s.Store.SearchChaptersAccessible(userID, strings.TrimSpace(a.NovelID), a.Query, a.Limit)
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
		Description: "Edit one chapter's titles and/or content: title (original), translatedTitle, translatedContent, refinedContent. Content values replace the WHOLE field, so read the current text with get_chapter first when editing. Omitted fields stay unchanged; empty strings clear them. Refused while the novel has active jobs.",
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
// set_chapter_status; anything else returns a tool error the model can read.
var agentChapterStatuses = map[string]bool{
	"pending":    true,
	"translated": true,
	"refined":    true,
	"done":       true,
	"error":      true,
}

func (s *Server) agentToolSetChapterStatus(userID string) ai.AgentTool {
	return ai.AgentTool{
		Name:        "set_chapter_status",
		Description: "Change one chapter's translation status (pending, translated, refined, done, error), optionally with an error note. This does not create, translate or delete content; it only flips the status flag and refreshes novel stats.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "novelId": {"type": "string", "description": "Novel id."},
    "chapterId": {"type": "string", "description": "Chapter id."},
    "status": {"type": "string", "enum": ["pending", "translated", "refined", "done", "error"]},
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
				return "", fmt.Errorf("invalid status %q: use pending, translated, refined, done or error", a.Status)
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
		Description: "Include or exclude one chapter from the novel (excluded chapters are hidden from readers and skipped by stats and translation jobs). Refused while the novel has active jobs.",
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

func truncateAgentContent(s string) string {
	if len(s) <= agentChapterContentMaxChars {
		return s
	}
	return s[:agentChapterContentMaxChars] + "\n…[truncated]"
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

// runAgentTurn executes one chat turn: resolve the runner, replay history,
// stream events through onEvent, then persist the extended trail. Callers
// must serialize turns per user (agentTurnLocks).
func (s *Server) runAgentTurn(ctx context.Context, userID string, session *store.AgentSession, selectedNovel *store.Novel, message string, onEvent func(ai.AgentEvent)) (ai.AgentChatOutput, *store.AgentSession, error) {
	runner, err := s.resolveAgentRunner(userID)
	if err != nil {
		return ai.AgentChatOutput{}, session, err
	}

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

	full := output.Messages
	if len(full) > agentMaxHistoryMessages {
		full = full[len(full)-agentMaxHistoryMessages:]
	}
	encoded, err := json.Marshal(full)
	if err != nil {
		return output, session, fmt.Errorf("encode agent history: %w", err)
	}
	saved, err := s.Store.SaveAgentSessionMessages(userID, session.ID, string(encoded))
	if err != nil {
		return output, session, fmt.Errorf("save agent session: %w", err)
	}
	return output, saved, nil
}

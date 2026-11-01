package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"translator-server/internal/ai"
	"translator-server/internal/store"
)

// agentComicTools is the comics toolset the assistant exposes when the chat's
// media selector is "comics" (or "both"). Deliberately small: reading and
// catalog questions. Comic text translation is deferred, so there are no
// write/translate tools here yet.
func (s *Server) agentComicTools(userID string) []ai.AgentTool {
	return []ai.AgentTool{
		s.agentToolListComics(userID),
		s.agentToolGetComic(userID),
		s.agentToolGetComicChapters(userID),
		s.agentToolCheckComicUpdates(userID),
	}
}

func (s *Server) agentToolListComics(userID string) ai.AgentTool {
	return ai.AgentTool{
		Name:        "list_comics",
		Description: "List the comics in the user's library (own plus shared with them), newest first, with progress counters.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "q": {"type": "string", "description": "Optional title/author/series filter."},
    "progress": {"type": "string", "enum": ["all", "downloaded", "completed", "ongoing"]},
    "limit": {"type": "integer", "description": "Max comics to return (1-50, default 20)."}
  }
}`),
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var a struct {
				Q        string `json:"q"`
				Progress string `json:"progress"`
				Limit    int    `json:"limit"`
			}
			if err := json.Unmarshal(args, &a); err != nil {
				return "", fmt.Errorf("invalid arguments")
			}
			limit := a.Limit
			if limit <= 0 {
				limit = 20
			}
			if limit > 50 {
				limit = 50
			}
			opts := store.ListComicOptions{
				Shared:   "all",
				Progress: a.Progress,
			}
			var (
				comics []store.Comic
				err    error
			)
			if strings.TrimSpace(a.Q) != "" {
				comics, _, err = s.Store.SearchComics(userID, strings.TrimSpace(a.Q), limit, 0, "created", "desc", opts)
			} else {
				comics, _, err = s.Store.ListComics(userID, limit, 0, "created", "desc", opts)
			}
			if err != nil {
				return "", err
			}
			out := make([]map[string]any, 0, len(comics))
			for _, c := range comics {
				out = append(out, map[string]any{
					"id": c.ID, "title": c.SourceTitle, "author": c.SourceAuthor,
					"chapterCount": c.ChapterCount, "downloadedCount": c.DownloadedCount,
					"status": c.Status, "isPublic": c.IsPublic,
				})
			}
			return marshalToolResult(out)
		},
	}
}

func (s *Server) agentToolGetComic(userID string) ai.AgentTool {
	return ai.AgentTool{
		Name:        "get_comic",
		Description: "Get one comic of the user's library by id: metadata plus download stats.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "id": {"type": "string", "description": "Comic id."}
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
			comic, err := s.Store.GetOwnedComic(userID, strings.TrimSpace(a.ID))
			if err != nil {
				return "", err
			}
			stats, statsErr := s.Store.GetOwnedComicChapterStats(userID, comic.ID)
			if statsErr != nil {
				return "", statsErr
			}
			return marshalToolResult(map[string]any{
				"id": comic.ID, "title": comic.SourceTitle, "author": comic.SourceAuthor,
				"description": comic.SourceDescription, "status": comic.Status,
				"tags": comic.Tags, "url": comic.URL,
				"stats": stats,
			})
		},
	}
}

func (s *Server) agentToolGetComicChapters(userID string) ai.AgentTool {
	return ai.AgentTool{
		Name:        "get_comic_chapters",
		Description: "List chapter summaries of one of the user's comics, optionally bounded by an inclusive chapter_order range.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "id": {"type": "string", "description": "Comic id."},
    "from": {"type": "integer", "description": "Minimum chapter_order (optional)."},
    "to": {"type": "integer", "description": "Maximum chapter_order; -1 means unbounded (optional)."},
    "limit": {"type": "integer", "description": "Max chapters to return (1-100, default 30)."}
  },
  "required": ["id"]
}`),
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var a struct {
				ID    string `json:"id"`
				From  int    `json:"from"`
				To    int    `json:"to"`
				Limit int    `json:"limit"`
			}
			if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.ID) == "" {
				return "", fmt.Errorf("invalid arguments")
			}
			limit := a.Limit
			if limit <= 0 {
				limit = 30
			}
			if limit > 100 {
				limit = 100
			}
			from := a.From
			if from < 0 {
				from = 0
			}
			to := a.To
			if to < 0 {
				to = -1
			}
			summaries, _, err := s.Store.GetOwnedComicChapterSummariesInOrderRange(userID, strings.TrimSpace(a.ID), from, to, limit)
			if err != nil {
				return "", err
			}
			return marshalToolResult(summaries)
		},
	}
}

func (s *Server) agentToolCheckComicUpdates(userID string) ai.AgentTool {
	return ai.AgentTool{
		Name:        "check_comic_updates",
		Description: "Re-fetch one of the user's comics from its source site and report how many new chapters appeared since the last check.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "id": {"type": "string", "description": "Comic id."}
  },
  "required": ["id"]
}`),
		Execute: func(ctx context.Context, args json.RawMessage) (string, error) {
			var a struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.ID) == "" {
				return "", fmt.Errorf("invalid id")
			}
			comic, err := s.Store.GetOwnedComic(userID, strings.TrimSpace(a.ID))
			if err != nil {
				return "", err
			}
			if strings.TrimSpace(comic.URL) == "" {
				return "", fmt.Errorf("this comic has no source URL")
			}
			summary, _, err := s.runComicUpdateCheck(ctx, userID, comic)
			if err != nil {
				return "", err
			}
			return marshalToolResult(summary)
		},
	}
}

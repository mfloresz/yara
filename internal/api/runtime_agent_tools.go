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

// Limits bounding the tools added for jobs, catalog questions, the glossary,
// source updates, cleanups, reading progress and exports. Every tool result is
// persisted into the session trail and replayed to the model on every later
// step, so each result stays a few KB at most — the caps below are the
// mechanism, not the model's discipline.
const (
	agentJobOverviewDefaultLimit = 10
	agentJobOverviewMaxLimit     = 20
	agentJobErrorMaxChars        = 200
	agentCreateJobMaxChapters    = 500
	agentCatalogDefaultLimit     = 20
	agentCatalogMaxLimit         = 50
	agentGlossaryDefaultLimit    = 50
	agentGlossaryMaxLimit        = 200
	agentGlossaryContextMaxChars = 200
	agentGlossaryBatchMax        = 100
	agentCleanupPreviewMax       = 25
	// agentCleanupProposeMax caps one cleanup proposal. The apply itself goes
	// through the HTTP clean endpoint, which has no per-call cap, so this is
	// only a sanity bound on what the model may propose in one card.
	agentCleanupProposeMax   = 500
	agentCleanupSampleHunks  = 5
	agentCleanupSampleLineMax    = 200
	agentEpubListMax             = 20
	// agentEpubMaxSourceChars caps the text the assistant may feed to the EPUB
	// builder. The builder materializes every chapter body plus the cover and
	// the generated file, all inside one agent turn that shares the process
	// with other users' turns.
	agentEpubMaxSourceChars = 3_000_000
)

// clampAgentLimit normalizes an optional tool limit argument.
func clampAgentLimit(value, def, max int) int {
	if value <= 0 {
		return def
	}
	if value > max {
		return max
	}
	return value
}

func truncateAgentRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return store.TruncateRunes(s, max) + "…"
}

// firstNonEmptyLine returns the first line with visible content, or "".
func firstNonEmptyLine(lines []string) string {
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			return line
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Catalog questions: tags, authors, series
// ---------------------------------------------------------------------------

func (s *Server) agentToolListTags(userID string) ai.AgentTool {
	return ai.AgentTool{
		Name: "list_tags",
		Description: "List the distinct tags across the user's OWN novels, alphabetically. query is a partial, accent-insensitive match. " +
			"To find the novels carrying a tag, use list_novels with field=tags.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "description": "Optional partial tag text to filter by."},
    "limit": {"type": "integer", "minimum": 1, "maximum": 50, "description": "Max tags (default 20)."}
  }
}`),
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var a struct {
				Query string `json:"query"`
				Limit int    `json:"limit"`
			}
			_ = json.Unmarshal(args, &a)
			tags, err := s.Store.ListNovelTagSuggestions(userID, a.Query, clampAgentLimit(a.Limit, agentCatalogDefaultLimit, agentCatalogMaxLimit))
			if err != nil {
				return "", err
			}
			return marshalToolResult(map[string]any{"tags": tags, "returned": len(tags)})
		},
	}
}

func (s *Server) agentToolListAuthors(userID string) ai.AgentTool {
	return ai.AgentTool{
		Name:        "list_authors",
		Description: "List the distinct authors across the user's OWN novels. query is a PARTIAL, case-insensitive match, so \"Cris\" also finds \"TM Cris\" — use it whenever the user half-remembers a name, then list the novels of each match with list_novels field=author.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "description": "Optional partial author name to filter by."},
    "limit": {"type": "integer", "minimum": 1, "maximum": 50, "description": "Max authors (default 20)."}
  }
}`),
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var a struct {
				Query string `json:"query"`
				Limit int    `json:"limit"`
			}
			_ = json.Unmarshal(args, &a)
			authors, err := s.Store.ListNovelAuthorSuggestions(userID, a.Query, clampAgentLimit(a.Limit, agentCatalogDefaultLimit, agentCatalogMaxLimit))
			if err != nil {
				return "", err
			}
			return marshalToolResult(map[string]any{"authors": authors, "returned": len(authors)})
		},
	}
}

func (s *Server) agentToolListSeries(userID string) ai.AgentTool {
	return ai.AgentTool{
		Name: "list_series",
		Description: "Aggregate the user's OWN novels per series with chapter translation progress (target series falling back to source). " +
			"complete=complete answers \"which series are fully translated\"; complete=incomplete the rest. query filters by partial series name.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "description": "Optional partial series name to filter by."},
    "complete": {"type": "string", "enum": ["all", "complete", "incomplete"], "description": "Filter by translation completeness (default all)."},
    "limit": {"type": "integer", "minimum": 1, "maximum": 50, "description": "Max series (default 20)."}
  }
}`),
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var a struct {
				Query    string `json:"query"`
				Complete string `json:"complete"`
				Limit    int    `json:"limit"`
			}
			_ = json.Unmarshal(args, &a)
			switch a.Complete {
			case "", "all", "complete", "incomplete":
			default:
				return "", fmt.Errorf("invalid complete %q: use all, complete or incomplete", a.Complete)
			}
			series, err := s.Store.ListOwnedSeriesProgress(userID, a.Query, defaultString(a.Complete, "all"), clampAgentLimit(a.Limit, agentCatalogDefaultLimit, agentCatalogMaxLimit))
			if err != nil {
				return "", err
			}
			return marshalToolResult(map[string]any{"series": series, "returned": len(series)})
		},
	}
}

// ---------------------------------------------------------------------------
// Jobs
// ---------------------------------------------------------------------------

// agentJobOverview shapes a JobOverview for the model: errorMessage truncated,
// counters only — chapter id lists and job options are never included.
func agentJobOverview(j store.JobOverview) map[string]any {
	return map[string]any{
		"id":                j.ID,
		"novelId":           j.NovelID,
		"novelTitle":        j.NovelTitle,
		"operation":         j.Operation,
		"status":            j.Status,
		"provider":          j.Provider,
		"model":             j.Model,
		"totalChapters":     j.TotalChapters,
		"completedChapters": j.CompletedChapters,
		"failedChapters":    j.FailedChapters,
		"errorMessage":      truncateAgentRunes(j.ErrorMessage, agentJobErrorMaxChars),
		"createdAt":         j.CreatedAt,
	}
}

func (s *Server) agentToolGetActiveJobs(userID string) ai.AgentTool {
	return ai.AgentTool{
		Name:        "get_active_jobs",
		Description: "List the user's pending/running jobs across ALL their novels: operation, progress counters and errors, newest first. Answer \"what is being translated right now\" with this, and \"which novels still have unfinished chapters\" with query_library (chapters_pending > 0).",
		InputSchema: json.RawMessage(`{"type": "object", "properties": {}}`),
		Execute: func(_ context.Context, _ json.RawMessage) (string, error) {
			jobs, err := s.Store.ListOwnedJobOverviews(userID, "", true, false, agentJobOverviewMaxLimit)
			if err != nil {
				return "", err
			}
			out := make([]map[string]any, 0, len(jobs))
			for _, j := range jobs {
				out = append(out, agentJobOverview(j))
			}
			return marshalToolResult(map[string]any{"jobs": out, "hasActive": len(out) > 0})
		},
	}
}

func (s *Server) agentToolGetNovelJobs(userID string) ai.AgentTool {
	return ai.AgentTool{
		Name:        "get_novel_jobs",
		Description: "List the jobs of one of the user's OWN novels, newest first — including finished ones (use failedOnly to focus on failures). Retries and cancellations are separate tools.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "novelId": {"type": "string", "description": "Novel id."},
    "failedOnly": {"type": "boolean", "description": "Only jobs that failed or have failed chapters."},
    "limit": {"type": "integer", "minimum": 1, "maximum": 20, "description": "Max jobs (default 10)."}
  },
  "required": ["novelId"]
}`),
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var a struct {
				NovelID    string `json:"novelId"`
				FailedOnly bool   `json:"failedOnly"`
				Limit      int    `json:"limit"`
			}
			if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.NovelID) == "" {
				return "", fmt.Errorf("invalid novelId")
			}
			if _, err := s.Store.GetOwnedNovel(userID, strings.TrimSpace(a.NovelID)); err != nil {
				return "", err
			}
			jobs, err := s.Store.ListOwnedJobOverviews(userID, strings.TrimSpace(a.NovelID), false, a.FailedOnly, clampAgentLimit(a.Limit, agentJobOverviewDefaultLimit, agentJobOverviewMaxLimit))
			if err != nil {
				return "", err
			}
			out := make([]map[string]any, 0, len(jobs))
			for _, j := range jobs {
				out = append(out, agentJobOverview(j))
			}
			return marshalToolResult(map[string]any{"jobs": out, "total": len(out)})
		},
	}
}

// agentCreateJobOperations is the closed set the model may enqueue. Downloads
// are missing on purpose: a download job's options carry the fetched chapter
// list, which only update_novel_from_url builds.
var agentCreateJobOperations = map[string]bool{
	"translate": true,
	"refine":    true,
	"check":     true,
}

func (s *Server) agentToolCreateJob(userID string) ai.AgentTool {
	return ai.AgentTool{
		Name: "create_job",
		Description: "Enqueue a background job on one of the user's OWN novels: operation translate, refine or check. " +
			"Select chapters with chapterIds, or a contiguous fromOrder/toOrder range (both bounds required), or neither for the whole novel. " +
			"chapterIds may come from get_novel_chapters, search_chapters or a query_library analysis (e.g. re-translate the chapters whose translation came out too short) — the job overwrites the previous translation, and excluded chapters are rejected. " +
			"Jobs run in the background after this turn; report the jobId and do not poll in a loop. Downloads never go through here: use update_novel_from_url. " +
			"Refused while the novel already has an active job.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "novelId": {"type": "string", "description": "Novel id."},
    "operation": {"type": "string", "enum": ["translate", "refine", "check"]},
    "chapterIds": {"type": "array", "items": {"type": "string"}, "description": "Explicit chapter ids (from get_novel_chapters, search_chapters or query_library; excluded chapters are rejected)."},
    "fromOrder": {"type": "integer", "minimum": 1, "description": "First chapter order of the range (inclusive; requires toOrder)."},
    "toOrder": {"type": "integer", "minimum": 1, "description": "Last chapter order of the range (inclusive; requires fromOrder)."},
    "provider": {"type": "string", "description": "Optional provider override."},
    "model": {"type": "string", "description": "Optional model override."}
  },
  "required": ["novelId", "operation"]
}`),
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var a struct {
				NovelID    string   `json:"novelId"`
				Operation  string   `json:"operation"`
				ChapterIDs []string `json:"chapterIds"`
				FromOrder  *int     `json:"fromOrder"`
				ToOrder    *int     `json:"toOrder"`
				Provider   string   `json:"provider"`
				Model      string   `json:"model"`
			}
			if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.NovelID) == "" {
				return "", fmt.Errorf("invalid novelId")
			}
			if !agentCreateJobOperations[a.Operation] {
				return "", fmt.Errorf("invalid operation %q: use translate, refine or check (downloads go through update_novel_from_url)", a.Operation)
			}
			novelID := strings.TrimSpace(a.NovelID)
			if _, err := s.Store.GetOwnedNovel(userID, novelID); err != nil {
				return "", err
			}
			var ids []string
			if a.Operation == "check" {
				if len(a.ChapterIDs) > 0 || a.FromOrder != nil || a.ToOrder != nil {
					return "", fmt.Errorf("check scans the whole source TOC: omit chapterIds and the order range")
				}
			} else {
				var err error
				ids, err = s.resolveAgentJobChapters(userID, novelID, a.ChapterIDs, a.FromOrder, a.ToOrder)
				if err != nil {
					return "", err
				}
			}
			idsJSON, _ := json.Marshal(ids)
			job := &store.Job{
				NovelID:       novelID,
				Status:        "pending",
				Operation:     a.Operation,
				Provider:      a.Provider,
				Model:         a.Model,
				ChapterIDs:    string(idsJSON),
				OptionsJSON:   "{}",
				TotalChapters: len(ids),
			}
			// Same admission as the HTTP handler: one active job per novel,
			// lock held until the job is created and enqueued.
			unlock, err := s.acquireNovelJobSlot(novelID)
			if err != nil {
				if errors.Is(err, errNovelBusy) {
					return "", fmt.Errorf("this novel already has an active job; use get_active_jobs to see it")
				}
				return "", err
			}
			defer unlock()
			if err := s.Store.CreateJob(userID, job); err != nil {
				return "", err
			}
			if len(ids) > 0 {
				if err := s.Store.UpdateChaptersStatusFast(ids, "processing", ""); err != nil {
					return "", err
				}
			}
			if !s.enqueueJob(job.ID) {
				if err := s.Store.ReconcileProcessingChaptersForJob(job.ID); err != nil {
					return "", err
				}
				return "", fmt.Errorf("the job queue is full; try again in a few minutes")
			}
			return marshalToolResult(map[string]any{
				"jobId":         job.ID,
				"operation":     job.Operation,
				"totalChapters": job.TotalChapters,
				"status":        job.Status,
			})
		},
	}
}

// resolveAgentJobChapters turns the create_job selection arguments into a
// bounded, validated chapter id list. Only ids are read, never bodies. Explicit
// chapterIds are checked against the novel's eligible ids so a typo cannot
// silently translate nothing.
func (s *Server) resolveAgentJobChapters(userID, novelID string, chapterIDs []string, from, to *int) ([]string, error) {
	if len(chapterIDs) > 0 {
		if from != nil || to != nil {
			return nil, fmt.Errorf("pass either chapterIds or fromOrder/toOrder, not both")
		}
		all, err := s.Store.ListOwnedChapterIDsInOrderRange(userID, novelID, nil, nil)
		if err != nil {
			return nil, err
		}
		known := make(map[string]bool, len(all))
		for _, id := range all {
			known[id] = true
		}
		ids := make([]string, 0, len(chapterIDs))
		for _, id := range chapterIDs {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if !known[id] {
				return nil, fmt.Errorf("chapterId %q does not belong to this novel (or is excluded)", id)
			}
			ids = append(ids, id)
		}
		if len(ids) == 0 {
			return nil, fmt.Errorf("chapterIds is required")
		}
		if len(ids) > agentCreateJobMaxChapters {
			return nil, fmt.Errorf("too many chapters (%d): split the request into calls of at most %d", len(ids), agentCreateJobMaxChapters)
		}
		return ids, nil
	}
	if (from == nil) != (to == nil) {
		return nil, fmt.Errorf("provide both fromOrder and toOrder, or neither")
	}
	ids, err := s.Store.ListOwnedChapterIDsInOrderRange(userID, novelID, from, to)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("no eligible chapters in that range")
	}
	if len(ids) > agentCreateJobMaxChapters {
		return nil, fmt.Errorf("too many chapters (%d) in that range: split it into calls of at most %d", len(ids), agentCreateJobMaxChapters)
	}
	return ids, nil
}

func (s *Server) agentToolCancelJob(userID string) ai.AgentTool {
	return ai.AgentTool{
		Name:        "cancel_job",
		Description: "Cancel one of the user's OWN jobs by id (get_active_jobs / get_novel_jobs list them). Pending jobs leave the queue; running ones stop at the next chapter boundary.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "jobId": {"type": "string", "description": "Job id."}
  },
  "required": ["jobId"]
}`),
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var a struct {
				JobID string `json:"jobId"`
			}
			if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.JobID) == "" {
				return "", fmt.Errorf("invalid jobId")
			}
			jobID := strings.TrimSpace(a.JobID)
			job, err := s.Store.GetOwnedJob(userID, jobID)
			if err != nil {
				// Same masking as the HTTP route: a foreign job id reads as
				// missing, never as somebody else's.
				return "", fmt.Errorf("job not found")
			}
			if err := s.Store.UpdateJobForUser(userID, jobID, map[string]any{"status": "cancelled"}); err != nil {
				return "", err
			}
			s.cancelJob(jobID)
			s.onJobCancelled(jobID)
			if err := s.Store.ReconcileProcessingChaptersForJob(jobID); err != nil {
				return "", err
			}
			if err := s.Store.RecalculateNovelStats(job.NovelID); err != nil {
				return "", err
			}
			return marshalToolResult(map[string]any{"ok": true, "jobId": jobID, "status": "cancelled"})
		},
	}
}

func (s *Server) agentToolRetryJob(userID string) ai.AgentTool {
	return ai.AgentTool{
		Name:        "retry_job",
		Description: "Re-queue one of the user's OWN failed or cancelled jobs by id. Refused while the job is still pending or running.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "jobId": {"type": "string", "description": "Job id."}
  },
  "required": ["jobId"]
}`),
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var a struct {
				JobID string `json:"jobId"`
			}
			if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.JobID) == "" {
				return "", fmt.Errorf("invalid jobId")
			}
			jobID := strings.TrimSpace(a.JobID)
			job, err := s.Store.GetOwnedJob(userID, jobID)
			if err != nil {
				return "", fmt.Errorf("job not found")
			}
			if job.Status == "pending" || job.Status == "running" {
				return "", fmt.Errorf("job is already active and cannot be re-queued; cancel it first")
			}
			if err := s.Store.UpdateJobForUser(userID, jobID, map[string]any{"status": "pending"}); err != nil {
				return "", err
			}
			if !s.enqueueJob(jobID) {
				return "", fmt.Errorf("the job queue is full; try again in a few minutes")
			}
			return marshalToolResult(map[string]any{"ok": true, "jobId": jobID, "status": "pending"})
		},
	}
}

// ---------------------------------------------------------------------------
// Glossary
// ---------------------------------------------------------------------------

// parseAgentGlossary decodes the novel's glossary JSON; corrupt content
// degrades to an empty list instead of failing the turn.
func parseAgentGlossary(raw string) []ai.GlossaryEntry {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var entries []ai.GlossaryEntry
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil
	}
	return entries
}

func (s *Server) agentToolGetGlossary(userID string) ai.AgentTool {
	return ai.AgentTool{
		Name:        "get_glossary",
		Description: "Read the glossary of one of the user's OWN novels: term pairs with optional context. Filter with query (partial match on source or target) and page with offset/limit instead of pulling the whole glossary.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "novelId": {"type": "string", "description": "Novel id."},
    "query": {"type": "string", "description": "Optional partial text to filter terms by."},
    "offset": {"type": "integer", "minimum": 0, "description": "Entries to skip (default 0)."},
    "limit": {"type": "integer", "minimum": 1, "maximum": 200, "description": "Max entries (default 50)."}
  },
  "required": ["novelId"]
}`),
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var a struct {
				NovelID string `json:"novelId"`
				Query   string `json:"query"`
				Offset  int    `json:"offset"`
				Limit   int    `json:"limit"`
			}
			if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.NovelID) == "" {
				return "", fmt.Errorf("invalid novelId")
			}
			novel, err := s.Store.GetOwnedNovel(userID, strings.TrimSpace(a.NovelID))
			if err != nil {
				return "", err
			}
			entries := parseAgentGlossary(novel.Glossary)
			query := strings.ToLower(strings.TrimSpace(a.Query))
			filtered := make([]ai.GlossaryEntry, 0, len(entries))
			for _, e := range entries {
				if query == "" ||
					strings.Contains(strings.ToLower(e.Source), query) ||
					strings.Contains(strings.ToLower(e.Target), query) {
					filtered = append(filtered, e)
				}
			}
			offset := a.Offset
			if offset < 0 {
				offset = 0
			}
			if offset > len(filtered) {
				offset = len(filtered)
			}
			limit := clampAgentLimit(a.Limit, agentGlossaryDefaultLimit, agentGlossaryMaxLimit)
			end := offset + limit
			if end > len(filtered) {
				end = len(filtered)
			}
			page := make([]map[string]any, 0, end-offset)
			for _, e := range filtered[offset:end] {
				page = append(page, map[string]any{
					"source":  e.Source,
					"target":  e.Target,
					"context": truncateAgentRunes(e.Context, agentGlossaryContextMaxChars),
				})
			}
			return marshalToolResult(map[string]any{
				"total":    len(filtered),
				"returned": len(page),
				"offset":   offset,
				"entries":  page,
			})
		},
	}
}

func (s *Server) agentToolUpdateGlossary(userID string) ai.AgentTool {
	return ai.AgentTool{
		Name: "update_glossary",
		Description: "Edit the glossary of one of the user's OWN novels: upsert matches entries by source term (case-insensitive) and remove deletes them. " +
			"Refused while the novel has active jobs — a running glossary generation would overwrite the edit.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "novelId": {"type": "string", "description": "Novel id."},
    "upsert": {
      "type": "array", "maxItems": 100,
      "items": {
        "type": "object",
        "properties": {
          "source": {"type": "string", "description": "Original-language term."},
          "target": {"type": "string", "description": "Translation to enforce."},
          "context": {"type": "string", "description": "Optional usage note."}
        },
        "required": ["source", "target"]
      }
    },
    "remove": {"type": "array", "items": {"type": "string"}, "maxItems": 100, "description": "Source terms to delete."}
  },
  "required": ["novelId"]
}`),
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var a struct {
				NovelID string             `json:"novelId"`
				Upsert  []ai.GlossaryEntry `json:"upsert"`
				Remove  []string           `json:"remove"`
			}
			if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.NovelID) == "" {
				return "", fmt.Errorf("invalid novelId")
			}
			if len(a.Upsert) == 0 && len(a.Remove) == 0 {
				return "", fmt.Errorf("nothing to do: provide upsert or remove entries")
			}
			if len(a.Upsert) > agentGlossaryBatchMax || len(a.Remove) > agentGlossaryBatchMax {
				return "", fmt.Errorf("too many entries in one call: at most %d", agentGlossaryBatchMax)
			}
			novelID := strings.TrimSpace(a.NovelID)
			novel, err := s.Store.GetOwnedNovel(userID, novelID)
			if err != nil {
				return "", err
			}
			active, err := s.Store.HasActiveJobsForNovel(novelID)
			if err != nil {
				return "", err
			}
			if active {
				return "", fmt.Errorf("refused while the novel has active jobs: a running glossary generation would overwrite this edit")
			}
			entries := parseAgentGlossary(novel.Glossary)
			removeSet := make(map[string]bool, len(a.Remove))
			for _, term := range a.Remove {
				removeSet[strings.ToLower(strings.TrimSpace(term))] = true
			}
			kept := make([]ai.GlossaryEntry, 0, len(entries))
			removed := 0
			for _, e := range entries {
				if removeSet[strings.ToLower(strings.TrimSpace(e.Source))] {
					removed++
					continue
				}
				kept = append(kept, e)
			}
			entries = kept
			index := make(map[string]int, len(entries))
			for i, e := range entries {
				index[strings.ToLower(strings.TrimSpace(e.Source))] = i
			}
			added, updated := 0, 0
			for _, e := range a.Upsert {
				src := strings.TrimSpace(e.Source)
				tgt := strings.TrimSpace(e.Target)
				if src == "" || tgt == "" {
					return "", fmt.Errorf("glossary entries need non-empty source and target")
				}
				key := strings.ToLower(src)
				entry := ai.GlossaryEntry{Source: src, Target: tgt, Context: strings.TrimSpace(e.Context)}
				if idx, ok := index[key]; ok {
					entries[idx] = entry
					updated++
					continue
				}
				entries = append(entries, entry)
				index[key] = len(entries) - 1
				added++
			}
			b, err := json.Marshal(entries)
			if err != nil {
				return "", err
			}
			if err := s.Store.UpdateNovelGlossary(userID, novelID, string(b)); err != nil {
				return "", err
			}
			return marshalToolResult(map[string]any{
				"ok":      true,
				"added":   added,
				"updated": updated,
				"removed": removed,
				"total":   len(entries),
			})
		},
	}
}

func (s *Server) agentToolGenerateGlossary(userID string) ai.AgentTool {
	return ai.AgentTool{
		Name: "generate_glossary",
		Description: "Enqueue a background glossary generation over a chapter range of one of the user's OWN novels (both bounds are chapter orders; chapterTo omitted means to the end). " +
			"Report the jobId; the glossary is written when the job finishes.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "novelId": {"type": "string", "description": "Novel id."},
    "chapterFrom": {"type": "integer", "minimum": 1, "description": "First chapter order to scan."},
    "chapterTo": {"type": "integer", "minimum": 1, "description": "Last chapter order to scan (omit for to the end)."},
    "mode": {"type": "string", "enum": ["together", "batch"], "description": "One pass over everything (default) or batched by token budget."},
    "includeExisting": {"type": "boolean", "description": "Rescan chapters that already produced terms (default true)."},
    "provider": {"type": "string", "description": "Optional provider override."},
    "model": {"type": "string", "description": "Optional model override."}
  },
  "required": ["novelId", "chapterFrom"]
}`),
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var a struct {
				NovelID         string `json:"novelId"`
				ChapterFrom     int    `json:"chapterFrom"`
				ChapterTo       int    `json:"chapterTo"`
				Mode            string `json:"mode"`
				IncludeExisting *bool  `json:"includeExisting"`
				Provider        string `json:"provider"`
				Model           string `json:"model"`
			}
			if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.NovelID) == "" {
				return "", fmt.Errorf("invalid novelId")
			}
			if a.ChapterFrom <= 0 {
				return "", fmt.Errorf("chapterFrom must be positive")
			}
			if a.ChapterTo > 0 && a.ChapterTo < a.ChapterFrom {
				return "", fmt.Errorf("chapterTo must be >= chapterFrom")
			}
			switch a.Mode {
			case "", "together", "batch":
			default:
				return "", fmt.Errorf("mode must be 'together' or 'batch'")
			}
			novelID := strings.TrimSpace(a.NovelID)
			if _, err := s.Store.GetOwnedNovel(userID, novelID); err != nil {
				return "", err
			}
			job, err := s.submitGlossaryJob(userID, novelID, glossaryJobOptions{
				ChapterFrom:     a.ChapterFrom,
				ChapterTo:       a.ChapterTo,
				Mode:            defaultString(a.Mode, "together"),
				Provider:        a.Provider,
				Model:           a.Model,
				IncludeExisting: a.IncludeExisting,
			})
			if err != nil {
				switch {
				case errors.Is(err, errGlossaryNoChapters):
					return "", fmt.Errorf("%s", err.Error())
				case errors.Is(err, errNovelBusy):
					return "", fmt.Errorf("this novel already has an active job; use get_active_jobs to see it")
				case errors.Is(err, errImportQueueFull):
					return "", fmt.Errorf("the job queue is full; try again in a few minutes")
				}
				return "", err
			}
			return marshalToolResult(map[string]any{
				"jobId":       job.ID,
				"operation":   job.Operation,
				"status":      job.Status,
				"chapterFrom": a.ChapterFrom,
				"chapterTo":   a.ChapterTo,
			})
		},
	}
}

// ---------------------------------------------------------------------------
// Source updates: check + download new chapters
// ---------------------------------------------------------------------------

func (s *Server) agentToolCheckNovelUpdates(userID string) ai.AgentTool {
	return ai.AgentTool{
		Name: "check_novel_updates",
		Description: "Fetch the source site's TOC of one of the user's OWN novels and report how many chapters are new (first/last new order included). " +
			"Read-only: to actually download them, call update_novel_from_url afterwards.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "novelId": {"type": "string", "description": "Novel id."}
  },
  "required": ["novelId"]
}`),
		Execute: func(ctx context.Context, args json.RawMessage) (string, error) {
			var a struct {
				NovelID string `json:"novelId"`
			}
			if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.NovelID) == "" {
				return "", fmt.Errorf("invalid novelId")
			}
			novel, err := s.Store.GetOwnedNovel(userID, strings.TrimSpace(a.NovelID))
			if err != nil {
				return "", err
			}
			if strings.TrimSpace(novel.URL) == "" {
				return "", fmt.Errorf("this novel has no source URL to check")
			}
			summary, _, err := s.runNovelUpdateCheck(ctx, userID, novel)
			if err != nil {
				return "", mapAgentImportError(err)
			}
			return marshalToolResult(summary)
		},
	}
}

func (s *Server) agentToolUpdateNovelFromURL(userID string) ai.AgentTool {
	return ai.AgentTool{
		Name: "update_novel_from_url",
		Description: "Download the new chapters of one of the user's OWN novels from its source URL: diffs the site's TOC against the stored chapters and enqueues a background download job. " +
			"Optionally restrict the claimed orders with startChapter/endChapter. Report the jobId; the download continues after this turn.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "novelId": {"type": "string", "description": "Novel id."},
    "startChapter": {"type": "integer", "minimum": 1, "description": "First new chapter order to claim."},
    "endChapter": {"type": "integer", "minimum": 1, "description": "Last new chapter order to claim."}
  },
  "required": ["novelId"]
}`),
		Execute: func(ctx context.Context, args json.RawMessage) (string, error) {
			var a struct {
				NovelID      string `json:"novelId"`
				StartChapter int    `json:"startChapter"`
				EndChapter   int    `json:"endChapter"`
			}
			if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.NovelID) == "" {
				return "", fmt.Errorf("invalid novelId")
			}
			if a.StartChapter > 0 && a.EndChapter > 0 && a.StartChapter > a.EndChapter {
				return "", fmt.Errorf("invalid chapter range: startChapter must be <= endChapter")
			}
			novel, err := s.Store.GetOwnedNovel(userID, strings.TrimSpace(a.NovelID))
			if err != nil {
				return "", err
			}
			if strings.TrimSpace(novel.URL) == "" {
				return "", fmt.Errorf("this novel has no source URL to update from")
			}
			result, err := s.updateNovelFromSource(ctx, userID, novel, a.StartChapter, a.EndChapter)
			if err != nil {
				return "", mapAgentImportError(err)
			}
			if result.ChaptersQueued == 0 {
				return marshalToolResult(map[string]any{
					"chaptersQueued": 0,
					"totalChapters":  result.TotalChapters,
					"message":        "the novel is already up to date",
				})
			}
			return marshalToolResult(map[string]any{
				"chaptersQueued":  result.ChaptersQueued,
				"firstNewChapter": result.FirstNewChapter,
				"lastNewChapter":  result.LastNewChapter,
				"jobId":           result.JobID,
				"totalChapters":   result.TotalChapters,
			})
		},
	}
}

// mapAgentImportError renders the shared import cores' error taxonomy as
// tool-error text — the same cases the HTTP handler maps to status codes.
func mapAgentImportError(err error) error {
	var storeErr *importStoreError
	if errors.As(err, &storeErr) {
		return fmt.Errorf("%s", storeErr.msg)
	}
	if errors.Is(err, errImportQueueFull) {
		return fmt.Errorf("the job queue is full; try again in a few minutes")
	}
	if errors.Is(err, errNovelBusy) {
		return fmt.Errorf("this novel already has an active job; use get_active_jobs to see it")
	}
	return fmt.Errorf("%s", parserErrorMessage(err))
}

// ---------------------------------------------------------------------------
// Chapter cleanup
// ---------------------------------------------------------------------------

type agentCleanupArgs struct {
	NovelID       string   `json:"novelId"`
	ChapterIDs    []string `json:"chapterIds"`
	FromOrder     *int     `json:"fromOrder"`
	ToOrder       *int     `json:"toOrder"`
	Mode          string   `json:"mode"`
	SearchText    string   `json:"searchText"`
	ReplaceText   string   `json:"replaceText"`
	CaseSensitive bool     `json:"caseSensitive"`
	UseRegex      bool     `json:"useRegex"`
	ApplyTo       string   `json:"applyTo"`
}

// resolveAgentCleanupChapters turns the selection arguments into a bounded,
// validated chapter id list — the same id-only reads as create_job.
func (s *Server) resolveAgentCleanupChapters(userID string, a *agentCleanupArgs, max int) ([]string, error) {
	if len(a.ChapterIDs) > 0 {
		if a.FromOrder != nil || a.ToOrder != nil {
			return nil, fmt.Errorf("pass either chapterIds or fromOrder/toOrder, not both")
		}
		all, err := s.Store.ListOwnedChapterIDsInOrderRange(userID, strings.TrimSpace(a.NovelID), nil, nil)
		if err != nil {
			return nil, err
		}
		known := make(map[string]bool, len(all))
		for _, id := range all {
			known[id] = true
		}
		ids := make([]string, 0, len(a.ChapterIDs))
		for _, id := range a.ChapterIDs {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if !known[id] {
				return nil, fmt.Errorf("chapterId %q does not belong to this novel (or is excluded)", id)
			}
			ids = append(ids, id)
		}
		if len(ids) == 0 {
			return nil, fmt.Errorf("chapterIds is required")
		}
		if len(ids) > max {
			return nil, fmt.Errorf("too many chapters (%d): process at most %d per call", len(ids), max)
		}
		return ids, nil
	}
	if a.FromOrder == nil || a.ToOrder == nil {
		return nil, fmt.Errorf("provide chapterIds, or both fromOrder and toOrder")
	}
	ids, err := s.Store.ListOwnedChapterIDsInOrderRange(userID, strings.TrimSpace(a.NovelID), a.FromOrder, a.ToOrder)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("no eligible chapters in that range")
	}
	if len(ids) > max {
		return nil, fmt.Errorf("too many chapters (%d) in that range: process at most %d per call", len(ids), max)
	}
	return ids, nil
}

func (s *Server) agentCleanupOptions(a *agentCleanupArgs) (CleanOptions, error) {
	if !isValidCleanMode(a.Mode) {
		return CleanOptions{}, fmt.Errorf("invalid mode")
	}
	if !isValidApplyTo(a.ApplyTo) {
		return CleanOptions{}, fmt.Errorf("invalid applyTo: use original, translated, refined or all")
	}
	return CleanOptions{
		Mode:          CleanMode(a.Mode),
		SearchText:    a.SearchText,
		ReplaceText:   a.ReplaceText,
		CaseSensitive: a.CaseSensitive,
		UseRegex:      a.UseRegex,
	}, nil
}

func (s *Server) agentToolPreviewChapterCleanup(userID string) ai.AgentTool {
	return ai.AgentTool{
		Name: "preview_chapter_cleanup",
		Description: "Dry-run a cleanup over up to " + strconv.Itoa(agentCleanupPreviewMax) + " chapters of one of the user's OWN novels and return, per changed chapter, how many edits it would receive plus a small sample. " +
			"Use it to sanity-check a rule before propose_cleanup, which surfaces the interactive approval card the user acts on. " +
			"Modes and applyTo follow the app's cleanup rules; searchText/replaceText apply to the replace mode.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "novelId": {"type": "string", "description": "Novel id."},
    "chapterIds": {"type": "array", "items": {"type": "string"}, "description": "Explicit chapter ids."},
    "fromOrder": {"type": "integer", "minimum": 1, "description": "First chapter order (inclusive; requires toOrder)."},
    "toOrder": {"type": "integer", "minimum": 1, "description": "Last chapter order (inclusive; requires fromOrder)."},
    "mode": {"type": "string", "description": "Cleanup mode, as in the app's cleanup editor."},
    "searchText": {"type": "string"},
    "replaceText": {"type": "string"},
    "caseSensitive": {"type": "boolean"},
    "useRegex": {"type": "boolean"},
    "applyTo": {"type": "string", "enum": ["original", "translated", "refined", "all"]}
  },
  "required": ["novelId", "mode", "applyTo"]
}`),
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var a agentCleanupArgs
			if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.NovelID) == "" {
				return "", fmt.Errorf("invalid novelId")
			}
			opts, err := s.agentCleanupOptions(&a)
			if err != nil {
				return "", err
			}
			novelID := strings.TrimSpace(a.NovelID)
			if _, err := s.Store.GetOwnedNovel(userID, novelID); err != nil {
				return "", err
			}
			ids, err := s.resolveAgentCleanupChapters(userID, &a, agentCleanupPreviewMax)
			if err != nil {
				return "", err
			}
			items := make([]map[string]any, 0)
			changed := 0
			for _, id := range ids {
				chapter, err := s.Store.GetOwnedChapter(userID, novelID, id)
				if err != nil {
					continue
				}
				res := ApplyClean(cleaningSource(chapter, a.ApplyTo), opts)
				if !res.Changed {
					continue
				}
				changed++
				hunks := diffLines(res.Original, res.Cleaned)
				sample := make([]string, 0, agentCleanupSampleHunks)
				for i, h := range hunks {
					if i >= agentCleanupSampleHunks {
						break
					}
					// Quote the first visible line of each side; modes that
					// only remove blank lines carry no visible text, so fall
					// back to a count.
					before := firstNonEmptyLine(h.Before)
					after := firstNonEmptyLine(h.After)
					switch {
					case before != "":
						sample = append(sample, truncateAgentRunes("- "+before, agentCleanupSampleLineMax))
						if after != "" {
							sample = append(sample, truncateAgentRunes("+ "+after, agentCleanupSampleLineMax))
						}
					case after != "":
						sample = append(sample, truncateAgentRunes("+ "+after, agentCleanupSampleLineMax))
					default:
						sample = append(sample, fmt.Sprintf("%d line(s) removed", len(h.Before)))
					}
				}
				items = append(items, map[string]any{
					"chapterId":    chapter.ID,
					"chapterOrder": chapter.ChapterOrder,
					"title":        chapter.Title,
					"changeCount":  len(hunks),
					"sample":       sample,
				})
			}
			return marshalToolResult(map[string]any{
				"requested": len(ids),
				"changed":   changed,
				"unchanged": len(ids) - changed,
				"items":     items,
			})
		},
	}
}

// agentProposeCleanupToolName is the terminal tool that surfaces a cleanup as
// an interactive approval card. The router maps its tool_call/tool_result pair
// to a "proposal" event carrying the resolved payload; nothing is written
// until the user approves the card, and the apply itself goes through the
// HTTP clean endpoint, never through a tool.
const agentProposeCleanupToolName = "propose_cleanup"

// agentCleanupProposal is the payload the frontend renders as an approval
// card: the interpreted rule, the requested scope and the counts the same
// ApplyClean pass the preview/apply endpoints will run produced. It carries no
// diff hunks — the card fetches those live with clean-preview-bulk.
type agentCleanupProposal struct {
	NovelID       string   `json:"novelId"`
	NovelTitle    string   `json:"novelTitle"`
	Mode          string   `json:"mode"`
	SearchText    string   `json:"searchText,omitempty"`
	ReplaceText   string   `json:"replaceText,omitempty"`
	CaseSensitive bool     `json:"caseSensitive"`
	UseRegex      bool     `json:"useRegex"`
	ApplyTo       string   `json:"applyTo"`
	ChapterIDs    []string `json:"chapterIds,omitempty"`
	FromOrder     *int     `json:"fromOrder,omitempty"`
	ToOrder       *int     `json:"toOrder,omitempty"`
	Considered    int      `json:"considered"`
	Affected      int      `json:"affected"`
	RemovedLines  int      `json:"removedLines"`
}

// buildCleanupProposal validates a cleanup rule, resolves its scope to a
// bounded chapter id list and dry-runs it to produce the counts shown on the
// approval card. Read-only: shared by the propose_cleanup tool (compact
// summary for the model) and the chat router (full payload for the proposal
// event).
func (s *Server) buildCleanupProposal(userID string, a *agentCleanupArgs) (*agentCleanupProposal, error) {
	opts, err := s.agentCleanupOptions(a)
	if err != nil {
		return nil, err
	}
	novelID := strings.TrimSpace(a.NovelID)
	novel, err := s.Store.GetOwnedNovel(userID, novelID)
	if err != nil {
		return nil, err
	}
	ids, err := s.resolveAgentCleanupChapters(userID, a, agentCleanupProposeMax)
	if err != nil {
		return nil, err
	}
	proposal := &agentCleanupProposal{
		NovelID:       novelID,
		NovelTitle:    firstNonEmpty(novel.TargetTitle, novel.SourceTitle),
		Mode:          a.Mode,
		SearchText:    a.SearchText,
		ReplaceText:   a.ReplaceText,
		CaseSensitive: a.CaseSensitive,
		UseRegex:      a.UseRegex,
		ApplyTo:       a.ApplyTo,
		ChapterIDs:    ids,
		FromOrder:     a.FromOrder,
		ToOrder:       a.ToOrder,
		Considered:    len(ids),
	}
	for _, id := range ids {
		chapter, err := s.Store.GetOwnedChapter(userID, novelID, id)
		if err != nil {
			continue
		}
		res := ApplyClean(cleaningSource(chapter, a.ApplyTo), opts)
		if !res.Changed {
			continue
		}
		proposal.Affected++
		proposal.RemovedLines += res.RemovedLines
	}
	return proposal, nil
}

func (s *Server) agentToolProposeCleanup(userID string) ai.AgentTool {
	return ai.AgentTool{
		Name: agentProposeCleanupToolName,
		Description: "Propose a text cleanup for one of the user's OWN novels and END THE TURN: the app renders an interactive approval card with the full per-chapter diff, and the cleanup only runs if the user approves it there. " +
			"The user's decision arrives as their next message. " +
			"Scope it with chapterIds or a fromOrder/toOrder range; omitting both is an error. " +
			"Use preview_chapter_cleanup first when you want to sanity-check how many chapters a rule touches. " +
			"There is no tool to apply a cleanup: never claim one was applied unless the user's message confirms it.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "novelId": {"type": "string", "description": "Novel id."},
    "chapterIds": {"type": "array", "items": {"type": "string"}, "description": "Explicit chapter ids (e.g. from a query_library analysis)."},
    "fromOrder": {"type": "integer", "minimum": 1, "description": "First chapter order (inclusive; requires toOrder)."},
    "toOrder": {"type": "integer", "minimum": 1, "description": "Last chapter order (inclusive; requires fromOrder)."},
    "mode": {"type": "string", "description": "Cleanup mode, as in the app's cleanup editor."},
    "searchText": {"type": "string", "description": "Search text or regex (with useRegex). For line modes, anchor with ^...$ to match whole lines."},
    "replaceText": {"type": "string", "description": "Replacement text for the search_replace mode."},
    "caseSensitive": {"type": "boolean"},
    "useRegex": {"type": "boolean"},
    "applyTo": {"type": "string", "enum": ["original", "translated", "refined", "all"]}
  },
  "required": ["novelId", "mode", "applyTo"]
}`),
		Terminal: true,
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var a agentCleanupArgs
			if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.NovelID) == "" {
				return "", fmt.Errorf("invalid novelId")
			}
			proposal, err := s.buildCleanupProposal(userID, &a)
			if err != nil {
				return "", err
			}
			return marshalToolResult(map[string]any{
				"proposed":     true,
				"considered":   proposal.Considered,
				"affected":     proposal.Affected,
				"removedLines": proposal.RemovedLines,
				"note":         "an approval card with the full diff was shown to the user; the cleanup only runs if they approve it there",
			})
		},
	}
}

// ---------------------------------------------------------------------------
// Reading progress
// ---------------------------------------------------------------------------

func (s *Server) agentToolGetReadingProgress(userID string) ai.AgentTool {
	return ai.AgentTool{
		Name:        "get_reading_progress",
		Description: "Report where the user left off in one of their OWN novels: chapter (order and title) plus scroll position.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "novelId": {"type": "string", "description": "Novel id."}
  },
  "required": ["novelId"]
}`),
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var a struct {
				NovelID string `json:"novelId"`
			}
			if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.NovelID) == "" {
				return "", fmt.Errorf("invalid novelId")
			}
			novelID := strings.TrimSpace(a.NovelID)
			if _, err := s.Store.GetOwnedNovel(userID, novelID); err != nil {
				return "", err
			}
			rp, err := s.Store.GetReadingProgress(userID, novelID)
			if err != nil {
				return marshalToolResult(map[string]any{"hasProgress": false, "novelId": novelID})
			}
			out := map[string]any{
				"hasProgress":   true,
				"novelId":       novelID,
				"chapterId":     rp.ChapterID,
				"scrollPercent": rp.ScrollPercent,
				"updatedAt":     rp.UpdatedAt,
			}
			if brief, err := s.Store.GetOwnedChapterBrief(userID, novelID, rp.ChapterID); err == nil {
				out["chapterOrder"] = brief.ChapterOrder
				out["title"] = brief.Title
				out["translatedTitle"] = brief.TranslatedTitle
			}
			return marshalToolResult(out)
		},
	}
}

func (s *Server) agentToolSetReadingProgress(userID string) ai.AgentTool {
	return ai.AgentTool{
		Name:        "set_reading_progress",
		Description: "Record where the user is in one of their OWN novels: pass the chapterId (from get_novel_chapters) or the chapterOrder, plus the scroll position (0-100).",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "novelId": {"type": "string", "description": "Novel id."},
    "chapterId": {"type": "string", "description": "Chapter id (use this or chapterOrder)."},
    "chapterOrder": {"type": "integer", "minimum": 1, "description": "Chapter order (use this or chapterId)."},
    "scrollPercent": {"type": "number", "minimum": 0, "maximum": 100, "description": "How far into the chapter the reader is (default 0)."}
  },
  "required": ["novelId"]
}`),
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var a struct {
				NovelID       string  `json:"novelId"`
				ChapterID     string  `json:"chapterId"`
				ChapterOrder  *int    `json:"chapterOrder"`
				ScrollPercent float64 `json:"scrollPercent"`
			}
			if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.NovelID) == "" {
				return "", fmt.Errorf("invalid novelId")
			}
			novelID := strings.TrimSpace(a.NovelID)
			if (strings.TrimSpace(a.ChapterID) == "") == (a.ChapterOrder == nil) {
				return "", fmt.Errorf("provide exactly one of chapterId or chapterOrder")
			}
			if a.ScrollPercent < 0 {
				a.ScrollPercent = 0
			}
			if a.ScrollPercent > 100 {
				a.ScrollPercent = 100
			}
			var brief *store.ChapterBrief
			var err error
			if strings.TrimSpace(a.ChapterID) != "" {
				brief, err = s.Store.GetOwnedChapterBrief(userID, novelID, strings.TrimSpace(a.ChapterID))
			} else {
				brief, err = s.Store.FindOwnedChapterBriefByOrder(userID, novelID, *a.ChapterOrder)
			}
			if err != nil {
				return "", err
			}
			rp, err := s.Store.UpsertReadingProgress(userID, novelID, brief.ID, a.ScrollPercent)
			if err != nil {
				return "", err
			}
			return marshalToolResult(map[string]any{
				"ok":            true,
				"chapterId":     rp.ChapterID,
				"chapterOrder":  brief.ChapterOrder,
				"scrollPercent": rp.ScrollPercent,
			})
		},
	}
}

// ---------------------------------------------------------------------------
// EPUB exports
// ---------------------------------------------------------------------------

func (s *Server) agentToolListNovelEpubs(userID string) ai.AgentTool {
	return ai.AgentTool{
		Name:        "list_novel_epubs",
		Description: "List the stored EPUB exports of one of the user's OWN novels, newest first (metadata only — the user downloads them from the app).",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "novelId": {"type": "string", "description": "Novel id."}
  },
  "required": ["novelId"]
}`),
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var a struct {
				NovelID string `json:"novelId"`
			}
			if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.NovelID) == "" {
				return "", fmt.Errorf("invalid novelId")
			}
			epubs, err := s.Store.ListEpubs(userID, strings.TrimSpace(a.NovelID))
			if err != nil {
				return "", err
			}
			if len(epubs) > agentEpubListMax {
				epubs = epubs[:agentEpubListMax]
			}
			out := make([]map[string]any, 0, len(epubs))
			for _, epub := range epubs {
				out = append(out, map[string]any{
					"id":            epub.ID,
					"fileKind":      epub.FileKind,
					"sourceVariant": epub.SourceVariant,
					"label":         epub.Label,
					"fileName":      epub.FileName,
					"fileSize":      epub.FileSize,
					"createdAt":     epub.CreatedAt,
				})
			}
			return marshalToolResult(map[string]any{"epubs": out, "total": len(out)})
		},
	}
}

func (s *Server) agentToolBuildEpub(userID string) ai.AgentTool {
	return ai.AgentTool{
		Name: "build_epub",
		Description: "Generate and store the EPUB export of one of the user's OWN novels for a content source (original, translated or refined). " +
			"Very large novels are refused — suggest the export UI for those; the result appears in the novel's EPUB list.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "novelId": {"type": "string", "description": "Novel id."},
    "source": {"type": "string", "enum": ["original", "translated", "refined"]}
  },
  "required": ["novelId", "source"]
}`),
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var a struct {
				NovelID string `json:"novelId"`
				Source  string `json:"source"`
			}
			if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.NovelID) == "" {
				return "", fmt.Errorf("invalid novelId")
			}
			switch a.Source {
			case "original", "translated", "refined":
			default:
				return "", fmt.Errorf("source must be original, translated or refined")
			}
			novelID := strings.TrimSpace(a.NovelID)
			// ponytail: the EPUB builder materializes every chapter body plus
			// the cover and the generated file, so the gate estimates the
			// export size from the stored char counters (aggregates only — no
			// bodies are loaded) and refuses above the ceiling. Upgrade path:
			// a build-epub job operation in runtime_worker.
			stats, err := s.Store.GetOwnedChapterStats(userID, novelID)
			if err != nil {
				return "", err
			}
			var estimate int
			switch a.Source {
			case "translated":
				// Each chapter exports its translation or falls back to the
				// original, so the sum is the conservative upper bound.
				estimate = stats.TranslatedCharacters + stats.OriginalCharacters
			case "refined":
				estimate = stats.RefinedCharacters + stats.TranslatedCharacters + stats.OriginalCharacters
			default:
				estimate = stats.OriginalCharacters
			}
			if estimate > agentEpubMaxSourceChars {
				return "", fmt.Errorf("this novel is too large to export from the assistant (~%d characters); use the export UI instead", estimate)
			}
			item, err := s.buildEpubForNovel(userID, novelID, a.Source)
			if err != nil {
				switch {
				case errors.Is(err, errEpubNoContent):
					return "", fmt.Errorf("%s", err.Error())
				}
				var storeErr *importStoreError
				if errors.As(err, &storeErr) {
					return "", fmt.Errorf("%s", storeErr.msg)
				}
				return "", err
			}
			return marshalToolResult(map[string]any{
				"ok":       true,
				"epubId":   item.ID,
				"fileName": item.FileName,
				"fileKind": item.FileKind,
				"fileSize": item.FileSize,
			})
		},
	}
}

// ---------------------------------------------------------------------------
// Description translation
// ---------------------------------------------------------------------------

func (s *Server) agentToolTranslateNovelDescription(userID string) ai.AgentTool {
	return ai.AgentTool{
		Name: "translate_novel_description",
		Description: "Translate a synopsis with the novel's own AI settings and glossary and return the text WITHOUT saving it — " +
			"apply the result with update_novel (targetDescription) if the user wants it stored. sourceText defaults to the novel's source description.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "novelId": {"type": "string", "description": "Novel id."},
    "sourceText": {"type": "string", "description": "Text to translate; omitted means the novel's source description."}
  },
  "required": ["novelId"]
}`),
		Execute: func(ctx context.Context, args json.RawMessage) (string, error) {
			var a struct {
				NovelID    string `json:"novelId"`
				SourceText string `json:"sourceText"`
			}
			if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.NovelID) == "" {
				return "", fmt.Errorf("invalid novelId")
			}
			novel, err := s.Store.GetOwnedNovel(userID, strings.TrimSpace(a.NovelID))
			if err != nil {
				return "", err
			}
			text := strings.TrimSpace(a.SourceText)
			if text == "" {
				text = strings.TrimSpace(novel.SourceDescription)
			}
			if text == "" {
				return "", fmt.Errorf("no text to translate: pass sourceText or fill the novel's source description first")
			}
			if utf8.RuneCountInString(text) > maxDescriptionChars {
				return "", fmt.Errorf("sourceText exceeds the maximum length (%d characters)", maxDescriptionChars)
			}
			translated, err := s.translateNovelDescriptionCore(ctx, userID, novel, text)
			if err != nil {
				switch {
				case errors.Is(err, errDescAIUnconfigured):
					return "", fmt.Errorf("no AI provider is configured for this project")
				case errors.Is(err, errDescAIFailed):
					return "", fmt.Errorf("the AI provider could not translate the description")
				case errors.Is(err, errDescAIEmpty):
					return "", fmt.Errorf("the AI provider returned an empty translation")
				}
				return "", fmt.Errorf("failed to resolve the AI settings")
			}
			return marshalToolResult(map[string]any{"translatedText": translated})
		},
	}
}

// ---------------------------------------------------------------------------
// Bulk chapter writes
// ---------------------------------------------------------------------------

func (s *Server) agentToolBulkSetChapterStatus(userID string) ai.AgentTool {
	return ai.AgentTool{
		Name: "bulk_set_chapter_status",
		Description: "Flip the translation status of a RANGE of chapters of one of the user's OWN novels in one call (both bounds are chapter orders, inclusive; excluded and processing chapters are skipped). " +
			"This does not create, translate or delete content — it only flips the status flag. Refused while the novel has active jobs.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "novelId": {"type": "string", "description": "Novel id."},
    "status": {"type": "string", "enum": ["pending", "translated", "refined", "done", "failed"]},
    "fromOrder": {"type": "integer", "minimum": 1, "description": "First chapter order (inclusive)."},
    "toOrder": {"type": "integer", "minimum": 1, "description": "Last chapter order (inclusive)."},
    "errorMessage": {"type": "string", "description": "Optional note stored with the chapters (cleared when omitted)."}
  },
  "required": ["novelId", "status", "fromOrder", "toOrder"]
}`),
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var a struct {
				NovelID      string `json:"novelId"`
				Status       string `json:"status"`
				FromOrder    int    `json:"fromOrder"`
				ToOrder      int    `json:"toOrder"`
				ErrorMessage string `json:"errorMessage"`
			}
			if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.NovelID) == "" {
				return "", fmt.Errorf("invalid novelId")
			}
			if !agentChapterStatuses[a.Status] {
				return "", fmt.Errorf("invalid status %q: use pending, translated, refined, done or failed", a.Status)
			}
			if a.FromOrder <= 0 || a.ToOrder <= 0 {
				return "", fmt.Errorf("fromOrder and toOrder are required and must be positive")
			}
			changed, err := s.Store.BulkSetChapterStatus(userID, strings.TrimSpace(a.NovelID), a.FromOrder, a.ToOrder, a.Status, a.ErrorMessage)
			if err != nil {
				if errors.Is(err, store.ErrActiveJobs) {
					return "", fmt.Errorf("refused while the novel has active jobs: cancel or wait for them first")
				}
				return "", err
			}
			return marshalToolResult(map[string]any{
				"ok":      true,
				"changed": changed,
				"status":  a.Status,
			})
		},
	}
}

func (s *Server) agentToolBulkSetChapterExcluded(userID string) ai.AgentTool {
	return ai.AgentTool{
		Name: "bulk_set_chapter_excluded",
		Description: "Include or exclude a RANGE of chapters of one of the user's OWN novels in one call (both bounds are chapter orders, inclusive). " +
			"Excluded chapters are hidden from readers and skipped by stats and translation jobs. Refused while the novel has active jobs.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "novelId": {"type": "string", "description": "Novel id."},
    "excluded": {"type": "boolean", "description": "true to exclude (hide), false to include."},
    "fromOrder": {"type": "integer", "minimum": 1, "description": "First chapter order (inclusive)."},
    "toOrder": {"type": "integer", "minimum": 1, "description": "Last chapter order (inclusive)."}
  },
  "required": ["novelId", "excluded", "fromOrder", "toOrder"]
}`),
		Execute: func(_ context.Context, args json.RawMessage) (string, error) {
			var a struct {
				NovelID   string `json:"novelId"`
				Excluded  bool   `json:"excluded"`
				FromOrder int    `json:"fromOrder"`
				ToOrder   int    `json:"toOrder"`
			}
			if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.NovelID) == "" {
				return "", fmt.Errorf("invalid novelId")
			}
			if a.FromOrder <= 0 || a.ToOrder <= 0 {
				return "", fmt.Errorf("fromOrder and toOrder are required and must be positive")
			}
			changed, err := s.Store.BulkExcludeChaptersInOrderRange(userID, strings.TrimSpace(a.NovelID), a.FromOrder, a.ToOrder, a.Excluded)
			if err != nil {
				if errors.Is(err, store.ErrActiveJobs) {
					return "", fmt.Errorf("refused while the novel has active jobs: cancel or wait for them first")
				}
				return "", err
			}
			return marshalToolResult(map[string]any{
				"ok":       true,
				"changed":  changed,
				"excluded": a.Excluded,
			})
		},
	}
}

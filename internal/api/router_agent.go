package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/pocketbase/core"
	pbrouter "github.com/pocketbase/pocketbase/tools/router"

	"translator-server/internal/ai"
	"translator-server/internal/store"
)

// agentChatTurnTimeout caps one whole agent turn (model steps + tool
// executions). Per-model HTTP timeouts still apply underneath.
const agentChatTurnTimeout = 8 * time.Minute

// agentTurnLock serializes one user's agent turns and is reference-counted so
// the map entry can be dropped once the last turn using it finishes.
//
// meta is separate from mu because refs is touched both before and after a
// turn takes the lock; using mu for both would self-deadlock.
type agentTurnLock struct {
	mu      sync.Mutex // the turn lock itself
	meta    sync.Mutex // guards refs and dropped
	refs    int
	dropped bool
}

// acquireAgentTurnLock takes the per-user turn lock and returns the release
// function.
//
// The entry is removed from the map once no turn holds or awaits it. Leaving
// it forever would accumulate one entry per account that ever used the chat —
// on a long-lived self-hosted instance with rotating accounts that is an
// unbounded leak keyed on user id, unlike the novel-keyed redownload locks
// whose cardinality is bounded by library size.
func (s *Server) acquireAgentTurnLock(userID string) func() {
	for {
		entry, _ := s.agentTurnLocks.LoadOrStore(userID, &agentTurnLock{})
		lock := entry.(*agentTurnLock)

		// Claim a reference BEFORE waiting on the turn lock, so a turn that is
		// queued behind another can never have the entry pulled out from under
		// it — which would let a third turn create a second lock for the same
		// user and run concurrently.
		lock.meta.Lock()
		if lock.dropped {
			lock.meta.Unlock()
			continue // lost the race with the last holder; retry with a new entry
		}
		lock.refs++
		lock.meta.Unlock()

		lock.mu.Lock()
		return func() {
			lock.mu.Unlock()

			lock.meta.Lock()
			defer lock.meta.Unlock()
			lock.refs--
			if lock.refs == 0 {
				lock.dropped = true
				s.agentTurnLocks.CompareAndDelete(userID, entry)
			}
		}
	}
}

// agentChatBodyLimit caps the chat request body: one message plus session and
// novel ids.
const agentChatBodyLimit int64 = 16 << 10

func registerV1AgentRoutes(authed *pbrouter.RouterGroup[*core.RequestEvent], s *Server) {
	agent := authed.Group("/agent")
	agent.POST("/chat", withJSONBodyLimit(agentChatBodyLimit, withIPRateLimit(s.agentLimiter, handleAgentChat(s))))
	agent.GET("/session", handleAgentGetSession(s))
	agent.DELETE("/session", handleAgentDeleteSession(s))
}

// agentChatRequest is one chat turn. Regenerate asks the server to drop the
// current answer and re-run it: the handler truncates the persisted trail to
// just before the last user message and re-runs the turn with the same text,
// so no second user message appears in the history.
type agentChatRequest struct {
	SessionID  string `json:"sessionId"`
	NovelID    string `json:"novelId"`
	Message    string `json:"message"`
	Regenerate bool   `json:"regenerate"`
	// Media scopes the toolset the model sees: "novels" (default), "comics"
	// or "both". Keeps the system prompt lean — one media's schemas at a
	// time — unless the user explicitly asks for both.
	Media string `json:"media"`
}

// agentChatOption is one clickable choice of a "question" event.
type agentChatOption struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// agentChatUsage mirrors ai.AgentUsage on the wire. PromptTokens is the last
// step's input size (context held at answer time); completion/total
// accumulate across the turn's steps.
type agentChatUsage struct {
	PromptTokens     int `json:"promptTokens"`
	CompletionTokens int `json:"completionTokens"`
	TotalTokens      int `json:"totalTokens"`
	CachedTokens     int `json:"cachedTokens,omitempty"`
	ReasoningTokens  int `json:"reasoningTokens,omitempty"`
}

// agentUsageFromAI maps the provider usage onto the wire shape, preserving
// the nil-when-empty contract so providers without usage report nothing.
func agentUsageFromAI(u *ai.AgentUsage) *agentChatUsage {
	if u == nil {
		return nil
	}
	return &agentChatUsage{
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		TotalTokens:      u.TotalTokens,
		CachedTokens:     u.CachedTokens,
		ReasoningTokens:  u.ReasoningTokens,
	}
}

// agentTurnModel reports which model served the turn for the usage display.
func agentTurnModel(aiSettings store.AISettings) string {
	return effectiveModel(aiSettings)
}

// agentTurnContextWindow reports the serving model's max input context in
// tokens (0 when the provider or model is unknown, e.g. a custom override).
func agentTurnContextWindow(aiSettings store.AISettings) int {
	providerKey := strings.TrimSpace(aiSettings.Provider)
	if providerKey == "" {
		providerKey = store.DefaultAISettings.Provider
	}
	return ai.ContextWindowFor(providerKey, effectiveModel(aiSettings))
}

// agentChatEvent is one NDJSON line of the streaming response.
type agentChatEvent struct {
	Type      string          `json:"type"`
	SessionID string          `json:"sessionId,omitempty"`
	Text      string          `json:"text,omitempty"`
	Step      int             `json:"step,omitempty"`
	Tool      string          `json:"tool,omitempty"`
	Args      string          `json:"args,omitempty"`
	Result    string          `json:"result,omitempty"`
	Message   map[string]any  `json:"message,omitempty"`
	Steps     int             `json:"steps,omitempty"`
	Usage     *agentChatUsage `json:"usage,omitempty"`
	Model     string          `json:"model,omitempty"`
	// ContextWindow is the serving model's max input context in tokens
	// (0 when unknown); the client ratios usage.promptTokens against it.
	ContextWindow int                   `json:"contextWindow,omitempty"`
	Code          string                `json:"code,omitempty"`
	ErrorDetail   string                `json:"error,omitempty"`
	Question      string                `json:"question,omitempty"`
	Options       []agentChatOption     `json:"options,omitempty"`
	Proposal      *agentCleanupProposal `json:"proposal,omitempty"`
}

// handleAgentChat streams one assistant turn as NDJSON. Pre-flight failures
// (bad body, unknown ids, missing provider) return standard v1 problem+json
// errors; once streaming has started, failures become {"type":"error"} lines.
func handleAgentChat(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		var in agentChatRequest
		if err := e.BindBody(&in); err != nil {
			return writeV1Error(e, http.StatusBadRequest, "validation_failed", "invalid request body")
		}
		message := strings.TrimSpace(in.Message)
		if message == "" {
			return writeV1Error(e, http.StatusBadRequest, "validation_failed", "message is required")
		}
		// Counted in runes, matching the constant's name and the tool-result
		// caps in the same feature. len() is bytes, so a CJK or emoji user was
		// rejected at ~2,667 characters against a documented 8,000 — three
		// times tighter than the limit says, and only for non-Latin text.
		if utf8.RuneCountInString(message) > agentMaxMessageChars {
			return writeV1Error(e, http.StatusBadRequest, "validation_failed", "message too long")
		}
		userID := e.Auth.Id

		// Serialize the whole turn, including the session/history READ, not
		// just the write. runAgentTurn rebuilds the trail from the session
		// snapshot, so a read taken before the lock lets two concurrent chats
		// start from the same history and the second SaveAgentSessionMessages
		// silently discards the first turn — a lost message with no error
		// anywhere. Taking the lock here makes read-modify-write atomic.
		lock := s.acquireAgentTurnLock(userID)
		defer lock()

		session, err := resolveAgentSession(s, userID, in.SessionID)
		if err != nil {
			return notFoundOrForbidden(e, err)
		}

		// Regenerate: drop the last user message and everything after it (the
		// old answer and its tool trail) from the in-memory session, then re-run
		// the turn below with Message. runAgentTurn rebuilds the trail from the
		// session snapshot, so the cut is what erases the previous answer — the
		// re-appended user message keeps the history valid and no second user
		// message ever appears.
		//
		// The truncation is deliberately NOT persisted on its own: if the re-run
		// fails (provider error, timeout, disconnect) nothing is saved, the old
		// answer survives server-side, and the client restores it from its own
		// state. Only a successful turn commits the shorter trail plus the new
		// answer, so a failed regenerate can never destroy a stored response.
		if in.Regenerate {
			history := agentHistoryFromSession(session)
			cut := -1
			for i := len(history) - 1; i >= 0; i-- {
				if history[i].Role == "user" {
					cut = i
					break
				}
			}
			if cut < 0 {
				return writeV1Error(e, http.StatusBadRequest, "validation_failed", "nothing to regenerate: the chat has no messages yet")
			}
			trimmed, err := json.Marshal(history[:cut])
			if err != nil {
				return writeV1Error(e, http.StatusInternalServerError, "internal_error", "could not rebuild the chat history")
			}
			session.Messages = string(trimmed)
		}

		var selectedNovel *store.Novel
		if strings.TrimSpace(in.NovelID) != "" {
			// Owner-only: the assistant's context novel must be one the user
			// owns, like every other novel it can reach. A novel owned by
			// someone else is masked as 404 rather than 403, so the chat never
			// confirms that an id exists in another user's library.
			selectedNovel, err = s.Store.GetOwnedNovel(userID, strings.TrimSpace(in.NovelID))
			if errors.Is(err, store.ErrForbidden) {
				err = store.ErrNotFound
			}
			if err != nil {
				return notFoundOrForbidden(e, err)
			}
		}

		// Resolve the runner before streaming so configuration problems get a
		// proper status code instead of a mid-stream error event. The resolved
		// runner is handed to runAgentTurn so the turn does not rebuild it.
		runner, aiSettings, err := s.resolveAgentRunner(userID)
		if err != nil {
			if errors.Is(err, errAgentUnsupportedProvider) {
				return writeV1Error(e, http.StatusBadRequest, "provider_unsupported", "the configured AI provider does not support agent chat")
			}
			slog.Error("agent runner unavailable", "error", err, "userID", userID)
			return writeV1Error(e, http.StatusInternalServerError, "agent_unavailable", "could not initialize the AI provider for agent chat")
		}

		e.Response.Header().Set("Content-Type", "application/x-ndjson")
		e.Response.Header().Set("Cache-Control", "no-store")
		e.Response.Header().Set("X-Accel-Buffering", "no")
		// Set before WriteHeader: the shared v1 middleware adds this after
		// e.Next() returns, which is too late once the stream has flushed.
		e.Response.Header().Set("X-API-Version", "v1")
		e.Response.WriteHeader(http.StatusOK)

		writeEvent := func(ev agentChatEvent) bool {
			line, err := json.Marshal(ev)
			if err != nil {
				return false
			}
			if _, err := e.Response.Write(append(line, '\n')); err != nil {
				return false
			}
			if flusher, ok := e.Response.(http.Flusher); ok {
				flusher.Flush()
			}
			return true
		}

		if !writeEvent(agentChatEvent{Type: "session", SessionID: session.ID}) {
			return nil
		}

		// Serialize turns per user (lock taken above, before the session read):
		// two concurrent chats would interleave history reads/writes on the
		// same session record.
		ctx, cancel := context.WithTimeout(e.Request.Context(), agentChatTurnTimeout)
		defer cancel()

		// pendingProposalArgs holds the propose_cleanup arguments between the
		// tool_call and tool_result events: the card is only emitted once the
		// execution succeeded. A rejected proposal (bad mode, foreign novel)
		// emits nothing; the model reads the error and may call the tool again
		// within the same turn, because a failed terminal tool does not end it.
		var pendingProposalArgs string
		output, session, err := s.runAgentTurn(ctx, runner, userID, session, selectedNovel, agentMediaScope(in.Media), message, func(ev ai.AgentEvent) {
			switch ev.Type {
			case "text_delta":
				writeEvent(agentChatEvent{Type: "text_delta", Step: ev.Step, Text: ev.Text})
			case "tool_call":
				// ask_user renders as a question bubble with clickable
				// options, not as a tool chip; fall back to the raw chip when
				// the model sent malformed arguments.
				if ev.ToolName == agentAskUserToolName {
					if question, options, ok := parseAgentQuestionArgs(ev.ToolArgs); ok {
						writeEvent(agentChatEvent{Type: "question", Step: ev.Step, Question: question, Options: options})
						break
					}
				}
				// propose_cleanup never renders as a tool chip either; its
				// approval card is emitted from the tool_result event below.
				if ev.ToolName == agentProposeCleanupToolName {
					pendingProposalArgs = ev.ToolArgs
					break
				}
				writeEvent(agentChatEvent{Type: "tool_call", Step: ev.Step, Tool: ev.ToolName, Args: ev.ToolArgs})
			case "tool_result":
				if ev.ToolName == agentAskUserToolName {
					break // the question event already carries the display
				}
				if ev.ToolName == agentProposeCleanupToolName {
					if args := strings.TrimSpace(pendingProposalArgs); args != "" && !strings.HasPrefix(ev.ToolResult, "error:") {
						var parsed agentCleanupArgs
						if err := json.Unmarshal([]byte(args), &parsed); err == nil {
							if payload, err := s.buildCleanupProposal(userID, &parsed); err == nil {
								writeEvent(agentChatEvent{Type: "proposal", Step: ev.Step, Proposal: payload})
							}
						}
					}
					pendingProposalArgs = ""
					break
				}
				writeEvent(agentChatEvent{Type: "tool_result", Step: ev.Step, Tool: ev.ToolName, Result: ev.ToolResult})
			}
		})
		if err != nil {
			if ctx.Err() != nil {
				writeEvent(agentChatEvent{Type: "error", Code: "timeout", ErrorDetail: "the agent turn timed out"})
				return nil
			}
			// The provider stopped sending without closing the response. This
			// is worth naming: it is the one failure the user can act on
			// (their provider or a proxy in front of it is wedged), and a
			// generic message would read as "yara is broken".
			if ai.IsStreamStalled(err) {
				slog.Warn("agent provider stalled mid-stream", "error", err, "userID", userID, "sessionID", session.ID)
				writeEvent(agentChatEvent{
					Type:        "error",
					Code:        "provider_stalled",
					ErrorDetail: "the AI provider stopped responding, try again",
				})
				return nil
			}
			// The full error chain (store, PocketBase validation, provider
			// internals) stays in the log; the client gets a generic message
			// like every other v1 error.
			slog.Error("agent turn failed", "error", err, "userID", userID, "sessionID", session.ID)
			writeEvent(agentChatEvent{Type: "error", Code: "agent_failed", ErrorDetail: "the assistant turn failed, try again"})
			return nil
		}

		writeEvent(agentChatEvent{
			Type:          "done",
			SessionID:     session.ID,
			Steps:         output.Steps,
			Usage:         agentUsageFromAI(output.Usage),
			Model:         agentTurnModel(aiSettings),
			ContextWindow: agentTurnContextWindow(aiSettings),
			Message: map[string]any{
				"role":    "assistant",
				"content": output.Text,
			},
		})
		return nil
	}
}

// parseAgentQuestionArgs decodes ask_user tool call arguments into the
// question payload for the frontend; ok=false falls back to the raw tool chip.
func parseAgentQuestionArgs(raw string) (string, []agentChatOption, bool) {
	var a struct {
		Question string            `json:"question"`
		Options  []agentChatOption `json:"options"`
	}
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return "", nil, false
	}
	options := make([]agentChatOption, 0, len(a.Options))
	for _, o := range a.Options {
		if strings.TrimSpace(o.Label) == "" || strings.TrimSpace(o.Value) == "" {
			continue
		}
		options = append(options, o)
	}
	if strings.TrimSpace(a.Question) == "" || len(options) == 0 {
		return "", nil, false
	}
	return a.Question, options, true
}

// resolveAgentSession returns the requested session, or the user's latest
// one, or a brand-new session when none exists.
func resolveAgentSession(s *Server, userID, sessionID string) (*store.AgentSession, error) {
	if strings.TrimSpace(sessionID) != "" {
		session, err := s.Store.GetAgentSession(userID, strings.TrimSpace(sessionID))
		// A session owned by someone else is reported as not found: a 403 here
		// would confirm that the id exists in another user's chat.
		if errors.Is(err, store.ErrForbidden) {
			err = store.ErrNotFound
		}
		return session, err
	}
	session, err := s.Store.GetLatestAgentSession(userID)
	if errors.Is(err, store.ErrNotFound) {
		return s.Store.CreateAgentSession(userID, "[]")
	}
	return session, err
}

// agentSessionRecord shapes a session for the v1 envelope with the message
// trail parsed for the frontend.
func agentSessionRecord(session *store.AgentSession) map[string]any {
	messages := []any{}
	if strings.TrimSpace(session.Messages) != "" {
		if err := json.Unmarshal([]byte(session.Messages), &messages); err != nil {
			messages = []any{}
		}
	}
	return map[string]any{
		"id":        session.ID,
		"ownerId":   session.OwnerID,
		"messages":  messages,
		"createdAt": session.CreatedAt,
		"updatedAt": session.UpdatedAt,
	}
}

func handleAgentGetSession(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		session, err := s.Store.GetLatestAgentSession(e.Auth.Id)
		if errors.Is(err, store.ErrNotFound) {
			return v1Respond(e, http.StatusOK, nil, nil, nil)
		}
		if err != nil {
			return writeV1Error(e, http.StatusInternalServerError, "internal_error", "could not load the agent session")
		}
		return v1Respond(e, http.StatusOK, agentSessionRecord(session), nil, nil)
	}
}

func handleAgentDeleteSession(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		if err := s.Store.DeleteAgentSessions(e.Auth.Id); err != nil {
			return writeV1Error(e, http.StatusInternalServerError, "internal_error", "could not reset the agent session")
		}
		return e.NoContent(http.StatusNoContent)
	}
}

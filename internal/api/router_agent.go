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

	"github.com/pocketbase/pocketbase/core"
	pbrouter "github.com/pocketbase/pocketbase/tools/router"

	"translator-server/internal/ai"
	"translator-server/internal/store"
)

// agentChatTurnTimeout caps one whole agent turn (model steps + tool
// executions). Per-model HTTP timeouts still apply underneath.
const agentChatTurnTimeout = 8 * time.Minute

// agentChatBodyLimit caps the chat request body: one message plus session and
// novel ids.
const agentChatBodyLimit int64 = 16 << 10

func registerV1AgentRoutes(authed *pbrouter.RouterGroup[*core.RequestEvent], s *Server) {
	agent := authed.Group("/agent")
	agent.POST("/chat", withJSONBodyLimit(agentChatBodyLimit, withIPRateLimit(s.agentLimiter, handleAgentChat(s))))
	agent.GET("/session", handleAgentGetSession(s))
	agent.DELETE("/session", handleAgentDeleteSession(s))
}

type agentChatRequest struct {
	SessionID string `json:"sessionId"`
	NovelID   string `json:"novelId"`
	Message   string `json:"message"`
}

// agentChatOption is one clickable choice of a "question" event.
type agentChatOption struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// agentChatEvent is one NDJSON line of the streaming response.
type agentChatEvent struct {
	Type        string            `json:"type"`
	SessionID   string            `json:"sessionId,omitempty"`
	Text        string            `json:"text,omitempty"`
	Step        int               `json:"step,omitempty"`
	Tool        string            `json:"tool,omitempty"`
	Args        string            `json:"args,omitempty"`
	Result      string            `json:"result,omitempty"`
	Message     map[string]any    `json:"message,omitempty"`
	Steps       int               `json:"steps,omitempty"`
	Code        string            `json:"code,omitempty"`
	ErrorDetail string            `json:"error,omitempty"`
	Question    string            `json:"question,omitempty"`
	Options     []agentChatOption `json:"options,omitempty"`
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
		if len(message) > agentMaxMessageChars {
			return writeV1Error(e, http.StatusBadRequest, "validation_failed", "message too long")
		}
		userID := e.Auth.Id

		// Serialize the whole turn, including the session/history READ, not
		// just the write. runAgentTurn rebuilds the trail from the session
		// snapshot, so a read taken before the lock lets two concurrent chats
		// start from the same history and the second SaveAgentSessionMessages
		// silently discards the first turn — a lost message with no error
		// anywhere. Taking the lock here makes read-modify-write atomic.
		lockAny, _ := s.agentTurnLocks.LoadOrStore(userID, &sync.Mutex{})
		lock := lockAny.(*sync.Mutex)
		lock.Lock()
		defer lock.Unlock()

		session, err := resolveAgentSession(s, userID, in.SessionID)
		if err != nil {
			return notFoundOrForbidden(e, err)
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
		runner, err := s.resolveAgentRunner(userID)
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

		output, session, err := s.runAgentTurn(ctx, runner, userID, session, selectedNovel, message, func(ev ai.AgentEvent) {
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
				writeEvent(agentChatEvent{Type: "tool_call", Step: ev.Step, Tool: ev.ToolName, Args: ev.ToolArgs})
			case "tool_result":
				if ev.ToolName == agentAskUserToolName {
					break // the question event already carries the display
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
			Type:      "done",
			SessionID: session.ID,
			Steps:     output.Steps,
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

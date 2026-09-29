package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// AgentTool is a model-callable tool. InputSchema is a JSON schema (draft
// 2020-12) for the tool arguments; Execute receives the raw arguments JSON
// and returns the tool-result string fed back to the model. Errors become
// tool results so the model can correct itself mid-loop.
type AgentTool struct {
	Name        string
	Description string
	InputSchema json.RawMessage
	// Terminal tools end the turn when called: the result is appended to the
	// trail but the model is not invoked again. The caller surfaces the call
	// itself (e.g. ask_user renders its arguments as clickable options).
	Terminal bool
	Execute  func(ctx context.Context, args json.RawMessage) (string, error)
}

// AgentMessage is one persisted conversation message. Role is "system",
// "user", "assistant" or "tool". Assistant messages may carry ToolCalls; tool
// results carry ToolCallID + ToolName.
type AgentMessage struct {
	Role       string          `json:"role"`
	Content    string          `json:"content,omitempty"`
	ToolCallID string          `json:"toolCallId,omitempty"`
	ToolName   string          `json:"toolName,omitempty"`
	ToolCalls  []AgentToolCall `json:"toolCalls,omitempty"`
}

// AgentToolCall records one assistant tool invocation, as sent by the model
// and replayed from persisted history.
type AgentToolCall struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Args string `json:"args"`
}

// AgentEvent streams progress of one AgentChat turn to the caller. Type is
// one of "text_delta", "tool_call", "tool_result". ToolName/ToolArgs travel
// with tool events; ToolResult is a truncated preview.
type AgentEvent struct {
	Type       string
	Step       int
	Text       string
	ToolName   string
	ToolArgs   string
	ToolResult string
}

// AgentChatInput is one agent turn: the resolved system prompt, the persisted
// history (excluding the new user message, which is appended last), the tool
// catalog and the event sink.
type AgentChatInput struct {
	System   string
	Messages []AgentMessage
	Tools    []AgentTool
	MaxSteps int
	OnEvent  func(AgentEvent)
}

// AgentChatOutput carries the final assistant text and the full new trail
// (previous history + user message + every assistant/tool message produced
// this turn) for the caller to persist.
type AgentChatOutput struct {
	Messages []AgentMessage
	Text     string
	Steps    int
}

// AgentProvider is implemented by providers that can run tool-using chat
// turns. The Google provider (Gemma on the Gemini API) does not support the
// tool loop, so agent mode is OpenAI-compatible providers only.
type AgentProvider interface {
	AgentChat(ctx context.Context, in AgentChatInput) (AgentChatOutput, error)
}

const defaultAgentMaxSteps = 8

// maxToolResultChars bounds each tool result fed back to the model; larger
// results are truncated with a marker so the model knows it is partial.
const maxToolResultChars = 8000

// maxEventToolResultChars bounds the preview sent to the UI per tool result.
const maxEventToolResultChars = 500

// AgentChat runs the streaming tool loop over eino: stream one assistant
// step, emit text deltas as they arrive, execute requested tools, repeat.
func (p *OpenAIProvider) AgentChat(ctx context.Context, in AgentChatInput) (AgentChatOutput, error) {
	m, err := p.einoChatModel(nil)
	if err != nil {
		return AgentChatOutput{}, err
	}
	infos, err := einoToolInfos(in.Tools)
	if err != nil {
		return AgentChatOutput{}, err
	}
	opts := append(p.einoCallOptions(), model.WithTools(infos))

	msgs := agentMessagesToEino(in.System, in.Messages)
	maxSteps := in.MaxSteps
	if maxSteps <= 0 {
		maxSteps = defaultAgentMaxSteps
	}
	emit := in.OnEvent
	if emit == nil {
		emit = func(AgentEvent) {}
	}

	var trail []AgentMessage
	trail = append(trail, in.Messages...)

	for step := 0; step < maxSteps; step++ {
		stream, err := m.Stream(ctx, msgs, opts...)
		if err != nil {
			return AgentChatOutput{}, err
		}
		var chunks []*schema.Message
		for {
			chunk, recvErr := stream.Recv()
			if recvErr == io.EOF {
				break
			}
			if recvErr != nil {
				stream.Close()
				return AgentChatOutput{}, recvErr
			}
			if chunk.Content != "" {
				emit(AgentEvent{Type: "text_delta", Step: step + 1, Text: chunk.Content})
			}
			chunks = append(chunks, chunk)
		}
		stream.Close()
		if len(chunks) == 0 {
			return AgentChatOutput{}, fmt.Errorf("openai agent chat: empty stream on step %d", step+1)
		}
		final, err := schema.ConcatMessages(chunks)
		if err != nil {
			return AgentChatOutput{}, fmt.Errorf("openai agent chat: concat stream: %w", err)
		}
		msgs = append(msgs, final)

		if len(final.ToolCalls) == 0 {
			trail = append(trail, agentMessageFromEino(final))
			return AgentChatOutput{Messages: trail, Text: final.Content, Steps: step + 1}, nil
		}

		calls := make([]AgentToolCall, 0, len(final.ToolCalls))
		for _, tc := range final.ToolCalls {
			calls = append(calls, AgentToolCall{ID: tc.ID, Name: tc.Function.Name, Args: tc.Function.Arguments})
		}
		trail = append(trail, agentMessageFromEino(final))

		terminal := false
		for _, call := range calls {
			emit(AgentEvent{Type: "tool_call", Step: step + 1, ToolName: call.Name, ToolArgs: call.Args})
			toolMsg := &schema.Message{
				Role:       schema.Tool,
				ToolCallID: call.ID,
				ToolName:   call.Name,
				Content:    truncateToolResult(runAgentTool(ctx, in.Tools, schema.ToolCall{ID: call.ID, Function: schema.FunctionCall{Name: call.Name, Arguments: call.Args}})),
			}
			msgs = append(msgs, toolMsg)
			trail = append(trail, AgentMessage{
				Role:       "tool",
				Content:    toolMsg.Content,
				ToolCallID: call.ID,
				ToolName:   call.Name,
			})
			emit(AgentEvent{Type: "tool_result", Step: step + 1, ToolName: call.Name, ToolResult: toolResultPreview(toolMsg.Content)})
			if t := agentToolByName(in.Tools, call.Name); t != nil && t.Terminal {
				terminal = true
			}
		}
		if terminal {
			return AgentChatOutput{Messages: trail, Steps: step + 1}, nil
		}
	}

	// Step exhaustion with pending tool results: force one final no-tools
	// call so the turn ends with an assistant message instead of dangling
	// tool results.
	final, err := m.Generate(ctx, msgs, p.einoCallOptions()...)
	if err != nil {
		return AgentChatOutput{}, err
	}
	msgs = append(msgs, final)
	trail = append(trail, agentMessageFromEino(final))
	return AgentChatOutput{Messages: trail, Text: final.Content, Steps: maxSteps}, nil
}

// agentToolByName finds a catalog tool by name; nil when the model invented
// one (runAgentTool already answers those with an error result).
func agentToolByName(tools []AgentTool, name string) *AgentTool {
	for i := range tools {
		if tools[i].Name == name {
			return &tools[i]
		}
	}
	return nil
}

// agentMessagesToEino maps persisted history (plus the system prompt) to
// eino messages.
func agentMessagesToEino(system string, msgs []AgentMessage) []*schema.Message {
	out := make([]*schema.Message, 0, len(msgs)+1)
	out = append(out, schema.SystemMessage(system))
	for _, m := range msgs {
		out = append(out, agentMessageToEino(m))
	}
	return out
}

func agentMessageToEino(m AgentMessage) *schema.Message {
	switch m.Role {
	case "user":
		return schema.UserMessage(m.Content)
	case "tool":
		return &schema.Message{
			Role:       schema.Tool,
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
			ToolName:   m.ToolName,
		}
	case "assistant":
		msg := &schema.Message{Role: schema.Assistant, Content: m.Content}
		for _, tc := range m.ToolCalls {
			msg.ToolCalls = append(msg.ToolCalls, schema.ToolCall{
				ID: tc.ID,
				Function: schema.FunctionCall{
					Name:      tc.Name,
					Arguments: tc.Args,
				},
			})
		}
		return msg
	default:
		return schema.UserMessage(m.Content)
	}
}

func agentMessageFromEino(m *schema.Message) AgentMessage {
	out := AgentMessage{Role: "assistant", Content: m.Content}
	for _, tc := range m.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, AgentToolCall{ID: tc.ID, Name: tc.Function.Name, Args: tc.Function.Arguments})
	}
	return out
}

func truncateToolResult(s string) string {
	if len(s) <= maxToolResultChars {
		return s
	}
	return s[:maxToolResultChars] + "\n…[truncated]"
}

func toolResultPreview(s string) string {
	preview := strings.ReplaceAll(s, "\n", " ")
	if len(preview) > maxEventToolResultChars {
		preview = preview[:maxEventToolResultChars] + "…"
	}
	return preview
}

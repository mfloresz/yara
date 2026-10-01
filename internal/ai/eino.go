package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"
)

// einoChatModel builds an eino OpenAI-compatible chat model for this provider.
// All known registry providers (venice, openrouter, opencode-*, inferx,
// lmstudio, meta) speak chat completions, which is what eino's openai
// component speaks. format is the response_format to request on every call of
// the returned model (nil = plain text).
//
// Note: eino's openai component does not speak the OpenAI Responses API.
// Provider options that goai used to switch protocols (useResponsesAPI) are
// ignored now; muse-spark model entries in the registry rely on it and will
// only work against endpoints that also expose chat completions.
func (p *OpenAIProvider) einoChatModel(format *einoopenai.ChatCompletionResponseFormat) (*einoopenai.ChatModel, error) {
	if p == nil || p.APIKey == "" {
		return nil, fmt.Errorf("openai not configured")
	}
	// Timeout is deliberately NOT set on the config. eino turns it into
	// &http.Client{Timeout: ...}, which covers the whole exchange INCLUDING the
	// body read, so it would kill a long streaming answer mid-flight. The
	// deadline is applied as a context instead (see einoCallContext), which is
	// what goai's WithTimeout did on main: it stops the work without
	// truncating a response that is actively arriving.
	client := &http.Client{Transport: http.DefaultTransport}
	return einoopenai.NewChatModel(context.Background(), &einoopenai.ChatModelConfig{
		APIKey:         p.APIKey,
		BaseURL:        p.BaseURL,
		Model:          p.modelID(),
		HTTPClient:     client,
		ResponseFormat: format,
	})
}

// einoCallContext applies this provider's configured timeout to ctx.
//
// This is the per-call deadline that translation and refine jobs rely on. The
// job worker passes a context.WithCancel (no deadline), so the timeout from
// Settings — or from the per-novel AI settings — reaches the provider only
// here. Dropping it, as an earlier version of this file did, left those jobs
// with no bound at all: a hung provider would keep a job running until the
// process died.
//
// The agent chat overrides it with agentIdleTimeout, because a turn makes
// several calls and each needs its own budget rather than one shared one.
func (p *OpenAIProvider) einoCallContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if p.Timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, p.Timeout)
}

// einoCallOptions carries the per-call headers and body extensions that goai
// used to embed via provider options: app User-Agent, the OpenCode session
// header, Venice's venice_parameters, the reasoning effort derived from model
// variant strings, and OpenRouter's flex service tier.
func (p *OpenAIProvider) einoCallOptions() []model.Option {
	opts := []model.Option{einoopenai.WithExtraHeader(p.headers())}
	po := p.providerOptions()
	extra := make(map[string]any, 3)
	if vp, ok := po["venice_parameters"]; ok {
		extra["venice_parameters"] = vp
	}
	if effort, ok := po["reasoning"]; ok {
		extra["reasoning"] = effort
	}
	if tier, ok := po["serviceTier"]; ok {
		// goai sent OpenRouter's service tier as service_tier on the wire;
		// keep that exact body key.
		extra["service_tier"] = tier
	}
	if len(extra) > 0 {
		opts = append(opts, einoopenai.WithExtraFields(extra))
	}
	return opts
}

// strictJSONSchema reports whether structured-output requests should use the
// strict json_schema response format. Providers whose registry entry disables
// it (LM Studio) or deepseek models (which previously ran with
// structuredOutputs=false) fall back to plain text + local JSON parsing.
func (p *OpenAIProvider) strictJSONSchema() bool {
	if strings.Contains(p.modelID(), "deepseek") {
		return false
	}
	strict, ok := p.ProviderOptions["strictJsonSchema"].(bool)
	return ok && strict
}

// jsonSchemaFormat wraps a raw JSON schema into eino's strict
// json_schema response format used by structured-output calls.
func jsonSchemaFormat(name, schemaJSON string) (*einoopenai.ChatCompletionResponseFormat, error) {
	var js jsonschema.Schema
	if err := json.Unmarshal([]byte(schemaJSON), &js); err != nil {
		return nil, fmt.Errorf("parse %s schema: %w", name, err)
	}
	return &einoopenai.ChatCompletionResponseFormat{
		Type: einoopenai.ChatCompletionResponseFormatTypeJSONSchema,
		JSONSchema: &einoopenai.ChatCompletionResponseFormatJSONSchema{
			Name:       name,
			JSONSchema: &js,
			Strict:     true,
		},
	}, nil
}

// generateStructured runs one system+user completion requesting the given
// strict JSON schema when the provider supports it, and returns the model's
// text content for local unmarshalling. Providers without strict JSON schema
// get the schema appended as instructions so the model still emits JSON.
func (p *OpenAIProvider) generateStructured(ctx context.Context, system, user, schemaName, schemaJSON string) (string, error) {
	if p.strictJSONSchema() {
		format, err := jsonSchemaFormat(schemaName, schemaJSON)
		if err != nil {
			return "", err
		}
		m, err := p.einoChatModel(format)
		if err != nil {
			return "", err
		}
		ctx, cancel := p.einoCallContext(ctx)
		defer cancel()
		out, err := generateWithRetry(ctx, m, systemUserMessages(system, user), p.einoCallOptions())
		if err != nil {
			return "", err
		}
		return out.Content, nil
	}
	m, err := p.einoChatModel(nil)
	if err != nil {
		return "", err
	}
	ctx, cancel := p.einoCallContext(ctx)
	defer cancel()
	system = system + "\n\nRespond with a single JSON object matching this schema and nothing else:\n" + schemaJSON
	out, err := generateWithRetry(ctx, m, systemUserMessages(system, user), p.einoCallOptions())
	if err != nil {
		return "", err
	}
	return stripJSONFences(out.Content), nil
}

// systemUserMessages builds the minimal two-message shape shared by every
// single-shot completion in this package.
func systemUserMessages(system, user string) []*schema.Message {
	return []*schema.Message{
		schema.SystemMessage(system),
		schema.UserMessage(user),
	}
}

// einoToolInfos converts AgentTool definitions into eino ToolInfo entries.
func einoToolInfos(tools []AgentTool) ([]*schema.ToolInfo, error) {
	infos := make([]*schema.ToolInfo, 0, len(tools))
	for _, t := range tools {
		var js jsonschema.Schema
		if err := json.Unmarshal(t.InputSchema, &js); err != nil {
			return nil, fmt.Errorf("parse schema for tool %s: %w", t.Name, err)
		}
		infos = append(infos, &schema.ToolInfo{
			Name:        t.Name,
			Desc:        t.Description,
			ParamsOneOf: schema.NewParamsOneOfByJSONSchema(&js),
		})
	}
	return infos, nil
}

// runAgentTool dispatches one tool call. Unknown tools and execution errors
// are returned as tool-result strings (not Go errors) so the model can react
// to them instead of aborting the whole loop.
func runAgentTool(ctx context.Context, tools []AgentTool, call schema.ToolCall) string {
	for _, t := range tools {
		if t.Name == call.Function.Name {
			result, err := t.Execute(ctx, json.RawMessage(call.Function.Arguments))
			if err != nil {
				return "error: " + err.Error()
			}
			return result
		}
	}
	return fmt.Sprintf("error: unknown tool %q", call.Function.Name)
}

// runToolLoop executes the classic agent loop over eino: generate, execute
// every requested tool, append tool results, repeat until the model answers
// without tool calls or maxSteps LLM calls have happened. It returns the full
// message trail including the final assistant message.
func runToolLoop(ctx context.Context, m model.ToolCallingChatModel, msgs []*schema.Message, tools []AgentTool, maxSteps int, opts []model.Option) ([]*schema.Message, error) {
	infos, err := einoToolInfos(tools)
	if err != nil {
		return nil, err
	}
	opts = append(opts, model.WithTools(infos))
	for step := 0; step < maxSteps; step++ {
		msg, err := generateWithRetry(ctx, m, msgs, opts)
		if err != nil {
			return msgs, err
		}
		msgs = append(msgs, msg)
		if len(msg.ToolCalls) == 0 {
			return msgs, nil
		}
		for _, tc := range msg.ToolCalls {
			result := runAgentTool(ctx, tools, tc)
			msgs = append(msgs, &schema.Message{
				Role:       schema.Tool,
				Content:    result,
				ToolCallID: tc.ID,
				ToolName:   tc.Function.Name,
			})
		}
	}
	return msgs, nil
}

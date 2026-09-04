package ai

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/zendev-sh/goai"
	"github.com/zendev-sh/goai/provider"
	"github.com/zendev-sh/goai/provider/openai"
	"github.com/zendev-sh/goai/provider/openrouter"
)

type OpenAIProvider struct {
	APIKey  string
	BaseURL string
	Model   string
	Timeout time.Duration
	// ProviderOptions are passed to goai on every call. Use for provider-specific
	// behavior toggles like forcing Chat Completions (e.g. Venice) or strict JSON schema.
	ProviderOptions map[string]any
	// OpenRouter selects goai's native OpenRouter provider, which adds the
	// gateway's recommended headers and usage reporting.
	OpenRouter bool
	// SessionID carries the opaque OpenCode session for cache grouping.
	// Only set for opencode-go/opencode-zen; empty for every other provider.
	SessionID string
}

// headers identifies the app and, when SessionID is set, the job conversation.
// User-Agent is always yara so OpenCode does not see a generic Go HTTP client.
func (p *OpenAIProvider) headers() map[string]string {
	h := map[string]string{"User-Agent": yaraUserAgent}
	if trimmed := strings.TrimSpace(p.SessionID); trimmed != "" {
		h[opencodeSessionHeader] = trimmed
	}
	return h
}

func (p *OpenAIProvider) model() (provider.LanguageModel, error) {
	if p == nil || p.APIKey == "" {
		return nil, fmt.Errorf("openai not configured")
	}
	headers := p.headers()
	opts := []openai.Option{openai.WithAPIKey(p.APIKey), openai.WithHeaders(headers)}
	if p.BaseURL != "" {
		opts = append(opts, openai.WithBaseURL(p.BaseURL))
	}
	if p.OpenRouter {
		openRouterOpts := []openrouter.Option{openrouter.WithAPIKey(p.APIKey), openrouter.WithHeaders(headers)}
		if p.BaseURL != "" {
			openRouterOpts = append(openRouterOpts, openrouter.WithBaseURL(p.BaseURL))
		}
		return openrouter.Chat(p.modelID(), openRouterOpts...), nil
	}
	return openai.Chat(p.modelID(), opts...), nil
}

// modelID maps UI-friendly model variants to the actual model ID accepted by
// the selected provider. The reasoning effort is sent separately in provider
// options.
func (p *OpenAIProvider) modelID() string {
	const prefix = "openai/gpt-5.6-luna (reasoning: "
	if strings.HasPrefix(p.Model, prefix) && strings.HasSuffix(p.Model, ")") {
		if !p.OpenRouter {
			return "gpt-5.6-luna"
		}
		return "openai/gpt-5.6-luna"
	}
	return p.Model
}

func (p *OpenAIProvider) providerOptions() map[string]any {
	opts := make(map[string]any, len(p.ProviderOptions)+2)
	for k, v := range p.ProviderOptions {
		opts[k] = v
	}
	const prefix = "openai/gpt-5.6-luna (reasoning: "
	if strings.HasPrefix(p.Model, prefix) && strings.HasSuffix(p.Model, ")") {
		effort := strings.TrimSuffix(strings.TrimPrefix(p.Model, prefix), ")")
		if effort == "none" || effort == "low" || effort == "medium" {
			opts["reasoning"] = map[string]any{"effort": effort}
		}
	}
	// OpenRouter serves luna (OpenAI gpt-5.6) on the flex tier for a ~50%
	// cost reduction at higher latency. Flex is opt-in per request and never
	// falls back to a standard-tier endpoint on capacity errors.
	if p.OpenRouter && strings.HasPrefix(p.Model, "openai/gpt-5.6-luna") {
		opts["serviceTier"] = "flex"
	}
	return opts
}

func (p *OpenAIProvider) opts() []goai.Option {
	opts := p.providerOptions()
	if strings.Contains(p.modelID(), "deepseek") {
		opts["structuredOutputs"] = false
	}
	if len(opts) == 0 {
		return nil
	}
	return []goai.Option{goai.WithProviderOptions(opts)}
}

func (p *OpenAIProvider) textOpts() []goai.Option {
	opts := p.providerOptions()
	if strings.Contains(p.modelID(), "deepseek") {
		opts["structuredOutputs"] = false
	}
	delete(opts, "strictJsonSchema")
	if len(opts) == 0 {
		return nil
	}
	return []goai.Option{goai.WithProviderOptions(opts)}
}

func (p *OpenAIProvider) TranslateTitle(ctx context.Context, in TranslateTitleInput) (string, error) {
	model, err := p.model()
	if err != nil {
		return "", err
	}
	opts := append(p.textOpts(),
		goai.WithSystem(buildTranslationTitleSystemPrompt(in)),
		goai.WithPrompt(buildTranslationTitlePrompt(in)),
		goai.WithTimeout(p.resolveTimeout()),
	)
	result, err := goai.GenerateText(ctx, model, opts...)
	if err != nil {
		return "", fmt.Errorf("openai translate title: %w", err)
	}
	return strings.TrimSpace(result.Text), nil
}

func (p *OpenAIProvider) TranslateText(ctx context.Context, in TranslateTextInput) (string, error) {
	model, err := p.model()
	if err != nil {
		return "", err
	}
	opts := append(p.textOpts(),
		goai.WithSystem(buildTranslationContentSystemPrompt(in)),
		goai.WithPrompt(buildTranslationContentPrompt(in)),
		goai.WithTimeout(p.resolveTimeout()),
	)
	result, err := goai.GenerateText(ctx, model, opts...)
	if err != nil {
		return "", fmt.Errorf("openai translate text: %w", err)
	}
	return strings.TrimSpace(result.Text), nil
}

func (p *OpenAIProvider) Check(ctx context.Context, in CheckInput) (CheckOutput, error) {
	model, err := p.model()
	if err != nil {
		return CheckOutput{}, err
	}
	system := "Analyze the following text for translation quality."
	if trimmed := strings.TrimSpace(in.SystemPrompt); trimmed != "" {
		system = trimmed
	}
	opts := append(p.opts(),
		goai.WithSystem(system),
		goai.WithPrompt(strings.TrimSpace(in.UserPrompt)),
		goai.WithTimeout(p.resolveTimeout()),
	)
	result, err := goai.GenerateObject[CheckOutput](ctx, model, opts...)
	if err != nil {
		return CheckOutput{}, fmt.Errorf("openai check: %w", err)
	}
	return result.Object, nil
}

func (p *OpenAIProvider) resolveTimeout() time.Duration {
	if p.Timeout > 0 {
		return p.Timeout
	}
	return 60 * time.Second
}

func (p *OpenAIProvider) GenerateGlossary(ctx context.Context, in GenerateGlossaryInput) (GenerateGlossaryOutput, error) {
	model, err := p.model()
	if err != nil {
		return GenerateGlossaryOutput{}, err
	}
	system := resolveGlossarySystemPrompt(in)
	prompt := buildGlossaryPrompt(in)

	opts := append(p.opts(),
		goai.WithSystem(system),
		goai.WithPrompt(prompt),
		goai.WithTimeout(p.resolveTimeout()),
	)
	result, err := goai.GenerateObject[GenerateGlossaryOutput](ctx, model, opts...)
	if err != nil {
		return GenerateGlossaryOutput{}, fmt.Errorf("openai generate glossary: %w", err)
	}
	return result.Object, nil
}

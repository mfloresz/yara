package ai

type ProviderInfo struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	BaseURL      string   `json:"baseUrl"`
	Models       []string `json:"models"`
	DefaultModel string   `json:"defaultModel"`
	OpenAICompat bool     `json:"openaiCompat"`
	// ContextWindow holds the max input context in tokens per model, used
	// to report context usage as a percentage. Sourced from
	// docs/model-context-limits.md — keep both in sync.
	ContextWindow map[string]int `json:"contextWindow,omitempty"`
	GoAIOptions   map[string]any `json:"goaiOptions,omitempty"`
	// ModelOptions holds per-model overrides merged over GoAIOptions for
	// requests targeting that model. Recognized keys (eino): strictJsonSchema,
	// venice_parameters.
	ModelOptions map[string]map[string]any `json:"modelOptions,omitempty"`
}

// ContextWindowFor returns the max input context in tokens for a provider
// model, or 0 when the provider or model is unknown.
func ContextWindowFor(providerID, model string) int {
	info, ok := ProviderByID(providerID)
	if !ok {
		return 0
	}
	return info.ContextWindow[model]
}

// OptionsForModel merges GoAIOptions with any per-model overrides.
func (p ProviderInfo) OptionsForModel(model string) map[string]any {
	overrides, ok := p.ModelOptions[model]
	if !ok || len(overrides) == 0 {
		return p.GoAIOptions
	}
	out := make(map[string]any, len(p.GoAIOptions)+len(overrides))
	for k, v := range p.GoAIOptions {
		out[k] = v
	}
	for k, v := range overrides {
		out[k] = v
	}
	return out
}

var knownProviders = []ProviderInfo{
	{
		ID:           "venice",
		Name:         "Venice",
		BaseURL:      "https://api.venice.ai/api/v1",
		Models:       []string{"deepseek-v4-flash-0731", "google-gemma-4-31b-it:disable_thinking=true", "aion-labs-aion-3-0-mini", "google-gemma-4-26b-a4b-it:disable_thinking=true"},
		DefaultModel: "deepseek-v4-flash-0731",
		OpenAICompat: true,
		ContextWindow: map[string]int{
			"deepseek-v4-flash-0731":                          262144,
			"google-gemma-4-31b-it:disable_thinking=true":     200000,
			"aion-labs-aion-3-0-mini":                         262000,
			"google-gemma-4-26b-a4b-it:disable_thinking=true": 200000,
		},
		GoAIOptions: map[string]any{
			"strictJsonSchema": true,
			"venice_parameters": map[string]any{
				"include_venice_system_prompt": false,
			},
		},
	},

	{
		ID:      "openrouter",
		Name:    "OpenRouter",
		BaseURL: "https://openrouter.ai/api/v1",
		Models: []string{
			"openai/gpt-5.6-luna (reasoning: none)",
			"openai/gpt-5.6-luna (reasoning: low)",
			"openai/gpt-5.6-luna (reasoning: medium)",
			"deepseek/deepseek-v4-flash-0731",
			"google/gemini-3.5-flash-lite",
			"tencent/hy-mt2-30b-a3b",
			"mistralai/mistral-small-2603",
			"mistralai/ministral-14b-2512",
			"inclusionai/ling-3.0-flash",
			"xiaomi/mimo-v2.6-flash",
		},
		DefaultModel: "openai/gpt-5.6-luna (reasoning: medium)",
		OpenAICompat: true,
		ContextWindow: map[string]int{
			"openai/gpt-5.6-luna (reasoning: none)":   262000,
			"openai/gpt-5.6-luna (reasoning: low)":    262000,
			"openai/gpt-5.6-luna (reasoning: medium)": 262000,
			"deepseek/deepseek-v4-flash-0731":         262144,
			"google/gemini-3.5-flash-lite":            262144,
			"tencent/hy-mt2-30b-a3b":                  8000,
			"mistralai/mistral-small-2603":            262000,
			"mistralai/ministral-14b-2512":            262000,
			"inclusionai/ling-3.0-flash":              262144,
			"xiaomi/mimo-v2.6-flash":                  524288,
		},
		GoAIOptions: map[string]any{
			"strictJsonSchema": true,
		},
	},

	{
		ID:      "opencode-go",
		Name:    "OpenCode Go",
		BaseURL: "https://opencode.ai/zen/go/v1",
		Models: []string{
			"deepseek-v4.1-flash",
		},
		DefaultModel: "deepseek-v4.1-flash",
		OpenAICompat: true,
		ContextWindow: map[string]int{
			"deepseek-v4.1-flash": 262144,
		},
		GoAIOptions: map[string]any{
			"strictJsonSchema": true,
		},
	},

	{
		ID:      "inferx",
		Name:    "InferX",
		BaseURL: "https://model.inferx.net/endpoints/v1",
		Models: []string{
			"deepseek-v4-flash-0731",
			"deepseek-v4.1-flash-1017-basic",
			"glm-5.3-flash",
			"gemma-4-31B-it-fp8",
			"Qwen3.6-35B-A3B-FP8",
		},
		DefaultModel: "deepseek-v4-flash-0731",
		OpenAICompat: true,
		ContextWindow: map[string]int{
			"deepseek-v4-flash-0731":         262144,
			"deepseek-v4.1-flash-1017-basic": 262144,
			"glm-5.3-flash":                  262144,
			"gemma-4-31B-it-fp8":             262144,
			"Qwen3.6-35B-A3B-FP8":            262000,
		},
		GoAIOptions: map[string]any{
			"strictJsonSchema": true,
		},
	},
}

func Providers() []ProviderInfo {
	out := make([]ProviderInfo, len(knownProviders))
	copy(out, knownProviders)
	return out
}

func ProviderByID(id string) (ProviderInfo, bool) {
	for _, p := range knownProviders {
		if p.ID == id {
			return p, true
		}
	}
	return ProviderInfo{}, false
}

func DefaultProvider() ProviderInfo {
	if len(knownProviders) == 0 {
		return ProviderInfo{}
	}
	return knownProviders[0]
}

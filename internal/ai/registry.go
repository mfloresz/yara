package ai

type ProviderInfo struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	BaseURL      string         `json:"baseUrl"`
	Models       []string       `json:"models"`
	DefaultModel string         `json:"defaultModel"`
	OpenAICompat bool           `json:"openaiCompat"`
	GoAIOptions  map[string]any `json:"goaiOptions,omitempty"`
}

var knownProviders = []ProviderInfo{
	{
		ID:           "venice",
		Name:         "Venice",
		BaseURL:      "https://api.venice.ai/api/v1",
		Models:       []string{"e2ee-deepseek-v4-flash", "mistral-small-3-2-24b-instruct", "google-gemma-4-31b-it:disable_thinking=true", "e2ee-gpt-oss-20b-p", "aion-labs-aion-3-0-mini", "e2ee-gemma-4-26b-a4b-uncensored-p", "google-gemma-4-26b-a4b-it:disable_thinking=true"},
		DefaultModel: "e2ee-deepseek-v4-flash",
		OpenAICompat: true,
		GoAIOptions: map[string]any{
			"useResponsesAPI":  false,
			"strictJsonSchema": true,
			"venice_parameters": map[string]any{
				"include_venice_system_prompt": false,
			},
		},
	},

	{
		ID:           "opencode-go",
		Name:         "OpenCode Go",
		BaseURL:      "https://opencode.ai/zen/go/v1",
		Models:       []string{"mimo-v2.5", "deepseek-v4-flash"},
		DefaultModel: "mimo-v2.5",
		OpenAICompat: true,
		GoAIOptions: map[string]any{
			"useResponsesAPI":  false,
			"strictJsonSchema": true,
		},
	},

	{
		ID:           "groq",
		Name:         "Groq",
		BaseURL:      "https://api.groq.com/openai/v1",
		Models:       []string{"openai/gpt-oss-120b", "openai/gpt-oss-20b"},
		DefaultModel: "openai/gpt-oss-20b",
		OpenAICompat: true,
		GoAIOptions: map[string]any{
			"useResponsesAPI":  false,
			"strictJsonSchema": true,
		},
	},

	{
		ID:           "lmstudio",
		Name:         "LM Studio",
		BaseURL:      "http://localhost:1234/v1",
		Models:       []string{"local-model"},
		DefaultModel: "local-model",
		OpenAICompat: true,
		GoAIOptions: map[string]any{
			"useResponsesAPI":  false,
			"strictJsonSchema": false,
		},
	},

	{
		ID:           "google",
		Name:         "Google Gemma",
		BaseURL:      "https://generativelanguage.googleapis.com",
		Models:       []string{"gemma-4-26b-a4b-it", "gemma-4-31b-it"},
		DefaultModel: "gemma-4-31b-it",
		OpenAICompat: false,
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

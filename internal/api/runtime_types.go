package api

import "translator-server/internal/store"

type promptTemplate struct {
	SystemPrompt string `json:"systemPrompt"`
	UserPrompt   string `json:"userPrompt"`
}

type promptSettings struct {
	Translation promptTemplate `json:"translation"`
	Title       promptTemplate `json:"title"`
	Refine      promptTemplate `json:"refine"`
	Check       promptTemplate `json:"check"`
}

type glossaryEntry struct {
	ID      string `json:"id,omitempty"`
	Source  string `json:"source"`
	Target  string `json:"target"`
	Context string `json:"context,omitempty"`
}

type novelAIOptions struct {
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	TimeoutMs     int    `json:"timeoutMs"`
	TitleEnabled  *bool  `json:"titleEnabled,omitempty"`
	TitleProvider string `json:"titleProvider"`
	TitleModel    string `json:"titleModel"`
}

type novelTranslationOptions struct {
	AutoSegment               *bool `json:"autoSegment,omitempty"`
	ThresholdChars            int   `json:"thresholdChars,omitempty"`
	MaxChars                  int   `json:"maxChars,omitempty"`
	MinChars                  int   `json:"minChars,omitempty"`
	MaxRetries                int   `json:"maxRetries,omitempty"`
	EnableCheck               *bool `json:"enableCheck,omitempty"`
	IncludePreviousTitleHints *bool `json:"includePreviousChapterTitles,omitempty"`
}

type resolvedJobConfig struct {
	AI               store.AISettings
	TitleAI          *store.AISettings
	Translation      store.TranslationDefaults
	Glossary         []glossaryEntry
	Prompts          promptSettings
	IncludePrevTitle bool
}

type chapterSegment struct {
	Index     int
	Text      string
	StartChar int
	EndChar   int
}

type chapterSegmentationStatus struct {
	Applied      bool
	SegmentCount int
}

package api

import (
	"testing"

	"translator-server/internal/ai"
	"translator-server/internal/store"
)

func TestBuildRefinePromptSubstitutesAllTemplateVars(t *testing.T) {
	cfg := resolvedJobConfig{
		Glossary: []glossaryEntry{
			{Source: "dragon", Target: "dragón"},
		},
		Prompts: promptSettings{
			Refine: promptTemplate{
				SystemPrompt: "System: {SOURCE_LANG}->{TARGET_LANG} glossary={GLOSSARY}",
				UserPrompt:   "User: {ORIGINAL} ||| {TRANSLATION}",
			},
		},
	}
	novel := &store.Novel{
		SourceLanguage: "en",
		TargetLanguage: "es",
	}
	chapter := &store.Chapter{
		OriginalContent: "Hello world",
	}
	current := "Hola mundo"

	systemPrompt, userPrompt := buildRefinePrompt(cfg, novel, chapter, current)

	wantSystem := "System: en->es glossary=- dragon → dragón"
	if systemPrompt != wantSystem {
		t.Fatalf("systemPrompt:\n  got:  %q\n  want: %q", systemPrompt, wantSystem)
	}

	wantUser := "User: Hello world ||| Hola mundo"
	if userPrompt != wantUser {
		t.Fatalf("userPrompt:\n  got:  %q\n  want: %q", userPrompt, wantUser)
	}
}

func TestBuildRefinePromptSupportsBothOriginalAndTranslationVariants(t *testing.T) {
	cfg := resolvedJobConfig{
		Prompts: promptSettings{
			Refine: promptTemplate{
				SystemPrompt: "S: {GLOSSARY}",
				UserPrompt:   "U: {ORIGINAL_TEXT} / {TRANSLATION_TEXT}",
			},
		},
	}
	novel := &store.Novel{SourceLanguage: "en", TargetLanguage: "es"}
	chapter := &store.Chapter{OriginalContent: "orig"}
	current := "trad"

	systemPrompt, userPrompt := buildRefinePrompt(cfg, novel, chapter, current)

	wantSystem := "S: (sin glosario)"
	if systemPrompt != wantSystem {
		t.Fatalf("systemPrompt: got %q, want %q", systemPrompt, wantSystem)
	}

	wantUser := "U: orig / trad"
	if userPrompt != wantUser {
		t.Fatalf("userPrompt: got %q, want %q", userPrompt, wantUser)
	}
}

func TestBuildRefinePromptTrimsWhitespace(t *testing.T) {
	cfg := resolvedJobConfig{
		Prompts: promptSettings{
			Refine: promptTemplate{
				SystemPrompt: "  {SOURCE_LANG}  \n  ",
				UserPrompt:   "\n{TARGET_LANG}\n",
			},
		},
	}
	novel := &store.Novel{SourceLanguage: "fr", TargetLanguage: "de"}
	chapter := &store.Chapter{}
	current := ""

	systemPrompt, userPrompt := buildRefinePrompt(cfg, novel, chapter, current)

	if systemPrompt != "fr" {
		t.Fatalf("systemPrompt should be trimmed to 'fr', got %q", systemPrompt)
	}
	if userPrompt != "de" {
		t.Fatalf("userPrompt should be trimmed to 'de', got %q", userPrompt)
	}
}

func TestBuildRefinePromptGlossaryWithContext(t *testing.T) {
	cfg := resolvedJobConfig{
		Glossary: []glossaryEntry{
			{Source: "moonlight", Target: "luz de luna", Context: "poético"},
		},
		Prompts: promptSettings{
			Refine: promptTemplate{
				SystemPrompt: "G: {GLOSSARY}",
				UserPrompt:   "U: {ORIGINAL} / {TRANSLATION}",
			},
		},
	}
	novel := &store.Novel{SourceLanguage: "en", TargetLanguage: "es"}
	chapter := &store.Chapter{OriginalContent: "text"}
	current := "texto"

	systemPrompt, _ := buildRefinePrompt(cfg, novel, chapter, current)

	want := "G: - moonlight → luz de luna (poético)"
	if systemPrompt != want {
		t.Fatalf("systemPrompt: got %q, want %q", systemPrompt, want)
	}
}

func TestBuildCheckPromptSubstitutesAllTemplateVars(t *testing.T) {
	cfg := resolvedJobConfig{
		Prompts: promptSettings{
			Check: promptTemplate{
				SystemPrompt: "System: {SOURCE_LANG}->{TARGET_LANG} glossary={GLOSSARY}",
				UserPrompt:   "User: {ORIGINAL} ||| {TRANSLATION}",
			},
		},
	}
	novel := &store.Novel{SourceLanguage: "en", TargetLanguage: "es"}
	chapter := &store.Chapter{OriginalContent: "Hello"}
	current := "Hola"

	checkInput := buildCheckPrompt(cfg, novel, chapter, current)

	wantSystem := "System: en->es glossary=(sin glosario)"
	if checkInput.SystemPrompt != wantSystem {
		t.Fatalf("SystemPrompt: got %q, want %q", checkInput.SystemPrompt, wantSystem)
	}

	wantUser := "User: Hello ||| Hola"
	if checkInput.UserPrompt != wantUser {
		t.Fatalf("UserPrompt: got %q, want %q", checkInput.UserPrompt, wantUser)
	}
}

func TestBuildCheckPromptIncludesOnlyExpectedVarsInSystem(t *testing.T) {
	cfg := resolvedJobConfig{
		Glossary: []glossaryEntry{{Source: "dragon", Target: "dragón"}},
		Prompts: promptSettings{
			Check: promptTemplate{
				SystemPrompt: "{ORIGINAL}|{TRANSLATION}|{SOURCE_LANG}|{TARGET_LANG}|{GLOSSARY}",
				UserPrompt:   "anything",
			},
		},
	}
	novel := &store.Novel{SourceLanguage: "en", TargetLanguage: "es"}
	chapter := &store.Chapter{OriginalContent: "keep"}
	current := "keep"

	checkInput := buildCheckPrompt(cfg, novel, chapter, current)

	// {ORIGINAL} and {TRANSLATION} should NOT be substituted in the system prompt
	want := "{ORIGINAL}|{TRANSLATION}|en|es|- dragon → dragón"
	if checkInput.SystemPrompt != want {
		t.Fatalf("SystemPrompt: got %q, want %q", checkInput.SystemPrompt, want)
	}
}

func TestBuildCheckPromptReturnsCheckInputType(t *testing.T) {
	checkInput := buildCheckPrompt(resolvedJobConfig{}, &store.Novel{}, &store.Chapter{}, "")
	if _, ok := interface{}(checkInput).(ai.CheckInput); !ok {
		t.Fatal("buildCheckPrompt should return ai.CheckInput")
	}
}

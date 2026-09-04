package store

import (
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// KnownPromptKeys are the built-in prompt slots. Admin overrides may only
// target these keys; user prompt settings use the same set.
var KnownPromptKeys = []string{"translation", "title", "refine", "check", "glossary"}

func IsKnownPromptKey(key string) bool {
	for _, k := range KnownPromptKeys {
		if k == key {
			return true
		}
	}
	return false
}

// AdminPrompt is the effective view of a prompt from the admin's perspective:
// it is the embedded default with the admin's own override applied (if any),
// plus a flag so the UI can label "Con override" / "Valor integrado".
type AdminPrompt struct {
	Key          string `json:"key"`
	Label        string `json:"label,omitempty"`
	Description  string `json:"description,omitempty"`
	SystemPrompt string `json:"systemPrompt"`
	UserPrompt   string `json:"userPrompt"`
	HasOverride  bool   `json:"hasOverride"`
	UpdatedAt    string `json:"updatedAt,omitempty"`
}

// MaxPromptLength caps admin-managed prompt text (same clamp as the client
// prompts the UI edits are expected to respect).
const MaxPromptLength = 20000

// ListPromptOverrides returns the admin's global prompt overrides, keyed set.
// They sit between the embedded defaults and per-user settings:
// embedded default < admin global override < user setting < per-novel prompt.
func (s *Store) ListPromptOverrides() ([]Prompt, error) {
	records, err := s.App.FindRecordsByFilter(PromptOverridesCollection, "", "key", 100, 0)
	if err != nil {
		return nil, err
	}
	out := make([]Prompt, 0, len(records))
	for _, record := range records {
		out = append(out, Prompt{
			Key:          record.GetString("key"),
			SystemPrompt: record.GetString("system_prompt"),
			UserPrompt:   record.GetString("user_prompt"),
			UpdatedAt:    record.GetString("updated"),
		})
	}
	return out, nil
}

// UpsertPromptOverride stores (or updates) the global override for a prompt
// key. Only known keys are accepted.
func (s *Store) UpsertPromptOverride(prompt Prompt) (Prompt, error) {
	if !IsKnownPromptKey(prompt.Key) {
		return Prompt{}, ErrInvalidInput
	}
	if len(prompt.SystemPrompt) > MaxPromptLength || len(prompt.UserPrompt) > MaxPromptLength {
		return Prompt{}, ErrInvalidInput
	}
	record, err := s.App.FindFirstRecordByFilter(PromptOverridesCollection, "key = {:key}", dbx.Params{"key": prompt.Key})
	if err != nil {
		collection, cErr := s.App.FindCollectionByNameOrId(PromptOverridesCollection)
		if cErr != nil {
			return Prompt{}, cErr
		}
		record = core.NewRecord(collection)
		record.Set("key", prompt.Key)
	}
	record.Set("system_prompt", prompt.SystemPrompt)
	record.Set("user_prompt", prompt.UserPrompt)
	if err := s.App.Save(record); err != nil {
		return Prompt{}, err
	}
	return Prompt{
		Key:          record.GetString("key"),
		SystemPrompt: record.GetString("system_prompt"),
		UserPrompt:   record.GetString("user_prompt"),
		UpdatedAt:    record.GetString("updated"),
	}, nil
}

func (s *Store) DeletePromptOverride(key string) error {
	record, err := s.App.FindFirstRecordByFilter(PromptOverridesCollection, "key = {:key}", dbx.Params{"key": key})
	if err != nil {
		return ErrNotFound
	}
	return s.App.Delete(record)
}

// ListEffectiveAdminPrompts returns the 5 known prompt slots with the admin's
// global override applied on top of the embedded defaults, plus a hasOverride
// flag so the UI can label "Con override" / "Valor integrado". This is the
// admin-side analogue of ListPrompts(userID): same precedence, minus the
// per-user layer (which the admin cannot edit).
func (s *Store) ListEffectiveAdminPrompts() ([]AdminPrompt, error) {
	defaults := []AdminPrompt{
		{Key: "translation", Label: "Traducción", Description: "Prompt global para traducción de capítulos.", SystemPrompt: DefaultTranslationSystemPrompt, UserPrompt: DefaultTranslationUserPrompt},
		{Key: "title", Label: "Traducción de Título", Description: "Prompt global para traducción de títulos de capítulo.", SystemPrompt: DefaultTitleTranslationSystemPrompt, UserPrompt: DefaultTitleTranslationUserPrompt},
		{Key: "refine", Label: "Refinamiento", Description: "Prompt global para mejorar traducciones generadas.", SystemPrompt: DefaultRefineSystemPrompt, UserPrompt: DefaultRefineUserPrompt},
		{Key: "check", Label: "Verificación", Description: "Prompt global para revisar calidad de traducción.", SystemPrompt: DefaultCheckSystemPrompt, UserPrompt: DefaultCheckUserPrompt},
		{Key: "glossary", Label: "Glosario", Description: "Prompt global para generar glosario de traducción.", SystemPrompt: DefaultGlossaryPrompt, UserPrompt: ""},
	}
	overrides, err := s.ListPromptOverrides()
	if err != nil {
		return nil, err
	}
	byKey := map[string]Prompt{}
	for _, item := range overrides {
		byKey[item.Key] = item
	}
	for i := range defaults {
		override, ok := byKey[defaults[i].Key]
		if !ok {
			continue
		}
		defaults[i].HasOverride = true
		defaults[i].UpdatedAt = override.UpdatedAt
		if strings.TrimSpace(override.SystemPrompt) != "" {
			defaults[i].SystemPrompt = override.SystemPrompt
		}
		if strings.TrimSpace(override.UserPrompt) != "" {
			defaults[i].UserPrompt = override.UserPrompt
		}
	}
	return defaults, nil
}

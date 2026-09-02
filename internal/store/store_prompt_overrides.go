package store

import (
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

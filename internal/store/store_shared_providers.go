package store

import (
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Shared provider keys let the admin make a provider usable for every user
// without distributing the raw key. Resolution order is always:
// user's own key (if configured) -> admin shared key (if provider is shared)
// -> not configured. The plaintext key never leaves the store layer.
func (s *Store) UpsertSharedProviderKey(providerKey, apiKey string, shared bool, updatedBy string) (SharedProviderKey, error) {
	if _, err := s.getProviderByKey(providerKey); err != nil {
		return SharedProviderKey{}, ErrNotFound
	}
	record, err := s.findSharedProviderKeyRecord(providerKey)
	if err != nil {
		collection, cErr := s.App.FindCollectionByNameOrId(SharedProviderKeysCollection)
		if cErr != nil {
			return SharedProviderKey{}, cErr
		}
		record = core.NewRecord(collection)
		record.Set("provider", providerKey)
		if strings.TrimSpace(apiKey) == "" {
			return SharedProviderKey{}, ErrInvalidInput
		}
	}
	if strings.TrimSpace(apiKey) != "" {
		encrypted, encErr := s.Encryptor.Encrypt(strings.TrimSpace(apiKey))
		if encErr != nil {
			return SharedProviderKey{}, encErr
		}
		record.Set("api_key_encrypted", encrypted)
		record.Set("api_key_configured", true)
		record.Set("api_key_updated_at", time.Now().UTC().Format(time.RFC3339))
	}
	record.Set("shared", shared)
	if updatedBy != "" {
		record.Set("updated_by", updatedBy)
	}
	if err := s.App.Save(record); err != nil {
		return SharedProviderKey{}, err
	}
	return sharedKeyFromRecord(record), nil
}

func (s *Store) DeleteSharedProviderKey(providerKey string) error {
	record, err := s.findSharedProviderKeyRecord(providerKey)
	if err != nil {
		return ErrNotFound
	}
	return s.App.Delete(record)
}

func (s *Store) ListSharedProviderKeys() ([]SharedProviderKey, error) {
	records, err := s.App.FindRecordsByFilter(SharedProviderKeysCollection, "", "provider", 200, 0)
	if err != nil {
		return nil, err
	}
	out := make([]SharedProviderKey, 0, len(records))
	for _, record := range records {
		out = append(out, sharedKeyFromRecord(record))
	}
	return out, nil
}

// GetDecryptedSharedKey returns the decrypted shared key for a provider, but
// only when the provider is marked as shared and a key is configured.
func (s *Store) GetDecryptedSharedKey(providerKey string) (string, bool, error) {
	record, err := s.findSharedProviderKeyRecord(providerKey)
	if err != nil || !record.GetBool("shared") || !record.GetBool("api_key_configured") {
		return "", false, nil
	}
	plaintext, err := s.Encryptor.Decrypt(record.GetString("api_key_encrypted"))
	if err != nil {
		return "", false, err
	}
	return plaintext, true, nil
}

func (s *Store) findSharedProviderKeyRecord(providerKey string) (*core.Record, error) {
	return s.App.FindFirstRecordByFilter(SharedProviderKeysCollection, "provider = {:provider}", dbx.Params{"provider": providerKey})
}

func sharedKeyFromRecord(record *core.Record) SharedProviderKey {
	updatedAt := record.GetString("api_key_updated_at")
	if record.GetDateTime("api_key_updated_at").IsZero() {
		updatedAt = ""
	}
	return SharedProviderKey{
		Provider:        record.GetString("provider"),
		Configured:      record.GetBool("api_key_configured"),
		Shared:          record.GetBool("shared"),
		APIKeyUpdatedAt: updatedAt,
	}
}

package store

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type WorkerToken struct {
	ID          string `json:"id"`
	UserID      string `json:"userId"`
	ExtensionID string `json:"extensionId"`
	TokenHash   string `json:"-"`
	Label       string `json:"label"`
	LastUsedAt  string `json:"lastUsedAt,omitempty"`
	CreatedAt   string `json:"createdAt,omitempty"`
	Revoked     bool   `json:"revoked"`
}

func generateToken() (plaintext string, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("generate token: %w", err)
	}
	plaintext = hex.EncodeToString(b)
	h := sha256.Sum256([]byte(plaintext))
	hash = hex.EncodeToString(h[:])
	return plaintext, hash, nil
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func (s *Store) CreateWorkerToken(userID, extensionID, label string) (*WorkerToken, string, error) {
	plaintext, hash, err := generateToken()
	if err != nil {
		return nil, "", err
	}

	collection, err := s.App.FindCollectionByNameOrId(WorkerTokensCollection)
	if err != nil {
		return nil, "", err
	}

	record := core.NewRecord(collection)
	record.Set("owner", userID)
	record.Set("extension_id", extensionID)
	record.Set("token_hash", hash)
	record.Set("label", label)
	record.Set("revoked", false)

	if err := s.App.Save(record); err != nil {
		return nil, "", fmt.Errorf("save worker token: %w", err)
	}

	token := &WorkerToken{
		ID:          record.Id,
		UserID:      userID,
		ExtensionID: extensionID,
		TokenHash:   hash,
		Label:       label,
		CreatedAt:   record.GetString("created"),
		Revoked:     false,
	}

	return token, plaintext, nil
}

func (s *Store) ValidateWorkerToken(token string) (*WorkerToken, error) {
	hash := hashToken(token)

	records, err := s.App.FindRecordsByFilter(
		WorkerTokensCollection,
		"token_hash = {:hash} && revoked = false",
		"",
		1, 0,
		dbx.Params{"hash": hash},
	)
	if err != nil || len(records) == 0 {
		return nil, fmt.Errorf("invalid or revoked token")
	}

	record := records[0]
	ownerID := record.GetString("owner")

	record.Set("last_used_at", time.Now().Format(time.RFC3339))
	if err := s.App.Save(record); err != nil {
		return nil, fmt.Errorf("update last used: %w", err)
	}

	return &WorkerToken{
		ID:          record.Id,
		UserID:      ownerID,
		ExtensionID: record.GetString("extension_id"),
		TokenHash:   hash,
		Label:       record.GetString("label"),
		LastUsedAt:  time.Now().Format(time.RFC3339),
		CreatedAt:   record.GetString("created"),
		Revoked:     false,
	}, nil
}

func (s *Store) ListWorkerTokens(userID string) ([]WorkerToken, error) {
	records, err := s.App.FindRecordsByFilter(
		WorkerTokensCollection,
		"owner = {:owner}",
		"-created",
		100, 0,
		dbx.Params{"owner": userID},
	)
	if err != nil {
		return nil, err
	}

	tokens := make([]WorkerToken, 0, len(records))
	for _, record := range records {
		tokens = append(tokens, WorkerToken{
			ID:          record.Id,
			UserID:      userID,
			ExtensionID: record.GetString("extension_id"),
			Label:       record.GetString("label"),
			LastUsedAt:  record.GetString("last_used_at"),
			CreatedAt:   record.GetString("created"),
			Revoked:     record.GetBool("revoked"),
		})
	}
	return tokens, nil
}

func (s *Store) RevokeWorkerToken(userID, tokenID string) error {
	records, err := s.App.FindRecordsByFilter(
		WorkerTokensCollection,
		"id = {:id}",
		"",
		1, 0,
		dbx.Params{"id": tokenID},
	)
	if err != nil || len(records) == 0 {
		return ErrNotFound
	}

	record := records[0]
	if record.GetString("owner") != userID {
		return ErrForbidden
	}

	record.Set("revoked", true)
	if err := s.App.Save(record); err != nil {
		return fmt.Errorf("revoke token: %w", err)
	}
	return nil
}

func (s *Store) DeleteWorkerToken(userID, tokenID string) error {
	records, err := s.App.FindRecordsByFilter(
		WorkerTokensCollection,
		"id = {:id}",
		"",
		1, 0,
		dbx.Params{"id": tokenID},
	)
	if err != nil || len(records) == 0 {
		return ErrNotFound
	}

	record := records[0]
	if record.GetString("owner") != userID {
		return ErrForbidden
	}

	if err := s.App.Delete(record); err != nil {
		return fmt.Errorf("delete token: %w", err)
	}
	return nil
}

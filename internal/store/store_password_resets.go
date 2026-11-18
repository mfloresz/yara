package store

import (
	"fmt"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// DefaultPasswordResetTTL is how long an admin-issued password reset link
// stays redeemable. Single use: marked used before the password change.
const DefaultPasswordResetTTL = 24 * time.Hour

type PasswordReset struct {
	ID        string `json:"id"`
	UserID    string `json:"userId"`
	Email     string `json:"email,omitempty"`
	ExpiresAt string `json:"expiresAt,omitempty"`
	UsedAt    string `json:"usedAt,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
}

func passwordResetFromRecord(record *core.Record, email string) *PasswordReset {
	out := &PasswordReset{
		ID:        record.Id,
		UserID:    record.GetString("user"),
		Email:     email,
		ExpiresAt: record.GetString("expires_at"),
		UsedAt:    record.GetString("used_at"),
		CreatedAt: record.GetString("created"),
	}
	if record.GetDateTime("used_at").IsZero() {
		out.UsedAt = ""
	}
	return out
}

// CreatePasswordReset issues a single-use reset token for an existing user.
// Only the SHA-256 hash is stored; the plaintext is returned once for the
// admin to share as a link.
func (s *Store) CreatePasswordReset(createdBy, userID string) (*PasswordReset, string, error) {
	user, err := s.App.FindRecordById(UsersCollection, userID)
	if err != nil {
		return nil, "", ErrNotFound
	}
	if user.GetBool("blocked") {
		return nil, "", ErrForbidden
	}
	plaintext, hash, err := generateToken()
	if err != nil {
		return nil, "", err
	}
	collection, err := s.App.FindCollectionByNameOrId(PasswordResetsCollection)
	if err != nil {
		return nil, "", err
	}
	record := core.NewRecord(collection)
	record.Set("user", userID)
	record.Set("token_hash", hash)
	record.Set("expires_at", time.Now().UTC().Add(DefaultPasswordResetTTL))
	if createdBy != "" {
		record.Set("created_by", createdBy)
	}
	if err := s.App.Save(record); err != nil {
		return nil, "", fmt.Errorf("save password reset: %w", err)
	}
	return passwordResetFromRecord(record, user.Email()), plaintext, nil
}

// FindPasswordResetByToken resolves a reset by its raw token without
// explaining why it is invalid (unknown / used / expired).
func (s *Store) FindPasswordResetByToken(token string) (*PasswordReset, error) {
	record, err := s.App.FindFirstRecordByFilter(PasswordResetsCollection, "token_hash = {:hash}", dbx.Params{"hash": hashToken(token)})
	if err != nil {
		return nil, ErrNotFound
	}
	if !record.GetDateTime("used_at").IsZero() {
		return nil, ErrInvitationUsed
	}
	if expiresAt := record.GetDateTime("expires_at").Time(); !record.GetDateTime("expires_at").IsZero() && time.Now().UTC().After(expiresAt) {
		return nil, ErrInvitationExpired
	}
	email := ""
	if user, err := s.App.FindRecordById(UsersCollection, record.GetString("user")); err == nil {
		email = user.Email()
	}
	return passwordResetFromRecord(record, email), nil
}

// RedeemPasswordReset atomically consumes the reset, changes the password,
// invalidates sessions and revokes every worker credential.
func (s *Store) RedeemPasswordReset(token, password string) (User, error) {
	if len(password) < 8 {
		return User{}, ErrInvalidInput
	}
	var result User
	err := s.App.RunInTransaction(func(tx core.App) error {
		st := New(tx, s.Encryptor)
		reset, err := st.FindPasswordResetByToken(token)
		if err != nil {
			return err
		}
		record, err := tx.FindRecordById(PasswordResetsCollection, reset.ID)
		if err != nil {
			return ErrNotFound
		}
		record.Set("used_at", time.Now().UTC())
		if err := tx.Save(record); err != nil {
			return fmt.Errorf("mark reset used: %w", err)
		}
		user, err := tx.FindRecordById(UsersCollection, reset.UserID)
		if err != nil {
			return ErrNotFound
		}
		user.SetPassword(password)
		user.RefreshTokenKey()
		if err := tx.Save(user); err != nil {
			return fmt.Errorf("set new password: %w", err)
		}
		if err := st.revokeAllWorkerTokens(reset.UserID); err != nil {
			return fmt.Errorf("revoke worker tokens: %w", err)
		}
		result = userFromRecord(user)
		return nil
	})
	return result, err
}

func (s *Store) revokeAllWorkerTokens(userID string) error {
	// Revoked records leave the filter, so always start at offset zero.
	for {
		records, err := s.App.FindRecordsByFilter(WorkerTokensCollection, "owner = {:owner} && revoked = false", "", 500, 0, dbx.Params{"owner": userID})
		if err != nil {
			return err
		}
		if len(records) == 0 {
			return nil
		}
		for _, record := range records {
			record.Set("revoked", true)
			if err := s.App.Save(record); err != nil {
				return err
			}
		}
	}
}

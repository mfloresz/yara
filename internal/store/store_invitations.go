package store

import (
	"fmt"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// DefaultInvitationTTL is how long an invitation link stays redeemable.
const DefaultInvitationTTL = 7 * 24 * time.Hour

func (s *Store) CreateInvitation(createdBy, email, role string) (*Invitation, string, error) {
	if role == "" {
		role = RoleUser
	}
	if role != RoleAdmin && role != RoleUser {
		return nil, "", ErrInvalidInput
	}
	email = normalizeEmail(email)
	if email == "" {
		return nil, "", ErrInvalidInput
	}
	if _, err := s.App.FindAuthRecordByEmail(UsersCollection, email); err == nil {
		return nil, "", ErrEmailTaken
	}

	plaintext, hash, err := generateToken()
	if err != nil {
		return nil, "", err
	}

	collection, err := s.App.FindCollectionByNameOrId(InvitationsCollection)
	if err != nil {
		return nil, "", err
	}
	record := core.NewRecord(collection)
	record.Set("email", email)
	record.Set("token_hash", hash)
	record.Set("role", role)
	record.Set("expires_at", time.Now().UTC().Add(DefaultInvitationTTL))
	if createdBy != "" {
		record.Set("created_by", createdBy)
	}
	if err := s.App.Save(record); err != nil {
		return nil, "", fmt.Errorf("save invitation: %w", err)
	}

	return invitationFromRecord(record), plaintext, nil
}

func (s *Store) ListInvitations() ([]Invitation, error) {
	records, err := s.App.FindRecordsByFilter(InvitationsCollection, "", "-created", 500, 0)
	if err != nil {
		return nil, err
	}
	out := make([]Invitation, 0, len(records))
	for _, record := range records {
		out = append(out, *invitationFromRecord(record))
	}
	return out, nil
}

func (s *Store) DeleteInvitation(id string) error {
	record, err := s.App.FindRecordById(InvitationsCollection, id)
	if err != nil {
		return ErrNotFound
	}
	return s.App.Delete(record)
}

// FindInvitationByToken resolves an invitation by its raw (unhashed) token.
// ErrNotFound is returned when the token is unknown.
func (s *Store) FindInvitationByToken(token string) (*Invitation, error) {
	record, err := s.App.FindFirstRecordByFilter(InvitationsCollection, "token_hash = {:hash}", dbx.Params{"hash": hashToken(token)})
	if err != nil {
		return nil, ErrNotFound
	}
	invitation := invitationFromRecord(record)
	if invitation.UsedAt != "" {
		return nil, ErrInvitationUsed
	}
	if expiresAt := record.GetDateTime("expires_at").Time(); !record.GetDateTime("expires_at").IsZero() && time.Now().UTC().After(expiresAt) {
		return nil, ErrInvitationExpired
	}
	return invitation, nil
}

// RedeemInvitation validates the invitation token and creates the invited
// user with the invited role, marking the invitation as used. Callers must
// serialize redemption (check-then-act) — the api layer holds a mutex around
// this call. ErrNotFound/ErrInvitationUsed/ErrInvitationExpired/ErrEmailTaken
// are returned as-is; handlers map used/expired to the same generic message.
func (s *Store) RedeemInvitation(token, password string) (User, error) {
	invitation, err := s.FindInvitationByToken(token)
	if err != nil {
		return User{}, err
	}

	if _, err := s.App.FindAuthRecordByEmail(UsersCollection, invitation.Email); err == nil {
		return User{}, ErrEmailTaken
	}

	result, err := s.CreateUser(invitation.Email, password, "")
	if err != nil {
		return User{}, fmt.Errorf("create invited user: %w", err)
	}
	if invitation.Role == RoleAdmin {
		result.User, err = s.UpdateUserRole(result.User.ID, RoleAdmin)
		if err != nil {
			return User{}, fmt.Errorf("apply invited role: %w", err)
		}
	}

	record, err := s.App.FindRecordById(InvitationsCollection, invitation.ID)
	if err != nil {
		return User{}, ErrNotFound
	}
	record.Set("used_at", time.Now().UTC())
	if err := s.App.Save(record); err != nil {
		return User{}, fmt.Errorf("mark invitation used: %w", err)
	}
	return result.User, nil
}

func invitationFromRecord(record *core.Record) *Invitation {
	invitation := &Invitation{
		ID:        record.Id,
		Email:     record.GetString("email"),
		Role:      record.GetString("role"),
		ExpiresAt: record.GetString("expires_at"),
		UsedAt:    record.GetString("used_at"),
		CreatedBy: record.GetString("created_by"),
		CreatedAt: record.GetString("created"),
	}
	if invitation.UsedAt == "0001-01-01 00:00:00.000Z" || record.GetDateTime("used_at").IsZero() {
		invitation.UsedAt = ""
	}
	return invitation
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

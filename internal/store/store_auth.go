package store

import (
	"errors"
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type AuthResult struct {
	Token string `json:"token"`
	User  User   `json:"user"`
}

// ErrLastAdmin is returned when a role change would leave the installation
// without any admin user.
var ErrLastAdmin = errors.New("at least one admin user is required")

// ErrInvalidInput is returned for domain-level validation failures such as
// an unknown role value.
var ErrInvalidInput = errors.New("invalid input")

func (s *Store) CreateUser(email, password, name string) (*AuthResult, error) {
	users, err := s.App.FindCollectionByNameOrId(UsersCollection)
	if err != nil {
		return nil, err
	}
	record := core.NewRecord(users)
	record.SetEmail(strings.TrimSpace(email))
	record.SetPassword(password)
	record.SetVerified(true)
	record.Set("name", strings.TrimSpace(name))
	record.Set("role", RoleUser)
	record.Set("theme", "system")
	record.Set("active_provider", DefaultAISettings.Provider)
	if err := s.App.Save(record); err != nil {
		return nil, err
	}
	token, err := record.NewAuthToken()
	if err != nil {
		return nil, err
	}
	return &AuthResult{Token: token, User: userFromRecord(record)}, nil
}

func (s *Store) AuthenticateUser(email, password string) (*AuthResult, error) {
	record, err := s.App.FindAuthRecordByEmail(UsersCollection, strings.TrimSpace(email))
	if err != nil || record == nil || !record.ValidatePassword(password) {
		return nil, fmt.Errorf("invalid credentials")
	}
	token, err := record.NewAuthToken()
	if err != nil {
		return nil, err
	}
	return &AuthResult{Token: token, User: userFromRecord(record)}, nil
}

func (s *Store) RefreshAuth(token string) (*AuthResult, error) {
	record, err := s.App.FindAuthRecordByToken(token, core.TokenTypeAuth)
	if err != nil {
		return nil, err
	}
	newToken, err := record.NewAuthToken()
	if err != nil {
		return nil, err
	}
	return &AuthResult{Token: newToken, User: userFromRecord(record)}, nil
}

func (s *Store) FindAuthRecord(token string) (*core.Record, error) {
	return s.App.FindAuthRecordByToken(token, core.TokenTypeAuth)
}

// CountUsers returns the total number of registered users.
func (s *Store) CountUsers() (int, error) {
	n, err := s.App.CountRecords(UsersCollection)
	return int(n), err
}

// HasAdmin reports whether at least one user has the admin role.
func (s *Store) HasAdmin() (bool, error) {
	n, err := s.adminCount()
	return n > 0, err
}

func (s *Store) adminCount() (int, error) {
	n, err := s.App.CountRecords(UsersCollection, dbx.HashExp{"role": RoleAdmin})
	return int(n), err
}

// ListUsers returns every user for the admin panel, newest first.
func (s *Store) ListUsers() ([]User, error) {
	records, err := s.App.FindRecordsByFilter(UsersCollection, "", "-created", 500, 0)
	if err != nil {
		return nil, err
	}
	out := make([]User, 0, len(records))
	for _, record := range records {
		out = append(out, userFromRecord(record))
	}
	return out, nil
}

// UpdateUserRole promotes or demotes a user. Demoting the last admin is
// rejected with ErrLastAdmin so the installation never loses admin access.
func (s *Store) UpdateUserRole(userID, role string) (User, error) {
	if role != RoleAdmin && role != RoleUser {
		return User{}, ErrInvalidInput
	}
	record, err := s.App.FindRecordById(UsersCollection, userID)
	if err != nil {
		return User{}, ErrNotFound
	}
	if role == RoleUser && record.GetString("role") == RoleAdmin {
		admins, err := s.adminCount()
		if err != nil {
			return User{}, err
		}
		if admins <= 1 {
			return User{}, ErrLastAdmin
		}
	}
	record.Set("role", role)
	if err := s.App.Save(record); err != nil {
		return User{}, err
	}
	return userFromRecord(record), nil
}

// PromoteUserByEmail grants the admin role to the user with the given email.
// Used by the -promote-admin bootstrap flag for pre-existing installs.
func (s *Store) PromoteUserByEmail(email string) (User, error) {
	record, err := s.App.FindAuthRecordByEmail(UsersCollection, strings.TrimSpace(email))
	if err != nil {
		return User{}, ErrNotFound
	}
	record.Set("role", RoleAdmin)
	if err := s.App.Save(record); err != nil {
		return User{}, err
	}
	return userFromRecord(record), nil
}

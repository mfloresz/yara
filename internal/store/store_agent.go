package store

import (
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

// AgentSession persists one chat conversation with the library assistant.
// Messages is a JSON array of ai.AgentMessage-shaped objects; the api layer
// owns encoding, trimming and replay. There is no per-session metadata beyond
// the owner: the assistant context (selected novel) travels per message.
type AgentSession struct {
	ID        string `json:"id"`
	OwnerID   string `json:"ownerId"`
	Messages  string `json:"messages"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// agentMessagesFieldMax caps the serialized chat trail. PocketBase falls back
// to 5000 chars when a TextField has no explicit Max, which truncated real
// histories at the first list_novels result.
const agentMessagesFieldMax = 1_000_000

// agentSessionsOwnerIndex is the unique owner index that keeps one chat
// session per user.
const agentSessionsOwnerIndex = "idx_agent_sessions_owner_unique"

func (s *Store) ensureAgentSessionsCollection(users *core.Collection) (*core.Collection, error) {
	if existing, err := s.App.FindCollectionByNameOrId(AgentSessionsCollection); err == nil {
		if err := s.migrateAgentMessagesMax(existing); err != nil {
			return nil, err
		}
		if err := s.migrateAgentSessionsUniqueOwner(existing); err != nil {
			return nil, err
		}
		return existing, nil
	}
	c := core.NewBaseCollection(AgentSessionsCollection)
	ownerOnly := "@request.auth.id != '' && owner = @request.auth.id"
	c.ListRule = types.Pointer(ownerOnly)
	c.ViewRule = types.Pointer(ownerOnly)
	c.CreateRule = nil
	c.UpdateRule = nil
	c.DeleteRule = nil
	c.Fields.Add(&core.RelationField{Name: "owner", Required: true, CollectionId: users.Id, MaxSelect: 1, CascadeDelete: true})
	c.Fields.Add(&core.TextField{Name: "messages", Max: agentMessagesFieldMax})
	addSystemDateFields(c)
	// One chat session per user: the owner is unique, so two concurrent turns
	// that both try to create a session cannot end up with two records the
	// "latest session" lookup would have to disambiguate.
	c.AddIndex(agentSessionsOwnerIndex, true, "owner", "")
	if err := s.App.Save(c); err != nil {
		return nil, err
	}
	return c, nil
}

// migrateAgentMessagesMax raises the messages field cap on collections created
// before the explicit Max existed (their TextField ran on PocketBase's 5000
// char default and rejected real session saves).
func (s *Store) migrateAgentMessagesMax(c *core.Collection) error {
	field := c.Fields.GetByName("messages")
	if field == nil {
		return nil
	}
	textField, ok := field.(*core.TextField)
	if !ok || textField.Max >= agentMessagesFieldMax {
		return nil
	}
	textField.Max = agentMessagesFieldMax
	return s.App.Save(c)
}

// migrateAgentSessionsUniqueOwner installs the one-session-per-owner unique
// index on collections created before it existed. Leftover duplicates (a race
// between two turns that both found no session) are collapsed first, keeping
// the most recently updated record, because SQLite refuses to create a unique
// index while duplicates exist and the server must still boot.
func (s *Store) migrateAgentSessionsUniqueOwner(c *core.Collection) error {
	for _, index := range c.Indexes {
		if strings.HasPrefix(index, "CREATE UNIQUE INDEX "+agentSessionsOwnerIndex) {
			return nil
		}
	}
	records, err := s.App.FindRecordsByFilter(AgentSessionsCollection, "owner != ''", "-updated", 0, 0)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, record := range records {
		owner := record.GetString("owner")
		if seen[owner] {
			if err := s.App.Delete(record); err != nil {
				return err
			}
			continue
		}
		seen[owner] = true
	}
	c.AddIndex(agentSessionsOwnerIndex, true, "owner", "")
	return s.App.Save(c)
}

func agentSessionFromRecord(r *core.Record) *AgentSession {
	return &AgentSession{
		ID:        r.Id,
		OwnerID:   r.GetString("owner"),
		Messages:  r.GetString("messages"),
		CreatedAt: r.GetString("created"),
		UpdatedAt: r.GetString("updated"),
	}
}

// CreateAgentSession persists a new empty session for the owner. The unique
// owner index makes creation idempotent per user: if a concurrent turn got
// there first, that session is returned instead of a duplicate.
func (s *Store) CreateAgentSession(ownerID, messages string) (*AgentSession, error) {
	collection, err := s.App.FindCollectionByNameOrId(AgentSessionsCollection)
	if err != nil {
		return nil, err
	}
	record := core.NewRecord(collection)
	record.Set("owner", ownerID)
	record.Set("messages", messages)
	if err := s.App.Save(record); err != nil {
		if existing, getErr := s.GetLatestAgentSession(ownerID); getErr == nil {
			return existing, nil
		}
		return nil, err
	}
	return agentSessionFromRecord(record), nil
}

// GetAgentSession returns one session owned by the requesting user.
func (s *Store) GetAgentSession(ownerID, sessionID string) (*AgentSession, error) {
	record, err := s.App.FindRecordById(AgentSessionsCollection, sessionID)
	if err != nil {
		return nil, ErrNotFound
	}
	if record.GetString("owner") != ownerID {
		return nil, ErrForbidden
	}
	return agentSessionFromRecord(record), nil
}

// GetLatestAgentSession returns the user's most recently updated session, or
// ErrNotFound when the user has none yet.
func (s *Store) GetLatestAgentSession(ownerID string) (*AgentSession, error) {
	records, err := s.App.FindRecordsByFilter(
		AgentSessionsCollection,
		"owner = {:owner}",
		"-updated",
		1,
		0,
		dbx.Params{"owner": ownerID},
	)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, ErrNotFound
	}
	return agentSessionFromRecord(records[0]), nil
}

// SaveAgentSessionMessages replaces the message trail of one owned session.
func (s *Store) SaveAgentSessionMessages(ownerID, sessionID, messages string) (*AgentSession, error) {
	record, err := s.App.FindRecordById(AgentSessionsCollection, sessionID)
	if err != nil {
		return nil, ErrNotFound
	}
	if record.GetString("owner") != ownerID {
		return nil, ErrForbidden
	}
	record.Set("messages", messages)
	if err := s.App.Save(record); err != nil {
		return nil, err
	}
	return agentSessionFromRecord(record), nil
}

// DeleteAgentSessions removes every session of the requesting user; used by
// the chat reset action. All-or-nothing: a partial delete would leave the
// chat showing a conversation the user believes they just cleared.
func (s *Store) DeleteAgentSessions(ownerID string) error {
	records, err := s.App.FindRecordsByFilter(AgentSessionsCollection, "owner = {:owner}", "", 0, 0, dbx.Params{"owner": ownerID})
	if err != nil {
		return err
	}
	return s.App.RunInTransaction(func(txApp core.App) error {
		for _, record := range records {
			if err := txApp.Delete(record); err != nil {
				return err
			}
		}
		return nil
	})
}

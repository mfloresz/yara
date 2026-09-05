package store

import (
	"github.com/pocketbase/dbx"
)

// AdminUserNovel is the lightweight novel row shown in the admin user drawer.
type AdminUserNovel struct {
	ID           string `json:"id"`
	SourceTitle  string `json:"sourceTitle"`
	TargetTitle  string `json:"targetTitle,omitempty"`
	IsPublic     bool   `json:"isPublic"`
	Status       string `json:"status,omitempty"`
	ChapterCount int    `json:"chapterCount"`
}

type AdminUserStats struct {
	User        User             `json:"user"`
	OwnedCount  int              `json:"ownedCount"`
	SharedCount int              `json:"sharedCount"`
	Novels      []AdminUserNovel `json:"novels"`
}

// GetAdminUserStats returns novel counts plus the owned novel list for the
// admin user drawer. Shared = owned AND is_public.
func (s *Store) GetAdminUserStats(userID string) (AdminUserStats, error) {
	record, err := s.App.FindRecordById(UsersCollection, userID)
	if err != nil {
		return AdminUserStats{}, ErrNotFound
	}
	records, err := s.App.FindRecordsByFilter(NovelsCollection, "owner = {:owner}", "-created", 500, 0, dbx.Params{"owner": userID})
	if err != nil {
		return AdminUserStats{}, err
	}
	novels := make([]AdminUserNovel, 0, len(records))
	shared := 0
	for _, record := range records {
		isPublic := record.GetBool("is_public")
		if isPublic {
			shared++
		}
		novels = append(novels, AdminUserNovel{
			ID:           record.Id,
			SourceTitle:  record.GetString("source_title"),
			TargetTitle:  record.GetString("target_title"),
			IsPublic:     isPublic,
			Status:       record.GetString("status"),
			ChapterCount: asInt(record.GetFloat("chapter_count"), 0),
		})
	}
	return AdminUserStats{
		User:        userFromRecord(record),
		OwnedCount:  len(records),
		SharedCount: shared,
		Novels:      novels,
	}, nil
}

// BlockUser prevents login and API/extension use without deleting data.
func (s *Store) BlockUser(actorID, userID string) (User, error) {
	if actorID == userID {
		return User{}, ErrInvalidInput
	}
	record, err := s.App.FindRecordById(UsersCollection, userID)
	if err != nil {
		return User{}, ErrNotFound
	}
	if record.GetString("role") == RoleAdmin {
		admins, err := s.adminCount()
		if err != nil {
			return User{}, err
		}
		if admins <= 1 {
			return User{}, ErrLastAdmin
		}
	}
	record.Set("blocked", true)
	record.RefreshTokenKey()
	if err := s.App.Save(record); err != nil {
		return User{}, err
	}
	if err := s.revokeAllWorkerTokens(userID); err != nil {
		return User{}, err
	}
	return userFromRecord(record), nil
}

func (s *Store) UnblockUser(userID string) (User, error) {
	record, err := s.App.FindRecordById(UsersCollection, userID)
	if err != nil {
		return User{}, ErrNotFound
	}
	record.Set("blocked", false)
	if err := s.App.Save(record); err != nil {
		return User{}, err
	}
	return userFromRecord(record), nil
}

// DeleteUserWithNovels deletes the user and every novel they own (chapters,
// jobs, epubs and reading progress cascade from the novel). Per-user settings
// and worker tokens are deleted explicitly. Active jobs reject with
// ErrActiveJobs so the in-process worker never references deleted novels.
func (s *Store) DeleteUserWithNovels(actorID, userID string) error {
	if actorID == userID {
		return ErrInvalidInput
	}
	record, err := s.App.FindRecordById(UsersCollection, userID)
	if err != nil {
		return ErrNotFound
	}
	if record.GetString("role") == RoleAdmin {
		admins, err := s.adminCount()
		if err != nil {
			return err
		}
		if admins <= 1 {
			return ErrLastAdmin
		}
	}
	if has, err := s.HasActiveJobs(userID); err != nil {
		return err
	} else if has {
		return ErrActiveJobs
	}
	novels, err := s.App.FindRecordsByFilter(NovelsCollection, "owner = {:owner}", "", 5000, 0, dbx.Params{"owner": userID})
	if err != nil {
		return err
	}
	for _, novel := range novels {
		if err := s.App.Delete(novel); err != nil {
			return err
		}
	}
	if err := s.deleteUserOwnedRows(userID); err != nil {
		return err
	}
	return s.App.Delete(record)
}

// TransferNovelsAndDeleteUser moves every owned novel (and its jobs) to
// another user, then deletes the source user and their personal settings.
func (s *Store) TransferNovelsAndDeleteUser(actorID, userID, toUserID string) (int, error) {
	if actorID == userID || userID == toUserID || toUserID == "" {
		return 0, ErrInvalidInput
	}
	record, err := s.App.FindRecordById(UsersCollection, userID)
	if err != nil {
		return 0, ErrNotFound
	}
	if _, err := s.App.FindRecordById(UsersCollection, toUserID); err != nil {
		return 0, ErrNotFound
	}
	if record.GetString("role") == RoleAdmin {
		admins, err := s.adminCount()
		if err != nil {
			return 0, err
		}
		if admins <= 1 {
			return 0, ErrLastAdmin
		}
	}
	if has, err := s.HasActiveJobs(userID); err != nil {
		return 0, err
	} else if has {
		return 0, ErrActiveJobs
	}
	novels, err := s.App.FindRecordsByFilter(NovelsCollection, "owner = {:owner}", "", 5000, 0, dbx.Params{"owner": userID})
	if err != nil {
		return 0, err
	}
	moved := 0
	for _, novel := range novels {
		novel.Set("owner", toUserID)
		if err := s.App.Save(novel); err != nil {
			return moved, err
		}
		moved++
	}
	jobs, err := s.App.FindRecordsByFilter(JobsCollection, "owner = {:owner}", "", 5000, 0, dbx.Params{"owner": userID})
	if err != nil {
		return moved, err
	}
	for _, job := range jobs {
		job.Set("owner", toUserID)
		if err := s.App.Save(job); err != nil {
			return moved, err
		}
	}
	if err := s.deleteUserOwnedRows(userID); err != nil {
		return moved, err
	}
	return moved, s.App.Delete(record)
}

// deleteUserOwnedRows removes per-user config that must not orphan: provider
// settings, prompt settings, translation settings, worker tokens, reading
// progress and pending password resets. Novel-bound rows cascade from novels.
func (s *Store) deleteUserOwnedRows(userID string) error {
	params := dbx.Params{"owner": userID, "user": userID}
	for _, target := range []struct {
		collection string
		filter     string
	}{
		{UserProviderSettingsCollection, "owner = {:owner}"},
		{UserPromptSettingsCollection, "owner = {:owner}"},
		{UserTranslationCollection, "owner = {:owner}"},
		{WorkerTokensCollection, "owner = {:owner}"},
		{ReadingProgressCollection, "user = {:user}"},
		{PasswordResetsCollection, "user = {:user}"},
	} {
		records, err := s.App.FindRecordsByFilter(target.collection, target.filter, "", 5000, 0, params)
		if err != nil {
			return err
		}
		for _, record := range records {
			if err := s.App.Delete(record); err != nil {
				return err
			}
		}
	}
	return nil
}

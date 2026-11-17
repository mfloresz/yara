package store

import (
	"context"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

var ErrInvalidCurrentPassword = errors.New("invalid current password")

// ChangePassword changes only the authenticated user's password. tokenKey is
// the key from the authenticated record, not a client-supplied target. Checking
// it again inside the transaction prevents an in-flight request authenticated
// before a concurrent password change from reviving a revoked session.
func (s *Store) ChangePassword(userID, tokenKey, currentPassword, newPassword, confirmation string) error {
	if currentPassword == "" || newPassword == "" || newPassword != confirmation || currentPassword == newPassword || len(newPassword) > 72 {
		return ErrInvalidInput
	}
	return s.App.RunInTransaction(func(tx core.App) error {
		user, err := tx.FindRecordById(UsersCollection, userID)
		if err != nil {
			return err
		}
		if user.GetBool("blocked") || user.TokenKey() != tokenKey {
			return ErrForbidden
		}
		if !user.ValidatePassword(currentPassword) {
			return ErrInvalidCurrentPassword
		}
		user.SetPassword(newPassword)
		// Apply the actual PocketBase collection policy, including its character
		// minimum, maximum and optional pattern. Never trim or normalize passwords.
		if err := user.Collection().Fields.GetByName(core.FieldNamePassword).ValidateValue(context.Background(), tx, user); err != nil {
			return ErrInvalidInput
		}
		user.RefreshTokenKey()
		if err := tx.Save(user); err != nil {
			return err
		}
		// Browser-worker credentials are independent of PocketBase auth JWTs.
		// Revoke them in the same transaction; page through every active token,
		// rather than leaving credentials beyond the first 500 usable.
		for {
			records, err := tx.FindRecordsByFilter(WorkerTokensCollection, "owner = {:owner} && revoked = false", "", 500, 0, dbx.Params{"owner": userID})
			if err != nil {
				return err
			}
			if len(records) == 0 {
				break
			}
			for _, record := range records {
				record.Set("revoked", true)
				if err := tx.Save(record); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

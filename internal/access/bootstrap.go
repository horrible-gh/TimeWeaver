package access

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/horrible-gh/TimeWeaver/internal/store"
)

// HasBootstrapPassword is a read-only startup check. A password-change gate is
// not ownership: anybody who knows a published seed password could claim the
// account. The executable therefore refuses to listen until active seed
// credentials have been replaced locally.
func HasBootstrapPassword(ctx context.Context, db *store.Store) (bool, error) {
	if db == nil || db.DB == nil {
		return false, errors.New("database is required")
	}
	comparison := "u.password=?"
	if db.Dialect == "mysql" {
		comparison = "BINARY u.password=BINARY ?"
	}
	var count int
	if err := db.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM users u JOIN tw_user_auth a ON a.user_id=u.user_id WHERE a.status='active' AND "+comparison, legacySeedHash).Scan(&count); err != nil {
		return false, unavailable()
	}
	return count != 0, nil
}

// BootstrapAdministrator changes only the untouched, active seed administrator.
// It is an offline ownership step, not a password-reset or recovery backdoor.
// Password, access version, session revocation and audit commit atomically.
func BootstrapAdministrator(ctx context.Context, db *store.Store, password string) error {
	if db == nil || db.DB == nil || db.ReadOnly() {
		return errors.New("administrator setup requires a writable database")
	}
	status, err := Status(ctx, db)
	if err != nil {
		return err
	}
	if !status.Ready {
		return errors.New("initialize the access schema before administrator setup")
	}
	hash, err := hashPassword(password)
	if err != nil {
		return errors.New("administrator password must contain at least 12 characters and at most 1024 UTF-8 bytes")
	}
	if checkPassword(legacySeedHash, password) {
		return errors.New("administrator password must differ from the published seed password")
	}
	tx, err := store.BeginWrite(ctx, db.DB, db.Dialect, nil)
	if err != nil {
		return unavailable()
	}
	defer tx.Rollback()
	var id, current, role, state string
	err = tx.QueryRowContext(ctx, "SELECT u.user_id,u.password,u.role,a.status FROM users u JOIN tw_user_auth a ON a.user_id=u.user_id WHERE u.user_id='timeweaver'"+store.LockClause(db.Dialect, false)).Scan(&id, &current, &role, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("seed administrator is absent; established accounts were not changed")
	}
	if err != nil {
		return unavailable()
	}
	if id != "timeweaver" || role != "admin" || state != "active" || !equalSecret(current, legacySeedHash) {
		return errors.New("seed administrator was already changed or disabled; established accounts were not changed")
	}
	now := time.Now().Unix()
	if _, err = tx.ExecContext(ctx, "UPDATE users SET password=? WHERE user_id=?", hash, id); err != nil {
		return unavailable()
	}
	if _, err = tx.ExecContext(ctx, "UPDATE tw_user_auth SET version=version+1,must_change_password=FALSE,password_changed_at=? WHERE user_id=?", now, id); err != nil {
		return unavailable()
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM tw_session WHERE user_id=?", id); err != nil {
		return unavailable()
	}
	if err = audit(ctx, tx, now, id, "administrator_bootstrap"); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return unavailable()
	}
	return nil
}

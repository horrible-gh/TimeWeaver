package access

import (
	"context"
	"database/sql"
)

// WithGroup holds the user/session lock until the authorized mutation commits.
// Callers resolve resource ownership inside fn, then verify each source/target
// group with u.Can. Request-supplied group IDs are never proof of ownership.
func (s *Service) WithGroup(ctx context.Context, token, csrf string, group int64, required string, fn func(*sql.Tx, User) error) error {
	tx, p, err := s.sessionTx(ctx, token)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if !equalSecret(p.CSRF, csrf) {
		return failure(403, "csrf_failed")
	}
	if p.User.MustChangePassword {
		return failure(403, "password_change_required")
	}
	if !p.User.Can(group, required) {
		return failure(403, "forbidden")
	}
	if fn == nil {
		return failure(422, "invalid_request")
	}
	if err = fn(tx, p.User); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE tw_session SET last_seen=? WHERE digest=?", s.now().Unix(), p.session); err != nil {
		return unavailable()
	}
	if tx.Commit() != nil {
		return unavailable()
	}
	return nil
}

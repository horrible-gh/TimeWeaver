package access

import (
	"context"
	"database/sql"

	"github.com/horrible-gh/TimeWeaver/internal/store"
)

// WithRead holds the authenticated user lock through scoped reads. Polling does
// not refresh idle expiry. Callers must filter every resource with User.Can or
// GroupFilter; the request's group_id is not an authorization decision.
func (s *Service) WithRead(ctx context.Context, token string, fn func(*sql.Tx, User) error) error {
	return s.withRequest(ctx, token, "", false, false, fn)
}

// WithGroup holds the user/session lock until the authorized mutation commits.
func (s *Service) WithGroup(ctx context.Context, token, csrf string, group int64, required string, fn func(*sql.Tx, User) error) error {
	return s.withRequest(ctx, token, csrf, true, false, func(tx *sql.Tx, u User) error {
		if !u.Can(group, required) {
			return failure(403, "forbidden")
		}
		if fn == nil {
			return failure(422, "invalid_request")
		}
		return fn(tx, u)
	})
}

// WithMutation authenticates a human mutation in the same transaction as its
// writes. The callback MUST resolve each resource's actual group from storage
// and call User.Can, or require the global administrator role. Never authorize
// an existing resource using a client-supplied group identifier.
func (s *Service) WithMutation(ctx context.Context, token, csrf string, fn func(*sql.Tx, User) error) error {
	return s.withRequest(ctx, token, csrf, true, false, fn)
}

func (s *Service) withRequest(ctx context.Context, token, csrf string, write, admin bool, fn func(*sql.Tx, User) error) error {
	if !validToken(token) {
		return failure(401, "unauthenticated")
	}
	// Writable SQLite stores begin IMMEDIATE transactions. Acquiring the write
	// reservation before reading the principal serializes authorization and
	// revocation across independent connections and Service instances.
	tx, err := s.db.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return unavailable()
	}
	defer tx.Rollback()
	if admin {
		// Serialize policy changes across service instances, before actor/target
		// locks. A->B and B->A admin edits cannot deadlock or remove both admins.
		var version int
		var state string
		if tx.QueryRowContext(ctx, "SELECT version,state FROM tw_access_schema WHERE id=1"+store.LockClause(s.db.Dialect, false)).Scan(&version, &state) != nil || version != schemaVersion || state != "ready" {
			return unavailable()
		}
	}
	p, err := s.principalTx(ctx, tx, token)
	if err != nil {
		return err
	}
	if write && !equalSecret(p.CSRF, csrf) {
		return failure(403, "csrf_failed")
	}
	if p.User.MustChangePassword {
		return failure(403, "password_change_required")
	}
	if admin && p.User.Role != "admin" {
		return failure(403, "forbidden")
	}
	if fn == nil {
		return failure(422, "invalid_request")
	}
	if err = fn(tx, p.User); err != nil {
		return err
	}
	if write {
		if _, err = tx.ExecContext(ctx, "UPDATE tw_session SET last_seen=? WHERE digest=?", s.now().Unix(), p.session); err != nil {
			return unavailable()
		}
	}
	if tx.Commit() != nil {
		return unavailable()
	}
	return nil
}

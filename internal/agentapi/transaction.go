package agentapi

import (
	"context"
	"errors"
	"time"

	"github.com/go-sql-driver/mysql"
)

var errIdentityDeadlock = errors.New("identity transaction was rolled back by the database")

// Only 1213 during a statement is retryable: InnoDB has rolled back the WHOLE
// transaction. A lock timeout, dropped connection or failed COMMIT does not give
// that guarantee and must never cause automatic token rotation to be replayed.
func identityStatementError(err error) error {
	var db *mysql.MySQLError
	if errors.As(err, &db) && db.Number == 1213 {
		return errIdentityDeadlock
	}
	return failure("unavailable")
}

func identityCommitError(err error) error {
	if err == nil {
		return nil
	}
	return failure("unavailable")
}

func retryIdentityTransaction(ctx context.Context, once func() (credentialIssue, error)) (credentialIssue, error) {
	const attempts = 3
	for attempt := 0; attempt < attempts; attempt++ {
		if ctx.Err() != nil {
			return credentialIssue{}, failure("unavailable")
		}
		result, err := once()
		// once has returned, so its deferred Rollback has run before any retry.
		if !errors.Is(err, errIdentityDeadlock) {
			return result, err
		}
		if attempt+1 == attempts {
			break
		}
		timer := time.NewTimer(time.Duration(5<<attempt) * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return credentialIssue{}, failure("unavailable")
		case <-timer.C:
		}
	}
	return credentialIssue{}, failure("unavailable")
}

func (s *Identity) enroll(ctx context.Context, token []byte, name string, version *string, refresh []byte) (credentialIssue, error) {
	return retryIdentityTransaction(ctx, func() (credentialIssue, error) {
		return s.enrollOnce(ctx, token, name, version, refresh)
	})
}

func (s *Identity) rotate(ctx context.Context, current, next []byte) (credentialIssue, error) {
	return retryIdentityTransaction(ctx, func() (credentialIssue, error) {
		return s.rotateOnce(ctx, current, next)
	})
}

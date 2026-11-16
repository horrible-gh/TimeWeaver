package agentapi

import (
	"context"
	"errors"
	"testing"

	"github.com/go-sql-driver/mysql"
)

func TestIdentityRetryIsLimitedToRolledBackStatementDeadlocks(t *testing.T) {
	deadlock := &mysql.MySQLError{Number: 1213, Message: "must-not-leak"}
	if !errors.Is(identityStatementError(deadlock), errIdentityDeadlock) {
		t.Fatal("deadlock must be marked separately")
	}
	for _, err := range []error{
		&mysql.MySQLError{Number: 1205, Message: "timeout"},
		&mysql.MySQLError{Number: 1062, Message: "duplicate"},
		errors.New("connection lost"),
		context.Canceled,
	} {
		requireCode(t, identityStatementError(err), 503, "unavailable")
	}
	for _, err := range []error{deadlock, errors.New("connection lost")} {
		calls := 0
		_, result := retryIdentityTransaction(context.Background(), func() (credentialIssue, error) {
			calls++
			return credentialIssue{}, identityCommitError(err)
		})
		if calls != 1 {
			t.Fatal("ambiguous commit replayed", calls)
		}
		requireCode(t, result, 503, "unavailable")
	}
	if identityCommitError(nil) != nil {
		t.Fatal("successful commit rejected")
	}
	calls := 0
	result, err := retryIdentityTransaction(context.Background(), func() (credentialIssue, error) {
		calls++
		if calls < 3 {
			return credentialIssue{}, identityStatementError(deadlock)
		}
		return credentialIssue{DeviceIdentity: DeviceIdentity{DeviceID: 7}}, nil
	})
	if err != nil || calls != 3 || result.DeviceID != 7 {
		t.Fatal("bounded recovery failed", calls, err)
	}
	calls = 0
	_, err = retryIdentityTransaction(context.Background(), func() (credentialIssue, error) {
		calls++
		return credentialIssue{}, identityStatementError(deadlock)
	})
	if calls != 3 {
		t.Fatal("unbounded retry", calls)
	}
	requireCode(t, err, 503, "unavailable")
	canceled, cancel := context.WithCancel(context.Background())
	calls = 0
	_, err = retryIdentityTransaction(canceled, func() (credentialIssue, error) {
		calls++
		cancel()
		return credentialIssue{}, identityStatementError(deadlock)
	})
	if calls != 1 {
		t.Fatal("cancellation ignored", calls)
	}
	requireCode(t, err, 503, "unavailable")
}

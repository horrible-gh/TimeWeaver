package access

import (
	"context"
	"testing"
	"time"
)

func TestSQLiteConfiguredSessionAbsoluteTimeout(t *testing.T) {
	db, _ := sqliteInitialized(t)
	ctx := context.Background()
	// A longer idle timeout isolates the absolute deadline in this test.
	s, err := NewWithTimeouts(ctx, db, 4*time.Hour, 2*time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := fixedSessionClock(s)
	ordinary, token, err := s.Login(ctx, "reader", testPassword, "127.0.0.2", "")
	if err != nil || ordinary.Session.ExpiresAt != now.Add(2*time.Hour).Unix() || ordinary.Session.Remembered {
		t.Fatal("configured ordinary deadline was ignored", ordinary.Session, err)
	}
	remembered, rememberedToken, err := s.LoginWithRemember(ctx, "global", testPassword, "127.0.0.3", "", true)
	if err != nil || remembered.Session.ExpiresAt != now.Add(30*24*time.Hour).Unix() || !remembered.Session.Remembered || remembered.Session.RefreshAfter != now.Add(12*time.Hour).Unix() {
		t.Fatal("ordinary timeout changed remembered policy", remembered.Session, err)
	}
	*now = now.Add(119 * time.Minute)
	if current, err := s.Session(ctx, token, true); err != nil || current.Session.ExpiresAt != ordinary.Session.ExpiresAt {
		t.Fatal("activity changed the absolute deadline", current.Session, err)
	}
	// Restarting with a different configuration does not extend a stored session.
	restarted, err := NewWithTimeouts(ctx, db, 4*time.Hour, 24*time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}
	restarted.now = s.now
	if current, err := restarted.Session(ctx, token, false); err != nil || current.Session.ExpiresAt != ordinary.Session.ExpiresAt {
		t.Fatal("restart rewrote the absolute deadline", current.Session, err)
	}
	*now = now.Add(time.Minute)
	_, err = restarted.Session(ctx, token, false)
	requireError(t, err, "unauthenticated")
	if current, err := restarted.Session(ctx, rememberedToken, false); err != nil || current.Session.ExpiresAt != remembered.Session.ExpiresAt {
		t.Fatal("configured deadline expired the remembered session", current.Session, err)
	}
	if _, err := db.DB.Exec("UPDATE tw_user_auth SET must_change_password=TRUE WHERE user_id='reader'"); err != nil {
		t.Fatal(err)
	}
	restricted, _, err := s.LoginWithRemember(ctx, "reader", testPassword, "127.0.0.4", "", true)
	if err != nil || restricted.Session.Remembered || restricted.Session.ExpiresAt != now.Add(2*time.Hour).Unix() {
		t.Fatal("restricted session ignored the configured ordinary lifetime", restricted.Session, err)
	}
}

func TestSQLiteSessionAbsoluteTimeoutValidationAndDefault(t *testing.T) {
	db, _ := sqliteInitialized(t)
	ctx := context.Background()
	for _, invalid := range []time.Duration{-time.Hour, time.Hour - time.Second, 8760*time.Hour + time.Second} {
		if _, err := NewWithTimeouts(ctx, db, 30*time.Minute, invalid, nil); err == nil {
			t.Fatalf("invalid absolute timeout accepted: %s", invalid)
		}
	}
	s, err := NewWithTimeouts(ctx, db, 30*time.Minute, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := fixedSessionClock(s)
	p, _, err := s.Login(ctx, "reader", testPassword, "127.0.0.2", "")
	if err != nil || p.Session.ExpiresAt != now.Add(12*time.Hour).Unix() {
		t.Fatal("zero-valued embedding configuration lost the default", p.Session, err)
	}
}

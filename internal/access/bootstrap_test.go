package access

import (
	"context"
	"testing"

	"github.com/horrible-gh/TimeWeaver/internal/store"
)

func TestSQLiteBootstrapOwnershipAndPreservation(t *testing.T) {
	testBootstrapOwnershipAndPreservation(t, sqliteInitialized)
}

func TestMySQLBootstrapOwnershipAndPreservation(t *testing.T) {
	testBootstrapOwnershipAndPreservation(t, initialized)
}

func testBootstrapOwnershipAndPreservation(t *testing.T, fixture initializedAccessFixture) {
	db, service := fixture(t)
	ctx := context.Background()
	if pending, err := HasBootstrapPassword(ctx, db); err != nil || !pending {
		t.Fatal("seed administrator was not identified", pending, err)
	}
	// A previously issued restricted session must lose access when the local
	// owner replaces the public credential.
	tx, err := store.BeginWrite(ctx, db.DB, db.Dialect, nil)
	if err != nil {
		t.Fatal(err)
	}
	u, err := service.loadUser(ctx, tx, "timeweaver")
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	_, oldSession, err := service.issueSession(ctx, tx, u, "", false)
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = BootstrapAdministrator(ctx, db, "short"); err == nil {
		t.Fatal("short bootstrap password accepted")
	}
	const ownedPassword = "locally-owned-administrator-123"
	if err = BootstrapAdministrator(ctx, db, ownedPassword); err != nil {
		t.Fatal(err)
	}
	if pending, err := HasBootstrapPassword(ctx, db); err != nil || pending {
		t.Fatal("bootstrap did not finish", pending, err)
	}
	if _, err = service.Session(ctx, oldSession, false); err == nil {
		t.Fatal("bootstrap retained a session issued with the seed credential")
	}
	p, _, err := service.Login(ctx, "timeweaver", ownedPassword, "127.0.0.1", "")
	if err != nil || p.User.MustChangePassword || !p.User.Can(0, "editor") {
		t.Fatal("owned administrator cannot use the dashboard", err)
	}
	var ownedHash, otherHash string
	if err = db.DB.QueryRow("SELECT password FROM users WHERE user_id='timeweaver'").Scan(&ownedHash); err != nil {
		t.Fatal(err)
	}
	if err = BootstrapAdministrator(ctx, db, "a-different-password-123"); err == nil {
		t.Fatal("bootstrap reset an established password")
	}
	var after string
	if err = db.DB.QueryRow("SELECT password FROM users WHERE user_id='timeweaver'").Scan(&after); err != nil || after != ownedHash {
		t.Fatal("established password was changed", err)
	}
	if err = db.DB.QueryRow("SELECT password FROM users WHERE user_id='reader'").Scan(&otherHash); err != nil || otherHash != testHash {
		t.Fatal("bootstrap changed another user's credential", err)
	}
	var audits int
	if err = db.DB.QueryRow("SELECT COUNT(*) FROM tw_audit WHERE action='administrator_bootstrap'").Scan(&audits); err != nil || audits != 1 {
		t.Fatal("bootstrap audit missing or duplicated", audits, err)
	}
	if _, err = db.DB.Exec("UPDATE users SET password=? WHERE user_id='reader'", legacySeedHash); err != nil {
		t.Fatal(err)
	}
	if pending, err := HasBootstrapPassword(ctx, db); err != nil || !pending {
		t.Fatal("another active seed credential escaped the startup gate", pending, err)
	}
	if _, err = db.DB.Exec("UPDATE tw_user_auth SET status='disabled' WHERE user_id='reader'"); err != nil {
		t.Fatal(err)
	}
	if pending, err := HasBootstrapPassword(ctx, db); err != nil || pending {
		t.Fatal("disabled account blocked established owners", pending, err)
	}
}

func TestSQLiteBootstrapRollsBackWithAuditFailure(t *testing.T) {
	db, _ := sqliteInitialized(t)
	if _, err := db.DB.Exec("CREATE TRIGGER reject_bootstrap BEFORE INSERT ON tw_audit BEGIN SELECT RAISE(ABORT,'fixture failure'); END"); err != nil {
		t.Fatal(err)
	}
	if err := BootstrapAdministrator(context.Background(), db, "locally-owned-password-123"); err == nil {
		t.Fatal("bootstrap ignored an audit failure")
	}
	var hash string
	var version int
	var mustChange bool
	if err := db.DB.QueryRow("SELECT u.password,a.version,a.must_change_password FROM users u JOIN tw_user_auth a ON a.user_id=u.user_id WHERE u.user_id='timeweaver'").Scan(&hash, &version, &mustChange); err != nil {
		t.Fatal(err)
	}
	if hash != legacySeedHash || version != 1 || !mustChange {
		t.Fatal("failed bootstrap partially changed the administrator")
	}
}

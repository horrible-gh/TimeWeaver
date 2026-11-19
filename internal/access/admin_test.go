package access

import (
	"bytes"
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/horrible-gh/TimeWeaver/internal/store"
)

func adminFixture(t *testing.T) (*Service, Principal, string) {
	t.Helper()
	db, s := initialized(t)
	if _, err := db.DB.Exec("UPDATE users SET password=? WHERE user_id='timeweaver'", testHash); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("UPDATE tw_user_auth SET must_change_password=FALSE WHERE user_id='timeweaver'"); err != nil {
		t.Fatal(err)
	}
	p, token, err := s.Login(context.Background(), "timeweaver", testPassword, "127.0.0.1", "")
	if err != nil {
		t.Fatal(err)
	}
	return s, p, token
}
func TestMySQLAdministrationAndRevocation(t *testing.T) {
	testAdministrationAndRevocation(t, adminFixture)
}

func testAdministrationAndRevocation(t *testing.T, fixture adminAccessFixture) {
	s, p, token := fixture(t)
	ctx := context.Background()
	reader, readerToken, err := s.Login(ctx, "reader", testPassword, "127.0.0.2", "")
	if err != nil {
		t.Fatal(err)
	}
	in := UserInput{ID: "new-user", Name: "New User", Role: "user", Status: "active", Password: "created-user-password", Groups: []Membership{{7, "viewer"}}}
	requireError(t, s.CreateUser(ctx, readerToken, reader.CSRF, in), "forbidden")
	requireError(t, s.CreateUser(ctx, token, "wrong", in), "csrf_failed")
	if err = s.CreateUser(ctx, token, p.CSRF, in); err != nil {
		t.Fatal(err)
	}
	requireError(t, s.CreateUser(ctx, token, p.CSRF, in), "already_exists")
	newp, newtoken, err := s.Login(ctx, in.ID, in.Password, "127.0.0.3", "")
	if err != nil || !newp.User.MustChangePassword {
		t.Fatal("new-user password gate", err)
	}
	requireError(t, s.WithRead(ctx, newtoken, func(*sql.Tx, User) error { return nil }), "password_change_required")
	if err = s.ChangePassword(ctx, newtoken, newp.CSRF, in.Password, "changed-user-password"); err != nil {
		t.Fatal(err)
	}
	newp, newtoken, err = s.Login(ctx, in.ID, "changed-user-password", "127.0.0.3", "")
	if err != nil {
		t.Fatal(err)
	}
	if !newp.User.Can(7, "viewer") || newp.User.Can(7, "operator") || newp.User.Can(8, "viewer") {
		t.Fatal("membership enforcement")
	}
	in.Password = ""
	in.Version = newp.User.version
	in.Groups = []Membership{{8, "operator"}}
	if _, err = s.UpdateUser(ctx, token, p.CSRF, in); err != nil {
		t.Fatal(err)
	}
	_, err = s.Session(ctx, newtoken, false)
	requireError(t, err, "unauthenticated")
	_, err = s.UpdateUser(ctx, token, p.CSRF, in)
	requireError(t, err, "stale_user")
	newp, newtoken, err = s.Login(ctx, in.ID, "changed-user-password", "127.0.0.3", "")
	if err != nil {
		t.Fatal(err)
	}
	if !newp.User.Can(8, "operator") || newp.User.Can(7, "viewer") || newp.User.GroupID != 8 {
		t.Fatal("updated scopes")
	}
	in.Version = newp.User.version
	in.Name = "Must roll back"
	in.Groups = []Membership{{999999, "editor"}}
	_, err = s.UpdateUser(ctx, token, p.CSRF, in)
	requireError(t, err, "group_not_found")
	var name string
	if s.db.DB.QueryRow("SELECT name FROM users WHERE user_id=?", in.ID).Scan(&name) != nil || name != "New User" {
		t.Fatal("partial invalid update")
	}
	if err = s.ResetPassword(ctx, token, p.CSRF, in.ID, "reset-user-password", in.Version); err != nil {
		t.Fatal(err)
	}
	_, err = s.Session(ctx, newtoken, false)
	requireError(t, err, "unauthenticated")
	np, _, err := s.Login(ctx, in.ID, "reset-user-password", "127.0.0.3", "")
	if err != nil || !np.User.MustChangePassword {
		t.Fatal("reset restriction", err)
	}
	users, err := s.ListUsers(ctx, token)
	if err != nil || len(users) != 4 {
		t.Fatal(len(users), err)
	}
	_, err = s.ListUsers(ctx, readerToken)
	requireError(t, err, "forbidden")
	rows, err := s.db.DB.Query("SELECT target,detail FROM tw_audit WHERE action IN ('user_create','user_update','password_reset')")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var target, detail string
		if rows.Scan(&target, &detail) != nil {
			t.Fatal("audit read")
		}
		if target != in.ID || strings.Contains(detail, "password") && !strings.Contains(detail, "must_change_password") || strings.Contains(detail, "reset-user-password") || strings.Contains(detail, "pbkdf2") {
			t.Fatal("unsafe audit")
		}
	}
}
func TestMySQLConcurrentLastAdminProtection(t *testing.T) {
	testConcurrentLastAdminProtection(t, adminFixture)
}

func testConcurrentLastAdminProtection(t *testing.T, fixture adminAccessFixture) {
	s, p, token := fixture(t)
	ctx := context.Background()
	if _, err := s.db.DB.Exec("INSERT INTO users(user_id,name,password,role,group_id) VALUES('second','Second',?,'admin',0)", testHash); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.DB.Exec("INSERT INTO tw_user_auth(user_id) VALUES('second')"); err != nil {
		t.Fatal(err)
	}
	other, err := New(ctx, s.db, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	q, second, err := other.Login(ctx, "second", testPassword, "127.0.0.2", "")
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, item := range []struct {
		s     *Service
		p     Principal
		token string
	}{{s, p, token}, {other, q, second}} {
		wg.Add(1)
		go func(v struct {
			s     *Service
			p     Principal
			token string
		}) {
			defer wg.Done()
			<-start
			_, err := v.s.UpdateUser(ctx, v.token, v.p.CSRF, UserInput{ID: v.p.User.ID, Name: v.p.User.Name, Role: "user", Status: "active", Version: v.p.User.version, Groups: []Membership{}})
			results <- err
		}(item)
	}
	close(start)
	wg.Wait()
	close(results)
	success, blocked := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else {
			requireError(t, err, "last_admin")
			blocked++
		}
	}
	if success != 1 || blocked != 1 {
		t.Fatal(success, blocked)
	}
	var remaining int
	if s.db.DB.QueryRow("SELECT COUNT(*) FROM users u JOIN tw_user_auth a ON a.user_id=u.user_id WHERE u.role='admin' AND a.status='active'").Scan(&remaining) != nil || remaining != 1 {
		t.Fatal("lost last admin", remaining)
	}
}
func TestMySQLGroupRemovalCannotGrantGlobalRights(t *testing.T) {
	testGroupRemovalCannotGrantGlobalRights(t, adminFixture)
}

func testGroupRemovalCannotGrantGlobalRights(t *testing.T, fixture adminAccessFixture) {
	s, p, token := fixture(t)
	ctx := context.Background()
	r, old, err := s.Login(ctx, "reader", testPassword, "127.0.0.2", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveGroup(ctx, old, r.CSRF, GroupInput{Name: "Forbidden", Status: "active"}, true); err == nil {
		t.Fatal("nonadmin group creation")
	}
	requireError(t, s.DeleteGroup(ctx, token, p.CSRF, 0), "group_reserved")
	if _, err = s.db.DB.Exec("INSERT INTO devices(group_id,device_name) VALUES(7,'moved-device')"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.DB.Exec("INSERT INTO agent_enrollment_token(enrollment_id,token_hash,group_id,expires_at) VALUES(?,?,7,?)", bytes.Repeat([]byte{1}, 16), bytes.Repeat([]byte{2}, 32), store.TimeValue(s.db.Dialect, time.Now().UTC().Add(24*time.Hour))); err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteGroup(ctx, token, p.CSRF, 7); err != nil {
		t.Fatal(err)
	}
	_, err = s.Session(ctx, old, false)
	requireError(t, err, "unauthenticated")
	r, newtoken, err := s.Login(ctx, "reader", testPassword, "127.0.0.2", "")
	if err != nil {
		t.Fatal(err)
	}
	if r.User.GroupID != 0 || len(r.User.Groups) != 0 || r.User.Can(0, "viewer") || r.User.Can(8, "viewer") {
		t.Fatal("group-zero privilege escalation")
	}
	groups, err := s.ListGroups(ctx, newtoken)
	if err != nil || len(groups) != 0 {
		t.Fatal(groups, err)
	}
	var group int
	var revoked bool
	if s.db.DB.QueryRow("SELECT group_id FROM devices WHERE device_name='moved-device'").Scan(&group) != nil || group != 0 {
		t.Fatal("legacy reassignment")
	}
	if s.db.DB.QueryRow("SELECT revoked_at IS NOT NULL FROM agent_enrollment_token").Scan(&revoked) != nil || !revoked {
		t.Fatal("unused enrollment not revoked")
	}
	// Explicit reinitialization never resurrects the removed grant.
	if _, err = Initialize(ctx, s.db); err != nil {
		t.Fatal(err)
	}
	r, err = s.Session(ctx, newtoken, false)
	if err != nil || len(r.User.Groups) != 0 {
		t.Fatal("grant resurrected", err)
	}
}
func TestMySQLAdministrationAuditFailureIsAtomic(t *testing.T) {
	testAdministrationAuditFailureIsAtomic(t, adminFixture)
}

func testAdministrationAuditFailureIsAtomic(t *testing.T, fixture adminAccessFixture) {
	s, p, token := fixture(t)
	ctx := context.Background()
	reader, old, err := s.Login(ctx, "reader", testPassword, "127.0.0.2", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.DB.Exec("DROP TABLE tw_audit"); err != nil {
		t.Fatal(err)
	}
	_, err = s.UpdateUser(ctx, token, p.CSRF, UserInput{ID: "reader", Name: "Changed", Role: "user", Status: "disabled", Version: reader.User.version, Groups: []Membership{{8, "editor"}}})
	requireError(t, err, "unavailable")
	current, err := s.Session(ctx, old, false)
	if err != nil || current.User.Name != "Reader" || !current.User.Can(7, "editor") || current.User.Can(8, "viewer") {
		t.Fatal("partial user update", err)
	}
	requireError(t, s.DeleteGroup(ctx, token, p.CSRF, 7), "unavailable")
	if _, err = s.Session(ctx, old, false); err != nil {
		t.Fatal("partial group deletion", err)
	}
	var n int
	if s.db.DB.QueryRow("SELECT COUNT(*) FROM `groups` WHERE group_id=7").Scan(&n) != nil || n != 1 {
		t.Fatal("group deletion did not roll back")
	}
}
func TestMySQLAccessV1UpgradePreservesSessionsAndRemovedGrants(t *testing.T) {
	testAccessV1UpgradePreservesSessionsAndRemovedGrants(t, adminFixture)
}

func testAccessV1UpgradePreservesSessionsAndRemovedGrants(t *testing.T, fixture adminAccessFixture) {
	s, _, token := fixture(t)
	ctx := context.Background()
	// Reconstruct the old v1 layout from the current initialized fixture.
	if _, err := s.db.DB.Exec("DROP TABLE tw_user_identity"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.DB.Exec("DELETE FROM tw_group_member WHERE user_id='reader'"); err != nil {
		t.Fatal(err)
	}
	dropColumns := []string{"ALTER TABLE tw_audit DROP COLUMN target,DROP COLUMN detail"}
	if s.db.Dialect == "sqlite" {
		dropColumns = []string{"ALTER TABLE tw_audit DROP COLUMN target", "ALTER TABLE tw_audit DROP COLUMN detail"}
	}
	for _, query := range dropColumns {
		if _, err := s.db.DB.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.DB.Exec("UPDATE tw_access_schema SET version=1,state='ready'"); err != nil {
		t.Fatal(err)
	}
	status, err := Status(ctx, s.db)
	if err != nil || status.Ready || status.Version != 1 {
		t.Fatal(status, err)
	}
	if status, err = Initialize(ctx, s.db); err != nil || !status.Ready || status.Version != 3 {
		t.Fatal("v1-to-v3 upgrade failed", status, err)
	}
	if _, err = s.Session(ctx, token, false); err != nil {
		t.Fatal("upgrade revoked session", err)
	}
	var n int
	if s.db.DB.QueryRow("SELECT COUNT(*) FROM tw_group_member WHERE user_id='reader'").Scan(&n) != nil || n != 0 {
		t.Fatal("upgrade regranted permissions")
	}
	if _, err = Initialize(ctx, s.db); err != nil {
		t.Fatal("upgrade not idempotent", err)
	}
}

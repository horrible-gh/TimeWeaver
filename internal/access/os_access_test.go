package access

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const linkedOSPassword = "isolated-os-provider-password"

type linkedOSAccount struct {
	identity OSIdentity
	password string
}

// This provider exists only in the test binary. No environment variable or
// production handler can select it or bypass the platform authenticator.
type linkedOSProvider struct {
	available atomic.Bool
	calls     atomic.Int64
	mu        sync.Mutex
	accounts  map[string]linkedOSAccount
	entered   chan struct{}
	release   chan struct{}
}

func newLinkedOSProvider() *linkedOSProvider {
	p := &linkedOSProvider{accounts: map[string]linkedOSAccount{
		"host-one":    {OSIdentity{Subject: "linux:uid:42421", Name: "host-one"}, linkedOSPassword},
		"host-alias":  {OSIdentity{Subject: "linux:uid:42421", Name: "host-one"}, linkedOSPassword},
		"host-two":    {OSIdentity{Subject: "linux:uid:42422", Name: "host-two"}, linkedOSPassword},
		"reader":      {OSIdentity{Subject: "linux:uid:42423", Name: "reader"}, testPassword},
		"windows-one": {OSIdentity{Subject: "windows:sid:S-1-5-21-123-456-789-1001", Name: `HOST\windows-one`}, linkedOSPassword},
	}}
	p.available.Store(true)
	return p
}

func (p *linkedOSProvider) Available() bool { return p.available.Load() }
func (p *linkedOSProvider) Resolve(ctx context.Context, name string) (OSIdentity, error) {
	if !p.Available() {
		return OSIdentity{}, ErrOSUnavailable
	}
	if err := ctx.Err(); err != nil {
		return OSIdentity{}, err
	}
	p.mu.Lock()
	account, ok := p.accounts[name]
	p.mu.Unlock()
	if !ok {
		return OSIdentity{}, ErrOSCredentials
	}
	return account.identity, nil
}
func (p *linkedOSProvider) Authenticate(ctx context.Context, name, password string) (OSIdentity, error) {
	p.calls.Add(1)
	if !p.Available() {
		return OSIdentity{}, ErrOSUnavailable
	}
	p.mu.Lock()
	account, ok := p.accounts[name]
	entered, release := p.entered, p.release
	p.mu.Unlock()
	if entered != nil {
		select {
		case entered <- struct{}{}:
		default:
		}
		select {
		case <-release:
		case <-ctx.Done():
			return OSIdentity{}, ctx.Err()
		}
	}
	if !ok || password != account.password {
		return OSIdentity{}, ErrOSCredentials
	}
	return account.identity, nil
}

func linkedOSFixture(t *testing.T) (*Service, Principal, string, *linkedOSProvider) {
	t.Helper()
	local, admin, token := adminFixture(t)
	provider := newLinkedOSProvider()
	s, err := NewWithOS(context.Background(), local.db, 30*time.Minute, provider)
	if err != nil {
		t.Fatal(err)
	}
	return s, admin, token, provider
}

func linkedOSCreate(t *testing.T, s *Service, admin Principal, token, id, osName, role string, groups []Membership) AdminUser {
	t.Helper()
	in := UserInput{ID: id, Name: "Linked " + osName, Role: role, Status: "active", AuthMethod: "os", OSName: osName, Groups: groups}
	if err := s.CreateUser(context.Background(), token, admin.CSRF, in); err != nil {
		t.Fatal(err)
	}
	users, err := s.ListUsers(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	for _, user := range users {
		if id != "" && user.ID == id || id == "" && user.Name == in.Name {
			return user
		}
	}
	t.Fatal("created OS account missing from management")
	return AdminUser{}
}

func linkedOSInput(user AdminUser) UserInput {
	return UserInput{ID: user.ID, Name: user.Name, Email: user.Email, Role: user.Role, Status: user.Status, Version: user.Version, Groups: user.Groups, AuthMethod: user.AuthMethod, OSName: user.OSName}
}

func TestMySQLOSIdentityIsolationAndExplicitGrants(t *testing.T) {
	s, admin, adminToken, provider := linkedOSFixture(t)
	ctx := context.Background()
	var before, after int
	if err := s.db.DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&before); err != nil {
		t.Fatal(err)
	}
	// The local account really accepts these credentials; OS authentication of
	// an unlinked identity must not fall back to it or silently create a user.
	if _, _, err := s.Login(ctx, "reader", testPassword, "127.0.0.2", ""); err != nil {
		t.Fatal(err)
	}
	_, _, err := s.LoginOS(ctx, "reader", testPassword, "127.0.0.2", "")
	requireError(t, err, "invalid_credentials")
	if err = s.db.DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&after); err != nil || after != before {
		t.Fatal("unlinked OS login provisioned a user", before, after, err)
	}
	linked := linkedOSCreate(t, s, admin, adminToken, "os-linked", "host-one", "user", []Membership{{7, "viewer"}})
	p, token, err := s.LoginOS(ctx, "host-alias", linkedOSPassword, "127.0.0.3", "")
	if err != nil || p.User.ID != linked.ID || p.User.AuthMethod != "os" || p.User.MustChangePassword || p.User.Role != "user" || !p.User.Can(7, "viewer") || p.User.Can(7, "operator") || p.User.Can(8, "viewer") || p.User.Can(0, "viewer") {
		t.Fatal("linked OS login changed application identity or grants", p.User, err)
	}
	var password, method, fingerprint string
	if err = s.db.DB.QueryRow("SELECT password FROM users WHERE user_id=?", linked.ID).Scan(&password); err != nil || password != "!os" {
		t.Fatal("OS password was stored or local login enabled", password, err)
	}
	if err = s.db.DB.QueryRow("SELECT auth_method,password_fingerprint FROM tw_session WHERE digest=?", digest(token)).Scan(&method, &fingerprint); err != nil || method != "os" || fingerprint == digest("!os") {
		t.Fatal("session was not bound to the OS identity", method, fingerprint, err)
	}
	providerCalls := provider.calls.Load()
	_, _, err = s.Login(ctx, linked.ID, linkedOSPassword, "127.0.0.3", "")
	requireError(t, err, "invalid_credentials")
	if provider.calls.Load() != providerCalls {
		t.Fatal("local login fell back to OS authentication")
	}
	unscoped := linkedOSCreate(t, s, admin, adminToken, "", "host-two", "user", nil)
	if unscoped.ID == "" || unscoped.AuthMethod != "os" || len(unscoped.Groups) != 0 || unscoped.GroupID != 0 {
		t.Fatal("empty OS ID or grants were not preserved", unscoped)
	}
	p, _, err = s.LoginOS(ctx, "host-two", linkedOSPassword, "127.0.0.4", "")
	if err != nil || p.User.Can(0, "viewer") || p.User.Can(7, "viewer") || p.User.Role != "user" {
		t.Fatal("default group metadata became a global grant", p.User, err)
	}
	windows := linkedOSCreate(t, s, admin, adminToken, "os-sid", "windows-one", "user", []Membership{{8, "operator"}})
	p, _, err = s.LoginOS(ctx, "windows-one", linkedOSPassword, "127.0.0.5", "")
	if err != nil || p.User.ID != windows.ID || !p.User.Can(8, "operator") || p.User.Can(7, "viewer") {
		t.Fatal("SID mapping lost application scope", p.User, err)
	}
	// Matching display/login names do not merge the two identity namespaces.
	sameName := linkedOSCreate(t, s, admin, adminToken, "os-reader", "reader", "user", []Membership{{8, "viewer"}})
	p, _, err = s.LoginOS(ctx, "reader", testPassword, "127.0.0.6", "")
	if err != nil || p.User.ID != sameName.ID || !p.User.Can(8, "viewer") || p.User.Can(7, "viewer") {
		t.Fatal("OS name collision merged local identity or grants", p.User, err)
	}
	p, _, err = s.Login(ctx, "reader", testPassword, "127.0.0.6", "")
	if err != nil || p.User.ID != "reader" || p.User.AuthMethod != "local" || !p.User.Can(7, "viewer") || p.User.Can(8, "viewer") {
		t.Fatal("OS link replaced the same-named local account", p.User, err)
	}
	users, err := s.ListUsers(ctx, adminToken)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(users)
	if strings.Contains(string(encoded), linkedOSPassword) || strings.Contains(string(encoded), "!os") || strings.Contains(string(encoded), testHash) {
		t.Fatal("administrative response leaked credential material")
	}
}

func TestMySQLOSManagementBindingAndPasswordBoundaries(t *testing.T) {
	s, admin, adminToken, provider := linkedOSFixture(t)
	ctx := context.Background()
	user := linkedOSCreate(t, s, admin, adminToken, "os-linked", "host-one", "user", []Membership{{7, "viewer"}})
	duplicate := UserInput{ID: "os-duplicate", Name: "Duplicate", Role: "user", Status: "active", AuthMethod: "os", OSName: "host-alias"}
	requireError(t, s.CreateUser(ctx, adminToken, admin.CSRF, duplicate), "os_identity_in_use")
	var count int
	if err := s.db.DB.QueryRow("SELECT COUNT(*) FROM users WHERE user_id='os-duplicate'").Scan(&count); err != nil || count != 0 {
		t.Fatal("duplicate identity left a partial user", count, err)
	}
	duplicate.OSName = "unknown-host"
	requireError(t, s.CreateUser(ctx, adminToken, admin.CSRF, duplicate), "os_account_not_found")
	p, token, err := s.LoginOS(ctx, "host-one", linkedOSPassword, "127.0.0.2", "")
	if err != nil {
		t.Fatal(err)
	}
	requireError(t, s.ChangePassword(ctx, token, p.CSRF, linkedOSPassword, "would-enable-local-login"), "os_password_managed")
	requireError(t, s.ResetPassword(ctx, adminToken, admin.CSRF, user.ID, "would-enable-local-login", user.Version), "os_password_managed")
	in := linkedOSInput(user)
	in.AuthMethod = "local"
	_, err = s.UpdateUser(ctx, adminToken, admin.CSRF, in)
	requireError(t, err, "auth_method_immutable")
	_, err = s.UpdateUser(ctx, adminToken, admin.CSRF, UserInput{ID: "reader", Name: "Reader", Role: "user", Status: "active", Version: 1, AuthMethod: "os", OSName: "host-two", Groups: []Membership{{7, "viewer"}}})
	requireError(t, err, "auth_method_immutable")
	in = linkedOSInput(user)
	in.OSName = "host-two"
	if _, err = s.UpdateUser(ctx, adminToken, admin.CSRF, in); err != nil {
		t.Fatal(err)
	}
	_, err = s.Session(ctx, token, false)
	requireError(t, err, "unauthenticated")
	_, _, err = s.LoginOS(ctx, "host-one", linkedOSPassword, "127.0.0.2", "")
	requireError(t, err, "invalid_credentials")
	p, token, err = s.LoginOS(ctx, "host-two", linkedOSPassword, "127.0.0.2", "")
	if err != nil || p.User.ID != user.ID || !p.User.Can(7, "viewer") {
		t.Fatal("rebind changed app identity or privileges", p.User, err)
	}
	_, err = s.UpdateUser(ctx, adminToken, admin.CSRF, in)
	requireError(t, err, "stale_user")
	users, err := s.ListUsers(ctx, adminToken)
	if err != nil {
		t.Fatal(err)
	}
	for _, current := range users {
		if current.ID == user.ID {
			in = linkedOSInput(current)
		}
	}
	provider.available.Store(false)
	in.OSName = "" // Disabling the provider must not block status/role maintenance.
	in.Status = "disabled"
	if _, err = s.UpdateUser(ctx, adminToken, admin.CSRF, in); err != nil {
		t.Fatal("disabled provider prevented administrative disable", err)
	}
	var subject, password string
	if err = s.db.DB.QueryRow("SELECT i.subject,u.password FROM tw_user_identity i JOIN users u ON u.user_id=i.user_id WHERE u.user_id=?", user.ID).Scan(&subject, &password); err != nil || subject != "linux:uid:42422" || password != "!os" {
		t.Fatal("metadata edit lost identity or enabled a password", subject, password, err)
	}
	rows, err := s.db.DB.Query("SELECT COALESCE(detail,'') FROM tw_audit")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var detail string
		if err = rows.Scan(&detail); err != nil || strings.Contains(detail, linkedOSPassword) || strings.Contains(detail, "would-enable-local-login") {
			t.Fatal("OS credential leaked into audit", err)
		}
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLOSMissingAdminTargetsPreserveSession(t *testing.T) {
	s, admin, token, _ := linkedOSFixture(t)
	ctx := context.Background()
	for _, operation := range []string{"update", "reset-password"} {
		t.Run(operation, func(t *testing.T) {
			var err error
			if operation == "update" {
				_, err = s.UpdateUser(ctx, token, admin.CSRF, UserInput{ID: "missing-target", Name: "Missing", Role: "user", Status: "active", Version: 1, AuthMethod: "os", OSName: "host-one"})
			} else {
				err = s.ResetPassword(ctx, token, admin.CSRF, "missing-target", "unused-new-password", 1)
			}
			var failure *Error
			if !errors.As(err, &failure) || failure.Status != http.StatusNotFound || failure.Code != "user_not_found" {
				t.Fatalf("missing administrative target must return 404 user_not_found, got %v", err)
			}
			current, err := s.Session(ctx, token, false)
			if err != nil || current.User.ID != admin.User.ID || current.User.version != admin.User.version || current.CSRF != admin.CSRF {
				t.Fatal("missing target invalidated the administrator session", err)
			}
		})
	}
}

func TestMySQLOSReleasedIdentityGetsDistinctGeneratedUser(t *testing.T) {
	s, admin, token, _ := linkedOSFixture(t)
	ctx := context.Background()
	first := linkedOSCreate(t, s, admin, token, "", "host-one", "user", []Membership{{7, "viewer"}})
	in := linkedOSInput(first)
	in.OSName = "host-two"
	if _, err := s.UpdateUser(ctx, token, admin.CSRF, in); err != nil {
		t.Fatal(err)
	}
	// The original application user retains its ID after rebinding. The now-free
	// identity must be linkable to another generated user without colliding with it.
	if err := s.CreateUser(ctx, token, admin.CSRF, UserInput{Name: "Replacement link", Role: "user", Status: "active", AuthMethod: "os", OSName: "host-one", Groups: []Membership{{8, "operator"}}}); err != nil {
		t.Fatal("released identity could not receive a new generated user ID", err)
	}
	firstLogin, _, err := s.LoginOS(ctx, "host-two", linkedOSPassword, "127.0.0.2", "")
	if err != nil || firstLogin.User.ID != first.ID || !firstLogin.User.Can(7, "viewer") || firstLogin.User.Can(8, "viewer") {
		t.Fatal("relinking changed the existing application user", firstLogin.User, err)
	}
	secondLogin, _, err := s.LoginOS(ctx, "host-one", linkedOSPassword, "127.0.0.3", "")
	if err != nil || secondLogin.User.ID == "" || secondLogin.User.ID == first.ID || !secondLogin.User.Can(8, "operator") || secondLogin.User.Can(7, "viewer") {
		t.Fatal("released identity reused an existing application identity or grants", secondLogin.User, err)
	}
}

func TestMySQLOSSessionChecksIdentityProviderAndExpiry(t *testing.T) {
	for _, change := range []string{"unlink", "rebind-without-version", "provider-disabled", "local-only-restart", "absolute-expiry", "idle-expiry"} {
		t.Run(change, func(t *testing.T) {
			s, admin, adminToken, provider := linkedOSFixture(t)
			ctx := context.Background()
			user := linkedOSCreate(t, s, admin, adminToken, "os-linked", "host-one", "user", []Membership{{7, "viewer"}})
			_, token, err := s.LoginOS(ctx, "host-one", linkedOSPassword, "127.0.0.2", "")
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "unlink":
				_, err = s.db.DB.Exec("DELETE FROM tw_user_identity WHERE user_id=?", user.ID)
			case "rebind-without-version":
				_, err = s.db.DB.Exec("UPDATE tw_user_identity SET subject='linux:uid:42422' WHERE user_id=?", user.ID)
			case "provider-disabled":
				provider.available.Store(false)
			case "local-only-restart":
				s, err = New(ctx, s.db, 30*time.Minute)
			case "absolute-expiry":
				_, err = s.db.DB.Exec("UPDATE tw_session SET expires_at=? WHERE digest=?", time.Now().Unix()-1, digest(token))
			case "idle-expiry":
				_, err = s.db.DB.Exec("UPDATE tw_session SET last_seen=? WHERE digest=?", time.Now().Unix()-1801, digest(token))
			}
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.Session(ctx, token, false)
			requireError(t, err, "unauthenticated")
			if _, err = s.Session(ctx, adminToken, false); err != nil {
				t.Fatal("OS state invalidated an unrelated local session", err)
			}
		})
	}
}

func TestMySQLOSLateAuthenticationCannotRestoreChangedBinding(t *testing.T) {
	for _, change := range []string{"disable", "rebind"} {
		t.Run(change, func(t *testing.T) {
			s, admin, adminToken, provider := linkedOSFixture(t)
			user := linkedOSCreate(t, s, admin, adminToken, "os-linked", "host-one", "user", []Membership{{7, "viewer"}})
			provider.entered, provider.release = make(chan struct{}, 1), make(chan struct{})
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			type result struct {
				token string
				err   error
			}
			done := make(chan result, 1)
			go func() {
				_, token, err := s.LoginOS(ctx, "host-one", linkedOSPassword, "127.0.0.2", "")
				done <- result{token, err}
			}()
			select {
			case <-provider.entered:
			case <-ctx.Done():
				t.Fatal("external authentication did not start")
			}
			in := linkedOSInput(user)
			if change == "disable" {
				in.Status = "disabled"
			} else {
				in.OSName = "host-two"
			}
			mutationCtx, finish := context.WithTimeout(ctx, 3*time.Second)
			_, err := s.UpdateUser(mutationCtx, adminToken, admin.CSRF, in)
			finish()
			close(provider.release)
			if err != nil {
				t.Fatal("external authentication held an administrative DB lock", err)
			}
			select {
			case result := <-done:
				requireError(t, result.err, "invalid_credentials")
				if result.token != "" {
					t.Fatal("late authentication issued a session")
				}
			case <-ctx.Done():
				t.Fatal("late authentication did not finish")
			}
		})
	}
}

func TestMySQLOSAdminCannotReplaceLocalRecoveryAdmin(t *testing.T) {
	s, admin, token, _ := linkedOSFixture(t)
	ctx := context.Background()
	linkedOSCreate(t, s, admin, token, "os-admin", "host-one", "admin", nil)
	in := UserInput{ID: admin.User.ID, Name: admin.User.Name, Role: "user", Status: "active", Version: admin.User.version, AuthMethod: "local", Groups: []Membership{}}
	_, err := s.UpdateUser(ctx, token, admin.CSRF, in)
	requireError(t, err, "last_local_admin")
	in.Role, in.Status = "admin", "disabled"
	_, err = s.UpdateUser(ctx, token, admin.CSRF, in)
	requireError(t, err, "last_local_admin")
	osAdmin, osToken, err := s.LoginOS(ctx, "host-one", linkedOSPassword, "127.0.0.2", "")
	if err != nil {
		t.Fatal(err)
	}
	requireError(t, s.ResetPassword(ctx, osToken, osAdmin.CSRF, admin.User.ID, "would-require-password-reset", admin.User.version), "last_local_admin")
}

func TestMySQLOSCanonicalAccountRateLimitAndAuditRollback(t *testing.T) {
	t.Run("aliases-share-rate-limit", func(t *testing.T) {
		s, admin, token, provider := linkedOSFixture(t)
		ctx := context.Background()
		linkedOSCreate(t, s, admin, token, "os-linked", "host-one", "user", nil)
		for i := 0; i < 5; i++ {
			name := "host-one"
			if i%2 == 1 {
				name = "host-alias"
			}
			_, _, err := s.LoginOS(ctx, name, "wrong-os-password", "127.0.0.2", "")
			requireError(t, err, "invalid_credentials")
		}
		restarted, err := NewWithOS(ctx, s.db, 30*time.Minute, provider)
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = restarted.LoginOS(ctx, "host-alias", linkedOSPassword, "127.0.0.3", "")
		requireError(t, err, "login_limited")
	})
	t.Run("audit-rolls-back-session-and-rebind", func(t *testing.T) {
		s, admin, token, _ := linkedOSFixture(t)
		ctx := context.Background()
		user := linkedOSCreate(t, s, admin, token, "os-linked", "host-one", "user", []Membership{{7, "viewer"}})
		if _, err := s.db.DB.Exec("RENAME TABLE tw_audit TO os_audit_offline"); err != nil {
			t.Fatal(err)
		}
		defer s.db.DB.Exec("RENAME TABLE os_audit_offline TO tw_audit")
		_, issued, err := s.LoginOS(ctx, "host-one", linkedOSPassword, "127.0.0.2", "")
		requireError(t, err, "unavailable")
		var count int
		if err = s.db.DB.QueryRow("SELECT COUNT(*) FROM tw_session WHERE auth_method='os'").Scan(&count); err != nil || count != 0 || issued != "" {
			t.Fatal("failed audit committed an OS session", count, err)
		}
		var lastLogin sql.NullInt64
		if err = s.db.DB.QueryRow("SELECT last_login_at FROM tw_user_auth WHERE user_id=?", user.ID).Scan(&lastLogin); err != nil || lastLogin.Valid {
			t.Fatal("failed login audit changed last_login_at", lastLogin, err)
		}
		in := linkedOSInput(user)
		in.OSName = "host-two"
		_, err = s.UpdateUser(ctx, token, admin.CSRF, in)
		requireError(t, err, "unavailable")
		var subject string
		var version int64
		if err = s.db.DB.QueryRow("SELECT i.subject,a.version FROM tw_user_identity i JOIN tw_user_auth a ON a.user_id=i.user_id WHERE i.user_id=?", user.ID).Scan(&subject, &version); err != nil || subject != "linux:uid:42421" || version != user.Version {
			t.Fatal("failed audit changed the linked identity", subject, version, err)
		}
	})
}

func TestMySQLOSAccessV2UpgradePreservesLocalSessionsAndRemovedGrants(t *testing.T) {
	local, _, _ := adminFixture(t)
	ctx := context.Background()
	_, token, err := local.Login(ctx, "reader", testPassword, "127.0.0.2", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"DELETE FROM tw_group_member WHERE user_id='reader'", "DROP TABLE tw_user_identity", "UPDATE tw_access_schema SET version=2,state='ready' WHERE id=1"} {
		if _, err = local.db.DB.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	before, err := local.db.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	status, err := Initialize(ctx, local.db)
	if err != nil || !status.Ready || status.Version != 3 {
		t.Fatal("v2 access upgrade failed", status, err)
	}
	upgraded, err := NewWithOS(ctx, local.db, 30*time.Minute, newLinkedOSProvider())
	if err != nil {
		t.Fatal(err)
	}
	p, err := upgraded.Session(ctx, token, false)
	if err != nil || p.User.AuthMethod != "local" || p.User.password != testHash || p.User.Can(7, "viewer") || len(p.User.Groups) != 0 {
		t.Fatal("upgrade changed local identity, session or revoked grants", p.User, err)
	}
	if _, err = Initialize(ctx, local.db); err != nil {
		t.Fatal("identity schema initialization was not idempotent", err)
	}
	var identities int
	if err = local.db.DB.QueryRow("SELECT COUNT(*) FROM tw_user_identity").Scan(&identities); err != nil || identities != 0 {
		t.Fatal("upgrade implicitly linked local users", identities, err)
	}
	after, err := local.db.Status(ctx)
	if err != nil || before.Pending != after.Pending || len(after.Unknown) != len(before.Unknown) || after.Blocked {
		t.Fatal("access upgrade altered frozen legacy migration history", after, err)
	}
	if _, err = local.db.DB.Exec("UPDATE tw_access_schema SET state='running' WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	if _, err = Initialize(ctx, local.db); err == nil {
		t.Fatal("unfinished OS identity DDL was silently replayed")
	}
	if _, err = NewWithOS(ctx, local.db, 30*time.Minute, newLinkedOSProvider()); err == nil {
		t.Fatal("unfinished OS identity schema accepted sessions")
	}
}

func TestMySQLOSHTTPMethodsTransportAndNoFallback(t *testing.T) {
	s, admin, token, provider := linkedOSFixture(t)
	linkedOSCreate(t, s, admin, token, "os-linked", "host-one", "user", []Membership{{7, "viewer"}})
	h, err := NewHTTP(s, "/timeweaver_svr", HTTPOptions{Origin: "https://tw.example"})
	if err != nil {
		t.Fatal(err)
	}
	methods := func() []string {
		w := request(h, "GET", "/timeweaver_svr/auth/methods", "", "", "")
		var body struct {
			Methods []string `json:"methods"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &body) != nil {
			t.Fatal(w.Code, w.Body.String())
		}
		return body.Methods
	}
	if got := methods(); !reflect.DeepEqual(got, []string{"local", "os"}) {
		t.Fatal("enabled OS method missing", got)
	}
	loginBody := url.Values{"username": {"host-alias"}, "password": {linkedOSPassword}, "method": {"os"}}
	w := request(h, "POST", "/timeweaver_svr/login", loginBody.Encode(), "", "")
	if w.Code != 200 || len(w.Result().Cookies()) != 1 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookie := w.Result().Cookies()[0]
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/timeweaver_svr/" {
		t.Fatal("OS session cookie lost its transport protections")
	}
	if strings.Contains(w.Body.String(), linkedOSPassword) || strings.Contains(w.Body.String(), cookie.Value) || strings.Contains(w.Body.String(), "access_token") {
		t.Fatal("OS login disclosed credentials or a bearer token")
	}
	for _, method := range []string{"local", "unknown"} {
		loginBody.Set("method", method)
		w = request(h, "POST", "/timeweaver_svr/login", loginBody.Encode(), "", "")
		want := 401
		if method == "unknown" {
			want = 422
		}
		if w.Code != want {
			t.Fatal("authentication method fallback", method, w.Code)
		}
	}
	loginBody["method"] = []string{"os", "local"}
	if w = request(h, "POST", "/timeweaver_svr/login", loginBody.Encode(), "", ""); w.Code != 422 {
		t.Fatal("ambiguous authentication method accepted", w.Code)
	}
	provider.available.Store(false)
	if got := methods(); !reflect.DeepEqual(got, []string{"local"}) {
		t.Fatal("unavailable OS method remained advertised", got)
	}
	loginBody.Set("method", "os")
	if w = request(h, "POST", "/timeweaver_svr/login", loginBody.Encode(), "", ""); w.Code != 503 || !strings.Contains(w.Body.String(), "os_login_unavailable") {
		t.Fatal("unavailable provider became a local login", w.Code, w.Body.String())
	}
	provider.available.Store(true)
	local, err := NewHTTP(s, "/timeweaver_svr", HTTPOptions{Origin: "http://127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	before := provider.calls.Load()
	r := httptest.NewRequest("POST", "http://127.0.0.1/timeweaver_svr/login", strings.NewReader(loginBody.Encode()))
	r.RemoteAddr = "203.0.113.9:5000"
	r.Header.Set("Origin", "http://127.0.0.1")
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	local.ServeHTTP(w, r)
	if w.Code != 403 || provider.calls.Load() != before {
		t.Fatal("remote plaintext request reached OS credential verification", w.Code)
	}
}

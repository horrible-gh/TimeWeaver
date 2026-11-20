package access

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/horrible-gh/TimeWeaver/internal/store"
)

// Historical migration fixtures must actually have the old shape; leaving v4
// columns in a v1/v2/v3 journal would correctly be rejected as partial DDL.
func restoreV3Sessions(t *testing.T, db *store.Store) {
	t.Helper()
	for _, index := range []string{"tw_session_token", "tw_session_previous"} {
		query := "DROP INDEX " + index
		if db.Dialect == "mysql" {
			query = "ALTER TABLE tw_session DROP INDEX " + index
		}
		if _, err := db.DB.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	for _, column := range rotationColumns {
		if _, err := db.DB.Exec("ALTER TABLE tw_session DROP COLUMN " + column); err != nil {
			t.Fatal(err)
		}
	}
}

func fixedSessionClock(s *Service) *time.Time {
	now := time.Now().UTC().Truncate(time.Second)
	s.now = func() time.Time { return now }
	return &now
}

func TestMySQLRememberedSessionLifetimeAndRestriction(t *testing.T) {
	testRememberedSessionLifetimeAndRestriction(t, initialized)
}
func TestSQLiteRememberedSessionLifetimeAndRestriction(t *testing.T) {
	testRememberedSessionLifetimeAndRestriction(t, sqliteInitialized)
}

func testRememberedSessionLifetimeAndRestriction(t *testing.T, fixture initializedAccessFixture) {
	db, s := fixture(t)
	ctx := context.Background()
	now := fixedSessionClock(s)
	p, remembered, err := s.LoginWithRemember(ctx, "reader", testPassword, "127.0.0.2", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Session.Remembered || p.Session.ExpiresAt != now.Add(30*24*time.Hour).Unix() || p.Session.RefreshAfter != now.Add(12*time.Hour).Unix() || p.Session.RefreshAfterSeconds != 12*3600 {
		t.Fatal("remembered deadline or renewal metadata", p.Session)
	}
	normalPrincipal, ordinary, err := s.Login(ctx, "reader", testPassword, "127.0.0.2", "")
	if err != nil || normalPrincipal.Session.Remembered || normalPrincipal.Session.ExpiresAt != now.Add(12*time.Hour).Unix() || normalPrincipal.Session.RefreshAfter != 0 || normalPrincipal.Session.RefreshAfterSeconds != 0 {
		t.Fatal("ordinary policy changed", normalPrincipal.Session, err)
	}
	if _, token, err := s.Refresh(ctx, ordinary, normalPrincipal.CSRF); err != nil || token != "" {
		t.Fatal("ordinary refresh changed its credential", err)
	}
	*now = now.Add(31 * time.Minute)
	if _, err = s.Session(ctx, ordinary, false); err == nil {
		t.Fatal("ordinary idle timeout was removed")
	}
	if _, _, err = s.Refresh(ctx, ordinary, normalPrincipal.CSRF); err == nil {
		t.Fatal("ordinary refresh recovered an expired idle session")
	}
	*now = now.Add(48 * time.Hour)
	restarted, err := New(ctx, db, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	restarted.now = s.now
	if current, err := restarted.Session(ctx, remembered, false); err != nil || current.CSRF != p.CSRF || current.Session.ExpiresAt != p.Session.ExpiresAt {
		t.Fatal("remembered session lost across inactivity/restart", current.Session, err)
	}
	// A different login must not prune an idle remembered family.
	if _, _, err = s.Login(ctx, "global", testPassword, "127.0.0.3", ""); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Session(ctx, remembered, false); err != nil {
		t.Fatal("login cleanup removed a remembered family", err)
	}
	if _, err = db.DB.Exec("UPDATE tw_user_auth SET must_change_password=TRUE WHERE user_id='reader'"); err != nil {
		t.Fatal(err)
	}
	restricted, restrictedToken, err := s.LoginWithRemember(ctx, "reader", testPassword, "127.0.0.4", "", true)
	if err != nil || !restricted.User.MustChangePassword || restricted.Session.Remembered || restricted.Session.ExpiresAt != now.Add(12*time.Hour).Unix() {
		t.Fatal("restricted password session became persistent", restricted.Session, err)
	}
	requireError(t, s.WithRead(ctx, restrictedToken, func(*sql.Tx, User) error { return nil }), "password_change_required")
	*now = time.Unix(p.Session.ExpiresAt, 0)
	_, err = s.Session(ctx, remembered, false)
	requireError(t, err, "unauthenticated")
	_, _, err = s.Refresh(ctx, remembered, p.CSRF)
	requireError(t, err, "unauthenticated")
}

func TestMySQLSessionRotationConcurrencyRecoveryAndRestart(t *testing.T) {
	testSessionRotationConcurrencyRecoveryAndRestart(t, initialized)
}
func TestSQLiteSessionRotationConcurrencyRecoveryAndRestart(t *testing.T) {
	testSessionRotationConcurrencyRecoveryAndRestart(t, sqliteInitialized)
}

func testSessionRotationConcurrencyRecoveryAndRestart(t *testing.T, fixture initializedAccessFixture) {
	db, s := fixture(t)
	ctx := context.Background()
	now := fixedSessionClock(s)
	p, old, err := s.LoginWithRemember(ctx, "reader", testPassword, "127.0.0.2", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, token, err := s.Refresh(ctx, old, p.CSRF); err != nil || token != "" {
		t.Fatal("early refresh unexpectedly rotated", err)
	}
	*now = now.Add(sessionRotationInterval)
	otherDB := db
	if db.Dialect == "sqlite" {
		otherDB = openSQLiteAccessStore(t, sqliteAccessPath(t, db))
	}
	other, err := New(ctx, otherDB, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	other.now = s.now
	type result struct {
		principal Principal
		token     string
		err       error
	}
	start, results := make(chan struct{}), make(chan result, 8)
	for i := 0; i < cap(results); i++ {
		target := s
		if i%2 == 1 {
			target = other
		}
		go func() {
			<-start
			fresh, token, err := target.Refresh(ctx, old, p.CSRF)
			results <- result{fresh, token, err}
		}()
	}
	close(start)
	next := ""
	for i := 0; i < cap(results); i++ {
		r := <-results
		if r.err != nil || r.token == "" || r.token == old || r.principal.CSRF != p.CSRF || r.principal.Session.ExpiresAt != p.Session.ExpiresAt || r.principal.Session.RefreshAfterSeconds != 12*3600 {
			t.Fatal("concurrent refresh failed or changed the family", r.err, r.principal.Session)
		}
		if next == "" {
			next = r.token
		}
		if next != r.token {
			t.Fatal("sibling refreshes produced different successors")
		}
	}
	var currentHash, previousHash, seed string
	var until, rotated int64
	if err = db.DB.QueryRow("SELECT token_digest,previous_digest,rotation_seed,previous_until,rotated_at FROM tw_session WHERE digest=?", p.session).Scan(&currentHash, &previousHash, &seed, &until, &rotated); err != nil {
		t.Fatal(err)
	}
	if currentHash != digest(next) || previousHash != digest(old) || seed == old || seed == next || !validToken(seed) || until != now.Add(time.Minute).Unix() || rotated != now.Unix() {
		t.Fatal("rotation stored a credential or changed its bounded overlap")
	}
	var audits int
	if err = db.DB.QueryRow("SELECT COUNT(*) FROM tw_audit WHERE action='session_refresh'").Scan(&audits); err != nil || audits != 1 {
		t.Fatal("concurrent refresh committed more than one rotation", audits, err)
	}
	*now = now.Add(59 * time.Second)
	bootstrap, err := other.Session(ctx, old, true)
	if err != nil || bootstrap.CSRF != p.CSRF || bootstrap.Session.RefreshAfterSeconds != 0 || bootstrap.Session.RefreshAfter != now.Unix() {
		t.Fatal("lost response could not bootstrap immediate recovery", bootstrap.Session, err)
	}
	recovered, retry, err := other.Refresh(ctx, old, bootstrap.CSRF)
	if err != nil || retry != next || recovered.Session.ExpiresAt != p.Session.ExpiresAt {
		t.Fatal("lost first response changed successor or deadline", err)
	}
	*now = now.Add(time.Second)
	_, _, err = s.Refresh(ctx, old, p.CSRF)
	requireError(t, err, "unauthenticated")
	if _, err = s.Session(ctx, next, false); err != nil {
		t.Fatal("stale predecessor revoked the current family", err)
	}
	if _, token, err := s.Refresh(ctx, next, p.CSRF); err != nil || token != "" {
		t.Fatal("current cookie rotated during the interval", err)
	}
	*now = time.Unix(rotated, 0).Add(sessionRotationInterval)
	_, third, err := s.Refresh(ctx, next, p.CSRF)
	if err != nil || third == "" || third == next || third == old {
		t.Fatal("second rotation failed", err)
	}
	var nextSeed string
	if err = db.DB.QueryRow("SELECT rotation_seed FROM tw_session WHERE digest=?", p.session).Scan(&nextSeed); err != nil || nextSeed == seed {
		t.Fatal("rotation reused its derivation seed", err)
	}
	if _, err = s.Session(ctx, old, false); err == nil {
		t.Fatal("two-generation predecessor was accepted")
	}
	if err = s.Logout(ctx, next, p.CSRF); err != nil {
		t.Fatal("logout through valid predecessor failed", err)
	}
	for _, token := range []string{old, next, third} {
		_, err = other.Session(ctx, token, false)
		requireError(t, err, "unauthenticated")
	}
}

func TestMySQLSessionV3UpgradePreservesCredentialsAndPolicy(t *testing.T) {
	testSessionV3UpgradePreservesCredentialsAndPolicy(t, initialized)
}
func TestSQLiteSessionV3UpgradePreservesCredentialsAndPolicy(t *testing.T) {
	testSessionV3UpgradePreservesCredentialsAndPolicy(t, sqliteInitialized)
}

func testSessionV3UpgradePreservesCredentialsAndPolicy(t *testing.T, fixture initializedAccessFixture) {
	db, s := fixture(t)
	ctx := context.Background()
	p, token, err := s.Login(ctx, "reader", testPassword, "127.0.0.2", "")
	if err != nil {
		t.Fatal(err)
	}
	read := func() []any {
		var id, csrf, fingerprint, method string
		var version, created, last, expires int64
		if err := db.DB.QueryRow("SELECT user_id,user_version,csrf,password_fingerprint,created_at,last_seen,expires_at,auth_method FROM tw_session WHERE digest=?", digest(token)).Scan(&id, &version, &csrf, &fingerprint, &created, &last, &expires, &method); err != nil {
			t.Fatal(err)
		}
		return []any{id, version, csrf, fingerprint, created, last, expires, method}
	}
	before := read()
	if _, err = db.DB.Exec("DELETE FROM tw_group_member WHERE user_id='reader'"); err != nil {
		t.Fatal(err)
	}
	restoreV3Sessions(t, db)
	if _, err = db.DB.Exec("UPDATE tw_access_schema SET version=3,state='ready' WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	if status, err := Status(ctx, db); err != nil || status.Ready || status.Version != 3 || status.State != "ready" {
		t.Fatal("v3 incorrectly reported current", status, err)
	}
	if _, err = New(ctx, db, 30*time.Minute); err == nil {
		t.Fatal("v3 was automatically used without explicit initialization")
	}
	if status, err := Initialize(ctx, db); err != nil || !status.Ready || status.Version != schemaVersion {
		t.Fatal("v3 upgrade failed", status, err)
	}
	if after := read(); !reflect.DeepEqual(before, after) {
		t.Fatal("v3 upgrade changed a credential or existing deadline", before, after)
	}
	var current string
	var rotated int64
	var remembered bool
	if err = db.DB.QueryRow("SELECT token_digest,rotated_at,remembered FROM tw_session WHERE digest=?", digest(token)).Scan(&current, &rotated, &remembered); err != nil || current != digest(token) || rotated != before[4].(int64) || remembered {
		t.Fatal("v3 credential was not preserved as ordinary", err)
	}
	restarted, err := New(ctx, db, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	currentPrincipal, err := restarted.Session(ctx, token, false)
	if err != nil || currentPrincipal.CSRF != p.CSRF || currentPrincipal.Session.Remembered || len(currentPrincipal.User.Groups) != 0 {
		t.Fatal("v3 upgrade lost a session or reconstructed a revoked grant", currentPrincipal, err)
	}
	if _, err = Initialize(ctx, db); err != nil {
		t.Fatal("v4 initialization was not idempotent", err)
	}
}

func TestMySQLRememberedHTTPContract(t *testing.T) {
	testRememberedHTTPContract(t, initialized)
}
func TestSQLiteRememberedHTTPContract(t *testing.T) {
	testRememberedHTTPContract(t, sqliteInitialized)
}

func testRememberedHTTPContract(t *testing.T, fixture initializedAccessFixture) {
	_, s := fixture(t)
	now := fixedSessionClock(s)
	h, err := NewHTTP(s, "/tw", HTTPOptions{Origin: "https://tw.example"})
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{"username": {"reader"}, "password": {testPassword}}
	for _, remember := range []string{"", "1", "yes", "TRUE", "false&remember=true"} {
		w := request(h, "POST", "/tw/login", form.Encode()+"&remember="+remember, "", "")
		if w.Code != 422 || len(w.Result().Cookies()) != 0 {
			t.Fatal("malformed remember option accepted", remember, w.Code)
		}
	}
	for _, option := range []string{"", "&remember=false"} {
		w := request(h, "POST", "/tw/login", form.Encode()+option, "", "")
		cookies := w.Result().Cookies()
		if w.Code != 200 || len(cookies) != 1 || cookies[0].MaxAge != 0 || !cookies[0].Expires.IsZero() {
			t.Fatal("ordinary login gained a persistent cookie", w.Code, w.Body.String())
		}
	}
	w := request(h, "POST", "/tw/login", form.Encode()+"&remember=true", "", "")
	var p Principal
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &p) != nil || !p.Session.Remembered {
		t.Fatal("remembered login failed", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing remembered cookie")
	}
	c := cookies[0]
	if c.MaxAge != 30*24*3600 || c.Expires.Unix() != p.Session.ExpiresAt || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Path != "/tw/" || c.Domain != "" {
		t.Fatal("remembered cookie changed its security or fixed deadline", c)
	}
	if strings.Contains(w.Body.String(), c.Value) || strings.Contains(w.Body.String(), testPassword) || strings.Contains(w.Body.String(), testHash) || strings.Contains(w.Body.String(), "rotation_seed") {
		t.Fatal("login response exposed a session secret")
	}
	*now = now.Add(13 * time.Hour)
	w = request(h, "GET", "/tw/auth/me", "", c.Value, "")
	if w.Code != 200 || len(w.Result().Cookies()) != 0 {
		t.Fatal("GET me rotated or lost a remembered session", w.Code)
	}
	for _, csrf := range []string{"", "wrong"} {
		w = request(h, "POST", "/tw/auth/refresh", "", c.Value, csrf)
		if w.Code != 403 || len(w.Result().Cookies()) != 0 {
			t.Fatal("refresh bypassed CSRF or changed cookie on failure", w.Code)
		}
	}
	for _, change := range []struct {
		status int
		apply  func(*http.Request)
	}{
		{403, func(r *http.Request) { r.Header.Del("Origin") }},
		{403, func(r *http.Request) { r.Header.Set("Origin", "https://evil.invalid") }},
		{403, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }},
		{401, func(r *http.Request) { r.Header.Set("Authorization", "Bearer an-agent-token") }},
	} {
		r := httptest.NewRequest("POST", "https://tw.example/tw/auth/refresh", nil)
		r.Header.Set("Origin", "https://tw.example")
		r.Header.Set("X-CSRF-Token", p.CSRF)
		r.AddCookie(c)
		change.apply(r)
		w = httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != change.status || len(w.Result().Cookies()) != 0 {
			t.Fatal("refresh bypassed origin/bearer boundary", w.Code, change.status)
		}
	}
	if w = request(h, "GET", "/tw/auth/refresh", "", c.Value, p.CSRF); w.Code != 405 || w.Header().Get("Allow") != "POST" {
		t.Fatal("refresh method contract", w.Code)
	}
	w = request(h, "POST", "/tw/auth/refresh/", "", c.Value, p.CSRF)
	var renewed Principal
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &renewed) != nil {
		t.Fatal("refresh failed", w.Code, w.Body.String())
	}
	cookies = w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Value == c.Value || cookies[0].Expires.Unix() != p.Session.ExpiresAt || cookies[0].MaxAge != 30*24*3600-13*3600 || renewed.CSRF != p.CSRF || renewed.Session.RefreshAfterSeconds != 12*3600 {
		t.Fatal("refresh extended the fixed deadline or changed CSRF", renewed.Session)
	}
	current := cookies[0]
	w = request(h, "POST", "/tw/auth/refresh", "", current.Value, p.CSRF)
	if w.Code != 200 || len(w.Result().Cookies()) != 0 {
		t.Fatal("no-op refresh overwrote a newer cookie", w.Code)
	}
	w = request(h, "POST", "/tw/auth/refresh", "", c.Value, p.CSRF)
	cookies = w.Result().Cookies()
	if w.Code != 200 || len(cookies) != 1 || cookies[0].Value != current.Value {
		t.Fatal("lost-response retry did not repeat the committed successor", w.Code)
	}
	for _, token := range []string{"", "broken"} {
		w = request(h, "POST", "/tw/auth/refresh", "", token, p.CSRF)
		if w.Code != 401 || len(w.Result().Cookies()) != 0 {
			t.Fatal("missing/corrupt cookie recovered without a credential", w.Code)
		}
	}
	*now = time.Unix(p.Session.ExpiresAt, 0)
	w = request(h, "POST", "/tw/auth/refresh", "", current.Value, p.CSRF)
	if w.Code != 401 || len(w.Result().Cookies()) != 0 {
		t.Fatal("expired refresh extended or cleared the cookie", w.Code)
	}
}

func TestMySQLRememberedRevocationAndAtomicRotation(t *testing.T) {
	testRememberedRevocationAndAtomicRotation(t, initialized)
}
func TestSQLiteRememberedRevocationAndAtomicRotation(t *testing.T) {
	testRememberedRevocationAndAtomicRotation(t, sqliteInitialized)
}

func testRememberedRevocationAndAtomicRotation(t *testing.T, fixture initializedAccessFixture) {
	t.Run("password-change", func(t *testing.T) {
		_, s := fixture(t)
		ctx := context.Background()
		now := fixedSessionClock(s)
		p, old, err := s.LoginWithRemember(ctx, "reader", testPassword, "127.0.0.2", "", true)
		if err != nil {
			t.Fatal(err)
		}
		*now = now.Add(sessionRotationInterval)
		_, next, err := s.Refresh(ctx, old, p.CSRF)
		if err != nil {
			t.Fatal(err)
		}
		called := false
		if err = s.WithGroup(ctx, old, p.CSRF, 7, "editor", func(tx *sql.Tx, u User) error {
			called = true
			_, err := tx.ExecContext(ctx, "UPDATE users SET name='Still authorized' WHERE user_id=?", u.ID)
			return err
		}); err != nil || !called {
			t.Fatal("in-flight mutation lost the stable CSRF/family during grace", err)
		}
		if err = s.ChangePassword(ctx, next, p.CSRF, testPassword, "new-remembered-session-password"); err != nil {
			t.Fatal(err)
		}
		for _, token := range []string{old, next} {
			_, _, err = s.Refresh(ctx, token, p.CSRF)
			requireError(t, err, "unauthenticated")
		}
	})
	for _, revoke := range []struct{ name, query string }{
		{"version", "UPDATE tw_user_auth SET version=version+1 WHERE user_id='reader'"},
		{"disabled", "UPDATE tw_user_auth SET status='disabled' WHERE user_id='reader'"},
		{"external-password", "UPDATE users SET password='changed-outside-go' WHERE user_id='reader'"},
	} {
		t.Run(revoke.name, func(t *testing.T) {
			db, s := fixture(t)
			ctx := context.Background()
			now := fixedSessionClock(s)
			p, old, err := s.LoginWithRemember(ctx, "reader", testPassword, "127.0.0.2", "", true)
			if err != nil {
				t.Fatal(err)
			}
			*now = now.Add(sessionRotationInterval)
			_, next, err := s.Refresh(ctx, old, p.CSRF)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.DB.Exec(revoke.query); err != nil {
				t.Fatal(err)
			}
			for _, token := range []string{old, next} {
				_, _, err = s.Refresh(ctx, token, p.CSRF)
				requireError(t, err, "unauthenticated")
			}
		})
	}
	t.Run("audit-rollback", func(t *testing.T) {
		db, s := fixture(t)
		ctx := context.Background()
		now := fixedSessionClock(s)
		p, old, err := s.LoginWithRemember(ctx, "reader", testPassword, "127.0.0.2", "", true)
		if err != nil {
			t.Fatal(err)
		}
		read := func() string {
			var current, previous, seed, csrf string
			var until, rotated, last, expires int64
			if err := db.DB.QueryRow("SELECT token_digest,COALESCE(previous_digest,''),COALESCE(rotation_seed,''),previous_until,rotated_at,last_seen,expires_at,csrf FROM tw_session WHERE digest=?", p.session).Scan(&current, &previous, &seed, &until, &rotated, &last, &expires, &csrf); err != nil {
				t.Fatal(err)
			}
			return fmt.Sprint(current, previous, seed, until, rotated, last, expires, csrf)
		}
		before := read()
		*now = now.Add(sessionRotationInterval)
		if _, _, err = s.Refresh(ctx, old, "wrong"); err == nil || read() != before {
			t.Fatal("CSRF failure changed the family", err)
		}
		if _, err = db.DB.Exec("DROP TABLE tw_audit"); err != nil {
			t.Fatal(err)
		}
		_, token, err := s.Refresh(ctx, old, p.CSRF)
		requireError(t, err, "unavailable")
		if token != "" || read() != before {
			t.Fatal("failed audit partially rotated or touched the family")
		}
		if _, err = s.Session(ctx, old, false); err != nil {
			t.Fatal("failed refresh lost the original credential", err)
		}
	})
}

func TestMySQLRememberedOSRotationChecksBindingAndProvider(t *testing.T) {
	testRememberedOSRotationChecksBindingAndProvider(t, linkedOSFixture)
}
func TestSQLiteRememberedOSRotationChecksBindingAndProvider(t *testing.T) {
	testRememberedOSRotationChecksBindingAndProvider(t, sqliteLinkedOSFixture)
}

func testRememberedOSRotationChecksBindingAndProvider(t *testing.T, fixture osAccessFixture) {
	s, admin, adminToken, provider := fixture(t)
	ctx := context.Background()
	linked := linkedOSCreate(t, s, admin, adminToken, "os-remembered", "host-one", "user", []Membership{{7, "viewer"}})
	now := fixedSessionClock(s)
	p, old, err := s.LoginOSWithRemember(ctx, "host-one", linkedOSPassword, "127.0.0.2", "", true)
	if err != nil || !p.Session.Remembered || p.User.ID != linked.ID {
		t.Fatal("OS remember option was not preserved", err)
	}
	calls := provider.calls.Load()
	*now = now.Add(48 * time.Hour)
	_, next, err := s.Refresh(ctx, old, p.CSRF)
	if err != nil || next == "" || provider.calls.Load() != calls {
		t.Fatal("OS renewal required or retained an OS password", err)
	}
	provider.available.Store(false)
	for _, token := range []string{old, next} {
		_, _, err = s.Refresh(ctx, token, p.CSRF)
		requireError(t, err, "unauthenticated")
	}
	provider.available.Store(true)
	if _, err = s.db.DB.Exec("UPDATE tw_user_identity SET subject='linux:uid:replacement' WHERE user_id=?", linked.ID); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{old, next} {
		_, _, err = s.Refresh(ctx, token, p.CSRF)
		requireError(t, err, "unauthenticated")
	}
}

func TestMySQLSessionSchemaRefusesPartialOrUntrackedUpgrade(t *testing.T) {
	testSessionSchemaRefusesPartialOrUntrackedUpgrade(t, initialized)
}
func TestSQLiteSessionSchemaRefusesPartialOrUntrackedUpgrade(t *testing.T) {
	testSessionSchemaRefusesPartialOrUntrackedUpgrade(t, sqliteInitialized)
}

func testSessionSchemaRefusesPartialOrUntrackedUpgrade(t *testing.T, fixture initializedAccessFixture) {
	ctx := context.Background()
	for _, drift := range []struct {
		name, column, sqlite, mysql string
	}{
		{"rotation-clock-type", "rotated_at", "TEXT NOT NULL DEFAULT 'broken'", "VARCHAR(64) NOT NULL DEFAULT 'broken'"},
		{"overlap-nullability", "previous_until", "INTEGER NULL DEFAULT 0", "BIGINT NULL DEFAULT 0"},
		{"remember-default", "remembered", "INTEGER NOT NULL DEFAULT 1 CHECK(remembered IN (0,1))", "BOOLEAN NOT NULL DEFAULT TRUE"},
		{"seed-type", "rotation_seed", "BLOB NULL", "VARBINARY(64) NULL"},
		{"current-nullability", "token_digest", "TEXT COLLATE BINARY NOT NULL DEFAULT ''", "CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT ''"},
		{"previous-default", "previous_digest", "TEXT COLLATE BINARY NULL DEFAULT 'invalid'", "CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL DEFAULT 'invalid'"},
	} {
		t.Run(drift.name, func(t *testing.T) {
			db, _ := fixture(t)
			if db.Dialect == "mysql" {
				if _, err := db.DB.Exec("ALTER TABLE tw_session MODIFY COLUMN " + drift.column + " " + drift.mysql); err != nil {
					t.Fatal(err)
				}
			} else {
				index := ""
				if drift.column == "token_digest" {
					index = "tw_session_token"
				} else if drift.column == "previous_digest" {
					index = "tw_session_previous"
				}
				if index != "" {
					if _, err := db.DB.Exec("DROP INDEX " + index); err != nil {
						t.Fatal(err)
					}
				}
				for _, query := range []string{"ALTER TABLE tw_session DROP COLUMN " + drift.column, "ALTER TABLE tw_session ADD COLUMN " + drift.column + " " + drift.sqlite} {
					if _, err := db.DB.Exec(query); err != nil {
						t.Fatal(err)
					}
				}
				if index != "" {
					if _, err := db.DB.Exec("CREATE UNIQUE INDEX " + index + " ON tw_session(" + drift.column + ")"); err != nil {
						t.Fatal(err)
					}
				}
			}
			status, err := Status(ctx, db)
			if err != nil || status.Ready || status.State != "incomplete" {
				t.Fatal("malformed rotation column reported ready", drift.column, status, err)
			}
			if _, err = New(ctx, db, 30*time.Minute); err == nil {
				t.Fatal("malformed rotation column enabled authentication", drift.column)
			}
			if _, err = Initialize(ctx, db); err == nil {
				t.Fatal("malformed rotation column was automatically rewritten", drift.column)
			}
		})
	}
	t.Run("unexpected-v3-column", func(t *testing.T) {
		db, _ := fixture(t)
		restoreV3Sessions(t, db)
		for _, query := range []string{"UPDATE tw_access_schema SET version=3,state='ready' WHERE id=1", "ALTER TABLE tw_session ADD COLUMN rotated_at BIGINT NOT NULL DEFAULT 0"} {
			if _, err := db.DB.Exec(query); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := Initialize(ctx, db); err == nil {
			t.Fatal("partial v4 shape was silently adopted")
		}
		status, err := Status(ctx, db)
		if err != nil || status.Version != 3 || status.State != "ready" || status.Ready {
			t.Fatal("refused upgrade changed its journal", status, err)
		}
	})
	t.Run("missing-v4-unique-key", func(t *testing.T) {
		db, _ := fixture(t)
		query := "DROP INDEX tw_session_token"
		if db.Dialect == "mysql" {
			query = "ALTER TABLE tw_session DROP INDEX tw_session_token"
		}
		if _, err := db.DB.Exec(query); err != nil {
			t.Fatal(err)
		}
		status, err := Status(ctx, db)
		if err != nil || status.Ready || status.State != "incomplete" {
			t.Fatal("missing rotation uniqueness reported ready", status, err)
		}
		if _, err = New(ctx, db, 30*time.Minute); err == nil {
			t.Fatal("session storage without uniqueness enabled")
		}
		if _, err = Initialize(ctx, db); err == nil {
			t.Fatal("drifted v4 shape was silently repaired")
		}
	})
	t.Run("real-ddl-failure", func(t *testing.T) {
		db, s := fixture(t)
		p, token, err := s.Login(ctx, "reader", testPassword, "127.0.0.2", "")
		if err != nil {
			t.Fatal(err)
		}
		restoreV3Sessions(t, db)
		if _, err = db.DB.Exec("UPDATE tw_access_schema SET version=3,state='ready' WHERE id=1"); err != nil {
			t.Fatal(err)
		}
		query := "CREATE INDEX tw_session_token ON tw_session(user_id)"
		if _, err = db.DB.Exec(query); err != nil {
			t.Fatal(err)
		}
		if _, err = Initialize(ctx, db); err == nil {
			t.Fatal("conflicting index allowed a partial upgrade")
		}
		status, err := Status(ctx, db)
		if err != nil || status.Ready {
			t.Fatal("failed upgrade reported ready", status, err)
		}
		if db.Dialect == "sqlite" {
			if status.Version != 3 || status.State != "ready" {
				t.Fatal("SQLite upgrade journal did not roll back", status)
			}
			for _, column := range rotationColumns {
				exists, err := sqliteAccessColumn(ctx, db.DB, "tw_session", column)
				if err != nil || exists {
					t.Fatal("SQLite failed upgrade retained partial columns", column, err)
				}
			}
		} else if status.Version != 4 || status.State != "running" {
			t.Fatal("MySQL failed DDL did not retain its running marker", status)
		}
		var csrf string
		var expires int64
		if err = db.DB.QueryRow("SELECT csrf,expires_at FROM tw_session WHERE digest=?", digest(token)).Scan(&csrf, &expires); err != nil || csrf != p.CSRF || expires != p.Session.ExpiresAt {
			t.Fatal("failed upgrade changed existing credentials", err)
		}
		if _, err = Initialize(ctx, db); err == nil {
			t.Fatal("failed or conflicted DDL was replayed")
		}
	})
}

func TestMySQLSessionLogoutWinsConcurrentRotation(t *testing.T) {
	testSessionLogoutWinsConcurrentRotation(t, initialized)
}
func TestSQLiteSessionLogoutWinsConcurrentRotation(t *testing.T) {
	testSessionLogoutWinsConcurrentRotation(t, sqliteInitialized)
}

func testSessionLogoutWinsConcurrentRotation(t *testing.T, fixture initializedAccessFixture) {
	db, s := fixture(t)
	ctx := context.Background()
	now := fixedSessionClock(s)
	p, old, err := s.LoginWithRemember(ctx, "reader", testPassword, "127.0.0.2", "", true)
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(sessionRotationInterval)
	otherDB := db
	if db.Dialect == "sqlite" {
		otherDB = openSQLiteAccessStore(t, sqliteAccessPath(t, db))
	}
	other, err := New(ctx, otherDB, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	other.now = s.now
	start, logout := make(chan struct{}), make(chan error, 1)
	type result struct {
		token string
		err   error
	}
	results := make(chan result, 8)
	go func() {
		<-start
		logout <- other.Logout(ctx, old, p.CSRF)
	}()
	for i := 0; i < cap(results); i++ {
		go func() {
			<-start
			_, token, err := s.Refresh(ctx, old, p.CSRF)
			results <- result{token, err}
		}()
	}
	close(start)
	var finished []result
	for i := 0; i < cap(results); i++ {
		finished = append(finished, <-results)
	}
	if err = <-logout; err != nil {
		t.Fatal("concurrent logout did not revoke the family", err)
	}
	for _, r := range finished {
		if r.err != nil {
			requireError(t, r.err, "unauthenticated")
		} else {
			_, err = s.Session(ctx, r.token, false)
			requireError(t, err, "unauthenticated")
		}
	}
	var remaining int
	if err = db.DB.QueryRow("SELECT COUNT(*) FROM tw_session WHERE digest=?", p.session).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatal("late refresh recreated a revoked family", remaining, err)
	}
}

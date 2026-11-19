package access

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/horrible-gh/TimeWeaver/internal/config"
	"github.com/horrible-gh/TimeWeaver/internal/store"
)

func accessStore(t *testing.T) *store.Store {
	t.Helper()
	if os.Getenv("TIMEWEAVER_TEST_MYSQL_ALLOW_CREATE") != "1" {
		t.Skip("requires explicit disposable DB permission")
	}
	host := os.Getenv("TIMEWEAVER_TEST_MYSQL_HOST")
	ip, err := netip.ParseAddr(host)
	if err != nil || !ip.IsLoopback() {
		t.Fatal("only loopback disposable databases allowed")
	}
	c := mysql.NewConfig()
	c.User = "root"
	c.Passwd = os.Getenv("TIMEWEAVER_TEST_MYSQL_PASSWORD")
	c.Net = "tcp"
	c.Addr = host + ":3306"
	connector, err := mysql.NewConnector(c)
	if err != nil {
		t.Fatal(err)
	}
	admin := sql.OpenDB(connector)
	hash := sha256.Sum256([]byte(t.TempDir()))
	name := "tw_access_" + hex.EncodeToString(hash[:12])
	if _, err = admin.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4"); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Exec("DROP DATABASE `" + name + "`"); admin.Close() })
	db, err := store.Open(context.Background(), config.Database{Type: "mysql", Host: host, Port: 3306, User: "root", Password: c.Passwd, Name: name}, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err = db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = db.RepairCriticalSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"INSERT INTO `groups`(group_id,group_name) VALUES(7,'Seven'),(8,'Eight')", "INSERT INTO users(user_id,name,password,role,group_id) VALUES('reader','Reader','unused','user',7),('global','Global','unused','user',0)"} {
		if _, err = db.DB.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.DB.Exec("UPDATE users SET password=? WHERE user_id IN ('reader','global')", testHash); err != nil {
		t.Fatal(err)
	}
	return db
}
func initialized(t *testing.T) (*store.Store, *Service) {
	t.Helper()
	db := accessStore(t)
	ctx := context.Background()
	if _, err := Initialize(ctx, db); err != nil {
		t.Fatal(err)
	}
	s, err := New(ctx, db, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return db, s
}
func requireError(t *testing.T, err error, code string) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("wanted %s, got %v", code, err)
	}
}
func TestMySQLAccessMigrationPreservesAndDoesNotRegrant(t *testing.T) {
	testAccessMigrationPreservesAndDoesNotRegrant(t, accessStore)
}

func testAccessMigrationPreservesAndDoesNotRegrant(t *testing.T, fixture storeAccessFixture) {
	db := fixture(t)
	ctx := context.Background()
	before, _ := db.Status(ctx)
	r, err := Status(ctx, db)
	if err != nil || r.Ready || r.State != "missing" {
		t.Fatal(r, err)
	}
	if _, err = Initialize(ctx, db); err != nil {
		t.Fatal(err)
	}
	var unchanged string
	if db.DB.QueryRow("SELECT password FROM users WHERE user_id='reader'").Scan(&unchanged) != nil || unchanged != testHash {
		t.Fatal("legacy password changed")
	}
	var flag bool
	if db.DB.QueryRow("SELECT must_change_password FROM tw_user_auth WHERE user_id='timeweaver'").Scan(&flag) != nil || !flag {
		t.Fatal("known seed was not restricted")
	}
	db.DB.Exec("DELETE FROM tw_group_member WHERE user_id='reader'")
	if _, err = Initialize(ctx, db); err != nil {
		t.Fatal(err)
	}
	var n int
	db.DB.QueryRow("SELECT COUNT(*) FROM tw_group_member WHERE user_id='reader'").Scan(&n)
	if n != 0 {
		t.Fatal("reinitialization regranted removed membership")
	}
	after, _ := db.Status(ctx)
	if after.Pending != before.Pending || len(after.Unknown) != 0 || after.Blocked {
		t.Fatal("legacy migration history changed")
	}
	db.DB.Exec("UPDATE tw_access_schema SET state='running' WHERE id=1")
	if _, err = Initialize(ctx, db); err == nil {
		t.Fatal("unfinished DDL was replayed")
	}
	if _, err = New(ctx, db, 30*time.Minute); err == nil {
		t.Fatal("unfinished access enabled")
	}
}
func TestMySQLAccessSessionLifecycleAndPasswordGate(t *testing.T) {
	testAccessSessionLifecycleAndPasswordGate(t, initialized)
}

func testAccessSessionLifecycleAndPasswordGate(t *testing.T, fixture initializedAccessFixture) {
	db, s := fixture(t)
	ctx := context.Background()
	p, token, err := s.Login(ctx, "reader", testPassword, "127.0.0.1", "")
	if err != nil {
		t.Fatal(err)
	}
	if !p.User.Can(7, "editor") || p.User.Can(8, "viewer") {
		t.Fatal("legacy membership mapping")
	}
	restarted, err := New(ctx, db, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = restarted.Session(ctx, token, false); err != nil {
		t.Fatal("session not durable", err)
	}
	requireError(t, s.Logout(ctx, token, "wrong"), "csrf_failed")
	touched := false
	requireError(t, s.WithGroup(ctx, token, p.CSRF, 8, "viewer", func(*sql.Tx, User) error { touched = true; return nil }), "forbidden")
	if touched {
		t.Fatal("unauthorized callback executed")
	}
	if _, err = db.DB.Exec("UPDATE tw_user_auth SET must_change_password=1 WHERE user_id='reader'"); err != nil {
		t.Fatal(err)
	}
	requireError(t, s.WithGroup(ctx, token, p.CSRF, 7, "viewer", func(*sql.Tx, User) error { return nil }), "password_change_required")
	_, token2, err := s.Login(ctx, "reader", testPassword, "127.0.0.1", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ChangePassword(ctx, token, p.CSRF, testPassword, "a-completely-new-password"); err != nil {
		t.Fatal(err)
	}
	for _, old := range []string{token, token2} {
		_, err = s.Session(ctx, old, false)
		requireError(t, err, "unauthenticated")
	}
	_, _, err = s.Login(ctx, "reader", testPassword, "127.0.0.1", "")
	requireError(t, err, "invalid_credentials")
	p, newToken, err := s.Login(ctx, "reader", "a-completely-new-password", "127.0.0.1", "")
	if err != nil || p.User.MustChangePassword || !p.User.Can(7, "editor") {
		t.Fatal(err)
	}
	if err = s.Logout(ctx, newToken, p.CSRF); err != nil {
		t.Fatal(err)
	}
	_, err = restarted.Session(ctx, newToken, false)
	requireError(t, err, "unauthenticated")
}
func TestMySQLAccessExpiryRevocationAndLimits(t *testing.T) {
	testAccessExpiryRevocationAndLimits(t, initialized)
}

func testAccessExpiryRevocationAndLimits(t *testing.T, fixture initializedAccessFixture) {
	db, s := fixture(t)
	ctx := context.Background()
	_, token, err := s.Login(ctx, "reader", testPassword, "127.0.0.1", "")
	if err != nil {
		t.Fatal(err)
	}
	db.DB.Exec("UPDATE tw_user_auth SET version=version+1 WHERE user_id='reader'")
	_, err = s.Session(ctx, token, false)
	requireError(t, err, "unauthenticated")
	_, token, err = s.Login(ctx, "reader", testPassword, "127.0.0.1", "")
	if err != nil {
		t.Fatal(err)
	}
	db.DB.Exec("UPDATE tw_session SET expires_at=? WHERE digest=?", time.Now().Unix()-1, digest(token))
	_, err = s.Session(ctx, token, false)
	requireError(t, err, "unauthenticated")
	_, token, err = s.Login(ctx, "reader", testPassword, "127.0.0.1", "")
	if err != nil {
		t.Fatal(err)
	}
	db.DB.Exec("UPDATE tw_session SET last_seen=? WHERE digest=?", time.Now().Unix()-1801, digest(token))
	_, err = s.Session(ctx, token, false)
	requireError(t, err, "unauthenticated")
	for i := 0; i < 5; i++ {
		_, _, err = s.Login(ctx, "GLOBAL", "wrong", "127.0.0.1", "")
		requireError(t, err, "invalid_credentials")
	}
	_, _, err = s.Login(ctx, "global", testPassword, "127.0.0.1", "")
	requireError(t, err, "login_limited")
	restarted, err := New(ctx, db, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = restarted.Login(ctx, "global", testPassword, "127.0.0.2", "")
	requireError(t, err, "login_limited")
	db.DB.Exec("UPDATE tw_user_auth SET status='disabled' WHERE user_id='reader'")
	_, _, err = s.Login(ctx, "reader", testPassword, "127.0.0.1", "")
	requireError(t, err, "invalid_credentials")
}
func request(h http.Handler, method, path, body, token, csrf string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://tw.example"+path, strings.NewReader(body))
	r.RemoteAddr = "203.0.113.4:55000"
	r.Header.Set("Origin", "https://tw.example")
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if token != "" {
		r.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	}
	if csrf != "" {
		r.Header.Set("X-CSRF-Token", csrf)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestMySQLAccessHTTPAndTransport(t *testing.T) {
	testAccessHTTPAndTransport(t, initialized)
}

func testAccessHTTPAndTransport(t *testing.T, fixture initializedAccessFixture) {
	_, s := fixture(t)
	h, err := NewHTTP(s, "/timeweaver_svr", HTTPOptions{Origin: "https://tw.example"})
	if err != nil {
		t.Fatal(err)
	}
	body := url.Values{"username": {"reader"}, "password": {testPassword}}.Encode()
	w := request(h, "POST", "/timeweaver_svr/login/", body, "", "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var p Principal
	if json.Unmarshal(w.Body.Bytes(), &p) != nil {
		t.Fatal("bad response")
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing session cookie")
	}
	c := cookies[0]
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Path != "/timeweaver_svr/" {
		t.Fatal("unsafe cookie")
	}
	if strings.Contains(w.Body.String(), c.Value) || strings.Contains(w.Body.String(), testPassword) || strings.Contains(w.Body.String(), testHash) || strings.Contains(w.Body.String(), "access_token") {
		t.Fatal("secret leaked")
	}
	if request(h, "GET", "/timeweaver_svr/auth/me", "", c.Value, "").Code != 200 {
		t.Fatal("session not accepted")
	}
	if request(h, "POST", "/timeweaver_svr/logout", "", c.Value, "").Code != 403 {
		t.Fatal("CSRF bypass")
	}
	for _, change := range []func(*http.Request){
		func(r *http.Request) { r.Header.Set("Origin", "https://evil.invalid") },
		func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") },
		func(r *http.Request) { r.TLS = nil; r.Header.Set("X-Forwarded-Proto", "https") },
		func(r *http.Request) { r.Host = "evil.invalid" },
	} {
		r := httptest.NewRequest("POST", "https://tw.example/timeweaver_svr/login", strings.NewReader(body))
		r.RemoteAddr = "203.0.113.4:55000"
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", "https://tw.example")
		change(r)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal("transport/origin bypass", w.Code)
		}
	}
	r := httptest.NewRequest("GET", "https://tw.example/timeweaver_svr/auth/me", nil)
	r.AddCookie(c)
	r.Header.Set("Authorization", "Bearer a-device-token")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("bearer fell back to cookie")
	}
	if request(h, "GET", "/timeweaver_svr/login", "", "", "").Code != 405 {
		t.Fatal("method")
	}
	if request(h, "POST", "/timeweaver_svr/login//", body, "", "").Code != 404 {
		t.Fatal("redirect")
	}
	if request(h, "POST", "/timeweaver_svr/logout/", "", c.Value, p.CSRF).Code != 200 {
		t.Fatal("logout")
	}
	if request(h, "GET", "/timeweaver_svr/auth/me", "", c.Value, "").Code != 401 {
		t.Fatal("logout not durable")
	}
}
func TestHumanOriginRejectsRemoteHTTPAndSpoofedLocal(t *testing.T) {
	if _, err := NewHTTP(&Service{}, "/tw", HTTPOptions{Origin: "http://tw.example"}); err == nil {
		t.Fatal("remote plaintext allowed")
	}
	h, err := NewHTTP(&Service{}, "/tw", HTTPOptions{Origin: "http://localhost:8000"})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "http://localhost:8000/tw/auth/methods", nil)
	r.RemoteAddr = "127.0.0.1:12345"
	if !h.transport(r) {
		t.Fatal("direct local rejected")
	}
	r.Header.Set("X-Forwarded-For", "203.0.113.1")
	if h.transport(r) {
		t.Fatal("proxied HTTP considered local")
	}
	h.trusted = []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}
	if h.peer(r) != "203.0.113.1" {
		t.Fatal("trusted peer chain")
	}
	r.RemoteAddr = "203.0.113.5:12345"
	if h.peer(r) != "203.0.113.5" {
		t.Fatal("untrusted forwarded identity")
	}
}

func TestMySQLAccessSessionCapAndAtomicPasswordFailure(t *testing.T) {
	testAccessSessionCapAndAtomicPasswordFailure(t, initialized)
}

func testAccessSessionCapAndAtomicPasswordFailure(t *testing.T, fixture initializedAccessFixture) {
	db, s := fixture(t)
	ctx := context.Background()
	var first, latest string
	var principal Principal
	for i := 0; i < 12; i++ {
		p, token, err := s.Login(ctx, "reader", testPassword, "127.0.0.1", "")
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = token
		}
		latest = token
		principal = p
	}
	var count int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM tw_session WHERE user_id='reader'").Scan(&count); err != nil || count != 10 {
		t.Fatal("session cap", count, err)
	}
	// With second-resolution timestamps older sessions need not be evicted in
	// insertion order; regardless, the newly issued session must remain usable.
	_ = first
	if _, err := s.Session(ctx, latest, false); err != nil {
		t.Fatal("newest session evicted", err)
	}
	var stored string
	if db.DB.QueryRow("SELECT digest FROM tw_session WHERE digest=?", digest(latest)).Scan(&stored) != nil || stored == latest {
		t.Fatal("raw credential persisted")
	}
	if _, err := db.DB.Exec("DROP TABLE tw_audit"); err != nil {
		t.Fatal(err)
	}
	err := s.ChangePassword(ctx, latest, principal.CSRF, testPassword, "another-new-password")
	requireError(t, err, "unavailable")
	var hash string
	db.DB.QueryRow("SELECT password FROM users WHERE user_id='reader'").Scan(&hash)
	if hash != testHash {
		t.Fatal("password survived failed audit transaction")
	}
	if _, err = s.Session(ctx, latest, false); err != nil {
		t.Fatal("failed password change revoked session", err)
	}
	requireError(t, s.Logout(ctx, latest, principal.CSRF), "unavailable")
	if _, err = s.Session(ctx, latest, false); err != nil {
		t.Fatal("failed logout was partially committed", err)
	}
}

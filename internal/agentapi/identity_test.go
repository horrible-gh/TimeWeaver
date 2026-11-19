package agentapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/golang-jwt/jwt/v5"
	"github.com/horrible-gh/TimeWeaver/internal/config"
	"github.com/horrible-gh/TimeWeaver/internal/store"
)

const testSecret = "identity-test-only-0123456789abcdef0123456789"

func identityStore(t *testing.T) *store.Store {
	t.Helper()
	if os.Getenv("TIMEWEAVER_TEST_MYSQL_ALLOW_CREATE") != "1" {
		t.Skip("requires explicit disposable database permission")
	}
	host := os.Getenv("TIMEWEAVER_TEST_MYSQL_HOST")
	ip, err := netip.ParseAddr(host)
	if err != nil || !ip.IsLoopback() {
		t.Fatal("identity tests require a loopback disposable database")
	}
	cfg := mysql.NewConfig()
	cfg.User = "root"
	cfg.Passwd = os.Getenv("TIMEWEAVER_TEST_MYSQL_PASSWORD")
	cfg.Net = "tcp"
	cfg.Addr = host + ":3306"
	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	admin := sql.OpenDB(connector)
	hash := sha256.Sum256([]byte(t.TempDir()))
	name := "tw_identity_" + hex.EncodeToString(hash[:12])
	if _, err = admin.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4"); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Exec("DROP DATABASE `" + name + "`"); admin.Close() })
	db, err := store.Open(context.Background(), config.Database{Type: "mysql", Host: host, Port: 3306, User: "root", Password: cfg.Passwd, Name: name}, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err = db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return db
}
func identityService(t *testing.T, db *store.Store, agentKey string) *Identity {
	t.Helper()
	s, err := NewIdentity(db, config.Settings{SecretKey: testSecret, AgentSecretKey: agentKey})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func httpService(t *testing.T, s *Identity) *IdentityHTTP {
	t.Helper()
	h, err := NewIdentityHTTP(s, "/timeweaver_svr", HTTPOptions{Capacity: 1000, Refill: 1, Concurrency: 16})
	if err != nil {
		t.Fatal(err)
	}
	return h
}
func enrollment(t *testing.T, s *Identity, name *string, group int64) string {
	t.Helper()
	raw, err := randomSecret("enr_")
	if err != nil {
		t.Fatal(err)
	}
	id := digest(raw)[:16]
	now, err := store.DBNow(context.Background(), s.db.DB, s.db.Dialect)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.db.DB.Exec(`INSERT INTO agent_enrollment_token(enrollment_id,token_hash,device_name,group_id,created_at,expires_at) VALUES (?,?,?,?,?,?)`, id, digest(raw), name, group, store.TimeValue(s.db.Dialect, now), store.TimeValue(s.db.Dialect, now.Add(24*time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func requireCode(t *testing.T, err error, status int, code string) {
	t.Helper()
	var e *APIError
	if !errors.As(err, &e) || e.Status != status || e.Code != code {
		t.Fatalf("wanted %d/%s, got %v", status, code, err)
	}
}
func post(h http.Handler, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:12345"
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestJWTRejectsInvalidClaimsWithoutDatabase(t *testing.T) {
	now := time.Unix(1900000000, 0)
	s := &Identity{key: []byte(testSecret), now: func() time.Time { return now }}
	base := func() jwt.MapClaims {
		return jwt.MapClaims{"sub": "device:1", "aud": "timeweaver-agent", "typ": "agent", "credential_id": 1, "iat": now.Unix(), "exp": now.Add(time.Minute).Unix()}
	}
	for _, field := range []string{"sub", "aud", "typ", "credential_id", "iat", "exp"} {
		c := base()
		delete(c, field)
		raw, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(s.key)
		_, err := s.VerifyAccess(context.Background(), raw)
		requireCode(t, err, 401, "invalid_token")
	}
	for _, field := range []string{"sub", "aud", "typ", "credential_id"} {
		c := base()
		c[field] = "wrong"
		raw, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(s.key)
		_, err := s.VerifyAccess(context.Background(), raw)
		requireCode(t, err, 401, "invalid_token")
	}
	c := base()
	c["exp"] = now.Add(-time.Second).Unix()
	raw, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(s.key)
	_, err := s.VerifyAccess(context.Background(), raw)
	requireCode(t, err, 401, "token_expired")
	c = base()
	c["iat"] = now.Add(time.Minute).Unix()
	raw, _ = jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(s.key)
	_, err = s.VerifyAccess(context.Background(), raw)
	requireCode(t, err, 401, "invalid_token")
	for _, alg := range []*jwt.SigningMethodHMAC{jwt.SigningMethodHS384, jwt.SigningMethodHS512} {
		raw, _ := jwt.NewWithClaims(alg, base()).SignedString(s.key)
		_, err := s.VerifyAccess(context.Background(), raw)
		requireCode(t, err, 401, "invalid_token")
	}
	raw, _ = jwt.NewWithClaims(jwt.SigningMethodHS256, base()).SignedString([]byte("other-secret"))
	_, err = s.VerifyAccess(context.Background(), raw)
	requireCode(t, err, 401, "invalid_token")
	for _, raw := range []string{"", strings.Repeat("a", 9000), "a.b.c"} {
		_, err := s.VerifyAccess(context.Background(), raw)
		requireCode(t, err, 401, "invalid_token")
	}
}
func TestIdentityHTTPValidationAndLimits(t *testing.T) {
	h := httpService(t, &Identity{})
	for _, path := range []string{"/enroll", "/enroll/", "/token", "/token/", "/token/refresh", "/token/refresh/"} {
		for _, body := range []string{"{}", "null", "{", `{"refresh_token":3}`, `{"refresh_token":"valid"} {}`, strings.Repeat(" ", 17<<10)} {
			w := post(h, "/timeweaver_svr/api/agent/v1"+path, body)
			if w.Code != 422 || w.Header().Get("Location") != "" {
				t.Fatal(path, w.Code)
			}
		}
	}
	for _, path := range []string{"/wrong", "/timeweaver_svr/api/agent/v1/token//", "/timeweaver_svr/api/agent/v1/../token"} {
		w := post(h, path, "{}")
		if w.Code != 404 || w.Header().Get("Location") != "" {
			t.Fatal(path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/timeweaver_svr/api/agent/v1/token", nil))
	if w.Code != 405 {
		t.Fatal(w.Code)
	}
	// Saturation is handled without opening a database or echoing the token.
	for i := 0; i < cap(h.slots); i++ {
		h.slots <- struct{}{}
	}
	w = post(h, "/timeweaver_svr/api/agent/v1/token", `{"refresh_token":"sensitive-test-token"}`)
	if w.Code != 503 || w.Header().Get("Retry-After") != "5" || strings.Contains(w.Body.String(), "sensitive") {
		t.Fatal(w.Code, w.Body.String())
	}
	h.options.Capacity = 1
	h.buckets = map[string]bucket{}
	h.limit("a", "Request rate exceeded")
	requireCode(t, h.limit("a", "Request rate exceeded"), 429, "rate_limited")
	r := httptest.NewRequest("POST", "/", nil)
	r.RemoteAddr = "127.0.0.1:8888"
	r.Header.Set("X-Forwarded-For", "203.0.113.9")
	if h.address(r) != "127.0.0.1" {
		t.Fatal("trusted forged forwarding header")
	}
	h.options.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}
	if h.address(r) != "203.0.113.9" {
		t.Fatal("trusted proxy was ignored")
	}
}
func TestMySQLIdentityLifecycleAndConcurrency(t *testing.T) {
	s := identityService(t, identityStore(t), "")
	ctx := context.Background()
	name := "테스트-device"
	token := enrollment(t, s, &name, 0)
	_, err := s.Enroll(ctx, token, "wrong", nil)
	requireCode(t, err, 403, "enrollment_token_invalid")
	first, err := s.Enroll(ctx, token, name, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.DeviceID <= 0 || !strings.HasPrefix(first.RefreshToken, "rft_") || len(first.RefreshToken) != 68 {
		t.Fatal("invalid token data")
	}
	if _, err = s.VerifyAccess(ctx, first.AccessToken); err != nil {
		t.Fatal(err)
	}
	_, err = s.Enroll(ctx, token, name, nil)
	requireCode(t, err, 403, "enrollment_token_invalid")
	second, err := s.Rotate(ctx, first.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.VerifyAccess(ctx, first.AccessToken)
	requireCode(t, err, 403, "device_revoked")
	_, err = s.Rotate(ctx, first.RefreshToken)
	requireCode(t, err, 401, "invalid_token")
	if _, err = s.VerifyAccess(ctx, second.AccessToken); err != nil {
		t.Fatal(err)
	}
	var stored []byte
	if err = s.db.DB.QueryRow("SELECT token_hash FROM agent_device_credential WHERE revoked_at IS NULL AND device_id=?", first.DeviceID).Scan(&stored); err != nil || !bytes.Equal(stored, digest(second.RefreshToken)) {
		t.Fatal("refresh digest storage mismatch", err)
	}
	// Only one simultaneous use of a refresh token may commit.
	var wg sync.WaitGroup
	results := make(chan error, 2)
	tokens := make(chan TokenData, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := s.Rotate(ctx, second.RefreshToken)
			results <- e
			if e == nil {
				tokens <- v
			}
		}()
	}
	wg.Wait()
	close(results)
	successes := 0
	for e := range results {
		if e == nil {
			successes++
		} else {
			requireCode(t, e, 401, "invalid_token")
		}
	}
	if successes != 1 {
		t.Fatal("refresh replay", successes)
	}
	third := <-tokens
	newEnrollment := enrollment(t, s, &name, 0)
	reenrolled, err := s.Enroll(ctx, newEnrollment, name, nil)
	if err != nil || reenrolled.DeviceID != first.DeviceID {
		t.Fatal("device identity changed", err)
	}
	_, err = s.VerifyAccess(ctx, third.AccessToken)
	requireCode(t, err, 403, "device_revoked")
	// Inactive-device refusal must roll back without consuming the enrollment.
	if _, err = s.db.DB.Exec("UPDATE devices SET status='inactive' WHERE device_id=?", first.DeviceID); err != nil {
		t.Fatal(err)
	}
	unused := enrollment(t, s, &name, 0)
	_, err = s.Enroll(ctx, unused, name, nil)
	requireCode(t, err, 403, "device_inactive")
	_, err = s.Rotate(ctx, reenrolled.RefreshToken)
	requireCode(t, err, 403, "device_inactive")
	_, err = s.VerifyAccess(ctx, reenrolled.AccessToken)
	requireCode(t, err, 403, "device_revoked")
	if _, err = s.db.DB.Exec("UPDATE devices SET status='active' WHERE device_id=?", first.DeviceID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Enroll(ctx, unused, name, nil); err != nil {
		t.Fatal("rejected request consumed enrollment", err)
	}
	// Only one simultaneous enrollment token use may commit.
	other := "parallel"
	single := enrollment(t, s, &other, 0)
	results = make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := s.Enroll(ctx, single, other, nil); results <- e }()
	}
	wg.Wait()
	close(results)
	successes = 0
	for e := range results {
		if e == nil {
			successes++
		} else {
			requireCode(t, e, 403, "enrollment_token_invalid")
		}
	}
	if successes != 1 {
		t.Fatal("enrollment replay", successes)
	}
}
func TestPythonIdentityInterop(t *testing.T) {
	if os.Getenv("TIMEWEAVER_IDENTITY_INTEROP") != "1" {
		t.Skip("requires Python reference dependencies")
	}
	db := identityStore(t)
	var database string
	if err := db.DB.QueryRow("SELECT DATABASE()").Scan(&database); err != nil {
		t.Fatal(err)
	}
	// Run the frozen Python implementation, not a hand-written JWT oracle.
	for _, key := range []string{"", testSecret, "separate-agent-secret-0123456789abcdef0123456789"} {
		s := identityService(t, db, key)
		h := httpService(t, s)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/verify" {
				v, err := h.Authenticate(r)
				if err != nil {
					writeFailure(w, err)
				} else {
					writeJSON(w, 200, v)
				}
				return
			}
			h.ServeHTTP(w, r)
		}))
		cmd := exec.Command("python", filepath.Join("..", "..", "scripts", "test-go-identity-compat.py"), server.URL, database)
		cmd.Env = append(os.Environ(), "TW_INTEROP_SECRET="+testSecret, "TW_INTEROP_AGENT_SECRET="+key)
		output, err := cmd.CombinedOutput()
		server.Close()
		if err != nil {
			t.Fatalf("Python interoperability failed: %v\n%s", err, output)
		}
		t.Log(strings.TrimSpace(string(output)))
	}
}

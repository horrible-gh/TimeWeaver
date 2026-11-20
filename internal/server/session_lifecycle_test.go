package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/horrible-gh/TimeWeaver/internal/access"
)

const sessionTestOrigin = "http://127.0.0.1:19071"

type browserPrincipal struct {
	CSRF    string `json:"csrf_token"`
	Session struct {
		Remembered          bool  `json:"remembered"`
		ExpiresAt           int64 `json:"expires_at"`
		RefreshAfterSeconds int64 `json:"refresh_after_seconds"`
	} `json:"session"`
}

func sessionSurface(t *testing.T, f *deviceFixture) *BrowserSurface {
	t.Helper()
	app, err := NewBrowserSurface(context.Background(), f.db, f.probe, f.settings, http.NotFoundHandler(), slog.New(slog.NewTextHandler(io.Discard, nil)), access.HTTPOptions{Origin: sessionTestOrigin})
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func sessionRequest(app *BrowserSurface, method, path, body, token, csrf string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, sessionTestOrigin+app.context+path, strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:43210"
	if method != http.MethodGet {
		req.Header.Set("Origin", sessionTestOrigin)
	}
	if token != "" {
		req.AddCookie(&http.Cookie{Name: "tw_session", Value: token})
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	return rec
}

func sessionPrincipal(t *testing.T, rec *httptest.ResponseRecorder) browserPrincipal {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("session HTTP status %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("session response can be cached")
	}
	var p browserPrincipal
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil || p.CSRF == "" || p.Session.ExpiresAt == 0 {
		t.Fatal("missing session metadata", err)
	}
	return p
}

func responseSessionCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	var found *http.Cookie
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == "tw_session" {
			if found != nil {
				t.Fatal("duplicate session cookie")
			}
			found = cookie
		}
	}
	if found == nil {
		t.Fatal("session response omitted its cookie")
	}
	if !found.HttpOnly || found.SameSite != http.SameSiteStrictMode || found.Path != "/timeweaver_svr/" {
		t.Fatal("incorrect session cookie scope or protection")
	}
	return found
}

func sessionLogin(t *testing.T, app *BrowserSurface, remembered bool) (browserPrincipal, *http.Cookie) {
	t.Helper()
	remember := "false"
	if remembered {
		remember = "true"
	}
	form := url.Values{"username": {"viewer"}, "password": {deviceTestPassword}, "remember": {remember}}
	rec := sessionRequest(app, http.MethodPost, "/login", form.Encode(), "", "", map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	return sessionPrincipal(t, rec), responseSessionCookie(t, rec)
}

func credentialDigest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func TestMySQLBrowserSessionLifecycle(t *testing.T) {
	f := deviceFixtureFor(t)
	app := sessionSurface(t, f)
	ordinary, cookie := sessionLogin(t, app, false)
	if ordinary.Session.Remembered || cookie.MaxAge != 0 || !cookie.Expires.IsZero() {
		t.Fatal("ordinary login became a persistent cookie")
	}
	if _, err := fixtureExec(t, f.db, "UPDATE tw_session SET last_seen=? WHERE token_digest=?", time.Now().Add(-2*time.Hour).Unix(), credentialDigest(cookie.Value)); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/auth/me", "/auth/refresh"} {
		method := http.MethodGet
		if path == "/auth/refresh" {
			method = http.MethodPost
		}
		rec := sessionRequest(app, method, path, "", cookie.Value, ordinary.CSRF, nil)
		if rec.Code != http.StatusUnauthorized || len(rec.Result().Cookies()) != 0 {
			t.Fatal("ordinary idle expiry renewed or cleared a potentially newer cookie", rec.Code)
		}
	}

	remembered, first := sessionLogin(t, app, true)
	if !remembered.Session.Remembered || first.MaxAge <= 29*24*3600 || first.Expires.Unix() != remembered.Session.ExpiresAt {
		t.Fatal("remembered cookie lacks its fixed persistent deadline")
	}
	for _, tc := range []struct {
		name, method, csrf string
		headers            map[string]string
		status             int
	}{
		{"method", http.MethodGet, remembered.CSRF, nil, http.StatusMethodNotAllowed},
		{"origin-missing", http.MethodPost, remembered.CSRF, map[string]string{"Origin": ""}, http.StatusForbidden},
		{"origin-cross-site", http.MethodPost, remembered.CSRF, map[string]string{"Origin": "http://other.invalid"}, http.StatusForbidden},
		{"csrf-missing", http.MethodPost, "", nil, http.StatusForbidden},
		{"csrf-forged", http.MethodPost, "wrong", nil, http.StatusForbidden},
		{"bearer", http.MethodPost, remembered.CSRF, map[string]string{"Authorization": "Bearer device-or-legacy-jwt"}, http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := sessionRequest(app, tc.method, "/auth/refresh", "", first.Value, tc.csrf, tc.headers)
			if rec.Code != tc.status || len(rec.Result().Cookies()) != 0 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
				t.Fatal("refresh transport boundary failed", rec.Code)
			}
		})
	}
	if _, err := fixtureExec(t, f.db, "UPDATE tw_session SET rotated_at=?,last_seen=? WHERE token_digest=?", time.Now().Add(-13*time.Hour).Unix(), time.Now().Add(-48*time.Hour).Unix(), credentialDigest(first.Value)); err != nil {
		t.Fatal(err)
	}
	app = sessionSurface(t, f)
	me := sessionRequest(app, http.MethodGet, "/auth/me", "", first.Value, "", nil)
	p := sessionPrincipal(t, me)
	if !p.Session.Remembered || p.Session.RefreshAfterSeconds != 0 || p.CSRF != remembered.CSRF || len(me.Result().Cookies()) != 0 {
		t.Fatal("restarted service did not bootstrap an idle remembered session without rotating on GET")
	}
	// Deliberately discard this successful rotation response as a lost network
	// reply. A newly constructed service must reconstruct the same successor.
	committed := sessionRequest(app, http.MethodPost, "/auth/refresh/", "", first.Value, p.CSRF, nil)
	rotated := sessionPrincipal(t, committed)
	next := responseSessionCookie(t, committed)
	if next.Value == first.Value || rotated.CSRF != p.CSRF || rotated.Session.ExpiresAt != remembered.Session.ExpiresAt || next.Expires != first.Expires {
		t.Fatal("rotation failed to preserve family CSRF and absolute deadline")
	}
	app = sessionSurface(t, f)
	recovered := sessionRequest(app, http.MethodPost, "/auth/refresh", "", first.Value, p.CSRF, nil)
	sessionPrincipal(t, recovered)
	if responseSessionCookie(t, recovered).Value != next.Value {
		t.Fatal("lost rotation response did not recover the identical successor after restart")
	}

	// Two tabs submit the same due credential. Both responses must set the same
	// successor, so response ordering cannot roll the browser back or fork a family.
	concurrent, before := sessionLogin(t, app, true)
	if _, err := fixtureExec(t, f.db, "UPDATE tw_session SET rotated_at=? WHERE token_digest=?", time.Now().Add(-13*time.Hour).Unix(), credentialDigest(before.Value)); err != nil {
		t.Fatal(err)
	}
	var responses [2]*httptest.ResponseRecorder
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range responses {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			responses[i] = sessionRequest(app, http.MethodPost, "/auth/refresh", "", before.Value, concurrent.CSRF, nil)
		}(i)
	}
	close(start)
	wg.Wait()
	var successor string
	for _, response := range responses {
		actual := sessionPrincipal(t, response)
		issued := responseSessionCookie(t, response).Value
		if issued == before.Value || successor != "" && issued != successor || actual.Session.ExpiresAt != concurrent.Session.ExpiresAt {
			t.Fatal("concurrent browser refresh forked or extended the session")
		}
		successor = issued
	}
	if _, err := fixtureExec(t, f.db, "UPDATE tw_session SET previous_until=? WHERE token_digest=?", time.Now().Add(-time.Second).Unix(), credentialDigest(successor)); err != nil {
		t.Fatal(err)
	}
	stale := sessionRequest(app, http.MethodPost, "/auth/refresh", "", before.Value, concurrent.CSRF, nil)
	if stale.Code != http.StatusUnauthorized || len(stale.Result().Cookies()) != 0 {
		t.Fatal("expired predecessor renewed or erased the successor")
	}
	sessionPrincipal(t, sessionRequest(app, http.MethodGet, "/auth/me", "", successor, "", nil))
	logout := sessionRequest(app, http.MethodPost, "/logout", "", successor, concurrent.CSRF, nil)
	if logout.Code != http.StatusOK || responseSessionCookie(t, logout).MaxAge != -1 {
		t.Fatal("logout did not remove the browser cookie")
	}
	for _, token := range []string{before.Value, successor} {
		rec := sessionRequest(app, http.MethodPost, "/auth/refresh", "", token, concurrent.CSRF, nil)
		if rec.Code != http.StatusUnauthorized || len(rec.Result().Cookies()) != 0 {
			t.Fatal("logged-out family could be renewed")
		}
	}
	if _, err := fixtureExec(t, f.db, "UPDATE tw_session SET expires_at=? WHERE token_digest=?", time.Now().Add(-time.Second).Unix(), credentialDigest(next.Value)); err != nil {
		t.Fatal(err)
	}
	expired := sessionRequest(app, http.MethodPost, "/auth/refresh", "", next.Value, remembered.CSRF, nil)
	if expired.Code != http.StatusUnauthorized || len(expired.Result().Cookies()) != 0 {
		t.Fatal("remembered absolute deadline was extended")
	}
}

func TestSQLiteBrowserSessionLifecycle(t *testing.T) {
	t.Setenv("TIMEWEAVER_TEST_SQLITE", "1")
	TestMySQLBrowserSessionLifecycle(t)
}

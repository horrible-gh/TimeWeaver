package webui

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

func fixture() fstest.MapFS {
	return fstest.MapFS{
		"index.html":               {Data: []byte(`<html><head><link href="/css/plain.css" rel="stylesheet"></head><body><div id="app"></div><script src="/js/login.12345678.js"></script></body></html>`)},
		"dashboard/index.html":     {Data: []byte(`<html><head></head><body><div id=app></div><script src=/js/dashboard.abcdef12.js></script></body></html>`)},
		"js/login.12345678.js":     {Data: []byte(`console.log("login")`)},
		"js/dashboard.abcdef12.js": {Data: []byte(`console.log("dashboard")`)},
		"css/plain.css":            {Data: []byte(`body{margin:0}`)},
	}
}
func newFixture(t *testing.T) *Handler {
	t.Helper()
	h, err := New(fixture(), "/timeweaver_svr")
	if err != nil {
		t.Fatal(err)
	}
	return h
}
func response(h http.Handler, method, target string) *httptest.ResponseRecorder {
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest(method, target, nil))
	return r
}

func TestLoginDashboardAndDeepLinks(t *testing.T) {
	h := newFixture(t)
	for _, target := range []string{"/", "/login", "/login/", "/dashboard", "/dashboard/", "/dashboard/tasks", "/dashboard/tasks/", "/dashboard/schedule-history"} {
		t.Run(target, func(t *testing.T) {
			w := response(h, "GET", target)
			if w.Code != 200 {
				t.Fatalf("status %d", w.Code)
			}
			if !strings.Contains(w.Body.String(), `<meta name="tw-api-context" content="/timeweaver_svr">`) {
				t.Fatal("runtime context missing")
			}
			if w.Header().Get("Location") != "" || w.Header().Get("Cache-Control") != "no-cache" {
				t.Fatal("unexpected redirect or cache")
			}
		})
	}
}
func TestAPIMissesNeverFallBackToHTML(t *testing.T) {
	h := newFixture(t)
	for _, method := range []string{"GET", "POST", "HEAD"} {
		for _, target := range []string{"/timeweaver_svr", "/timeweaver_svr/api/agent/v1/snapshot", "/timeweaver_svr/api/agent/v1/snapshot/"} {
			w := response(h, method, target)
			if w.Code != 404 || w.Header().Get("Content-Type") != "application/json" || strings.Contains(w.Body.String(), "<html") || w.Header().Get("Location") != "" {
				t.Fatalf("API miss changed for %s %s", method, target)
			}
			if method == "HEAD" && w.Body.Len() != 0 {
				t.Fatal("HEAD returned body")
			}
		}
	}
}
func TestStaticCacheAndConditionalRequests(t *testing.T) {
	h := newFixture(t)
	hashed := response(h, "GET", "/js/login.12345678.js")
	if hashed.Code != 200 || !strings.Contains(hashed.Header().Get("Cache-Control"), "immutable") {
		t.Fatal("hashed asset cache")
	}
	plain := response(h, "GET", "/css/plain.css")
	if plain.Code != 200 || plain.Header().Get("Cache-Control") != "no-cache" {
		t.Fatal("unhashed asset cached permanently")
	}
	head := response(h, "HEAD", "/js/login.12345678.js")
	if head.Code != 200 || head.Body.Len() != 0 || head.Header().Get("Content-Length") == "" {
		t.Fatal("HEAD handling")
	}
	req := httptest.NewRequest("GET", "/js/login.12345678.js", nil)
	req.Header.Set("If-None-Match", hashed.Header().Get("ETag"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 304 || w.Body.Len() != 0 {
		t.Fatal("conditional GET handling")
	}
}
func TestMissingAndUnsafePaths(t *testing.T) {
	h := newFixture(t)
	for _, target := range []string{"/dashboard/missing.js", "/js/missing.js", "/unknown", "/.env", "/js/../index.html", "/js/%2e%2e/index.html", "/js//login.12345678.js", "/js/login.12345678.js.map"} {
		if w := response(h, "GET", target); w.Code != 404 || w.Header().Get("Location") != "" {
			t.Errorf("unexpected response for %s: %d", target, w.Code)
		}
	}
	if w := response(h, "POST", "/login"); w.Code != 405 || w.Header().Get("Allow") != "GET, HEAD" {
		t.Fatal("unsupported method")
	}
}
func TestIncompleteBundlesAreRejected(t *testing.T) {
	for _, missing := range []string{"index.html", "dashboard/index.html", "js/login.12345678.js", "css/plain.css"} {
		assets := fixture()
		delete(assets, missing)
		if _, err := New(assets, "/time_weaver"); err == nil {
			t.Fatalf("accepted missing %s", missing)
		}
	}
	assets := fixture()
	assets["index.html"] = &fstest.MapFile{Data: []byte("UI not built")}
	if _, err := New(assets, "/time_weaver"); err == nil {
		t.Fatal("accepted placeholder")
	}
	if _, err := New(fixture(), "/dashboard"); err == nil {
		t.Fatal("accepted colliding context")
	}
	assets = fixture()
	assets["index.html"] = &fstest.MapFile{Data: []byte(`<html><head></head><div id=app></div><script src="https://example.invalid/app.js"></script></html>`)}
	if _, err := New(assets, "/time_weaver"); err == nil {
		t.Fatal("accepted external-only bundle")
	}
}
func TestBundledAssets(t *testing.T) {
	_, err := fs.Stat(embedded, "assets/dist/index.html")
	if err != nil && os.Getenv("TW_REQUIRE_EMBEDDED_UI") != "1" {
		t.Skip("Vue not built; fixture tests still cover handler behavior")
	}
	h, err := Bundled("/test_context")
	if err != nil {
		t.Fatal(err)
	}
	if w := response(h, "GET", "/dashboard/tasks"); w.Code != 200 || !strings.Contains(w.Body.String(), "/test_context") {
		t.Fatal("built UI cannot serve deep links")
	}
}

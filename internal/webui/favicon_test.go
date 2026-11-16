package webui

import (
	"bytes"
	"encoding/binary"
	"io/fs"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestBundledBrandedFavicon(t *testing.T) {
	if _, err := fs.Stat(embedded, "assets/dist/index.html"); err != nil && os.Getenv("TW_REQUIRE_EMBEDDED_UI") != "1" {
		t.Skip("Vue not built")
	}
	h, err := Bundled("/timeweaver_svr")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/login", "/dashboard/tasks"} {
		w := response(h, "GET", path)
		if !strings.Contains(w.Body.String(), `/favicon.ico?v=tw-clock-1`) {
			t.Fatal("favicon missing from built page", path)
		}
	}
	w := response(h, "GET", "/favicon.ico?v=tw-clock-1")
	raw := w.Body.Bytes()
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "image/") || len(raw) < 54 || binary.LittleEndian.Uint16(raw[2:]) != 1 || binary.LittleEndian.Uint16(raw[4:]) != 3 {
		t.Fatal("invalid built favicon")
	}
	source, err := os.ReadFile("../../client/public/favicon.ico")
	if err != nil || !bytes.Equal(raw, source) {
		t.Fatal("favicon changed during embed")
	}
	if w.Header().Get("Cache-Control") != "no-cache" {
		t.Fatal("unversioned icon must revalidate")
	}
	request := httptest.NewRequest("GET", "/favicon.ico?v=tw-clock-1", nil)
	request.Header.Set("If-None-Match", w.Header().Get("ETag"))
	cached := httptest.NewRecorder()
	h.ServeHTTP(cached, request)
	if cached.Code != 304 || cached.Body.Len() != 0 {
		t.Fatal("icon conditional request")
	}
}

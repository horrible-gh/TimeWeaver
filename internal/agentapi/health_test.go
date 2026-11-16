package agentapi

import (
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/horrible-gh/TimeWeaver/internal/config"
	"github.com/horrible-gh/TimeWeaver/internal/store"
)

func TestHealthProbeTimeoutAdmissionAndMethods(t *testing.T) {
	h := &HealthHTTP{timeout: 20 * time.Millisecond, retry: 7, slots: make(chan struct{}, 2)}
	h.probe = func(ctx context.Context) bool { <-ctx.Done(); return true }
	w := httptest.NewRecorder()
	start := time.Now()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/health", nil))
	if w.Code != 503 || w.Header().Get("Retry-After") != "7" || strings.TrimSpace(w.Body.String()) != `{"ready":false}` || time.Since(start) > time.Second || len(h.slots) != 0 {
		t.Fatal(w.Code, w.Body)
	}
	h.probe = func(context.Context) bool { t.Fatal("saturated probe executed"); return true }
	h.slots <- struct{}{}
	h.slots <- struct{}{}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/health", nil))
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
	<-h.slots
	<-h.slots
	h.probe = func(context.Context) bool { return true }
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/health", nil))
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"ready":true}` || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/health", nil))
	if w.Code != 405 || w.Header().Get("Allow") != "GET" {
		t.Fatal(w.Code)
	}
	if _, err := NewHealthHTTP(nil, time.Second, 5); err == nil {
		t.Fatal("nil pool")
	}
}

func TestMySQLHealthHasIndependentReadOnlyPool(t *testing.T) {
	db := identityStore(t)
	var name string
	if err := db.DB.QueryRow("SELECT DATABASE()").Scan(&name); err != nil {
		t.Fatal(err)
	}
	c := config.Database{Type: "mysql", Host: os.Getenv("TIMEWEAVER_TEST_MYSQL_HOST"), Port: 3306, User: "root", Password: os.Getenv("TIMEWEAVER_TEST_MYSQL_PASSWORD"), Name: name}
	probe, err := store.Open(context.Background(), c, true)
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()
	h, err := NewHealthHTTP(probe, 250*time.Millisecond, 5)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = NewHealthHTTP(db, time.Second, 5); err == nil {
		t.Fatal("writable health pool accepted")
	}
	db.DB.SetMaxOpenConns(1)
	busy, err := db.DB.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	check := func(code int) {
		t.Helper()
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/health", nil))
		if w.Code != code {
			t.Fatal(w.Code, w.Body)
		}
	}
	check(200) // No business connection is available; probe must still work.
	busy.Close()
	conn1, err := probe.DB.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn1.Close()
	conn2, err := probe.DB.Conn(context.Background())
	if err != nil {
		conn1.Close()
		t.Fatal(err)
	}
	defer conn2.Close()
	check(503) // Pool acquisition is bounded by the same health deadline.
	conn1.Close()
	conn2.Close()
	check(200)
	if _, err = probe.DB.Exec("DELETE FROM migrations"); err == nil {
		t.Fatal("probe connection can write")
	}
	if _, err = db.DB.Exec("DROP TABLE agent_event"); err != nil {
		t.Fatal(err)
	}
	check(503)
	probe.Close()
	check(503)
}

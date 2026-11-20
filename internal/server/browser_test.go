package server

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/horrible-gh/TimeWeaver/internal/access"
	"github.com/horrible-gh/TimeWeaver/internal/config"
	"github.com/horrible-gh/TimeWeaver/internal/store"
	"github.com/horrible-gh/TimeWeaver/internal/webui"
)

func TestBrowserSessionFlow(t *testing.T) {
	if os.Getenv("TIMEWEAVER_BROWSER_ACCESS") != "1" {
		t.Skip("requires installed browser and explicit disposable DB permission")
	}
	db, probe, database := serverDatabaseWithConfig(t)
	ctx := context.Background()
	password := "browser-migration-password"
	salt := []byte("browser-fixture-salt")
	key, err := pbkdf2.Key(sha256.New, password, salt, 29000, 32)
	if err != nil {
		t.Fatal(err)
	}
	ab64 := func(b []byte) string { return strings.ReplaceAll(base64.RawStdEncoding.EncodeToString(b), "+", ".") }
	hash := "$pbkdf2-sha256$29000$" + ab64(salt) + "$" + ab64(key)
	if _, err = fixtureExec(t, db, "INSERT INTO users(user_id,name,password,role,group_id) VALUES('browser','Browser Tester',?,'user',0)", hash); err != nil {
		t.Fatal(err)
	}
	if _, err = db.RepairCriticalSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = access.Initialize(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err = fixtureExec(t, db, "UPDATE tw_user_auth SET must_change_password=1 WHERE user_id='browser'"); err != nil {
		t.Fatal(err)
	}
	settings := config.Settings{Context: "/timeweaver_svr", SecretKey: "browser-only-0123456789abcdef0123456789abcdef", HTTP: config.DefaultHTTPSettings(), IdleTimeout: 30 * time.Minute}
	ui, err := webui.Bundled(settings.Context)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	origin := "http://" + ln.Addr().String()
	f := &browserSessionFixture{db: db, probe: probe, database: database, settings: settings, ui: ui, origin: origin, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if err = f.start(ln); err != nil {
		ln.Close()
		t.Fatal(err)
	}
	defer func() {
		if err := f.stop(); err != nil {
			t.Error(err)
		}
		f.db.Close()
		f.probe.Close()
	}()
	// This separate, secret-protected test listener is deliberately absent from
	// production. It advances disposable fixture rows and restarts both HTTP and
	// database pools without timing sleeps or exposing a product testing endpoint.
	controlKey := rand.Text()
	control := httptest.NewServer(f.control(controlKey))
	defer control.Close()
	deadline, stop := context.WithTimeout(ctx, 180*time.Second)
	defer stop()
	script := filepath.Join("..", "..", "scripts", "test-browser-access.py")
	cmd := exec.CommandContext(deadline, "python", script)
	input, _ := json.Marshal(map[string]string{"origin": origin, "context": settings.Context, "user": "browser", "password": password, "control_origin": control.URL, "control_key": controlKey})
	cmd.Stdin = strings.NewReader(string(input))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("browser session test failed: %v\n%s", err, out)
	}
	t.Log(string(out))
}

type browserSessionFixture struct {
	db, probe *store.Store
	database  config.Database
	settings  config.Settings
	ui        http.Handler
	origin    string
	logger    *slog.Logger
	mu        sync.Mutex
	cancel    context.CancelFunc
	done      chan error
	loseNext  atomic.Bool
	lost      atomic.Int64
}

func (f *browserSessionFixture) start(ln net.Listener) error {
	app, err := NewBrowserSurface(context.Background(), f.db, f.probe, f.settings, f.ui, f.logger, access.HTTPOptions{Origin: f.origin})
	if err != nil {
		return err
	}
	transport := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSuffix(r.URL.Path, "/") != f.settings.Context+"/auth/refresh" {
			app.ServeHTTP(w, r)
			return
		}
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, r)
		// Lose the response only after the renewal transaction committed. In
		// particular, do not deliver its Set-Cookie to the browser.
		if rec.Code == http.StatusOK && f.loseNext.CompareAndSwap(true, false) {
			f.lost.Add(1)
			w.Header().Set("Retry-After", "1")
			jsonReply(w, r, http.StatusServiceUnavailable, map[string]any{"detail": map[string]string{"code": "unavailable", "message": "test transport interruption"}})
			return
		}
		for name, values := range rec.Header() {
			for _, value := range values {
				w.Header().Add(name, value)
			}
		}
		w.WriteHeader(rec.Code)
		_, _ = w.Write(rec.Body.Bytes())
	})
	run, cancel := context.WithCancel(context.Background())
	f.cancel, f.done = cancel, make(chan error, 1)
	done := f.done
	go func() {
		done <- serve(run, ln, transport, app.sweeper, 10*time.Second, f.logger, func() { app.draining.Store(true) })
	}()
	return nil
}

func (f *browserSessionFixture) stop() error {
	if f.cancel == nil {
		return nil
	}
	f.cancel()
	select {
	case err := <-f.done:
		f.cancel = nil
		return err
	case <-time.After(15 * time.Second):
		return errors.New("browser session fixture did not stop")
	}
}

func (f *browserSessionFixture) restart() error {
	if err := f.stop(); err != nil {
		return err
	}
	if err := errors.Join(f.db.Close(), f.probe.Close()); err != nil {
		return err
	}
	db, err := store.Open(context.Background(), f.database, false)
	if err != nil {
		return err
	}
	probe, err := store.Open(context.Background(), f.database, true)
	if err != nil {
		db.Close()
		return err
	}
	f.db, f.probe = db, probe
	ln, err := net.Listen("tcp", strings.TrimPrefix(f.origin, "http://"))
	if err != nil {
		return err
	}
	if err := f.start(ln); err != nil {
		ln.Close()
		return err
	}
	return nil
}

func (f *browserSessionFixture) control(key string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("X-Fixture-Key") != key {
			http.NotFound(w, r)
			return
		}
		var in struct{ Action, Cookie string }
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&in); err != nil {
			http.Error(w, "invalid control request", http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		var err error
		if in.Action == "restart" {
			err = f.restart()
		} else if in.Action != "status" {
			sum := sha256.Sum256([]byte(in.Cookie))
			credential := hex.EncodeToString(sum[:])
			now := time.Now().Unix()
			var query string
			var args []any
			switch in.Action {
			case "due", "lose-refresh":
				query, args = "UPDATE tw_session SET rotated_at=?,last_seen=? WHERE token_digest=?", []any{now - 13*3600, now - 2*24*3600, credential}
			case "idle":
				query, args = "UPDATE tw_session SET last_seen=? WHERE token_digest=?", []any{now - 2*3600, credential}
			case "expire":
				query, args = "UPDATE tw_session SET expires_at=? WHERE token_digest=?", []any{now - 1, credential}
			default:
				err = errors.New("unknown fixture action")
			}
			if err == nil {
				var changed int64
				result, updateErr := f.db.DB.ExecContext(r.Context(), query, args...)
				err = updateErr
				if err == nil {
					changed, err = result.RowsAffected()
					if err == nil && changed != 1 {
						err = fmt.Errorf("fixture action changed %d sessions", changed)
					}
				}
			}
			if err == nil && in.Action == "lose-refresh" {
				f.loseNext.Store(true)
			}
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonReply(w, r, http.StatusOK, map[string]int64{"lost_refreshes": f.lost.Load()})
	})
}

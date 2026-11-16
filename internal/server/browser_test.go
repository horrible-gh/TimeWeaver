package server

import (
	"context"
	"crypto/pbkdf2"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/horrible-gh/TimeWeaver/internal/access"
	"github.com/horrible-gh/TimeWeaver/internal/config"
	"github.com/horrible-gh/TimeWeaver/internal/webui"
)

func TestBrowserSessionFlow(t *testing.T) {
	if os.Getenv("TIMEWEAVER_BROWSER_ACCESS") != "1" {
		t.Skip("requires installed browser and explicit disposable DB permission")
	}
	db, probe := serverDatabase(t)
	ctx := context.Background()
	password := "browser-migration-password"
	salt := []byte("browser-fixture-salt")
	key, err := pbkdf2.Key(sha256.New, password, salt, 29000, 32)
	if err != nil {
		t.Fatal(err)
	}
	ab64 := func(b []byte) string { return strings.ReplaceAll(base64.RawStdEncoding.EncodeToString(b), "+", ".") }
	hash := "$pbkdf2-sha256$29000$" + ab64(salt) + "$" + ab64(key)
	if _, err = db.DB.Exec("INSERT INTO users(user_id,name,password,role,group_id) VALUES('browser','Browser Tester',?,'user',0)", hash); err != nil {
		t.Fatal(err)
	}
	if _, err = db.RepairCriticalSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = access.Initialize(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err = db.DB.Exec("UPDATE tw_user_auth SET must_change_password=1 WHERE user_id='browser'"); err != nil {
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
	app, err := NewBrowserSurface(ctx, db, probe, settings, ui, slog.New(slog.NewTextHandler(io.Discard, nil)), access.HTTPOptions{Origin: origin})
	if err != nil {
		ln.Close()
		t.Fatal(err)
	}
	run, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- app.Serve(run, ln) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(15 * time.Second):
			t.Error("browser test server did not stop")
		}
	}()
	deadline, stop := context.WithTimeout(ctx, 70*time.Second)
	defer stop()
	script := filepath.Join("..", "..", "scripts", "test-browser-access.py")
	cmd := exec.CommandContext(deadline, "python", script)
	input, _ := json.Marshal(map[string]string{"origin": origin, "context": settings.Context, "user": "browser", "password": password})
	cmd.Stdin = strings.NewReader(string(input))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("browser session test failed: %v\n%s", err, out)
	}
	t.Log(string(out))
}

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

func TestBrowserAdministrationFlow(t *testing.T) {
	if os.Getenv("TIMEWEAVER_BROWSER_ACCESS") != "1" {
		t.Skip("requires browser and disposable loopback DB")
	}
	db, probe := serverDatabase(t)
	ctx := context.Background()
	password := "browser-admin-password"
	salt := []byte("admin-browser-salt")
	key, err := pbkdf2.Key(sha256.New, password, salt, 29000, 32)
	if err != nil {
		t.Fatal(err)
	}
	ab64 := func(b []byte) string { return strings.ReplaceAll(base64.RawStdEncoding.EncodeToString(b), "+", ".") }
	hash := "$pbkdf2-sha256$29000$" + ab64(salt) + "$" + ab64(key)
	for _, q := range []string{"INSERT INTO `groups`(group_id,group_name) VALUES(7,'Seven'),(8,'Eight')", "INSERT INTO devices(group_id,device_name) VALUES(7,'device-seven'),(8,'device-eight')"} {
		if _, err = fixtureExec(t, db, q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = fixtureExec(t, db, "INSERT INTO users(user_id,name,password,role,group_id) VALUES('browser-admin','Browser Admin',?,'admin',0)", hash); err != nil {
		t.Fatal(err)
	}
	if _, err = db.RepairCriticalSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = access.Initialize(ctx, db); err != nil {
		t.Fatal(err)
	}
	settings := config.Settings{Context: "/timeweaver_svr", SecretKey: "admin-browser-0123456789abcdef0123456789abcdef", HTTP: config.DefaultHTTPSettings(), IdleTimeout: 30 * time.Minute}
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
			t.Error("admin browser server did not stop")
		}
	}()
	timeout, stop := context.WithTimeout(ctx, 100*time.Second)
	defer stop()
	cmd := exec.CommandContext(timeout, "python", filepath.Join("..", "..", "scripts", "test-browser-administration.py"))
	input, _ := json.Marshal(map[string]string{"origin": origin, "context": settings.Context, "user": "browser-admin", "password": password})
	cmd.Stdin = strings.NewReader(string(input))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("administration browser failed: %v\n%s", err, out)
	}
	t.Log(string(out))
	var actor, target string
	if fixtureRow(t, db, "SELECT actor,target FROM tw_audit WHERE action='user_create' ORDER BY id DESC LIMIT 1").Scan(&actor, &target) != nil || actor != "browser-admin" || target != "browser-limited" {
		t.Fatal("administrator audit identity")
	}
}

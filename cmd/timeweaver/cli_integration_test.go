package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/horrible-gh/TimeWeaver/internal/access"
	"github.com/horrible-gh/TimeWeaver/internal/config"
	"github.com/horrible-gh/TimeWeaver/internal/store"
	"github.com/horrible-gh/TimeWeaver/internal/webui"
)

// Package tests alone cannot prove that the installed executable starts the
// composed server. Exercise its real command dispatch, database ownership,
// embedded assets and persisted credentials on both native CI operating systems.
func TestCLIExecutableServerFlow(t *testing.T) {
	if _, err := webui.Bundled("/cli_api"); err != nil {
		if os.Getenv("TW_REQUIRE_EMBEDDED_UI") == "1" {
			t.Fatal(err)
		}
		t.Skip("build the embedded UI to exercise the installed server executable")
	}
	bin := filepath.Join(t.TempDir(), "timeweaver")
	goTool := filepath.Join(runtime.GOROOT(), "bin", "go")
	if runtime.GOOS == "windows" {
		bin += ".exe"
		goTool += ".exe"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	build := exec.CommandContext(ctx, goTool, "build", "-trimpath", "-ldflags", "-X main.version=cli-test -X main.commit=executable-fixture", "-o", bin, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+runtime.GOOS, "GOARCH="+runtime.GOARCH)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build executable: %v\n%s", err, out)
	}

	t.Run("readiness refuses missing and incomplete databases", func(t *testing.T) {
		f := newCLIExecutableFixture(t, bin)
		f.command(t, "", 0, "check", "--env", f.file)
		f.command(t, "", 1, "check", "--database", "--env", f.file)
		f.command(t, "", 1, "serve", "--env", f.file)
		f.command(t, "", 2, "setup", "--env", f.file)
		if _, err := os.Stat(f.database); !os.IsNotExist(err) {
			t.Fatal("configuration/readiness/startup created a missing SQLite database")
		}
		f.command(t, "", 0, "migrate", "up", "--allow-write", "--env", f.file)
		f.command(t, "", 1, "serve", "--env", f.file)
		f.command(t, "", 0, "access", "init", "--allow-write", "--env", f.file)
		f.command(t, "", 1, "check", "--database", "--env", f.file)
		out := f.command(t, "", 1, "serve", "--env", f.file)
		if !strings.Contains(strings.ToLower(out), "password") {
			t.Fatal("startup did not explain the unchanged bootstrap password")
		}
		f.command(t, f.password+"\n", 0, "setup", "--allow-write", "--admin-password-stdin", "--env", f.file)
		f.assertReady(t)
	})

	t.Run("fresh setup serves browser and agent APIs and survives restart", func(t *testing.T) {
		f := newCLIExecutableFixture(t, bin)
		out := f.command(t, "", 0, "version")
		if !strings.Contains(out, "cli-test") || !strings.Contains(out, "executable-fixture") || strings.Contains(out, "not a production server") {
			t.Fatalf("incorrect installed version metadata: %s", out)
		}
		f.command(t, f.password+"\n", 0, "setup", "--allow-write", "--admin-password-stdin", "--env", f.file)
		// Re-running preparation must preserve established credentials and finish
		// without asking for another password or silently resetting the account.
		f.command(t, "", 0, "setup", "--allow-write", "--env", f.file)
		f.assertReady(t)
		process := f.start(t)
		for _, path := range []string{"/", "/dashboard/schedules"} {
			resp, body := f.request(t, http.MethodGet, path, "", "", nil)
			if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") || !bytes.Contains(body, []byte(`<meta name="tw-api-context" content="/cli_api">`)) {
				t.Fatalf("embedded UI/context not served at %s: HTTP %d", path, resp.StatusCode)
			}
			if path == "/" {
				asset := regexp.MustCompile(`<script[^>]+src="(/[^\"]+\.js)"`).FindSubmatch(body)
				if len(asset) != 2 {
					t.Fatal("embedded entry has no local executable JavaScript asset")
				}
				assetResp, assetBody := f.request(t, http.MethodGet, string(asset[1]), "", "", nil)
				if assetResp.StatusCode != 200 || len(assetBody) == 0 || !strings.Contains(assetResp.Header.Get("Content-Type"), "javascript") {
					t.Fatal("embedded JavaScript asset was not served")
				}
			}
		}
		resp, _ := f.request(t, http.MethodGet, "/cli_api/dashboard/devices/get_devices", "", "", nil)
		if resp.StatusCode != 401 {
			t.Fatalf("anonymous dashboard read: HTTP %d", resp.StatusCode)
		}
		resp, _ = f.request(t, http.MethodGet, "/cli_api/api/agent/v1/snapshot", "", "", nil)
		if resp.StatusCode != 401 {
			t.Fatalf("anonymous agent snapshot: HTTP %d", resp.StatusCode)
		}
		form := url.Values{"username": {"timeweaver"}, "password": {f.password}, "remember": {"false"}}
		resp, body := f.request(t, http.MethodPost, "/cli_api/login", form.Encode(), "application/x-www-form-urlencoded", nil)
		var ordinary access.Principal
		if resp.StatusCode != 200 || json.Unmarshal(body, &ordinary) != nil || ordinary.Session.Remembered {
			t.Fatalf("ordinary administrator login: HTTP %d", resp.StatusCode)
		}
		remaining := time.Until(time.Unix(ordinary.Session.ExpiresAt, 0))
		if remaining < 2*time.Hour-time.Minute || remaining > 2*time.Hour {
			t.Fatalf("SESSION_ABSOLUTE_HOURS=2 was not applied by the executable: remaining %s", remaining)
		}
		f.secrets = append(f.secrets, ordinary.CSRF)
		for _, cookie := range resp.Cookies() {
			f.secrets = append(f.secrets, cookie.Value)
		}
		form.Set("remember", "true")
		resp, body = f.request(t, http.MethodPost, "/cli_api/login", form.Encode(), "application/x-www-form-urlencoded", nil)
		if resp.StatusCode != 200 {
			t.Fatalf("bootstrap administrator login: HTTP %d", resp.StatusCode)
		}
		var principal access.Principal
		if err := json.Unmarshal(body, &principal); err != nil || principal.User.ID != "timeweaver" || principal.User.Role != "admin" || principal.User.MustChangePassword || principal.CSRF == "" || !principal.Session.Remembered {
			t.Fatal("bootstrap administrator did not receive an unrestricted remembered session")
		}
		remaining = time.Until(time.Unix(principal.Session.ExpiresAt, 0))
		if remaining < 30*24*time.Hour-time.Minute || remaining > 30*24*time.Hour {
			t.Fatalf("ordinary session configuration changed the remembered-session lifetime: remaining %s", remaining)
		}
		if len(resp.Cookies()) != 1 || !resp.Cookies()[0].HttpOnly || resp.Cookies()[0].Path != "/cli_api/" || resp.Cookies()[0].MaxAge <= 0 {
			t.Fatal("remembered browser cookie lacks expected scope or persistence")
		}
		f.secrets = append(f.secrets, resp.Cookies()[0].Value, principal.CSRF)
		resp, _ = f.request(t, http.MethodGet, "/cli_api/dashboard/devices/get_devices", "", "", nil)
		if resp.StatusCode != 200 {
			t.Fatalf("authenticated dashboard read: HTTP %d", resp.StatusCode)
		}
		resp, body = f.request(t, http.MethodPost, "/cli_api/dashboard/agent-enrollment-tokens", `{"group_id":0,"device_name":"cli-agent","ttl_hours":1}`, "application/json", map[string]string{"X-CSRF-Token": principal.CSRF})
		var enrollment struct {
			Data struct{ Token string }
		}
		if resp.StatusCode != 201 || json.Unmarshal(body, &enrollment) != nil || enrollment.Data.Token == "" {
			t.Fatalf("administrator enrollment issuance: HTTP %d", resp.StatusCode)
		}
		f.secrets = append(f.secrets, enrollment.Data.Token)
		enrollBody, _ := json.Marshal(map[string]string{"enrollment_token": enrollment.Data.Token, "device_name": "cli-agent", "agent_version": "cli-test"})
		resp, body = f.request(t, http.MethodPost, "/cli_api/api/agent/v1/enroll", string(enrollBody), "application/json", nil)
		var credentials struct {
			Data struct {
				DeviceID     int64  `json:"device_id"`
				AccessToken  string `json:"access_token"`
				RefreshToken string `json:"refresh_token"`
			}
		}
		if resp.StatusCode != 200 || json.Unmarshal(body, &credentials) != nil || credentials.Data.DeviceID == 0 || credentials.Data.AccessToken == "" || credentials.Data.RefreshToken == "" {
			t.Fatalf("agent enrollment: HTTP %d", resp.StatusCode)
		}
		f.secrets = append(f.secrets, credentials.Data.AccessToken, credentials.Data.RefreshToken)
		resp, _ = f.request(t, http.MethodGet, "/cli_api/api/agent/v1/snapshot", "", "", map[string]string{"Authorization": "Bearer " + credentials.Data.AccessToken})
		if resp.StatusCode != 200 {
			t.Fatalf("enrolled agent snapshot: HTTP %d", resp.StatusCode)
		}
		process.stop(t)
		f.assertRedacted(t, process.output.String())
		// Simulate an established installation from just before the newest
		// additive migration. Read-only checks must not apply it; routine
		// serve must apply it without setup and preserve both credentials.
		db, err := store.OpenExisting(context.Background(), config.Database{Type: "sqlite", Path: f.database})
		if err != nil {
			t.Fatal(err)
		}
		for _, q := range []string{
			"ALTER TABLE task_detail DROP COLUMN house_keep_delete_empty_dirs",
			"DELETE FROM migrations WHERE filename='go_sqlite_002.sql'",
			"DELETE FROM tw_migration_attempt WHERE filename='go_sqlite_002.sql'",
		} {
			if _, err = db.DB.Exec(q); err != nil {
				db.Close()
				t.Fatal(err)
			}
		}
		db.Close()
		f.command(t, "", 1, "check", "--database", "--env", f.file)
		f.command(t, "", 1, "serve", "--no-auto-migrate", "--env", f.file)
		f.command(t, "", 1, "check", "--database", "--env", f.file)
		process = f.start(t)
		if !strings.Contains(process.output.String(), "database migration complete") {
			t.Fatal("routine startup did not report its automatic migration")
		}
		f.assertReady(t)
		resp, _ = f.request(t, http.MethodGet, "/cli_api/auth/me", "", "", nil)
		if resp.StatusCode != 200 {
			t.Fatalf("browser session after restart: HTTP %d", resp.StatusCode)
		}
		resp, _ = f.request(t, http.MethodGet, "/cli_api/api/agent/v1/snapshot", "", "", map[string]string{"Authorization": "Bearer " + credentials.Data.AccessToken})
		if resp.StatusCode != 200 {
			t.Fatalf("agent credential after restart: HTTP %d", resp.StatusCode)
		}
		resp, _ = f.request(t, http.MethodPost, "/cli_api/logout", "", "", map[string]string{"X-CSRF-Token": principal.CSRF})
		if resp.StatusCode != 200 {
			t.Fatalf("browser logout: HTTP %d", resp.StatusCode)
		}
		resp, _ = f.request(t, http.MethodGet, "/cli_api/auth/me", "", "", nil)
		if resp.StatusCode != 401 {
			t.Fatalf("browser session survived logout: HTTP %d", resp.StatusCode)
		}
		process.stop(t)
		f.assertRedacted(t, process.output.String())
	})
}

type cliExecutableFixture struct {
	binary, file, database, origin, password string
	env                                      []string
	secrets                                  []string
	client                                   *http.Client
}

func newCLIExecutableFixture(t *testing.T, binary string) *cliExecutableFixture {
	t.Helper()
	dir := t.TempDir()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	f := &cliExecutableFixture{binary: binary, file: filepath.Join(dir, "server.env"), database: filepath.Join(dir, "server.sqlite3"), origin: fmt.Sprintf("http://127.0.0.1:%d", port), password: "cli-owned-administrator-password"}
	secret := "cli-only-secret-0123456789abcdef0123456789abcdef"
	raw := fmt.Sprintf("DB_TYPE=sqlite3\nDB_PATH=./server.sqlite3\nSECRET_KEY=%s\nCONTEXT=/cli_api\nLISTEN_HOST=127.0.0.1\nLISTEN_PORT=%d\nPUBLIC_ORIGIN=%s\nOS_LOGIN_ENABLED=false\nSESSION_ABSOLUTE_HOURS=2\n", secret, port, f.origin)
	if err = os.WriteFile(f.file, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	// Process values override dotenv, so remove ambient application settings to
	// prove that this explicit fixture file is what the executable actually uses.
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		key = strings.ToUpper(key)
		switch key {
		case "SECRET_KEY", "AGENT_SECRET_KEY", "CONTEXT", "LISTEN_HOST", "LISTEN_PORT", "PUBLIC_ORIGIN", "OS_LOGIN_ENABLED", "TRUSTED_PROXIES", "ACCESS_TOKEN_EXPIRE_MINUTES", "SESSION_ABSOLUTE_HOURS", "HEALTH_RETRY_AFTER":
			continue
		}
		if strings.HasPrefix(key, "DB_") || strings.HasPrefix(key, "RATE_LIMIT_") || strings.HasPrefix(key, "BLOCKING_DB_") {
			continue
		}
		f.env = append(f.env, item)
	}
	f.secrets = []string{secret, f.password}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{Proxy: nil}
	t.Cleanup(transport.CloseIdleConnections)
	f.client = &http.Client{Timeout: 5 * time.Second, Transport: transport, Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return f
}

func (f *cliExecutableFixture) command(t *testing.T, stdin string, want int, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, f.binary, args...)
	cmd.Env = f.env
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	f.assertRedacted(t, string(out))
	code := 0
	if err != nil {
		code = -1
		if cmd.ProcessState != nil {
			code = cmd.ProcessState.ExitCode()
		}
	}
	if code != want {
		t.Fatalf("%v: exit %d, want %d: %s", args, code, want, out)
	}
	return string(out)
}

func (f *cliExecutableFixture) assertReady(t *testing.T) {
	t.Helper()
	out := f.command(t, "", 0, "check", "--database", "--env", f.file)
	var readiness struct {
		Scope             string `json:"scope"`
		Ready             bool   `json:"ready"`
		DatabaseConnected bool   `json:"database_connected"`
		EmbeddedUIReady   bool   `json:"embedded_ui_ready"`
		ServerImplemented bool   `json:"server_implemented"`
	}
	if err := json.Unmarshal([]byte(out), &readiness); err != nil || readiness.Scope != "runtime-readiness" || !readiness.Ready || !readiness.DatabaseConnected || !readiness.EmbeddedUIReady || !readiness.ServerImplemented {
		t.Fatalf("prepared database did not report complete runtime readiness: %s", out)
	}
}

func (f *cliExecutableFixture) assertRedacted(t *testing.T, output string) {
	t.Helper()
	for _, secret := range f.secrets {
		if secret != "" && strings.Contains(output, secret) {
			t.Fatal("command output exposed a credential")
		}
	}
}

func (f *cliExecutableFixture) request(t *testing.T, method, path, body, contentType string, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, f.origin+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", f.origin)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return resp, data
}

type cliExecutableProcess struct {
	cmd     *exec.Cmd
	output  *cliExecutableOutput
	done    chan error
	stopped bool
}

type cliExecutableOutput struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (o *cliExecutableOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.b.Write(p)
}
func (o *cliExecutableOutput) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.b.String()
}

func (f *cliExecutableFixture) start(t *testing.T) *cliExecutableProcess {
	t.Helper()
	p := &cliExecutableProcess{cmd: exec.Command(f.binary, "serve", "--env", f.file), output: &cliExecutableOutput{}, done: make(chan error, 1)}
	p.cmd.Env = f.env
	p.cmd.Stdout, p.cmd.Stderr = p.output, p.output
	if err := p.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { p.done <- p.cmd.Wait() }()
	t.Cleanup(func() {
		if !p.stopped {
			p.cmd.Process.Kill()
			select {
			case <-p.done:
			case <-time.After(10 * time.Second):
				t.Error("server process did not stop after forced test cleanup")
			}
			p.stopped = true
		}
		f.assertRedacted(t, p.output.String())
	})
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-p.done:
			p.stopped = true
			t.Fatalf("server exited before readiness: %v\n%s", err, p.output.String())
		case <-deadline.C:
			t.Fatalf("server did not become ready: %s", p.output.String())
		case <-ticker.C:
			resp, err := f.client.Get(f.origin + "/cli_api/api/agent/v1/health")
			if err != nil {
				continue
			}
			var health struct{ Ready bool }
			decodeErr := json.NewDecoder(resp.Body).Decode(&health)
			resp.Body.Close()
			if resp.StatusCode == 200 && decodeErr == nil && health.Ready {
				return p
			}
		}
	}
}

func (p *cliExecutableProcess) stop(t *testing.T) {
	t.Helper()
	if p.stopped {
		t.Fatal("test attempted to stop an already exited server")
	}
	var err error
	if runtime.GOOS == "windows" {
		// os.Process.Signal(os.Interrupt) is unsupported on Windows. Native
		// cancellation/draining is covered in internal/server; here verify a
		// process restart recovers its SQLite WAL and persistent credentials.
		err = p.cmd.Process.Kill()
	} else {
		err = p.cmd.Process.Signal(os.Interrupt)
	}
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-p.done:
		p.stopped = true
		if runtime.GOOS != "windows" && err != nil {
			t.Fatalf("graceful server exit: %v\n%s", err, p.output.String())
		}
	case <-time.After(20 * time.Second):
		t.Fatal("server did not release its listener and database after stop")
	}
}

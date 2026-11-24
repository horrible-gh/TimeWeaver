package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/horrible-gh/TimeWeaver/internal/access"
	"github.com/horrible-gh/TimeWeaver/internal/config"
	"github.com/horrible-gh/TimeWeaver/internal/store"
	"github.com/horrible-gh/TimeWeaver/internal/webui"
)

// Production Python installations can retain agent migration names long after
// the corresponding SQL files were retired. Exercise the actual CLI readiness
// and startup path with that history on both MySQL and MariaDB in CI.
func TestMySQLLegacyAgentHistorySetupCheckAndServe(t *testing.T) {
	if os.Getenv("TIMEWEAVER_TEST_MYSQL_ALLOW_CREATE") != "1" {
		t.Skip("requires an explicitly authorized disposable MySQL/MariaDB instance")
	}
	host := os.Getenv("TIMEWEAVER_TEST_MYSQL_HOST")
	ip, err := netip.ParseAddr(host)
	if err != nil || !ip.IsLoopback() {
		t.Fatal("only a loopback disposable database is allowed")
	}
	if _, err = webui.Bundled("/timeweaver_svr"); err != nil {
		t.Fatal("the database CLI integration requires the compiled UI", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	c := mysql.NewConfig()
	c.User, c.Passwd = "root", os.Getenv("TIMEWEAVER_TEST_MYSQL_PASSWORD")
	c.Net, c.Addr, c.ParseTime = "tcp", net.JoinHostPort(host, "3306"), true
	c.Timeout = 5 * time.Second
	connector, err := mysql.NewConnector(c)
	if err != nil {
		t.Fatal(err)
	}
	admin := sql.OpenDB(connector)
	digest := sha256.Sum256([]byte(t.TempDir()))
	databaseName := "tw_cli_legacy_" + hex.EncodeToString(digest[:12])
	if _, err = admin.ExecContext(ctx, "CREATE DATABASE `"+databaseName+"` CHARACTER SET utf8mb4"); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Exec("DROP DATABASE `" + databaseName + "`"); admin.Close() })
	database := config.Database{Type: "mysql", Host: host, Port: 3306, User: c.User, Password: c.Passwd, Name: databaseName}
	db, err := store.Open(ctx, database, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	// Model Python's filename-only journal, rather than a fresh Go database.
	if _, err = db.DB.ExecContext(ctx, "DROP TABLE tw_migration_attempt"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"devices_001.sql", "devices_002.sql", "devices_003.sql",
		"execution_log_001.sql", "execution_log_002.sql", "execution_log_003.sql",
		"manual_execution_001.sql", "manual_execution_002.sql", "manual_execution_003.sql",
		"schedule_detail_001.sql", "schedule_detail_002.sql",
		"schedule_group_001.sql", "schedule_group_002.sql",
	} {
		if _, err = db.DB.ExecContext(ctx, "INSERT INTO migrations(filename,applied_at) VALUES(?,'2025-01-02 03:04:05')", name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.DB.ExecContext(ctx, "UPDATE users SET name='Legacy owner' WHERE user_id='timeweaver'"); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	origin := "http://127.0.0.1:" + strconv.Itoa(port)
	file := filepath.Join(t.TempDir(), "server.env")
	if err = os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{
		"SECRET_KEY": "cli-legacy-only-0123456789abcdef0123456789abcdef", "AGENT_SECRET_KEY": "",
		"CONTEXT": "/timeweaver_svr", "LISTEN_HOST": "127.0.0.1", "LISTEN_PORT": strconv.Itoa(port),
		"PUBLIC_ORIGIN": origin, "TRUSTED_PROXIES": "", "OS_LOGIN_ENABLED": "false",
		"ACCESS_TOKEN_EXPIRE_MINUTES": "30", "SESSION_ABSOLUTE_HOURS": "12",
		"DB_TYPE": "mysql", "DB_HOST": host, "DB_PORT": "3306", "DB_USER": c.User,
		"DB_PASSWORD": c.Passwd, "DB_DATABASE": databaseName, "DB_SCHEMA": "",
	} {
		t.Setenv(key, value)
	}
	const password = "legacy-cli-owned-administrator-123"
	var out, diagnostics bytes.Buffer
	if code := runSetupInput([]string{"--env", file, "--allow-write", "--admin-password-stdin"}, strings.NewReader(password+"\n"), &out, &diagnostics); code != 0 {
		t.Fatalf("setup rejected retired Python history: exit %d: %s", code, &diagnostics)
	}
	out.Reset()
	diagnostics.Reset()
	if code := run([]string{"check", "--database", "--env", file}, &out, &diagnostics); code != 0 {
		t.Fatalf("readiness rejected prepared legacy history: exit %d: %s", code, &diagnostics)
	}
	var report struct {
		Ready    bool            `json:"ready"`
		Database readinessReport `json:"database"`
	}
	if err = json.Unmarshal(out.Bytes(), &report); err != nil || !report.Ready || !report.Database.Ready || report.Database.Migrations == nil || report.Database.Migrations.Blocked || report.Database.Migrations.Pending != 0 || len(report.Database.Migrations.Unknown) != 0 {
		t.Fatalf("legacy installation was not ready: %s", &out)
	}
	// An existing, configured MariaDB/MySQL installation from before the
	// latest additive migration must upgrade on serve, not require setup.
	for _, query := range []string{
		"ALTER TABLE task_detail DROP COLUMN house_keep_delete_empty_dirs",
		"DELETE FROM migrations WHERE filename='timeweaver_server_016.sql'",
	} {
		if _, err = db.DB.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.DB.ExecContext(ctx, "INSERT INTO task_detail(detail_id,command) VALUES(?,?)", []byte("0123456789abcdef"), "preserved task"); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	diagnostics.Reset()
	if code := run([]string{"check", "--database", "--env", file}, &out, &diagnostics); code != 1 {
		t.Fatal("read-only check must report a pending migration", code)
	}
	out.Reset()
	diagnostics.Reset()
	if code := run([]string{"serve", "--no-auto-migrate", "--env", file}, &out, &diagnostics); code != 1 {
		t.Fatal("automatic migration opt-out was ignored", code)
	}
	runContext, stop := context.WithCancel(ctx)
	done := make(chan int, 1)
	logs := &cliExecutableOutput{}
	stopped := false
	go func() { done <- runServeContext(runContext, []string{"--env", file}, logs, logs) }()
	t.Cleanup(func() {
		stop()
		if !stopped {
			select {
			case <-done:
			case <-time.After(15 * time.Second):
				t.Error("legacy CLI server did not stop")
			}
		}
	})
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	ready := false
	for !ready {
		select {
		case code := <-done:
			stopped = true
			t.Fatalf("serve rejected legacy history: exit %d: %s", code, logs.String())
		case <-deadline.C:
			t.Fatalf("server did not become ready: %s", logs.String())
		case <-ticker.C:
			response, requestErr := client.Get(origin + "/timeweaver_svr/api/agent/v1/health")
			if requestErr != nil {
				continue
			}
			var health struct{ Ready bool }
			decodeErr := json.NewDecoder(response.Body).Decode(&health)
			response.Body.Close()
			ready = response.StatusCode == 200 && decodeErr == nil && health.Ready
		}
	}
	form := url.Values{"username": {"timeweaver"}, "password": {password}}
	request, err := http.NewRequest(http.MethodPost, origin+"/timeweaver_svr/login", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", origin)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var principal access.Principal
	decodeErr := json.NewDecoder(io.LimitReader(response.Body, 16384)).Decode(&principal)
	response.Body.Close()
	if response.StatusCode != 200 || decodeErr != nil || principal.User.ID != "timeweaver" || principal.User.Name != "Legacy owner" {
		t.Fatal("legacy installation cannot authenticate its preserved administrator", response.StatusCode, decodeErr)
	}
	stop()
	select {
	case code := <-done:
		stopped = true
		if code != 0 {
			t.Fatal("legacy CLI server failed to stop", code)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("legacy CLI server did not stop")
	}
	if !strings.Contains(logs.String(), "database migration complete") {
		t.Fatal("serve did not report automatic migration")
	}
	var rows, journals int
	if err = db.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM migrations WHERE applied_at='2025-01-02 03:04:05'").Scan(&rows); err != nil || rows != 13 {
		t.Fatal("setup/check/serve changed historical rows or timestamps", rows, err)
	}
	if err = db.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM tw_migration_attempt").Scan(&journals); err != nil || journals != 1 {
		t.Fatal("startup replayed previously applied migrations", journals, err)
	}
	var name, state, command string
	var flag int
	if err = db.DB.QueryRowContext(ctx, "SELECT filename,state FROM tw_migration_attempt").Scan(&name, &state); err != nil || name != "timeweaver_server_016.sql" || state != "applied" {
		t.Fatal("automatic migration journal incomplete", name, state, err)
	}
	if err = db.DB.QueryRowContext(ctx, "SELECT command,house_keep_delete_empty_dirs FROM task_detail WHERE detail_id=?", []byte("0123456789abcdef")).Scan(&command, &flag); err != nil || command != "preserved task" || flag != 0 {
		t.Fatal("automatic migration changed existing task data", command, flag, err)
	}
}

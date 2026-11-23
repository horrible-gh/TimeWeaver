package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/horrible-gh/TimeWeaver/internal/access"
	"github.com/horrible-gh/TimeWeaver/internal/config"
	"github.com/horrible-gh/TimeWeaver/internal/store"
)

func TestSQLiteReadinessTransitions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cfg := config.Database{Type: "sqlite3", Path: filepath.Join(t.TempDir(), "readiness.sqlite3")}
	db, err := store.Open(ctx, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	probe, err := store.Open(ctx, cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()
	state, err := inspectReadiness(ctx, probe)
	if err == nil || state.Ready || state.Migrations == nil || state.Migrations.Pending == 0 || state.Schema != nil {
		t.Fatalf("empty database readiness: %+v, %v", state, err)
	}
	if _, err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = db.DB.ExecContext(ctx, "UPDATE `groups` SET group_name='Wrong' WHERE group_id=0"); err != nil {
		t.Fatal(err)
	}
	state, err = inspectReadiness(ctx, probe)
	if err == nil || state.Ready || state.Schema == nil || len(state.Schema.Issues) == 0 || state.Access != nil {
		t.Fatalf("damaged critical schema readiness: %+v, %v", state, err)
	}
	if _, err = db.RepairCriticalSchema(ctx); err != nil {
		t.Fatal(err)
	}
	state, err = inspectReadiness(ctx, probe)
	if err == nil || state.Ready || state.Access == nil || state.Access.Ready {
		t.Fatalf("missing human access readiness: %+v, %v", state, err)
	}
	if _, err = access.Initialize(ctx, db); err != nil {
		t.Fatal(err)
	}
	state, err = inspectReadiness(ctx, probe)
	if err == nil || state.Ready || !state.BootstrapPassword {
		t.Fatalf("known initial password allowed startup: %+v, %v", state, err)
	}
	if err = access.BootstrapAdministrator(ctx, db, "readiness-fixture-owned-password"); err != nil {
		t.Fatal(err)
	}
	state, err = inspectReadiness(ctx, probe)
	if err != nil || !state.Ready || state.BootstrapPassword {
		t.Fatalf("prepared database refused: %+v, %v", state, err)
	}
	if _, err = db.DB.ExecContext(ctx, "INSERT INTO migrations(filename) VALUES('unknown-change.sql')"); err != nil {
		t.Fatal(err)
	}
	state, err = inspectReadiness(ctx, probe)
	if err == nil || state.Ready || !state.Migrations.Blocked {
		t.Fatalf("unknown migration allowed startup: %+v, %v", state, err)
	}
}

func TestCheckDatabaseMissingDoesNotCreateSQLite(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, ".env")
	if err := os.WriteFile(filename, nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SECRET_KEY", "readiness-cli-secret-0123456789abcdef0123456789")
	t.Setenv("AGENT_SECRET_KEY", "")
	t.Setenv("DB_TYPE", "sqlite3")
	t.Setenv("DB_PATH", "must-not-be-created.sqlite3")
	t.Setenv("CONTEXT", "/timeweaver_svr")
	t.Setenv("LISTEN_HOST", "127.0.0.1")
	t.Setenv("LISTEN_PORT", "8000")
	t.Setenv("PUBLIC_ORIGIN", "http://127.0.0.1:8000")
	t.Setenv("TRUSTED_PROXIES", "")
	var out, diag bytes.Buffer
	if code := run([]string{"check", "--database", "--env", filename}, &out, &diag); code != 1 {
		t.Fatalf("missing database readiness exit %d: %s", code, &diag)
	}
	var report map[string]any
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report["scope"] != "runtime-readiness" || report["ready"] != false || report["database_connected"] != false || report["server_implemented"] != true || report["read_only"] != true {
		t.Fatal("missing database reported as ready", report)
	}
	if _, err := os.Stat(filepath.Join(dir, "must-not-be-created.sqlite3")); !os.IsNotExist(err) {
		t.Fatal("readiness check created a database", err)
	}
	if strings.Contains(out.String()+diag.String(), os.Getenv("SECRET_KEY")) {
		t.Fatal("readiness check leaked a secret")
	}
}

func TestServeRejectsUnknownArgumentsBeforeReadingConfiguration(t *testing.T) {
	for _, args := range [][]string{{"serve", "unexpected"}, {"serve", "--allow-write"}} {
		var out, diag bytes.Buffer
		if code := run(args, &out, &diag); code != 2 || strings.Contains(diag.String(), "cannot read environment") {
			t.Fatalf("serve arguments accepted or validated too late: %v, %d %s", args, code, &diag)
		}
	}
	var out, diag bytes.Buffer
	if code := run([]string{"serve", "--help"}, &out, &diag); code != 0 || !strings.Contains(diag.String(), "env") {
		t.Fatalf("serve help unavailable: %d %s", code, &diag)
	}
}

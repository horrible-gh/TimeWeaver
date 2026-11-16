package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrationWriteRequiresOptIn(t *testing.T) {
	var out, diag bytes.Buffer
	if code := run([]string{"migrate", "up", "--env", "does-not-exist"}, &out, &diag); code != 2 || !strings.Contains(diag.String(), "--allow-write") {
		t.Fatal(code, &diag)
	}
	for _, args := range [][]string{{"migrate"}, {"migrate", "unknown"}, {"migrate", "status", "--allow-write"}} {
		if code := run(args, &out, &diag); code != 2 {
			t.Fatal(args, code)
		}
	}
}
func TestSQLiteMigrationCLIRefusesBeforeOpening(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, ".env")
	path := filepath.Join(dir, "must-not-exist.sqlite3")
	os.WriteFile(file, []byte("# test"), 0600)
	t.Setenv("DB_TYPE", "sqlite3")
	t.Setenv("DB_PATH", path)
	t.Setenv("SECRET_KEY", "migration-test-key-0123456789abcdef0123456789")
	t.Setenv("AGENT_SECRET_KEY", "")
	t.Setenv("CONTEXT", "/time_weaver")
	var out, diag bytes.Buffer
	if code := run([]string{"migrate", "up", "--allow-write", "--env", file}, &out, &diag); code != 2 || !strings.Contains(diag.String(), "SQLite") {
		t.Fatal(code, &diag)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("incomplete up created SQLite DB")
	}
}

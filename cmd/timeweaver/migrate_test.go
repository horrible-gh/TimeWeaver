package main

import (
	"bytes"
	"encoding/json"
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
func TestSQLiteCLIWriteAndReadBoundaries(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, ".env")
	path := filepath.Join(dir, "timeweaver.sqlite3")
	if err := os.WriteFile(file, []byte("# disposable CLI fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DB_TYPE", "sqlite3")
	t.Setenv("DB_PATH", path)
	t.Setenv("SECRET_KEY", "migration-test-key-0123456789abcdef0123456789")
	t.Setenv("AGENT_SECRET_KEY", "")
	t.Setenv("CONTEXT", "/time_weaver")
	var out, diag bytes.Buffer
	for _, command := range []string{"migrate", "schema", "access"} {
		out.Reset()
		diag.Reset()
		if code := run([]string{command, "status", "--env", file}, &out, &diag); code != 1 {
			t.Fatalf("missing-file status %s: %d %s", command, code, diag.String())
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("read-only status created the missing SQLite file")
		}
	}
	for _, command := range [][]string{{"migrate", "up"}, {"access", "init"}, {"schema", "repair"}} {
		out.Reset()
		diag.Reset()
		if code := run(append(command, "--env", file), &out, &diag); code != 2 {
			t.Fatalf("write without opt-in: %v %d", command, code)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("unapproved write opened the SQLite file")
		}
	}
	for _, command := range [][]string{
		{"migrate", "up", "--allow-write"},
		{"schema", "status"},
		{"access", "init", "--allow-write"},
		{"access", "status"},
		{"migrate", "up", "--allow-write"},
		{"migrate", "status"},
	} {
		out.Reset()
		diag.Reset()
		if code := run(append(command, "--env", file), &out, &diag); code != 0 {
			t.Fatalf("SQLite CLI %v: %d %s", command, code, diag.String())
		}
		var result map[string]any
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if command[0] == "access" && result["ready"] != true {
			t.Fatalf("access schema not ready: %s", out.String())
		}
		if command[0] == "migrate" && (result["dialect"] != "sqlite" || result["pending"] != float64(0) || result["blocked"] != false) {
			t.Fatalf("SQLite migration not complete: %s", out.String())
		}
	}
}

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionAndUnsupportedServe(t *testing.T) {
	var output, diagnostics bytes.Buffer
	if run([]string{"version"}, &output, &diagnostics) != 0 || !strings.Contains(output.String(), "migration build") {
		t.Fatal("version not explicit about readiness")
	}
	output.Reset()
	if run([]string{"serve"}, &output, &diagnostics) != 2 || output.Len() != 0 {
		t.Fatal("incomplete server must not start")
	}
	if run(nil, &output, &diagnostics) != 2 {
		t.Fatal("missing command accepted")
	}
	if run([]string{"version", "extra"}, &output, &diagnostics) != 2 {
		t.Fatal("unused argument accepted")
	}
}
func TestCheckIsReadOnlyAndRedacted(t *testing.T) {
	// Explicit env overrides make this independent of a CI runner's environment.
	t.Setenv("SECRET_KEY", "cli-test-secret-0123456789abcdef0123456789")
	t.Setenv("AGENT_SECRET_KEY", "")
	t.Setenv("DB_TYPE", "sqlite3")
	t.Setenv("DB_PATH", "never-created.sqlite3")
	t.Setenv("CONTEXT", "/timeweaver_svr")
	dir := t.TempDir()
	file := filepath.Join(dir, ".env")
	if err := os.WriteFile(file, []byte("# configuration only\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, diagnostics bytes.Buffer
	if code := run([]string{"check", "--env", file}, &out, &diagnostics); code != 0 {
		t.Fatalf("exit %d: %s", code, &diagnostics)
	}
	var report map[string]any
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report["scope"] != "configuration-only" || report["database_connected"] != false || report["server_implemented"] != false || report["read_only"] != true {
		t.Fatal("misleading check report")
	}
	if strings.Contains(out.String(), os.Getenv("SECRET_KEY")) {
		t.Fatal("secret in report")
	}
	if _, err := os.Stat(filepath.Join(dir, "never-created.sqlite3")); !os.IsNotExist(err) {
		t.Fatal("check created database")
	}
}
func TestCheckDoesNotLeakInvalidSecret(t *testing.T) {
	t.Setenv("SECRET_KEY", "private-short-value")
	filename := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(filename, nil, 0600); err != nil {
		t.Fatal(err)
	}
	var out, diagnostics bytes.Buffer
	if run([]string{"check", "--env", filename}, &out, &diagnostics) != 2 {
		t.Fatal("invalid secret accepted")
	}
	if strings.Contains(out.String()+diagnostics.String(), "private-short-value") {
		t.Fatal("secret printed")
	}
}

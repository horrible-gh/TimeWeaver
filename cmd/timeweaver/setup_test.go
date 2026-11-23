package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupRequiresApprovalBeforeOpeningDatabase(t *testing.T) {
	var out, diagnostics bytes.Buffer
	if code := runSetupInput([]string{"--env", "missing.env"}, strings.NewReader(""), &out, &diagnostics); code != 2 || !strings.Contains(diagnostics.String(), "--allow-write") || strings.Contains(diagnostics.String(), "cannot read environment") {
		t.Fatal(code, diagnostics.String())
	}
}

func TestSQLiteSetupRejectsBadPasswordBeforeCreatingDatabase(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "settings.env")
	database := filepath.Join(dir, "new.sqlite3")
	if err := os.WriteFile(filename, []byte("SECRET_KEY=setup-test-0123456789abcdef0123456789\nDB_TYPE=sqlite3\nDB_PATH=new.sqlite3\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, password := range []string{"", "short", strings.Repeat("a", 1025), "first-password-123\nsecond-line", "embedded-nul\x00password-123"} {
		var out, diagnostics bytes.Buffer
		code := runSetupInput([]string{"--env", filename, "--allow-write", "--admin-password-stdin"}, strings.NewReader(password), &out, &diagnostics)
		if code != 2 {
			t.Fatal("invalid password accepted", code)
		}
		if _, err := os.Stat(database); !os.IsNotExist(err) {
			t.Fatal("invalid input created the database")
		}
		if password != "" && strings.Contains(out.String()+diagnostics.String(), password) {
			t.Fatal("password disclosed in CLI output")
		}
	}
}

func TestSetupPasswordInputPreservesUnicodeAndSpaces(t *testing.T) {
	const expected = "  새로운-관리자-비밀번호-123  "
	for _, ending := range []string{"", "\n", "\r\n"} {
		var diagnostics bytes.Buffer
		got, err := readSetupPassword(strings.NewReader(expected+ending), &diagnostics, true)
		if err != nil || got != expected || diagnostics.Len() != 0 {
			t.Fatal("stdin password changed or was echoed", err)
		}
	}
	var diagnostics bytes.Buffer
	if _, err := readSetupPassword(strings.NewReader(expected), &diagnostics, false); err == nil {
		t.Fatal("unattended setup accepted an echoed password input")
	}
}

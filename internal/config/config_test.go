package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testSecret = "test-only-secret-0123456789abcdef0123456789"

func settingsFile(t *testing.T, extra string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "server")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(dir, ".env")
	if err := os.WriteFile(name, []byte("SECRET_KEY="+testSecret+"\n"+extra), 0600); err != nil {
		t.Fatal(err)
	}
	return name
}
func noEnv(string) (string, bool) { return "", false }

func TestLoadLegacySQLiteAliases(t *testing.T) {
	for _, alias := range []string{"sqlite", "sqlite3", "local"} {
		t.Run(alias, func(t *testing.T) {
			filename := settingsFile(t, "DB_TYPE="+alias+"\nDB_PORT=0\nDB_PATH=./timeweaver.sqlite3\nCONTEXT=/timeweaver_svr\n")
			s, err := Load(filename, noEnv)
			if err != nil {
				t.Fatal(err)
			}
			if s.Context != "/timeweaver_svr" || s.Database.Type != "sqlite3" || s.IdleTimeout != 30*time.Minute {
				t.Fatalf("unexpected nonsecret settings")
			}
			if s.Database.Path != filepath.Join(filepath.Dir(filename), "timeweaver.sqlite3") {
				t.Fatal("relative database location changed")
			}
			if _, err = os.Stat(s.Database.Path); !os.IsNotExist(err) {
				t.Fatal("configuration loading must not create a database")
			}
		})
	}
}
func TestEnvironmentOverridesAndSecretsAreNotSerialized(t *testing.T) {
	file := settingsFile(t, "DB_TYPE=mysql\nDB_HOST=localhost\nDB_USER=user\nDB_PASSWORD='p@ss # with spaces'\nDB_DATABASE=tw\n")
	lookup := func(key string) (string, bool) {
		if key == "CONTEXT" {
			return "/custom", true
		}
		return "", false
	}
	s, err := Load(file, lookup)
	if err != nil {
		t.Fatal(err)
	}
	if s.Context != "/custom" || s.Database.Port != 3306 || s.Database.Password != "p@ss # with spaces" {
		t.Fatal("settings not preserved")
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{testSecret, s.Database.Password} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatal("secret serialized")
		}
	}
}

func TestOSLoginConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, fileValue, environment string
		want, invalid                bool
	}{
		{name: "enabled by default", want: true},
		{name: "disabled in file", fileValue: "false"},
		{name: "environment overrides file", fileValue: "true", environment: "false"},
		{name: "invalid value fails closed", fileValue: "maybe", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			extra := ""
			if tc.fileValue != "" {
				extra = "OS_LOGIN_ENABLED=" + tc.fileValue + "\n"
			}
			s, err := Load(settingsFile(t, extra), func(key string) (string, bool) {
				return tc.environment, key == "OS_LOGIN_ENABLED" && tc.environment != ""
			})
			if tc.invalid {
				if err == nil || !strings.Contains(err.Error(), "OS_LOGIN_ENABLED") {
					t.Fatalf("invalid OS login configuration was not rejected: %v", err)
				}
				return
			}
			if err != nil || s.OSLoginEnabled != tc.want {
				t.Fatalf("OS login configuration: enabled=%v, error=%v", s.OSLoginEnabled, err)
			}
		})
	}
}

func TestInvalidSettings(t *testing.T) {
	for _, extra := range []string{"CONTEXT=/dashboard/api\n", "CONTEXT=\n", "CONTEXT=/time_weaver/\n", "DB_TYPE=other\n", "DB_PATH=:memory:\n", "LISTEN_PORT=0\n", "LISTEN_PORT=65536\n", "LISTEN_HOST=http://localhost\n", "DB_TYPE=mysql\n", "ACCESS_TOKEN_EXPIRE_MINUTES=-1\n", "AGENT_SECRET_KEY=short\n"} {
		t.Run(strings.Split(extra, "=")[0], func(t *testing.T) {
			_, err := Load(settingsFile(t, extra), noEnv)
			if err == nil {
				t.Fatal("invalid settings accepted")
			}
			if strings.Contains(err.Error(), testSecret) {
				t.Fatal("secret in error")
			}
		})
	}
	_, err := Load(settingsFile(t, ""), func(key string) (string, bool) { return "", key == "SECRET_KEY" })
	if err == nil {
		t.Fatal("empty environment override did not override the file")
	}
}
func TestContextPaths(t *testing.T) {
	for _, context := range []string{"/", "", "//tw", "/a/../b", "/a/./b", "/js", "/css/x", "/login", "/dashboard", "/tw?x", "/tw#x", "/tw%2fapi", "/tw\"bad", "/tw/"} {
		if ValidateContext(context) == nil {
			t.Errorf("accepted invalid context %q", context)
		}
	}
	for _, context := range []string{"/time_weaver", "/timeweaver_svr", "/nested/api-v2", "/api.v2"} {
		if err := ValidateContext(context); err != nil {
			t.Errorf("valid context rejected: %s", context)
		}
	}
}
func TestDotenvSyntax(t *testing.T) {
	values, err := parseEnv("\ufeff# ignored\r\nexport KEY = 'a#b' # comment\r\nPASS=a#b\nPLAIN=value # comment\nDOUBLE=\"a\\nline\"\nEMPTY=\n")
	if err != nil {
		t.Fatal(err)
	}
	if values["KEY"] != "a#b" || values["PASS"] != "a#b" || values["PLAIN"] != "value" || values["DOUBLE"] != "a\nline" || values["EMPTY"] != "" {
		t.Fatal("dotenv decoding mismatch")
	}
	for _, raw := range []string{"NO_EQUALS", "KEY=a\nKEY=b", "KEY='unterminated", "KEY=\"value\" secret", "KEY=${OTHER}", "A B=value"} {
		if _, err := parseEnv(raw); err == nil {
			t.Errorf("invalid dotenv accepted: %q", raw)
		}
	}
}
func TestAgentSigningKeyMatchesPythonRule(t *testing.T) {
	expected := sha256.Sum256([]byte("timeweaver-agent:" + testSecret))
	for _, agent := range []string{"", testSecret} {
		s := Settings{SecretKey: testSecret, AgentSecretKey: agent}
		if !bytes.Equal(s.AgentSigningKey(), expected[:]) {
			t.Fatal("derived agent key changed")
		}
	}
	explicit := Settings{SecretKey: testSecret, AgentSecretKey: "independent-key-0123456789abcdef0123456789"}
	if string(explicit.AgentSigningKey()) != explicit.AgentSecretKey {
		t.Fatal("explicit agent key changed")
	}
	key := explicit.AgentSigningKey()
	key[0] = 'x'
	if string(explicit.AgentSigningKey()) != explicit.AgentSecretKey {
		t.Fatal("returned key aliases settings")
	}
}

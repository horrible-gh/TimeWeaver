package config

import (
	"strings"
	"testing"
)

func TestPublicOriginConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, extra, want string
	}{
		{"legacy wildcard", "", "http://localhost:8000"},
		{"IPv6 wildcard", "LISTEN_HOST=::\n", "http://localhost:8000"},
		{"IPv4 loopback", "LISTEN_HOST=127.0.0.1\nLISTEN_PORT=8081\n", "http://127.0.0.1:8081"},
		{"IPv6 loopback", "LISTEN_HOST=::1\n", "http://[::1]:8000"},
		{"local hostname", "LISTEN_HOST=localhost\n", "http://localhost:8000"},
		{"TLS proxy", "PUBLIC_ORIGIN=https://timeweaver.example/\nTRUSTED_PROXIES=127.0.0.1,::1\n", "https://timeweaver.example"},
		{"explicit canonical local origin", "PUBLIC_ORIGIN=http://localhost:8080\n", "http://localhost:8080"},
		{"browser canonical HTTPS port", "PUBLIC_ORIGIN=https://TimeWeaver.Example:443/\nTRUSTED_PROXIES=127.0.0.1\n", "https://timeweaver.example"},
		{"browser canonical HTTP port", "PUBLIC_ORIGIN=http://LOCALHOST:00080\n", "http://localhost"},
		{"canonical IPv6", "PUBLIC_ORIGIN=http://[0:0:0:0:0:0:0:1]:08000\n", "http://[::1]:8000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := Load(settingsFile(t, tc.extra), noEnv)
			if err != nil || s.PublicOrigin != tc.want {
				t.Fatalf("public origin %q, error %v; want %q", s.PublicOrigin, err, tc.want)
			}
		})
	}
}

func TestPublicOriginRejectsUnusableAndUnsafeConfiguration(t *testing.T) {
	for _, extra := range []string{
		"LISTEN_HOST=192.0.2.10\n", "LISTEN_HOST=server.example\n",
		"PUBLIC_ORIGIN=https://timeweaver.example\n",
		"PUBLIC_ORIGIN=http://timeweaver.example\n",
		"PUBLIC_ORIGIN=http://0.0.0.0:8000\n",
		"PUBLIC_ORIGIN=http://localhost:0\n",
		"PUBLIC_ORIGIN=http://localhost:65536\n",
		"PUBLIC_ORIGIN=http://localhost:\n",
		"PUBLIC_ORIGIN=http://localhost/path\n",
		"PUBLIC_ORIGIN=http://localhost/%2f\n",
		"PUBLIC_ORIGIN=http://localhost?\n",
		"PUBLIC_ORIGIN=http://localhost?private-value\n",
		"PUBLIC_ORIGIN=http://user:private-value@localhost\n",
		"PUBLIC_ORIGIN=http://localhost/#private-value\n",
		"PUBLIC_ORIGIN=//localhost\n",
	} {
		t.Run(strings.TrimSpace(extra), func(t *testing.T) {
			_, err := Load(settingsFile(t, extra), noEnv)
			if err == nil || !strings.Contains(err.Error(), "PUBLIC_ORIGIN") {
				t.Fatalf("invalid origin accepted or misleading diagnostic: %v", err)
			}
			if strings.Contains(err.Error(), "private-value") {
				t.Fatal("invalid origin leaked supplied credentials or query")
			}
		})
	}
}

func TestPublicOriginEnvironmentOverridesFile(t *testing.T) {
	s, err := Load(settingsFile(t, "PUBLIC_ORIGIN=http://localhost:8080\n"), func(key string) (string, bool) {
		return "http://127.0.0.1:8081", key == "PUBLIC_ORIGIN"
	})
	if err != nil || s.PublicOrigin != "http://127.0.0.1:8081" {
		t.Fatal("origin environment override was lost", err)
	}
}

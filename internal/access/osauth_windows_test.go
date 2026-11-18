//go:build windows

package access

import (
	"context"
	"os"
	"testing"
)

func TestNativeOSWindowsNameScope(t *testing.T) {
	for _, test := range []struct {
		name, username, domain, lookup string
	}{
		{"operator", "operator", ".", `HOST\operator`},
		{`.\operator`, "operator", ".", `HOST\operator`},
		{`HOST\operator`, "operator", "HOST", `HOST\operator`},
		{`DOMAIN\operator`, "operator", "DOMAIN", `DOMAIN\operator`},
		{"operator@example.test", "operator@example.test", "", "operator@example.test"},
	} {
		got, err := splitWindowsOSName(test.name, "HOST")
		if err != nil || got.username != test.username || got.domain != test.domain || got.lookup != test.lookup {
			t.Errorf("incorrect Windows account scope for %q: %+v %v", test.name, got, err)
		}
	}
	for _, name := range []string{"", `\operator`, `DOMAIN\`, `DOMAIN\nested\operator`, "operator/other", "operator@", "@domain", "one@two@three", "name\x00"} {
		if _, err := splitWindowsOSName(name, "HOST"); err == nil {
			t.Errorf("malformed Windows account accepted: %q", name)
		}
	}
}

func TestLiveNativeOSWindowsAliases(t *testing.T) {
	name, password := os.Getenv("TIMEWEAVER_OS_AUTH_TEST_USER"), os.Getenv("TIMEWEAVER_OS_AUTH_TEST_PASSWORD")
	if name == "" || password == "" {
		t.Skip("requires disposable native Windows identity")
	}
	host, err := os.Hostname()
	if err != nil || host == "" {
		t.Fatal("Windows computer name unavailable")
	}
	p := NewOSAuthenticator()
	var expected OSIdentity
	for _, alias := range []string{name, `.\` + name, host + `\` + name} {
		identity, err := p.Resolve(context.Background(), alias)
		if err != nil {
			t.Fatalf("local account alias did not resolve: %v", err)
		}
		if expected.Subject != "" && identity != expected {
			t.Fatal("local aliases resolved to different subjects or canonical names")
		}
		expected = identity
		authenticated, err := p.Authenticate(context.Background(), alias, password)
		if err != nil || authenticated != expected {
			t.Fatalf("native logon returned a different identity for local alias: %v", err)
		}
	}
}

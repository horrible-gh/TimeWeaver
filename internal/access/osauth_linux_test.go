//go:build linux

package access

import (
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestNativeOSLinuxHostScope(t *testing.T) {
	const raw = "0123456789abcdef0123456789abcdef"
	a, err := linuxHostScopeFromID([]byte(raw + "\n"))
	if err != nil || len(a) != 64 || strings.Contains(a, raw) {
		t.Fatalf("machine ID was not privately scoped: %q %v", a, err)
	}
	// Changing this value would silently detach existing administrator links.
	const expected = "c79bcf3efc2f68ac37564dd177602d11f7fe9473fedadc61847515dcd8bcf781"
	if a != expected {
		t.Fatalf("persistent Linux identity format changed: %s", a)
	}
	b, err := linuxHostScopeFromID([]byte(raw))
	if err != nil || a != b {
		t.Fatal("machine scope changed with the optional newline")
	}
	c, err := linuxHostScopeFromID([]byte("abcdef0123456789abcdef0123456789"))
	if err != nil || c == a {
		t.Fatal("different hosts share an OS subject scope")
	}
	for _, raw := range []string{"", "uninitialized\n", strings.Repeat("0", 32), strings.Repeat("f", 31), strings.Repeat("g", 32), strings.Repeat("A", 32), raw + "\n\n", " " + raw} {
		if _, err := linuxHostScopeFromID([]byte(raw)); !errors.Is(err, ErrOSUnavailable) {
			t.Errorf("invalid machine ID did not disable OS login: %q %v", raw, err)
		}
	}
}

func TestNativeOSLinuxLocalAccountAndShell(t *testing.T) {
	passwd := []byte("root:x:0:0:root:/root:/bin/sh\noperator:x:1201:1201:Operator:/home/operator:/bin/bash\n")
	a, err := parseLinuxAccount(passwd, "operator")
	if err != nil || a.name != "operator" || a.uid != "1201" || a.shell != "/bin/bash" {
		t.Fatalf("incorrect local account mapping: %+v %v", a, err)
	}
	for _, uid := range []string{"-1", "+1", "01", "4294967295", "4294967296", "not-a-uid"} {
		if _, err := parseLinuxAccount([]byte("operator:x:"+uid+":1::/:/bin/sh\n"), "operator"); !errors.Is(err, ErrOSCredentials) {
			t.Errorf("invalid UID accepted: %q", uid)
		}
	}
	if _, err := parseLinuxAccount(passwd, "directory-only-account"); !errors.Is(err, ErrOSCredentials) {
		t.Fatal("non-local account resolved")
	}
	for _, name := range []string{"-c", "name:other", "name/other", `domain\name`, "name other", "name\tother"} {
		if validLinuxName(name) {
			t.Errorf("unsafe Linux username accepted: %q", name)
		}
	}
	for _, shell := range []string{"/usr/sbin/nologin", "/sbin/nologin", "/bin/false", "relative-shell", "/timeweaver-nonexistent-shell"} {
		if linuxProbeShellAllowed(shell) {
			t.Errorf("restricted shell accepted: %q", shell)
		}
	}
	if !linuxProbeShellAllowed("") || !linuxProbeShellAllowed("/bin/sh") {
		t.Fatal("fixed standard shell rejected")
	}
}

func TestNativeOSLinuxProbeContainsNoCredentialArguments(t *testing.T) {
	const secret = "not-a-real-native-password"
	cmd := linuxAuthenticationCommand(context.Background(), "operator", strings.NewReader(secret+"\n"))
	want := []string{"/usr/bin/su", "--shell", "/bin/sh", "--command", "exec /usr/bin/id -u", "--", "operator"}
	if !reflect.DeepEqual(cmd.Args, want) || cmd.Dir != "/" || cmd.Stderr != io.Discard {
		t.Fatal("authentication probe is not the fixed command")
	}
	if strings.Contains(strings.Join(append(cmd.Args, cmd.Env...), "\n"), secret) || len(cmd.Env) != 3 {
		t.Fatal("credential or inherited environment escaped into child arguments")
	}
	if cmd.Cancel == nil || cmd.WaitDelay <= 2*time.Second || cmd.WaitDelay > 5*time.Second {
		t.Fatal("probe does not leave bounded time for su child cleanup")
	}
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setsid {
		t.Fatal("probe can inherit the server's controlling terminal")
	}
	output := &limitedOSOutput{limit: 8}
	// os/exec copies stdout with io.Copy. Do not accidentally expose a
	// bytes.Buffer.ReadFrom method that bypasses the bounded Write method.
	source := struct{ io.Reader }{strings.NewReader("123456789")}
	if _, err := io.Copy(output, source); err == nil || !output.overflow || output.Len() != 8 {
		t.Fatal("native probe output was not bounded")
	}
}

func TestNativeOSLinuxRootDisabled(t *testing.T) {
	if os.Getuid() != 0 && os.Geteuid() != 0 {
		t.Skip("CI also executes this check as root without credentials")
	}
	p := NewOSAuthenticator()
	if p.Available() {
		t.Fatal("root process advertised OS login")
	}
	if _, err := p.Resolve(context.Background(), "root"); !errors.Is(err, ErrOSUnavailable) {
		t.Fatal("root process resolved an OS login identity")
	}
	if _, err := p.Authenticate(context.Background(), "root", "invalid"); !errors.Is(err, ErrOSUnavailable) {
		t.Fatal("root process reached OS authentication")
	}
}

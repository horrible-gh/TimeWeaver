//go:build linux

package access

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"
)

const linuxIdentityDomain = "github.com/horrible-gh/TimeWeaver/os-account/linux/v1"

type linuxOSAccount struct {
	name  string
	uid   string
	shell string
}

func (p *nativeOSAuthenticator) Available() bool {
	// PAM su commonly trusts real UID 0 without checking a password. Refuse
	// either root identity, including a process that dropped only one UID.
	if os.Getuid() == 0 || os.Geteuid() == 0 {
		return false
	}
	if !trustedLinuxExecutable("/usr/bin/su", true) || !trustedLinuxExecutable("/usr/bin/id", false) || !trustedLinuxExecutable("/bin/sh", false) {
		return false
	}
	_, err := linuxHostScope()
	return err == nil
}

func trustedLinuxExecutable(path string, setuid bool) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || info.Mode().Perm()&0022 != 0 {
		return false
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	return ok && owner.Uid == 0 && (!setuid || info.Mode()&os.ModeSetuid != 0)
}

func (p *nativeOSAuthenticator) Resolve(ctx context.Context, name string) (OSIdentity, error) {
	if !p.Available() {
		return OSIdentity{}, ErrOSUnavailable
	}
	return p.run(ctx, func(ctx context.Context) (OSIdentity, error) {
		identity, _, err := resolveLinuxIdentity(ctx, name)
		return identity, err
	})
}

func (p *nativeOSAuthenticator) Authenticate(ctx context.Context, name, password string) (OSIdentity, error) {
	if !p.Available() {
		return OSIdentity{}, ErrOSUnavailable
	}
	if !validOSPassword(password) || strings.ContainsAny(password, "\r\n") {
		return OSIdentity{}, ErrOSCredentials
	}
	return p.run(ctx, func(ctx context.Context) (OSIdentity, error) {
		expected, account, err := resolveLinuxIdentity(ctx, name)
		if err != nil {
			return OSIdentity{}, err
		}
		// su ignores --shell for restricted shells. Check before invoking it,
		// so authentication never executes an account's custom login shell.
		if !linuxProbeShellAllowed(account.shell) {
			return OSIdentity{}, ErrOSCredentials
		}
		secret := append([]byte(password), '\n')
		defer clear(secret)
		cmd := linuxAuthenticationCommand(ctx, account.name, bytes.NewReader(secret))
		output := &limitedOSOutput{limit: 128}
		cmd.Stdout = output
		if err := cmd.Run(); err != nil {
			if ctx.Err() != nil {
				return OSIdentity{}, ctx.Err()
			}
			return OSIdentity{}, ErrOSCredentials
		}
		if output.overflow || strings.TrimSpace(output.String()) != account.uid {
			return OSIdentity{}, ErrOSCredentials
		}
		if err := ctx.Err(); err != nil {
			return OSIdentity{}, err
		}
		return expected, nil
	})
}

func linuxAuthenticationCommand(ctx context.Context, name string, password io.Reader) *exec.Cmd {
	// Both the executable and the shell program are fixed. The username is a
	// separate argument after --, and the password is never argv or environment.
	cmd := exec.CommandContext(ctx, "/usr/bin/su", "--shell", "/bin/sh", "--command", "exec /usr/bin/id -u", "--", name)
	cmd.Dir = "/"
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
	cmd.Stdin = password
	cmd.Stderr = io.Discard
	// A server started from a terminal must not let a PAM module read from
	// that controlling terminal instead of this request's input pipe.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	// util-linux su forwards SIGTERM to its child and waits up to two seconds
	// before SIGKILL. Killing su immediately could leave that child behind.
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 3 * time.Second
	return cmd
}

func resolveLinuxIdentity(ctx context.Context, name string) (OSIdentity, linuxOSAccount, error) {
	if err := ctx.Err(); err != nil {
		return OSIdentity{}, linuxOSAccount{}, err
	}
	if !validLinuxName(name) {
		return OSIdentity{}, linuxOSAccount{}, ErrOSCredentials
	}
	scope, err := linuxHostScope()
	if err != nil {
		return OSIdentity{}, linuxOSAccount{}, ErrOSUnavailable
	}
	passwd, err := readLinuxIdentityFile("/etc/passwd", 4<<20)
	if err != nil {
		return OSIdentity{}, linuxOSAccount{}, ErrOSUnavailable
	}
	account, err := parseLinuxAccount(passwd, name)
	if err != nil {
		return OSIdentity{}, linuxOSAccount{}, err
	}
	return OSIdentity{Subject: "linux:uid:" + account.uid + ":" + scope, Name: account.name}, account, nil
}

func validLinuxName(name string) bool {
	if !validOSName(name) || len(name) > 256 || strings.HasPrefix(name, "-") || strings.ContainsAny(name, "/\\:") {
		return false
	}
	return !strings.ContainsFunc(name, unicode.IsSpace)
}

// Read /etc/passwd directly to keep the supported account set local even when
// a development build has cgo enabled. No shadow file or password hash is read.
func parseLinuxAccount(passwd []byte, name string) (linuxOSAccount, error) {
	for _, line := range strings.Split(string(passwd), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) != 7 || fields[0] != name {
			continue
		}
		uid, err := strconv.ParseUint(fields[2], 10, 32)
		if err != nil || uid == 1<<32-1 || strconv.FormatUint(uid, 10) != fields[2] {
			return linuxOSAccount{}, ErrOSCredentials
		}
		return linuxOSAccount{name: fields[0], uid: fields[2], shell: fields[6]}, nil
	}
	return linuxOSAccount{}, ErrOSCredentials
}

func linuxProbeShellAllowed(shell string) bool {
	if shell == "" || shell == "/bin/sh" {
		return true
	}
	if !filepath.IsAbs(shell) || filepath.Base(shell) == "nologin" || filepath.Base(shell) == "false" {
		return false
	}
	shells, err := readLinuxIdentityFile("/etc/shells", 64<<10)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(shells), "\n") {
		entry, _, _ := strings.Cut(line, "#")
		if strings.TrimSpace(entry) == shell {
			return true
		}
	}
	return false
}

func readLinuxIdentityFile(path string, limit int64) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 {
		return nil, ErrOSUnavailable
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != 0 {
		return nil, ErrOSUnavailable
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, ErrOSUnavailable
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(b)) > limit {
		return nil, ErrOSUnavailable
	}
	return b, nil
}

func linuxHostScope() (string, error) {
	b, err := readLinuxIdentityFile("/etc/machine-id", 64)
	if err != nil {
		return "", ErrOSUnavailable
	}
	return linuxHostScopeFromID(b)
}

func linuxHostScopeFromID(raw []byte) (string, error) {
	id := strings.TrimSuffix(string(raw), "\n")
	if len(id) != 32 || id != strings.ToLower(id) {
		return "", ErrOSUnavailable
	}
	machine, err := hex.DecodeString(id)
	if err != nil || bytes.Equal(machine, make([]byte, 16)) {
		return "", ErrOSUnavailable
	}
	// systemd recommends an application-specific keyed hash, never exposure
	// of the machine ID. Keep this domain and subject format stable across
	// releases: they form the administrator's persistent account binding.
	mac := hmac.New(sha256.New, machine)
	_, _ = mac.Write([]byte(linuxIdentityDomain))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

type limitedOSOutput struct {
	buffer   bytes.Buffer
	limit    int
	overflow bool
}

func (w *limitedOSOutput) Len() int       { return w.buffer.Len() }
func (w *limitedOSOutput) String() string { return w.buffer.String() }

func (w *limitedOSOutput) Write(b []byte) (int, error) {
	if len(b) > w.limit-w.Len() {
		w.overflow = true
		n, _ := w.buffer.Write(b[:w.limit-w.Len()])
		return n, errors.New("OS authentication output exceeded limit")
	}
	return w.buffer.Write(b)
}

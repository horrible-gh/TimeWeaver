//go:build windows

package access

import (
	"context"
	"os"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var windowsLogonUser = windows.NewLazySystemDLL("advapi32.dll").NewProc("LogonUserW")

const (
	windowsLogonNetwork         = 3
	windowsLogonProviderDefault = 0
)

type windowsOSName struct {
	username string
	domain   string
	lookup   string
}

func (p *nativeOSAuthenticator) Available() bool {
	return windowsLogonUser.Find() == nil
}

func (p *nativeOSAuthenticator) Resolve(ctx context.Context, name string) (OSIdentity, error) {
	if !p.Available() {
		return OSIdentity{}, ErrOSUnavailable
	}
	return p.run(ctx, func(ctx context.Context) (OSIdentity, error) {
		return resolveWindowsIdentity(ctx, name)
	})
}

func (p *nativeOSAuthenticator) Authenticate(ctx context.Context, name, password string) (OSIdentity, error) {
	if !p.Available() {
		return OSIdentity{}, ErrOSUnavailable
	}
	if !validOSPassword(password) {
		return OSIdentity{}, ErrOSCredentials
	}
	return p.run(ctx, func(ctx context.Context) (OSIdentity, error) {
		expected, err := resolveWindowsIdentity(ctx, name)
		if err != nil {
			return OSIdentity{}, err
		}
		host, err := os.Hostname()
		if err != nil || host == "" {
			return OSIdentity{}, ErrOSUnavailable
		}
		account, err := splitWindowsOSName(name, host)
		if err != nil {
			return OSIdentity{}, err
		}
		u, err := windows.UTF16FromString(account.username)
		if err != nil {
			return OSIdentity{}, ErrOSCredentials
		}
		secret, err := windows.UTF16FromString(password)
		if err != nil {
			return OSIdentity{}, ErrOSCredentials
		}
		defer func() {
			clear(secret)
			runtime.KeepAlive(secret)
		}()
		var domain *uint16
		if account.domain != "" {
			domain, err = windows.UTF16PtrFromString(account.domain)
			if err != nil {
				return OSIdentity{}, ErrOSCredentials
			}
		}
		if err := ctx.Err(); err != nil {
			return OSIdentity{}, err
		}
		var token windows.Token
		defer func() {
			if token != 0 {
				_ = token.Close()
			}
		}()
		// NETWORK validates credentials now without caching them. Never use
		// NEW_CREDENTIALS, which only changes future outbound credentials.
		ok, _, _ := windowsLogonUser.Call(
			uintptr(unsafe.Pointer(&u[0])), uintptr(unsafe.Pointer(domain)),
			uintptr(unsafe.Pointer(&secret[0])), windowsLogonNetwork,
			windowsLogonProviderDefault, uintptr(unsafe.Pointer(&token)),
		)
		runtime.KeepAlive(u)
		runtime.KeepAlive(domain)
		runtime.KeepAlive(secret)
		if err := ctx.Err(); err != nil {
			return OSIdentity{}, err
		}
		if ok == 0 || token == 0 {
			return OSIdentity{}, ErrOSCredentials
		}
		user, err := token.GetTokenUser()
		if err != nil || user.User.Sid == nil {
			return OSIdentity{}, ErrOSCredentials
		}
		// Besides authenticating the password, require the token to identify
		// the exact requested user (including any local Guest mapping policy).
		if "windows:sid:"+user.User.Sid.String() != expected.Subject {
			return OSIdentity{}, ErrOSCredentials
		}
		if err := ctx.Err(); err != nil {
			return OSIdentity{}, err
		}
		return expected, nil
	})
}

func resolveWindowsIdentity(ctx context.Context, name string) (OSIdentity, error) {
	if err := ctx.Err(); err != nil {
		return OSIdentity{}, err
	}
	host, err := os.Hostname()
	if err != nil || host == "" {
		return OSIdentity{}, ErrOSUnavailable
	}
	account, err := splitWindowsOSName(name, host)
	if err != nil {
		return OSIdentity{}, err
	}
	// LookupSID avoids os/user's extra profile and primary-group lookups,
	// which need not be available to a restricted Windows service identity.
	sid, _, kind, err := windows.LookupSID("", account.lookup)
	if err != nil || sid == nil || kind != windows.SidTypeUser {
		return OSIdentity{}, ErrOSCredentials
	}
	if err := ctx.Err(); err != nil {
		return OSIdentity{}, err
	}
	username, domain, kind, err := sid.LookupAccount("")
	if err != nil || kind != windows.SidTypeUser || username == "" || domain == "" {
		return OSIdentity{}, ErrOSCredentials
	}
	canonical := domain + `\` + username
	value := sid.String()
	if !validOSName(canonical) || !strings.HasPrefix(value, "S-1-") {
		return OSIdentity{}, ErrOSCredentials
	}
	if err := ctx.Err(); err != nil {
		return OSIdentity{}, err
	}
	return OSIdentity{Subject: "windows:sid:" + value, Name: canonical}, nil
}

func splitWindowsOSName(name, host string) (windowsOSName, error) {
	if !validOSName(name) || host == "" || strings.Contains(name, "/") || strings.Count(name, `\`) > 1 {
		return windowsOSName{}, ErrOSCredentials
	}
	if domain, username, ok := strings.Cut(name, `\`); ok {
		if domain == "" || username == "" || strings.TrimSpace(domain) != domain || strings.TrimSpace(username) != username {
			return windowsOSName{}, ErrOSCredentials
		}
		lookup := name
		if domain == "." {
			lookup = host + `\` + username
		}
		return windowsOSName{username: username, domain: domain, lookup: lookup}, nil
	}
	if strings.Contains(name, "@") {
		username, domain, _ := strings.Cut(name, "@")
		if username == "" || domain == "" || strings.Contains(domain, "@") {
			return windowsOSName{}, ErrOSCredentials
		}
		// LogonUserW requires a NULL domain for the UPN form.
		return windowsOSName{username: name, lookup: name}, nil
	}
	// Unqualified names always mean this server's local account database,
	// including during the administrator's account-linking lookup.
	return windowsOSName{username: name, domain: ".", lookup: host + `\` + name}, nil
}

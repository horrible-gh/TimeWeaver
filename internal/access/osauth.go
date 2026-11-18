package access

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// OSIdentity identifies an account, independently of its current login name.
// Linux subjects also include an application-specific host identifier.
type OSIdentity struct {
	Subject string
	Name    string
}

// OSAuthenticator validates credentials through the host operating system.
// A successful authentication does not create an application user or grant roles.
type OSAuthenticator interface {
	Available() bool
	Resolve(context.Context, string) (OSIdentity, error)
	Authenticate(context.Context, string, string) (OSIdentity, error)
}

var (
	ErrOSUnavailable = errors.New("operating system authentication unavailable")
	ErrOSCredentials = errors.New("invalid operating system credentials")
)

const (
	osAuthTimeout = 15 * time.Second
	osAuthWorkers = 4
)

type nativeOSAuthenticator struct {
	admission chan struct{}
}

// NewOSAuthenticator returns the native provider. Availability is checked at
// use time; unsupported hosts and Linux root processes fail closed.
func NewOSAuthenticator() OSAuthenticator {
	return &nativeOSAuthenticator{admission: make(chan struct{}, osAuthWorkers)}
}

type osAuthenticationResult struct {
	identity OSIdentity
	err      error
}

// run keeps the admission slot until the native call has actually returned.
// In particular, cancellation cannot interrupt a Windows domain lookup or
// LogonUserW call, and must not allow cancelled requests to grow native workers.
func (p *nativeOSAuthenticator) run(ctx context.Context, fn func(context.Context) (OSIdentity, error)) (OSIdentity, error) {
	ctx, cancel := context.WithTimeout(ctx, osAuthTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return OSIdentity{}, err
	}
	select {
	case p.admission <- struct{}{}:
	default:
		return OSIdentity{}, ErrOSUnavailable
	}
	result := make(chan osAuthenticationResult, 1)
	go func() {
		defer func() { <-p.admission }()
		identity, err := fn(ctx)
		result <- osAuthenticationResult{identity, err}
	}()
	select {
	case <-ctx.Done():
		return OSIdentity{}, ctx.Err()
	case r := <-result:
		// If cancellation races a successful OS result, no caller may mint a
		// session from a request whose context has already ended.
		if err := ctx.Err(); err != nil {
			return OSIdentity{}, err
		}
		return r.identity, r.err
	}
}

func validOSName(name string) bool {
	if name == "" || len(name) > 1024 || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 255 || strings.TrimSpace(name) != name {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validOSPassword(password string) bool {
	return password != "" && len(password) <= 1024 && utf8.ValidString(password) && !strings.ContainsRune(password, '\x00')
}

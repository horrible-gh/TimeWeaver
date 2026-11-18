//go:build !linux && !windows

package access

import "context"

func (p *nativeOSAuthenticator) Available() bool { return false }

func (p *nativeOSAuthenticator) Resolve(context.Context, string) (OSIdentity, error) {
	return OSIdentity{}, ErrOSUnavailable
}

func (p *nativeOSAuthenticator) Authenticate(context.Context, string, string) (OSIdentity, error) {
	return OSIdentity{}, ErrOSUnavailable
}

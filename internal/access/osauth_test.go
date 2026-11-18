package access

import (
	"context"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestNativeOSAdmissionRetainsCancelledCalls(t *testing.T) {
	p := NewOSAuthenticator().(*nativeOSAuthenticator)
	blocked := make(chan struct{})
	defer close(blocked)
	for i := 0; i < osAuthWorkers; i++ {
		started := make(chan struct{})
		returned := make(chan error, 1)
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			_, err := p.run(ctx, func(context.Context) (OSIdentity, error) {
				close(started)
				<-blocked // Models a synchronous native call that cannot cancel.
				return OSIdentity{Subject: "late-result"}, nil
			})
			returned <- err
		}()
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("native operation did not start")
		}
		cancel()
		select {
		case err := <-returned:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancelled operation returned %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("request remained blocked on native operation")
		}
	}
	called := false
	identity, err := p.run(context.Background(), func(context.Context) (OSIdentity, error) {
		called = true
		return OSIdentity{Subject: "unexpected"}, nil
	})
	if !errors.Is(err, ErrOSUnavailable) || called || identity != (OSIdentity{}) {
		t.Fatalf("cancelled native calls lost admission slots: called=%v identity=%+v err=%v", called, identity, err)
	}
}

func TestNativeOSCancelledSuccessCannotEscape(t *testing.T) {
	p := NewOSAuthenticator().(*nativeOSAuthenticator)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	identity, err := p.run(ctx, func(context.Context) (OSIdentity, error) {
		cancel()
		return OSIdentity{Subject: "late-success", Name: "account"}, nil
	})
	if !errors.Is(err, context.Canceled) || identity != (OSIdentity{}) {
		t.Fatalf("success escaped request cancellation: %+v, %v", identity, err)
	}
	called := false
	if _, err = p.run(ctx, func(context.Context) (OSIdentity, error) {
		called = true
		return OSIdentity{}, nil
	}); !errors.Is(err, context.Canceled) || called {
		t.Fatalf("already-cancelled request started native work: called=%v err=%v", called, err)
	}
}

func TestNativeOSCredentialInput(t *testing.T) {
	for _, name := range []string{"", " name", "name ", "name\n", "name\x00", "name\x7f", string([]byte{0xff}), strings.Repeat("a", 256)} {
		if validOSName(name) {
			t.Errorf("unsafe or unbounded OS name accepted: %q", name)
		}
	}
	for _, password := range []string{"", "secret\x00suffix", string([]byte{0xff}), strings.Repeat("x", 1025)} {
		if validOSPassword(password) {
			t.Error("invalid or unbounded password accepted")
		}
	}
	if !validOSName(`DOMAIN\account name`) || !validOSPassword("valid-test-password") {
		t.Fatal("valid credential shape rejected")
	}
}

// The CI helpers create and remove these accounts on disposable native runners.
// This test never creates accounts or changes PAM, OS policy, or account state.
func TestLiveNativeOSAuthentication(t *testing.T) {
	name, password := os.Getenv("TIMEWEAVER_OS_AUTH_TEST_USER"), os.Getenv("TIMEWEAVER_OS_AUTH_TEST_PASSWORD")
	if name == "" && password == "" {
		t.Skip("requires disposable native OS identities")
	}
	if name == "" || password == "" {
		t.Fatal("incomplete native authentication fixture")
	}
	p := NewOSAuthenticator()
	if !p.Available() {
		t.Fatal("native provider unavailable on configured CI host")
	}
	expected, err := p.Resolve(context.Background(), name)
	if err != nil || expected.Subject == "" || expected.Name == "" {
		t.Fatalf("native account resolution failed: %v", err)
	}
	actual, err := p.Authenticate(context.Background(), name, password)
	if err != nil || actual != expected {
		t.Fatalf("authenticated identity differs from resolved account: %v", err)
	}
	if _, err = p.Authenticate(context.Background(), name, password+"-wrong"); !errors.Is(err, ErrOSCredentials) {
		t.Fatalf("incorrect OS password was not rejected: %v", err)
	}
	if _, err = p.Resolve(context.Background(), "tw-os-does-not-exist"); !errors.Is(err, ErrOSCredentials) {
		t.Fatalf("missing OS account was not rejected: %v", err)
	}
	fixtures := []string{"DISABLED", "EXPIRED", "PASSWORD_EXPIRED"}
	if runtime.GOOS == "linux" {
		fixtures = append(fixtures, "RESTRICTED")
	}
	for _, fixture := range fixtures {
		t.Run(fixture, func(t *testing.T) {
			blocked := os.Getenv("TIMEWEAVER_OS_AUTH_TEST_" + fixture)
			if blocked == "" {
				t.Fatal("native policy fixture is missing")
			}
			// Rejection must be due to OS policy or the fixed-shell preflight,
			// not a misspelled/missing account created by the CI helper.
			if _, err := p.Resolve(context.Background(), blocked); err != nil {
				t.Fatalf("policy fixture does not resolve: %v", err)
			}
			if _, err := p.Authenticate(context.Background(), blocked, password); !errors.Is(err, ErrOSCredentials) {
				t.Fatalf("restricted OS account was not rejected: %v", err)
			}
		})
	}
}

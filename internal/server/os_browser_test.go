package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/horrible-gh/TimeWeaver/internal/access"
	"github.com/horrible-gh/TimeWeaver/internal/dashboard"
	"github.com/horrible-gh/TimeWeaver/internal/webui"
)

const browserOSPassword = "isolated-browser-os-password"

type browserOSAuthenticator struct{ calls atomic.Int64 }

func (*browserOSAuthenticator) Available() bool { return true }
func (*browserOSAuthenticator) Resolve(ctx context.Context, name string) (access.OSIdentity, error) {
	if err := ctx.Err(); err != nil {
		return access.OSIdentity{}, err
	}
	switch name {
	case "browser-os-one", "browser-os-alias":
		return access.OSIdentity{Subject: "linux:uid:42431", Name: "browser-os-one"}, nil
	case "browser-os-two":
		return access.OSIdentity{Subject: "linux:uid:42432", Name: "browser-os-two"}, nil
	default:
		return access.OSIdentity{}, access.ErrOSCredentials
	}
}
func (p *browserOSAuthenticator) Authenticate(ctx context.Context, name, password string) (access.OSIdentity, error) {
	p.calls.Add(1)
	if password != browserOSPassword {
		return access.OSIdentity{}, access.ErrOSCredentials
	}
	return p.Resolve(ctx, name)
}

func TestBrowserOSFlow(t *testing.T) {
	if os.Getenv("TIMEWEAVER_BROWSER_ACCESS") != "1" {
		t.Skip("requires browser and explicitly allowed disposable DB")
	}
	f := deviceFixtureFor(t)
	ctx := context.Background()
	provider := &browserOSAuthenticator{}
	human, err := access.NewWithOS(ctx, f.db, f.settings.IdleTimeout, provider)
	if err != nil {
		t.Fatal(err)
	}
	ui, err := webui.Bundled(f.settings.Context)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	origin := "http://" + ln.Addr().String()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	agent, err := NewAgentSurface(f.db, f.probe, f.settings, ui, logger)
	if err != nil {
		ln.Close()
		t.Fatal(err)
	}
	guard, err := access.NewHTTP(human, f.settings.Context, access.HTTPOptions{Origin: origin})
	if err != nil {
		ln.Close()
		t.Fatal(err)
	}
	// Only this test binary assembles the injected authenticator. Production
	// construction chooses the native provider using validated configuration.
	app := &BrowserSurface{AgentSurface: agent, human: guard, dashboard: dashboard.NewHTTP(human, guard, f.settings.Context)}
	run, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- app.Serve(run, ln) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(15 * time.Second):
			t.Error("OS browser server did not stop")
		}
	}()
	limited, stop := context.WithTimeout(ctx, 110*time.Second)
	defer stop()
	cmd := exec.CommandContext(limited, "python", filepath.Join("..", "..", "scripts", "test-browser-os.py"))
	input, _ := json.Marshal(map[string]string{"origin": origin, "context": f.settings.Context, "user": "timeweaver", "password": deviceTestPassword, "os_password": browserOSPassword})
	cmd.Stdin = strings.NewReader(string(input))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("OS browser failed: %v\n%s", err, out)
	}
	if provider.calls.Load() < 3 {
		t.Fatal("OS browser flow did not reach the injected provider")
	}
	t.Log(string(out))
}

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
	"testing"
	"time"

	"github.com/horrible-gh/TimeWeaver/internal/access"
	"github.com/horrible-gh/TimeWeaver/internal/webui"
)

func TestBrowserSchedulesFlow(t *testing.T) {
	if os.Getenv("TIMEWEAVER_BROWSER_ACCESS") != "1" {
		t.Skip("requires browser and explicitly allowed disposable DB")
	}
	f := deviceFixtureFor(t)
	createSchedule(t, f, 8, "private-eight")
	ctx := context.Background()
	ui, err := webui.Bundled(f.settings.Context)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	origin := "http://" + ln.Addr().String()
	app, err := NewBrowserSurface(ctx, f.db, f.probe, f.settings, ui, slog.New(slog.NewTextHandler(io.Discard, nil)), access.HTTPOptions{Origin: origin})
	if err != nil {
		ln.Close()
		t.Fatal(err)
	}
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
			t.Error("schedule browser server did not stop")
		}
	}()
	limited, stop := context.WithTimeout(ctx, 110*time.Second)
	defer stop()
	cmd := exec.CommandContext(limited, "python", filepath.Join("..", "..", "scripts", "test-browser-schedules.py"))
	input, _ := json.Marshal(map[string]string{"origin": origin, "context": f.settings.Context, "user": "timeweaver", "password": deviceTestPassword})
	cmd.Stdin = strings.NewReader(string(input))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("schedule browser failed: %v\n%s", err, out)
	}
	t.Log(string(out))
}

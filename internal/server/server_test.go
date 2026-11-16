package server

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRoutingNeverRedirectsOrFallsBackForAPI(t *testing.T) {
	marker := func(value string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(value)) })
	}
	for _, context := range []string{"/timeweaver_svr", "/nested/api-v2"} {
		s := &AgentSurface{context: context, base: context + "/api/agent/v1", identity: marker("identity"), runtime: marker("runtime"), execution: marker("execution"), health: marker("health"), ui: marker("ui"), retry: 5}
		for path, want := range map[string]string{"/enroll": "identity", "/token": "identity", "/token/refresh": "identity", "/heartbeat": "runtime", "/snapshot": "runtime", "/health": "health", "/executions/x/results": "execution", "/manual-runs/3/claim": "execution", "/events": "execution"} {
			for _, suffix := range []string{"", "/"} {
				w := httptest.NewRecorder()
				s.ServeHTTP(w, httptest.NewRequest("GET", s.base+path+suffix, nil))
				if w.Body.String() != want || w.Header().Get("Location") != "" {
					t.Fatal(path, suffix, w.Code, w.Body)
				}
			}
		}
		for _, path := range []string{context + "/login", context + "/dashboard/tasks", s.base + "//snapshot", s.base + "/snapshot//", s.base + "/%73napshot", s.base + "/../snapshot", s.base + "/%2fhealth"} {
			w := httptest.NewRecorder()
			s.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
			if w.Code != 404 || strings.Contains(w.Body.String(), "ui") || w.Header().Get("Location") != "" {
				t.Fatal(path, w.Code, w.Body)
			}
		}
		for _, path := range []string{"/", "/login", "/dashboard/tasks", "/favicon.ico"} {
			w := httptest.NewRecorder()
			s.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
			if w.Body.String() != "ui" {
				t.Fatal(path, w.Body)
			}
		}
		s.draining.Store(true)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("GET", s.base+"/health", nil))
		if w.Code != 503 || w.Header().Get("Retry-After") != "5" {
			t.Fatal(w.Code)
		}
	}
}

func TestShutdownDrainsRequestsBeforeCancellingWork(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, release, draining := make(chan context.Context, 1), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseRequest := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseRequest()
	workerStopped := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- serve(ctx, ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			entered <- r.Context()
			<-release
			w.Write([]byte("committed"))
		}), func(ctx context.Context, _ func(error)) { <-ctx.Done(); close(workerStopped) }, time.Second, logger, func() { close(draining) })
	}()
	client := &http.Client{Timeout: 3 * time.Second}
	result := make(chan string, 1)
	go func() {
		resp, e := client.Get("http://" + ln.Addr().String())
		if e != nil {
			result <- e.Error()
			return
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		result <- string(b)
	}()
	var requestCtx context.Context
	select {
	case requestCtx = <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("no request")
	}
	cancel()
	<-draining
	if requestCtx.Err() != nil {
		t.Fatal("transaction request cancelled before drain")
	}
	select {
	case <-workerStopped:
		t.Fatal("worker stopped before request drained")
	default:
	}
	releaseRequest()
	if got := <-result; got != "committed" {
		t.Fatal(got)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	select {
	case <-workerStopped:
	default:
		t.Fatal("worker leaked")
	}
}

func TestForcedShutdownCancelsRequest(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, finished := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- serve(ctx, ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done(); close(finished) }), func(ctx context.Context, _ func(error)) { <-ctx.Done() }, 20*time.Millisecond, logger, func() {})
	}()
	client := &http.Client{Timeout: time.Second}
	go func() {
		resp, _ := client.Get("http://" + ln.Addr().String())
		if resp != nil {
			resp.Body.Close()
		}
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("no request")
	}
	cancel()
	if err = <-done; err == nil {
		t.Fatal("forced shutdown reported success")
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("request not cancelled")
	}
}

func TestRecoveryDoesNotDisclosePanicOrRequest(t *testing.T) {
	var logs strings.Builder
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	h := protect(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("private-password") }), logger)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/secret?token=private-token", nil))
	if w.Code != 500 || strings.Contains(w.Body.String()+logs.String(), "private") {
		t.Fatal("panic/request leaked")
	}
}

func TestCancelledStartupDoesNotStartWorker(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = serve(ctx, ln, http.NotFoundHandler(), func(context.Context, func(error)) { t.Error("started after cancellation") }, time.Second, slog.Default(), func() {})
	if err == nil {
		t.Fatal("cancelled startup accepted")
	}
	if _, err = net.DialTimeout("tcp", ln.Addr().String(), 100*time.Millisecond); err == nil {
		t.Fatal("listener leaked")
	}
}

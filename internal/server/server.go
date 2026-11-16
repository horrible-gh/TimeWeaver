// Package server composes the migration's agent API and embedded Vue surface.
// It does not enable the production CLI, migrate a DB or install an OS service.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/horrible-gh/TimeWeaver/internal/agentapi"
	"github.com/horrible-gh/TimeWeaver/internal/config"
	"github.com/horrible-gh/TimeWeaver/internal/store"
)

// AgentSurface borrows two already-open stores. Its caller closes them only
// after Serve returns. Construction starts no listener, migration or goroutine.
// Human/session/dashboard APIs remain absent, not bypassed by the device API.
type AgentSurface struct {
	base, context                            string
	identity, runtime, execution, health, ui http.Handler
	sweeper                                  func(context.Context, func(error))
	logger                                   *slog.Logger
	retry                                    int
	used, draining                           atomic.Bool
}

func NewAgentSurface(db, probe *store.Store, settings config.Settings, ui http.Handler, logger *slog.Logger) (*AgentSurface, error) {
	if err := config.ValidateContext(settings.Context); err != nil {
		return nil, err
	}
	if err := settings.HTTP.Validate(); err != nil {
		return nil, err
	}
	if db == nil || probe == nil || db.DB == nil || probe.DB == nil || db.DB == probe.DB || ui == nil {
		return nil, errors.New("separate business/probe stores and a validated UI handler are required")
	}
	identity, err := agentapi.NewIdentity(db, settings)
	if err != nil {
		return nil, err
	}
	opts := agentapi.HTTPOptions{Capacity: settings.HTTP.Capacity, Refill: settings.HTTP.Refill, Concurrency: settings.HTTP.Concurrency, TrustedProxies: settings.HTTP.TrustedProxies}
	auth, err := agentapi.NewIdentityHTTP(identity, settings.Context, opts)
	if err != nil {
		return nil, err
	}
	runtime, err := agentapi.NewRuntime(db)
	if err != nil {
		return nil, err
	}
	runtimeHTTP, err := agentapi.NewRuntimeHTTP(auth, runtime)
	if err != nil {
		return nil, err
	}
	execution, err := agentapi.NewExecution(db)
	if err != nil {
		return nil, err
	}
	executionHTTP, err := agentapi.NewExecutionHTTP(auth, execution)
	if err != nil {
		return nil, err
	}
	health, err := agentapi.NewHealthHTTP(probe, settings.HTTP.HealthTimeout, settings.HTTP.HealthRetryAfter)
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &AgentSurface{base: settings.Context + "/api/agent/v1", context: settings.Context,
		identity: auth, runtime: runtimeHTTP, execution: executionHTTP, health: health, ui: ui,
		sweeper: execution.RunClaimSweeper, logger: logger, retry: settings.HTTP.HealthRetryAfter}, nil
}

func (s *AgentSurface) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Do not use ServeMux's automatic canonical-path/slash redirects. Existing
	// agents refuse them, and an API miss must never become a Vue HTML response.
	if !validPath(r) {
		apiNotFound(w, r)
		return
	}
	p := strings.TrimSuffix(r.URL.Path, "/")
	switch p {
	case s.base + "/enroll", s.base + "/token", s.base + "/token/refresh":
		s.identity.ServeHTTP(w, r)
	case s.base + "/heartbeat", s.base + "/snapshot":
		s.runtime.ServeHTTP(w, r)
	case s.base + "/health":
		if s.draining.Load() {
			w.Header().Set("Retry-After", strconv.Itoa(s.retry))
			jsonReply(w, r, 503, map[string]bool{"ready": false})
			return
		}
		s.health.ServeHTTP(w, r)
	default:
		if p == s.base || strings.HasPrefix(p, s.base+"/") {
			s.execution.ServeHTTP(w, r)
			return
		}
		if p == s.context || strings.HasPrefix(p, s.context+"/") {
			apiNotFound(w, r)
			return
		}
		s.ui.ServeHTTP(w, r)
	}
}
func validPath(r *http.Request) bool {
	p := r.URL.Path
	if !strings.HasPrefix(p, "/") || r.URL.RawPath != "" || strings.ContainsAny(p, "\\%\x00\r\n") {
		return false
	}
	if p == "/" {
		return true
	}
	for _, part := range strings.Split(strings.TrimSuffix(p, "/"), "/")[1:] {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}
func jsonReply(w http.ResponseWriter, r *http.Request, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_ = json.NewEncoder(w).Encode(v)
	}
}
func apiNotFound(w http.ResponseWriter, r *http.Request) {
	jsonReply(w, r, 404, map[string]any{"detail": map[string]string{"code": "not_found", "message": "API route not found"}})
}

// Serve takes ownership of an already-bound listener. The API's base path and
// UI share this one listener. There is intentionally no production serve flag yet.
func (s *AgentSurface) Serve(ctx context.Context, ln net.Listener) error {
	if ln == nil {
		return errors.New("listener is required")
	}
	if !s.used.CompareAndSwap(false, true) {
		ln.Close()
		return errors.New("server instance cannot be reused")
	}
	return serve(ctx, ln, s, s.sweeper, 10*time.Second, s.logger, func() { s.draining.Store(true) })
}

// Separate request/work contexts are essential: stopping acceptance must not
// cancel an in-flight result transaction before its graceful drain completes.
func serve(ctx context.Context, ln net.Listener, handler http.Handler, worker func(context.Context, func(error)), grace time.Duration, logger *slog.Logger, onDrain func()) error {
	if err := ctx.Err(); err != nil {
		ln.Close()
		return err
	}
	requestCtx, cancelRequests := context.WithCancel(context.Background())
	defer cancelRequests()
	workCtx, cancelWork := context.WithCancel(context.Background())
	defer cancelWork()
	srv := &http.Server{Handler: protect(handler, logger), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 15 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: 60 * time.Second,
		MaxHeaderBytes: 32 << 10, BaseContext: func(net.Listener) context.Context { return requestCtx },
		ErrorLog: log.New(io.Discard, "", 0)}
	serving := make(chan error, 1)
	workerDone := make(chan error, 1)
	go func() { serving <- srv.Serve(ln) }()
	go func() {
		var err error
		defer func() {
			if recover() != nil {
				err = errors.New("claim sweeper panicked")
			}
			workerDone <- err
		}()
		worker(workCtx, func(error) { logger.Warn("claim sweep failed; will retry") })
		if workCtx.Err() == nil {
			err = errors.New("claim sweeper stopped unexpectedly")
		}
	}()
	var cause error
	serverFinished, workerFinished := false, false
	select {
	case <-ctx.Done():
	case err := <-serving:
		serverFinished = true
		if !errors.Is(err, http.ErrServerClosed) {
			cause = errors.New("HTTP listener stopped")
		}
	case cause = <-workerDone:
		workerFinished = true
	}
	onDrain()
	deadline, cancel := context.WithTimeout(context.Background(), grace)
	err := srv.Shutdown(deadline)
	cancel()
	if err != nil {
		cancelRequests()
		_ = srv.Close()
		cause = errors.Join(cause, errors.New("HTTP shutdown exceeded its drain deadline"))
	}
	cancelWork()
	if !serverFinished {
		<-serving
	}
	if !workerFinished {
		select {
		case err := <-workerDone:
			cause = errors.Join(cause, err)
		case <-time.After(3 * time.Second):
			cause = errors.Join(cause, errors.New("claim sweeper did not stop"))
		}
	}
	return cause
}

type responseState struct {
	http.ResponseWriter
	wrote bool
}

func (w *responseState) WriteHeader(code int) { w.wrote = true; w.ResponseWriter.WriteHeader(code) }
func (w *responseState) Write(p []byte) (int, error) {
	w.wrote = true
	return w.ResponseWriter.Write(p)
}
func (w *responseState) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func protect(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state := &responseState{ResponseWriter: w}
		defer func() {
			if p := recover(); p != nil {
				// Never log the panic value, URL, body, Authorization or query string.
				logger.Error("HTTP handler aborted")
				if state.wrote {
					panic(http.ErrAbortHandler)
				}
				jsonReply(w, r, 500, map[string]any{"detail": map[string]string{"code": "internal_error", "message": "Request failed"}})
			}
		}()
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		next.ServeHTTP(state, r.WithContext(ctx))
	})
}

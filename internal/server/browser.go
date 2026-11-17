package server

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/horrible-gh/TimeWeaver/internal/access"
	"github.com/horrible-gh/TimeWeaver/internal/config"
	"github.com/horrible-gh/TimeWeaver/internal/dashboard"
	"github.com/horrible-gh/TimeWeaver/internal/store"
)

// BrowserSurface adds human session routes without altering device authentication.
// It requires an explicitly initialized access schema and a configured public
// origin. Dashboard data APIs and the production serve CLI are still gated.
type BrowserSurface struct {
	*AgentSurface
	human     http.Handler
	dashboard http.Handler
}

func NewBrowserSurface(ctx context.Context, db, probe *store.Store, settings config.Settings, ui http.Handler, logger *slog.Logger, options access.HTTPOptions) (*BrowserSurface, error) {
	a, err := NewAgentSurface(db, probe, settings, ui, logger)
	if err != nil {
		return nil, err
	}
	human, err := access.New(ctx, db, settings.IdleTimeout)
	if err != nil {
		return nil, err
	}
	options.TrustedProxies = settings.HTTP.TrustedProxies
	h, err := access.NewHTTP(human, settings.Context, options)
	if err != nil {
		return nil, err
	}
	return &BrowserSurface{a, h, dashboard.NewHTTP(human, h, settings.Context)}, nil
}
func (s *BrowserSurface) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !validPath(r) {
		apiNotFound(w, r)
		return
	}
	p := strings.TrimSuffix(r.URL.Path, "/")
	switch p {
	case s.context + "/login", s.context + "/logout", s.context + "/auth/me", s.context + "/auth/methods", s.context + "/auth/password":
		s.human.ServeHTTP(w, r)
	default:
		if strings.HasPrefix(p, s.context+"/dashboard/users") || strings.HasPrefix(p, s.context+"/dashboard/groups/") {
			s.human.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(p, s.context+"/dashboard/") {
			s.dashboard.ServeHTTP(w, r)
			return
		}
		s.AgentSurface.ServeHTTP(w, r)
	}
}
func (s *BrowserSurface) Serve(ctx context.Context, ln net.Listener) error {
	if ln == nil {
		return errors.New("listener is required")
	}
	if !s.used.CompareAndSwap(false, true) {
		ln.Close()
		return errors.New("server instance cannot be reused")
	}
	return serve(ctx, ln, s, s.sweeper, 10*time.Second, s.logger, func() { s.draining.Store(true) })
}

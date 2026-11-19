package agentapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/horrible-gh/TimeWeaver/internal/store"
)

// HealthHTTP is the legacy unauthenticated A8 probe, not browser/RBAC readiness.
// It has its own read-only pool and admission slots, independent of business DB
// work. There is no detached goroutine which can outlive a timed-out probe.
type HealthHTTP struct {
	probe   func(context.Context) bool
	timeout time.Duration
	retry   int
	slots   chan struct{}
}

func NewHealthHTTP(db *store.Store, timeout time.Duration, retry int) (*HealthHTTP, error) {
	if db == nil || db.DB == nil || (db.Dialect != "mysql" && db.Dialect != "sqlite") || !db.ReadOnly() {
		return nil, errors.New("health requires a separate read-only MySQL/MariaDB or SQLite store")
	}
	if timeout < time.Millisecond || timeout > 10*time.Second || retry < 1 || retry > 86400 {
		return nil, errors.New("invalid health probe settings")
	}
	db.DB.SetMaxOpenConns(2)
	db.DB.SetMaxIdleConns(2)
	query := `SELECT COUNT(*) FROM information_schema.tables
   WHERE table_schema=DATABASE() AND table_name IN
   ('devices','groups','schedule_group','schedule_detail','task_detail',
    'manual_execution','execution_log','agent_device_credential',
    'agent_enrollment_token','agent_event')`
	if db.Dialect == "sqlite" {
		query = `SELECT COUNT(*) FROM sqlite_schema WHERE type='table' AND name IN
   ('devices','groups','schedule_group','schedule_detail','task_detail',
    'manual_execution','execution_log','agent_device_credential',
    'agent_enrollment_token','agent_event')`
	}
	return &HealthHTTP{probe: func(ctx context.Context) bool {
		var count int
		err := db.DB.QueryRowContext(ctx, query).Scan(&count)
		return err == nil && count == 10
	}, timeout: timeout, retry: retry, slots: make(chan struct{}, 2)}, nil
}

func (h *HealthHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeFailure(w, &APIError{405, "method_not_allowed", "Method not allowed", nil})
		return
	}
	ready := false
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
		ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
		defer cancel()
		ready = h.probe(ctx) && ctx.Err() == nil
	default:
	}
	status := http.StatusOK
	if !ready {
		status = http.StatusServiceUnavailable
		w.Header().Set("Retry-After", strconv.Itoa(h.retry))
	}
	writeJSON(w, status, map[string]bool{"ready": ready})
}

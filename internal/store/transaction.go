package store

import (
	"context"
	"database/sql"
	"time"
)

// BeginWrite admits a writer before any application reads or mutations run.
// SQLite's short native busy timeout keeps cancellation responsive; a bounded
// retry window lets a competing immediate transaction finish under load. Only
// a failed BEGIN is retried. The caller still owns every statement and commit,
// including an uncertain commit outcome, and MySQL retains its existing path.
func BeginWrite(ctx context.Context, q interface {
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}, dialect string, options *sql.TxOptions) (*sql.Tx, error) {
	if dialect != "sqlite" {
		return q.BeginTx(ctx, options)
	}
	deadline := time.Now().Add(5 * time.Second)
	var lastBusy error
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if lastBusy != nil && !time.Now().Before(deadline) {
			return nil, lastBusy
		}
		// A successful sql.Tx retains this context for its entire lifetime.
		// Replacing it with a short admission context would cancel a valid
		// transaction after this function returned.
		tx, err := q.BeginTx(ctx, options)
		if err == nil || tx != nil || !IsBusy(err) || !time.Now().Before(deadline) {
			return tx, err
		}
		lastBusy = err
		timer := time.NewTimer(min(10*time.Millisecond, time.Until(deadline)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

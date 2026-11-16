package agentapi

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// SweepExpiredClaims performs bounded 500-row transactions until caught up or
// cancelled. SKIP LOCKED lets a claim/result transaction finish without a sweep
// waiting on its row. It never starts a goroutine or takes ownership of lifecycle.
func (s *Execution) SweepExpiredClaims(ctx context.Context) (int, error) {
	total := 0
	for {
		if ctx.Err() != nil {
			return total, executionError("unavailable")
		}
		count, err := s.sweepBatch(ctx)
		total += count
		if err != nil {
			return total, err
		}
		if count < 500 {
			return total, nil
		}
	}
}
func (s *Execution) sweepBatch(ctx context.Context) (int, error) {
	return executionTransaction(ctx, s.db, func(tx *sql.Tx) (int, error) {
		now, err := dbNow(ctx, tx)
		if err != nil {
			return 0, err
		}
		rows, err := tx.QueryContext(ctx, `SELECT manual_id FROM manual_execution WHERE status='processing' AND claim_expires_at<=? ORDER BY claim_expires_at,manual_id LIMIT 500 FOR UPDATE SKIP LOCKED`, now)
		if err != nil {
			return 0, err
		}
		ids := []int64{}
		for rows.Next() {
			var id int64
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return 0, err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		closeErr := rows.Close()
		if err != nil {
			return 0, err
		}
		if closeErr != nil {
			return 0, closeErr
		}
		if len(ids) == 0 {
			return 0, nil
		}
		args := []any{now}
		for _, id := range ids {
			args = append(args, id)
		}
		args = append(args, now)
		query := `UPDATE manual_execution SET status='wait',claim_token=NULL,claim_expires_at=NULL,modified_at=? WHERE manual_id IN (` + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + `) AND status='processing' AND claim_expires_at<=?`
		_, err = tx.ExecContext(ctx, query, args...)
		return len(ids), err
	})
}

// RunClaimSweeper is blocking, owned by the future server's root context. No
// first-run side effect occurs on construction. Storage errors retry next tick;
// the callback receives only sanitized errors, never query/token contents.
func (s *Execution) RunClaimSweeper(ctx context.Context, report func(error)) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			_, err := s.SweepExpiredClaims(sweepCtx)
			cancel()
			if ctx.Err() != nil {
				return
			}
			if err != nil && report != nil {
				report(err)
			}
		}
	}
}

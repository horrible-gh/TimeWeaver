package agentapi

import (
	"bytes"
	"context"
	"crypto/subtle"
	"database/sql"
	"database/sql/driver"
	"errors"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/horrible-gh/TimeWeaver/internal/store"
)

type claimRecord struct {
	data ClaimData
	now  time.Time
}
type resultRecord struct {
	data ResultData
	now  time.Time
}
type executionTarget struct {
	schedule              int64
	groupStop, detailStop bool
}
type manualRecord struct {
	detail []byte
	status string
	token  sql.NullString
	expiry sql.NullTime
}

// Statement deadlocks alone are retried after the entire transaction has been
// rolled back. An ambiguous COMMIT is returned as unavailable, never replayed.
func executionTransaction[T any](ctx context.Context, db *store.Store, operation func(*sql.Tx) (T, error)) (T, error) {
	var zero T
	for attempt := 0; attempt < 3; attempt++ {
		if ctx.Err() != nil {
			return zero, executionError("unavailable")
		}
		result, err := executionOnce(ctx, db, operation)
		if !errors.Is(err, errIdentityDeadlock) {
			return result, err
		}
		if attempt == 2 {
			break
		}
		delay := 20 * time.Millisecond
		if attempt == 1 {
			delay = 50 * time.Millisecond
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return zero, executionError("unavailable")
		case <-timer.C:
		}
	}
	return zero, executionError("unavailable")
}
func executionOnce[T any](ctx context.Context, db *store.Store, operation func(*sql.Tx) (T, error)) (T, error) {
	var zero T
	conn, err := db.DB.Conn(ctx)
	if err != nil {
		return zero, executionError("unavailable")
	}
	defer conn.Close()
	var previous int64
	if err = conn.QueryRowContext(ctx, "SELECT @@SESSION.innodb_lock_wait_timeout").Scan(&previous); err != nil {
		return zero, executionError("unavailable")
	}
	// Reset the session setting on this exact connection, including error/cancel
	// paths. Discard rather than return a contaminated connection to the pool.
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, e := conn.ExecContext(cleanup, "SET SESSION innodb_lock_wait_timeout = ?", previous); e != nil {
			conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}()
	if _, err = conn.ExecContext(ctx, "SET SESSION innodb_lock_wait_timeout = 3"); err != nil {
		return zero, executionError("unavailable")
	}
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return zero, executionError("unavailable")
	}
	defer func() {
		if e := tx.Rollback(); e != nil && !errors.Is(e, sql.ErrTxDone) {
			conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}()
	result, err := operation(tx)
	if err != nil {
		var api *APIError
		if errors.As(err, &api) {
			return zero, err
		}
		return zero, identityStatementError(err)
	}
	if err = tx.Commit(); err != nil {
		return zero, executionError("unavailable")
	}
	return result, nil
}

// Lock ordering matches the reference: schedule_group -> schedule_detail ->
// manual_execution. The unlocked candidate is rechecked after taking the locks.
func lockExecutionTarget(ctx context.Context, tx *sql.Tx, deviceID, expectedSchedule int64, detailID []byte, allowDeleted bool) (executionTarget, error) {
	var target executionTarget
	suffix := ""
	if !allowDeleted {
		suffix = " AND deleted_at IS NULL"
	}
	if err := tx.QueryRowContext(ctx, "SELECT schedule_id FROM schedule_detail WHERE detail_id=?"+suffix, detailID).Scan(&target.schedule); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return target, executionError("not_found")
		}
		return target, err
	}
	var device, groupStop, detailStop sql.NullInt64
	if err := tx.QueryRowContext(ctx, "SELECT target_device,is_error_stop FROM schedule_group WHERE schedule_id=? FOR UPDATE", target.schedule).Scan(&device, &groupStop); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return target, executionError("not_found")
		}
		return target, err
	}
	var lockedSchedule int64
	if err := tx.QueryRowContext(ctx, "SELECT schedule_id,is_error_stop FROM schedule_detail WHERE detail_id=?"+suffix+" FOR UPDATE", detailID).Scan(&lockedSchedule, &detailStop); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return target, executionError("not_found")
		}
		return target, err
	}
	if !device.Valid || device.Int64 != deviceID || target.schedule != expectedSchedule || lockedSchedule != expectedSchedule {
		return target, executionError("not_found")
	}
	target.groupStop, target.detailStop = groupStop.Int64 != 0, detailStop.Int64 != 0
	return target, nil
}
func lockManual(ctx context.Context, tx *sql.Tx, id int64) (manualRecord, error) {
	var r manualRecord
	err := tx.QueryRowContext(ctx, `SELECT detail_id,status,claim_token,claim_expires_at FROM manual_execution WHERE manual_id=? FOR UPDATE`, id).Scan(&r.detail, &r.status, &r.token, &r.expiry)
	if errors.Is(err, sql.ErrNoRows) {
		return r, executionError("not_found")
	}
	return r, err
}
func (s *Execution) claim(ctx context.Context, deviceID, manualID int64, token string) (claimRecord, error) {
	return executionTransaction(ctx, s.db, func(tx *sql.Tx) (claimRecord, error) {
		var result claimRecord
		var detail []byte
		var schedule int64
		err := tx.QueryRowContext(ctx, `SELECT me.detail_id,sd.schedule_id FROM manual_execution me JOIN schedule_detail sd ON sd.detail_id=me.detail_id AND sd.deleted_at IS NULL WHERE me.manual_id=?`, manualID).Scan(&detail, &schedule)
		if errors.Is(err, sql.ErrNoRows) {
			return result, executionError("not_found")
		}
		if err != nil {
			return result, err
		}
		if _, err = lockExecutionTarget(ctx, tx, deviceID, schedule, detail, false); err != nil {
			return result, err
		}
		manual, err := lockManual(ctx, tx, manualID)
		if err != nil {
			return result, err
		}
		if !bytes.Equal(detail, manual.detail) {
			return result, executionError("not_found")
		}
		now, err := dbNow(ctx, tx)
		if err != nil {
			return result, err
		}
		if manual.status == "processing" && manual.expiry.Valid && !manual.expiry.Time.After(now) {
			if _, err = tx.ExecContext(ctx, `UPDATE manual_execution SET status='wait',claim_token=NULL,claim_expires_at=NULL,modified_at=? WHERE manual_id=?`, now, manualID); err != nil {
				return result, err
			}
			manual.status = "wait"
		}
		if manual.status != "wait" {
			return result, executionError("already_claimed")
		}
		expires := now.Add(claimTTL)
		if _, err = tx.ExecContext(ctx, `UPDATE manual_execution SET status='processing',claim_token=?,claim_expires_at=?,modified_at=? WHERE manual_id=?`, token, expires, now, manualID); err != nil {
			return result, err
		}
		return claimRecord{ClaimData{manualID, token, expires.UTC(), nil}, now}, nil
	})
}
func (s *Execution) start(ctx context.Context, deviceID int64, r ExecutionStart, execID, detailID []byte) (time.Time, error) {
	return executionTransaction(ctx, s.db, func(tx *sql.Tx) (time.Time, error) {
		if _, err := lockExecutionTarget(ctx, tx, deviceID, r.ScheduleID, detailID, false); err != nil {
			return time.Time{}, err
		}
		now, err := dbNow(ctx, tx)
		if err != nil {
			return time.Time{}, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO execution_running(schedule_id,detail_id,execution_grp_id,attempt,start_time) VALUES (?,?,?,?,?) ON DUPLICATE KEY UPDATE execution_grp_id=VALUES(execution_grp_id),attempt=VALUES(attempt),start_time=VALUES(start_time)`, r.ScheduleID, detailID, execID, r.Attempt, r.StartedAt)
		return now, err
	})
}

type storedResult struct {
	id, schedule, code   int64
	manual               sql.NullInt64
	started              time.Time
	finished             sql.NullTime
	message, environment sql.NullString
}

func findExecutionResult(ctx context.Context, tx *sql.Tx, execID, detailID []byte, attempt int64) (storedResult, error) {
	var v storedResult
	err := tx.QueryRowContext(ctx, `SELECT execution_id,schedule_id,manual_id,start_time,end_time,result_code,result_message,environment_info FROM execution_log WHERE execution_grp_id=? AND detail_id=? AND attempt=?`, execID, detailID, attempt).Scan(&v.id, &v.schedule, &v.manual, &v.started, &v.finished, &v.code, &v.message, &v.environment)
	return v, err
}
func sameOptionalText(stored sql.NullString, value *string) bool {
	return stored.Valid == (value != nil) && (value == nil || stored.String == *value)
}
func duplicateResult(stored storedResult, r ExecutionResult, env *string, now time.Time) (resultRecord, error) {
	if stored.schedule != r.ScheduleID || stored.manual.Valid != (r.ManualID != nil) || (r.ManualID != nil && stored.manual.Int64 != *r.ManualID) || !stored.started.Equal(r.StartedAt) || !stored.finished.Valid || !stored.finished.Time.Equal(r.FinishedAt) || stored.code != r.ResultCode || !sameOptionalText(stored.message, r.ResultMessage) || !sameOptionalText(stored.environment, env) {
		return resultRecord{}, executionError("invalid_request")
	}
	return resultRecord{ResultData{stored.id, true, []AppliedTransition{}}, now}, nil
}
func clearExecutionRunning(ctx context.Context, tx *sql.Tx, r ExecutionStart, detailID []byte) error {
	// Deliberately retain the legacy schedule/detail scope. Attempt-specific
	// running-state semantics are a separate protocol change, not part of porting.
	_, err := tx.ExecContext(ctx, "DELETE FROM execution_running WHERE schedule_id=? AND detail_id=?", r.ScheduleID, detailID)
	return err
}
func (s *Execution) result(ctx context.Context, deviceID int64, r ExecutionResult, execID, detailID []byte, env *string) (resultRecord, error) {
	return executionTransaction(ctx, s.db, func(tx *sql.Tx) (resultRecord, error) {
		var out resultRecord
		target, err := lockExecutionTarget(ctx, tx, deviceID, r.ScheduleID, detailID, true)
		if err != nil {
			return out, err
		}
		now, err := dbNow(ctx, tx)
		if err != nil {
			return out, err
		}
		// A matching durable result is acknowledged BEFORE checking a now-cleared
		// manual lease, preserving outbox retries after a successful completion.
		existing, err := findExecutionResult(ctx, tx, execID, detailID, r.Attempt)
		if err == nil {
			out, err = duplicateResult(existing, r, env, now)
			if err == nil {
				err = clearExecutionRunning(ctx, tx, r.ExecutionStart, detailID)
			}
			return out, err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return out, err
		}
		if r.ManualID != nil {
			manual, err := lockManual(ctx, tx, *r.ManualID)
			if err != nil {
				return out, err
			}
			if !bytes.Equal(manual.detail, detailID) {
				return out, executionError("not_found")
			}
			if manual.status != "processing" || subtle.ConstantTimeCompare([]byte(manual.token.String), []byte(*r.ClaimToken)) != 1 || !manual.expiry.Valid || !manual.expiry.Time.After(now) {
				return out, executionError("claim_expired")
			}
		}
		inserted, err := tx.ExecContext(ctx, `INSERT INTO execution_log(execution_grp_id,schedule_id,detail_id,attempt,manual_id,start_time,end_time,result_code,result_message,environment_info) VALUES (?,?,?,?,?,?,?,?,?,?)`, execID, r.ScheduleID, detailID, r.Attempt, r.ManualID, r.StartedAt, r.FinishedAt, r.ResultCode, r.ResultMessage, env)
		if err != nil {
			var dbErr *mysql.MySQLError
			if !errors.As(err, &dbErr) || dbErr.Number != 1062 {
				return out, err
			}
			existing, lookupErr := findExecutionResult(ctx, tx, execID, detailID, r.Attempt)
			if lookupErr != nil {
				return out, err
			}
			out, err = duplicateResult(existing, r, env, now)
			if err == nil {
				err = clearExecutionRunning(ctx, tx, r.ExecutionStart, detailID)
			}
			return out, err
		}
		id, err := inserted.LastInsertId()
		if err != nil {
			return out, err
		}
		if err = clearExecutionRunning(ctx, tx, r.ExecutionStart, detailID); err != nil {
			return out, err
		}
		transitions := []AppliedTransition{}
		switch {
		case r.ManualID != nil:
			status := "done"
			if r.ResultCode != 0 {
				status = "failed"
			}
			_, err = tx.ExecContext(ctx, `UPDATE manual_execution SET status=?,claim_token=NULL,claim_expires_at=NULL,modified_at=? WHERE manual_id=?`, status, now, *r.ManualID)
			transitions = append(transitions, AppliedTransition{"manual_execution", *r.ManualID, status})
		case r.ResultCode != 0 && target.groupStop:
			_, err = tx.ExecContext(ctx, `UPDATE schedule_group SET status='error',modified_at=? WHERE schedule_id=?`, now, r.ScheduleID)
			transitions = append(transitions, AppliedTransition{"schedule_group", r.ScheduleID, "error"})
		case r.ResultCode != 0 && target.detailStop:
			_, err = tx.ExecContext(ctx, `UPDATE schedule_detail SET status='error',modified_at=? WHERE detail_id=?`, now, detailID)
			transitions = append(transitions, AppliedTransition{"schedule_detail", r.DetailID, "error"})
		}
		return resultRecord{ResultData{id, false, transitions}, now}, err
	})
}
func (s *Execution) event(ctx context.Context, deviceID int64, r AgentEvent, env *string) (time.Time, error) {
	// Append-only incident events have no idempotency key. Never replay an
	// ambiguous insert or commit; the client controls any delivery retry.
	tx, err := s.db.DB.BeginTx(ctx, nil)
	if err != nil {
		return time.Time{}, executionError("unavailable")
	}
	defer tx.Rollback()
	now, err := dbNow(ctx, tx)
	if err != nil {
		return time.Time{}, executionError("unavailable")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO agent_event(device_id,event_type,severity,occurred_at,message,environment_info) VALUES (?,?,?,?,?,?)`, deviceID, r.Type, r.Severity, r.OccurredAt, r.Message, env); err != nil {
		return time.Time{}, executionError("unavailable")
	}
	if err = tx.Commit(); err != nil {
		return time.Time{}, executionError("unavailable")
	}
	return now, nil
}

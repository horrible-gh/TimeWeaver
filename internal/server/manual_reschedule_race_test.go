package server

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/horrible-gh/TimeWeaver/internal/agentapi"
	"github.com/horrible-gh/TimeWeaver/internal/dashboard"
)

func TestMySQLManualRescheduleRejectsCachedClaim(t *testing.T) {
	f := deviceFixtureFor(t)
	ctx := context.Background()
	operator := f.sessions["operator"]
	schedule := createSchedule(t, f, 7, "cached-manual")
	detail := createTask(t, f, schedule)
	ids, err := f.dashboard.QueueManual(ctx, operator.token, operator.csrf, dashboard.ManualInput{DetailID: detail, Immediate: true})
	if err != nil || len(ids) != 1 {
		t.Fatal(ids, err)
	}
	var device int64
	var due time.Time
	if err = fixtureRow(t, f.db, "SELECT target_device,UTC_TIMESTAMP()+INTERVAL 1 HOUR FROM schedule_group WHERE schedule_id=?", schedule).Scan(&device, &due); err != nil {
		t.Fatal(err)
	}
	runtime, err := agentapi.NewRuntime(f.db)
	if err != nil {
		t.Fatal(err)
	}
	principal := agentapi.DeviceIdentity{DeviceID: device}
	stale, err := runtime.Snapshot(ctx, principal)
	if err != nil || len(stale.Data.ManualRuns) != 1 || !stale.Data.ManualRuns[0].Claimable {
		t.Fatal("missing stale immediate snapshot", err)
	}
	replacement, err := f.dashboard.UpdateManual(ctx, operator.token, operator.csrf, dashboard.ManualInput{ManualID: ids[0], ScheduleDatetime: due.UTC().Format(time.RFC3339)})
	if err != nil || replacement == ids[0] || replacement <= 0 {
		t.Fatal("reschedule reused a dispatchable ID", replacement, err)
	}
	executions, err := agentapi.NewExecution(f.db)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = executions.Claim(ctx, principal, stale.Data.ManualRuns[0].ID)
	expectCode(t, err, "already_claimed")
	_, _, err = executions.Claim(ctx, principal, replacement)
	expectCode(t, err, "not_due")
	var oldStatus, newStatus string
	var oldToken, newToken sql.NullString
	var storedDue time.Time
	if err = fixtureRow(t, f.db, "SELECT status,claim_token FROM manual_execution WHERE manual_id=?", ids[0]).Scan(&oldStatus, &oldToken); err != nil || oldStatus != "failed" || oldToken.Valid {
		t.Fatal("old request was not retired", oldStatus, err)
	}
	if err = fixtureRow(t, f.db, "SELECT status,claim_token,schedule_datetime FROM manual_execution WHERE manual_id=?", replacement).Scan(&newStatus, &newToken, &storedDue); err != nil || newStatus != "wait" || newToken.Valid || !storedDue.Equal(due) {
		t.Fatal("replacement reservation changed", newStatus, storedDue, err)
	}
	current, err := runtime.Snapshot(ctx, principal)
	if err != nil || len(current.Data.ManualRuns) != 1 || current.Data.ManualRuns[0].ID != replacement || current.Data.ManualRuns[0].Claimable {
		t.Fatal("snapshot contains stale or prematurely runnable request", err)
	}
}

func TestMySQLManualRescheduleAfterClaimHasNoReplacement(t *testing.T) {
	f := deviceFixtureFor(t)
	ctx := context.Background()
	operator := f.sessions["operator"]
	schedule := createSchedule(t, f, 7, "claimed-manual")
	detail := createTask(t, f, schedule)
	ids, err := f.dashboard.QueueManual(ctx, operator.token, operator.csrf, dashboard.ManualInput{DetailID: detail, Immediate: true})
	if err != nil || len(ids) != 1 {
		t.Fatal(ids, err)
	}
	var device int64
	if err = fixtureRow(t, f.db, "SELECT target_device FROM schedule_group WHERE schedule_id=?", schedule).Scan(&device); err != nil {
		t.Fatal(err)
	}
	executions, err := agentapi.NewExecution(f.db)
	if err != nil {
		t.Fatal(err)
	}
	claim, _, err := executions.Claim(ctx, agentapi.DeviceIdentity{DeviceID: device}, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := f.dashboard.UpdateManual(ctx, operator.token, operator.csrf, dashboard.ManualInput{ManualID: ids[0], Immediate: true})
	expectCode(t, err, "manual_not_pending")
	if replacement != 0 {
		t.Fatal("rejected reschedule returned a replacement")
	}
	var count int
	var status, token string
	if err = fixtureRow(t, f.db, "SELECT COUNT(*) FROM manual_execution").Scan(&count); err != nil || count != 1 {
		t.Fatal("claimed request was duplicated", count, err)
	}
	if err = fixtureRow(t, f.db, "SELECT status,claim_token FROM manual_execution WHERE manual_id=?", ids[0]).Scan(&status, &token); err != nil || status != "processing" || token != claim.Token {
		t.Fatal("claimed request changed", status, err)
	}
}

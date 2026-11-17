package agentapi

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

func TestManualRunDue(t *testing.T) {
	now := time.Date(2026, 11, 18, 1, 2, 3, 0, time.UTC)
	for _, tc := range []struct {
		name      string
		immediate bool
		scheduled time.Time
		want      bool
	}{
		{"future reservation", false, now.Add(time.Second), false},
		{"inclusive due second", false, now, true},
		{"overdue reservation", false, now.Add(-time.Second), true},
		{"immediate with legacy future date", true, now.Add(time.Hour), true},
		{"missing reserved date", false, time.Time{}, false},
		{"immediate with no date", true, time.Time{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := manualRunDue(tc.immediate, tc.scheduled, now); got != tc.want {
				t.Fatalf("due=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestMySQLManualReservationClaimBoundary(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	db := f.runtime.db.DB
	if _, err := db.Exec(`UPDATE manual_execution SET is_immediate=0,schedule_datetime=UTC_TIMESTAMP()+INTERVAL 1 HOUR WHERE manual_id=41`); err != nil {
		t.Fatal(err)
	}
	snapshot, err := f.runtime.Snapshot(ctx, f.principal)
	if err != nil || len(snapshot.Data.ManualRuns) != 1 || snapshot.Data.ManualRuns[0].Claimable {
		t.Fatal("future request was dispatchable", snapshot.Data.ManualRuns, err)
	}
	_, _, err = f.service.Claim(ctx, f.principal, 41)
	requireCode(t, err, 409, "not_due")
	var status string
	var token sql.NullString
	if err = db.QueryRow("SELECT status,claim_token FROM manual_execution WHERE manual_id=41").Scan(&status, &token); err != nil || status != "wait" || token.Valid {
		t.Fatal("early claim mutated queue", status, token.Valid, err)
	}
	// Releasing an expired lease does not make a future reservation due.
	if _, err = db.Exec(`UPDATE manual_execution SET status='processing',claim_token=?,claim_expires_at=UTC_TIMESTAMP()-INTERVAL 1 SECOND WHERE manual_id=41`, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	snapshot, err = f.runtime.Snapshot(ctx, f.principal)
	if err != nil || snapshot.Data.ManualRuns[0].Claimable {
		t.Fatal("expired lease bypassed reservation", err)
	}
	_, _, err = f.service.Claim(ctx, f.principal, 41)
	requireCode(t, err, 409, "not_due")
	if n, e := f.service.SweepExpiredClaims(ctx); e != nil || n != 1 {
		t.Fatal(n, e)
	}
	_, _, err = f.service.Claim(ctx, f.principal, 41)
	requireCode(t, err, 409, "not_due")
	// Existing immediate rows ignore a stale/future schedule_datetime value.
	if _, err = db.Exec(`UPDATE manual_execution SET is_immediate=1 WHERE manual_id=41`); err != nil {
		t.Fatal(err)
	}
	snapshot, err = f.runtime.Snapshot(ctx, f.principal)
	if err != nil || !snapshot.Data.ManualRuns[0].Claimable {
		t.Fatal("immediate request blocked", err)
	}
	if _, _, err = f.service.Claim(ctx, f.principal, 41); err != nil {
		t.Fatal("immediate request rejected", err)
	}
	if _, err = db.Exec(`UPDATE manual_execution SET status='wait',is_immediate=0,schedule_datetime=UTC_TIMESTAMP(),claim_token=NULL,claim_expires_at=NULL WHERE manual_id=41`); err != nil {
		t.Fatal(err)
	}
	if _, _, err = f.service.Claim(ctx, f.principal, 41); err != nil {
		t.Fatal("due second was not inclusive", err)
	}
}

func TestMySQLManualReservationRevisionChangesWhenDue(t *testing.T) {
	f := newExecutionFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	if _, err := f.runtime.db.DB.ExecContext(ctx, `UPDATE manual_execution SET is_immediate=0,schedule_datetime=UTC_TIMESTAMP()+INTERVAL 5 SECOND WHERE manual_id=41`); err != nil {
		t.Fatal(err)
	}
	before, err := f.runtime.Snapshot(ctx, f.principal)
	if err != nil || len(before.Data.ManualRuns) != 1 || before.Data.ManualRuns[0].Claimable {
		t.Fatal("initial reservation should not be due", err)
	}
	due := before.Data.ManualRuns[0].ScheduledAt
	// No writer changes this row. Crossing database time alone must change the
	// content hash, otherwise a conditional poll can keep the agent idle forever.
	for {
		next, e := f.runtime.Snapshot(ctx, f.principal)
		if e != nil {
			t.Fatal(e)
		}
		if next.Data.ManualRuns[0].Claimable {
			if next.ETag == before.ETag || next.Data.Revision == before.Data.Revision || next.Data.GeneratedAt.Before(due) || !next.Data.ManualRuns[0].ScheduledAt.Equal(due) {
				t.Fatal("due-time revision transition is incorrect")
			}
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("reservation never became dispatchable")
		case <-time.After(100 * time.Millisecond):
		}
	}
	if _, _, err = f.service.Claim(ctx, f.principal, 41); err != nil {
		t.Fatal("newly due request was not claimable", err)
	}
}

package agentapi

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

func TestMySQLExecutionFailureRollsBackResultAndTransition(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	db := f.runtime.db.DB
	claim, _, err := f.service.Claim(ctx, f.principal, 41)
	if err != nil {
		t.Fatal(err)
	}
	// Force a later statement to fail AFTER the insert, on this disposable DB.
	if _, err = db.Exec("RENAME TABLE execution_running TO execution_running_unavailable"); err != nil {
		t.Fatal(err)
	}
	r := executionBody()
	m := int64(41)
	r.ManualID = &m
	r.ClaimToken = &claim.Token
	r.ResultCode = 1
	_, _, err = f.service.Result(ctx, f.principal, r.ExecutionID, resultKey(r), r)
	requireCode(t, err, 503, "unavailable")
	if strings.Contains(err.Error(), claim.Token) {
		t.Fatal("token leaked")
	}
	var count int
	var status string
	var token sql.NullString
	if err = db.QueryRow("SELECT COUNT(*) FROM execution_log").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow("SELECT status,claim_token FROM manual_execution WHERE manual_id=41").Scan(&status, &token); err != nil {
		t.Fatal(err)
	}
	if count != 0 || status != "processing" || token.String != claim.Token {
		t.Fatal("transaction partially committed")
	}
	// Restoring storage permits the exact same outbox request to succeed once.
	if _, err = db.Exec("RENAME TABLE execution_running_unavailable TO execution_running"); err != nil {
		t.Fatal(err)
	}
	if _, _, err = f.service.Result(ctx, f.principal, r.ExecutionID, resultKey(r), r); err != nil {
		t.Fatal(err)
	}
}
func TestMySQLExecutionSweepSkipsLockedAndFutureRows(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	db := f.runtime.db.DB
	if _, err := db.Exec(`UPDATE manual_execution SET status='processing',claim_token=?,claim_expires_at=UTC_TIMESTAMP()-INTERVAL 1 SECOND WHERE manual_id=41`, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO manual_execution(manual_id,detail_id,status,claim_token,claim_expires_at) VALUES (42,UNHEX('11111111111111111111111111111111'),'processing',?,UTC_TIMESTAMP()+INTERVAL 1 HOUR),(43,UNHEX('11111111111111111111111111111111'),'processing',?,NULL)`, strings.Repeat("b", 64), strings.Repeat("c", 64)); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var id int
	if err = tx.QueryRow("SELECT manual_id FROM manual_execution WHERE manual_id=41 FOR UPDATE").Scan(&id); err != nil {
		t.Fatal(err)
	}
	deadline, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	n, err := f.service.SweepExpiredClaims(deadline)
	if err != nil || n != 0 {
		t.Fatal("locked row was not skipped", n, err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	n, err = f.service.SweepExpiredClaims(ctx)
	if err != nil || n != 1 {
		t.Fatal(n, err)
	}
	var retained int
	if err = db.QueryRow("SELECT COUNT(*) FROM manual_execution WHERE manual_id IN (42,43) AND status='processing' AND claim_token IS NOT NULL").Scan(&retained); err != nil || retained != 2 {
		t.Fatal(retained, err)
	}
}
func TestMySQLExecutionSessionSettingsRestored(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	db := f.runtime.db.DB
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.Exec("SET SESSION innodb_lock_wait_timeout=9"); err != nil {
		t.Fatal(err)
	}
	for _, device := range []int64{f.principal.DeviceID, f.principal.DeviceID + 1} {
		r := executionBody()
		p := f.principal
		p.DeviceID = device
		_, _ = f.service.Start(ctx, p, r.ExecutionID, r.ExecutionStart)
		var timeout int
		if err := db.QueryRow("SELECT @@SESSION.innodb_lock_wait_timeout").Scan(&timeout); err != nil || timeout != 9 {
			t.Fatal("pool session leaked", timeout, err)
		}
	}
}

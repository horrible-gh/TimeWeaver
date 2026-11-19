package agentapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/horrible-gh/TimeWeaver/internal/store"
)

func TestSQLiteExecutionClaimAndLostAcknowledgmentAcrossInstances(t *testing.T) {
	first, second, _ := sqliteAgentStores(t)
	f := newExecutionFixtureOn(t, first)
	other, err := NewExecution(second)
	if err != nil {
		t.Fatal(err)
	}
	services := []*Execution{f.service, other}
	ctx := context.Background()
	type claimed struct {
		data ClaimData
		err  error
	}
	const workers = 12
	start := make(chan struct{})
	claims := make(chan claimed, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		service := services[i%2]
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			v, _, err := service.Claim(ctx, f.principal, 41)
			claims <- claimed{v, err}
		}()
	}
	close(start)
	wg.Wait()
	close(claims)
	wins := 0
	var winner ClaimData
	for claim := range claims {
		if claim.err == nil {
			wins++
			winner = claim.data
		} else {
			requireCode(t, claim.err, 409, "already_claimed")
		}
	}
	if wins != 1 || !validClaimToken(winner.Token) || winner.ExecutionID != nil {
		t.Fatal("claim was not exclusive across server instances", wins)
	}
	r := executionBody()
	for _, attempt := range []int64{1, 2, 1} {
		value := r.ExecutionStart
		value.Attempt = attempt
		if _, err = other.Start(ctx, f.principal, r.ExecutionID, value); err != nil {
			t.Fatal("running-state upsert failed", err)
		}
	}
	var running int
	if err = first.DB.QueryRow("SELECT COUNT(*) FROM execution_running WHERE schedule_id=12 AND attempt=1").Scan(&running); err != nil || running != 1 {
		t.Fatal("running upsert duplicated the task", running, err)
	}
	manual := int64(41)
	r.ManualID, r.ClaimToken = &manual, &winner.Token
	r.ResultCode = 1
	// A task removed after dispatch must still accept its result and outbox ACK.
	if _, err = second.DB.Exec("UPDATE schedule_detail SET deleted_at=CURRENT_TIMESTAMP WHERE schedule_id=12"); err != nil {
		t.Fatal(err)
	}
	type reported struct {
		data ResultData
		err  error
	}
	results := make(chan reported, workers)
	start = make(chan struct{})
	for i := 0; i < workers; i++ {
		service := services[i%2]
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			v, _, err := service.Result(ctx, f.principal, r.ExecutionID, resultKey(r), r)
			results <- reported{v, err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	wins, duplicates := 0, 0
	var resultID int64
	for result := range results {
		if result.err != nil {
			t.Fatal("identical result replay failed", result.err)
		}
		if resultID == 0 {
			resultID = result.data.ExecutionID
		}
		if resultID != result.data.ExecutionID || resultID <= 0 {
			t.Fatal("result retries returned different identities")
		}
		if result.data.Duplicate {
			duplicates++
			if len(result.data.Transitions) != 0 {
				t.Fatal("replay repeated state transitions")
			}
		} else {
			wins++
			if len(result.data.Transitions) != 1 || result.data.Transitions[0].Status != "failed" {
				t.Fatal("manual result transition was lost", result.data)
			}
		}
	}
	if wins != 1 || duplicates != workers-1 {
		t.Fatal("idempotency key committed more than once", wins, duplicates)
	}
	var logs int
	var status string
	var lease sql.NullString
	if err = first.DB.QueryRow("SELECT COUNT(*) FROM execution_log").Scan(&logs); err != nil || logs != 1 {
		t.Fatal("lost-ACK retry duplicated the execution log", logs, err)
	}
	if err = first.DB.QueryRow("SELECT COUNT(*) FROM execution_running").Scan(&running); err != nil || running != 0 {
		t.Fatal("result did not clear running state", running, err)
	}
	if err = first.DB.QueryRow("SELECT status,claim_token FROM manual_execution WHERE manual_id=41").Scan(&status, &lease); err != nil || status != "failed" || lease.Valid {
		t.Fatal("manual completion did not clear its lease", status, lease.Valid, err)
	}
	changed := r
	changed.ResultMessage = strptr("different result")
	_, _, err = other.Result(ctx, f.principal, r.ExecutionID, resultKey(r), changed)
	requireCode(t, err, 422, "invalid_request")
	wrong := f.principal
	wrong.DeviceID++
	_, _, err = other.Result(ctx, wrong, r.ExecutionID, resultKey(r), r)
	requireCode(t, err, 404, "not_found")
	// HTTP transport also acknowledges a completed manual run before checking
	// the now-cleared lease, including the legacy trailing-slash route.
	body, _ := json.Marshal(r)
	w := runtimeRequest(f.handler, "POST", "/timeweaver_svr/api/agent/v1/executions/"+r.ExecutionID+"/results/", string(body), f.token.AccessToken, map[string]string{"Idempotency-Key": resultKey(r)})
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"duplicate":true`) || w.Header().Get("Location") != "" {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestSQLiteExecutionFailureRollsBackLogLeaseAndTransition(t *testing.T) {
	first, second, _ := sqliteAgentStores(t)
	f := newExecutionFixtureOn(t, first)
	ctx := context.Background()
	claim, _, err := f.service.Claim(ctx, f.principal, 41)
	if err != nil {
		t.Fatal(err)
	}
	r := executionBody()
	manual := int64(41)
	r.ManualID, r.ClaimToken, r.ResultCode = &manual, &claim.Token, 1
	if _, err = f.service.Start(ctx, f.principal, r.ExecutionID, r.ExecutionStart); err != nil {
		t.Fatal(err)
	}
	// Fail after the log insert and running-row deletion, while transitioning
	// the manual lease. Every earlier statement must be rolled back.
	if _, err = second.DB.Exec(`CREATE TRIGGER reject_manual_result BEFORE UPDATE OF status ON manual_execution WHEN NEW.status='failed' BEGIN SELECT RAISE(ABORT,'fixture completion storage unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	_, _, err = f.service.Result(ctx, f.principal, r.ExecutionID, resultKey(r), r)
	requireCode(t, err, 503, "unavailable")
	if strings.Contains(err.Error(), claim.Token) || strings.Contains(err.Error(), "fixture") {
		t.Fatal("storage error exposed internal details")
	}
	var logs, running int
	var status, token string
	if err = second.DB.QueryRow("SELECT COUNT(*) FROM execution_log").Scan(&logs); err != nil || logs != 0 {
		t.Fatal("failed completion committed a log", logs, err)
	}
	if err = second.DB.QueryRow("SELECT COUNT(*) FROM execution_running").Scan(&running); err != nil || running != 1 {
		t.Fatal("failed completion removed running state", running, err)
	}
	if err = second.DB.QueryRow("SELECT status,claim_token FROM manual_execution WHERE manual_id=41").Scan(&status, &token); err != nil || status != "processing" || token != claim.Token {
		t.Fatal("failed completion changed the lease", status, err)
	}
	if _, err = second.DB.Exec("DROP TRIGGER reject_manual_result"); err != nil {
		t.Fatal(err)
	}
	if _, _, err = f.service.Result(ctx, f.principal, r.ExecutionID, resultKey(r), r); err != nil {
		t.Fatal("rolled-back result could not be retried", err)
	}
	// Scheduled failures retain the established group-before-detail stop rule.
	if _, err = second.DB.Exec("UPDATE schedule_group SET is_error_stop=1 WHERE schedule_id=12"); err != nil {
		t.Fatal(err)
	}
	if _, err = second.DB.Exec("UPDATE schedule_detail SET is_error_stop=1 WHERE schedule_id=12"); err != nil {
		t.Fatal(err)
	}
	r.ExecutionID, r.ManualID, r.ClaimToken = "33333333-3333-3333-3333-333333333333", nil, nil
	result, _, err := f.service.Result(ctx, f.principal, r.ExecutionID, resultKey(r), r)
	if err != nil || len(result.Transitions) != 1 || result.Transitions[0].Target != "schedule_group" {
		t.Fatal("error-stop precedence changed", result, err)
	}
	if _, err = f.service.Event(ctx, f.principal, AgentEvent{Type: "sync_error", Severity: "error", OccurredAt: time.Now().UTC(), Message: strptr("SQLite event"), Environment: map[string]any{"detail": "한글"}}); err != nil {
		t.Fatal("event append failed", err)
	}
	var events int
	if err = first.DB.QueryRow("SELECT COUNT(*) FROM agent_event WHERE device_id=?", f.principal.DeviceID).Scan(&events); err != nil || events != 1 {
		t.Fatal(events, err)
	}
}

func TestSQLiteManualReservationAndConcurrentSweep(t *testing.T) {
	first, second, _ := sqliteAgentStores(t)
	f := newExecutionFixtureOn(t, first)
	other, err := NewExecution(second)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now, err := store.DBNow(ctx, first.DB, first.Dialect)
	if err != nil {
		t.Fatal(err)
	}
	future := now.Add(time.Hour).In(time.FixedZone("JST", 9*3600)).Format(time.RFC3339)
	if _, err = first.DB.Exec("UPDATE manual_execution SET is_immediate=0,schedule_datetime=? WHERE manual_id=41", future); err != nil {
		t.Fatal(err)
	}
	snapshot, err := f.runtime.Snapshot(ctx, f.principal)
	if err != nil || len(snapshot.Data.ManualRuns) != 1 || snapshot.Data.ManualRuns[0].Claimable || !snapshot.Data.ManualRuns[0].ScheduledAt.Equal(now.Add(time.Hour)) {
		t.Fatal("future reservation was dispatchable or changed timezone", err)
	}
	_, _, err = other.Claim(ctx, f.principal, 41)
	requireCode(t, err, 409, "not_due")
	if _, err = first.DB.Exec("UPDATE manual_execution SET schedule_datetime=NULL WHERE manual_id=41"); err != nil {
		t.Fatal(err)
	}
	snapshot, err = f.runtime.Snapshot(ctx, f.principal)
	if err != nil || snapshot.Data.ManualRuns[0].Claimable {
		t.Fatal("missing reservation time became immediate", err)
	}
	if _, err = first.DB.Exec("UPDATE manual_execution SET is_immediate=1 WHERE manual_id=41"); err != nil {
		t.Fatal(err)
	}
	claim, _, err := other.Claim(ctx, f.principal, 41)
	if err != nil {
		t.Fatal("legacy immediate request with no date was rejected", err)
	}
	expired := now.Add(-time.Hour).In(time.FixedZone("JST", 9*3600)).Format(time.RFC3339)
	if _, err = first.DB.Exec("UPDATE manual_execution SET claim_expires_at=? WHERE manual_id=41", expired); err != nil {
		t.Fatal(err)
	}
	r := executionBody()
	manual := int64(41)
	r.ManualID, r.ClaimToken = &manual, &claim.Token
	_, _, err = other.Result(ctx, f.principal, r.ExecutionID, resultKey(r), r)
	requireCode(t, err, 410, "claim_expired")
	tx, err := first.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for id := 1000; id < 1500; id++ {
		if _, err = tx.Exec("INSERT INTO manual_execution(manual_id,detail_id,status,claim_token,claim_expires_at) VALUES(?,?,'processing',?,?)", id, sqliteDetail(), strings.Repeat("a", 64), expired); err != nil {
			t.Fatal(err)
		}
	}
	for id, expiry := range map[int]any{2000: future, 2001: nil} {
		if _, err = tx.Exec("INSERT INTO manual_execution(manual_id,detail_id,status,claim_token,claim_expires_at) VALUES(?,?,'processing',?,?)", id, sqliteDetail(), strings.Repeat("b", 64), expiry); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	type swept struct {
		count int
		err   error
	}
	results := make(chan swept, 2)
	for _, service := range []*Execution{f.service, other} {
		go func() { n, err := service.SweepExpiredClaims(ctx); results <- swept{n, err} }()
	}
	total := 0
	for i := 0; i < 2; i++ {
		r := <-results
		if r.err != nil {
			t.Fatal(r.err)
		}
		total += r.count
	}
	if total != 501 {
		t.Fatal("cross-instance sweeps double-counted or lost expired leases", total)
	}
	var waiting, retained int
	if err = first.DB.QueryRow("SELECT COUNT(*) FROM manual_execution WHERE status='wait' AND claim_token IS NULL").Scan(&waiting); err != nil || waiting != 501 {
		t.Fatal("sweep did not release every expired lease", waiting, err)
	}
	if err = first.DB.QueryRow("SELECT COUNT(*) FROM manual_execution WHERE manual_id IN (2000,2001) AND status='processing' AND claim_token IS NOT NULL").Scan(&retained); err != nil || retained != 2 {
		t.Fatal("sweep released a future or undated lease", retained, err)
	}
	if _, _, err = other.Claim(ctx, f.principal, 41); err != nil {
		t.Fatal("swept immediate run could not be reclaimed", err)
	}
}

func TestSQLiteExecutionCanceledWriterDoesNotMutateQueue(t *testing.T) {
	first, second, _ := sqliteAgentStores(t)
	f := newExecutionFixtureOn(t, first)
	lock, err := second.DB.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, _, err = f.service.Claim(ctx, f.principal, 41)
	requireCode(t, err, 503, "unavailable")
	if time.Since(started) > 2*time.Second {
		t.Fatal("cancelled claim waited beyond its request budget")
	}
	if err = lock.Rollback(); err != nil {
		t.Fatal(err)
	}
	var status string
	var token sql.NullString
	if err = second.DB.QueryRow("SELECT status,claim_token FROM manual_execution WHERE manual_id=41").Scan(&status, &token); err != nil || status != "wait" || token.Valid {
		t.Fatal("cancelled writer changed the queue", status, err)
	}
	if _, _, err = f.service.Claim(context.Background(), f.principal, 41); err != nil {
		t.Fatal("cancelled writer left the pool unusable", err)
	}
}

func TestSQLiteExecutionWaitsForWriterBeforeClaim(t *testing.T) {
	first, second, _ := sqliteAgentStores(t)
	f := newExecutionFixtureOn(t, first)
	lock, err := second.DB.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	type claimResult struct {
		data ClaimData
		err  error
	}
	result := make(chan claimResult, 1)
	go func() {
		data, _, err := f.service.Claim(ctx, f.principal, 41)
		result <- claimResult{data, err}
	}()
	// Hold another store's write reservation beyond the driver's native busy
	// timeout. The claim must wait for admission without returning unavailable
	// or starting an operation against a stale, unlocked queue state.
	timer := time.NewTimer(350 * time.Millisecond)
	defer timer.Stop()
	select {
	case early := <-result:
		t.Fatal("claim completed while another writer held the database", early.err)
	case <-timer.C:
	}
	if err = lock.Rollback(); err != nil {
		t.Fatal(err)
	}
	var claimed claimResult
	select {
	case claimed = <-result:
	case <-ctx.Done():
		t.Fatal("claim did not resume after writer release", ctx.Err())
	}
	if claimed.err != nil {
		t.Fatal("claim failed after writer admission", claimed.err)
	}
	var status, token string
	if err = second.DB.QueryRow("SELECT status,claim_token FROM manual_execution WHERE manual_id=41").Scan(&status, &token); err != nil || status != "processing" || token != claimed.data.Token {
		t.Fatal("admitted claim did not persist its returned lease", status, err)
	}
}

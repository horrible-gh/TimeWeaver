package agentapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/horrible-gh/TimeWeaver/internal/store"
)

const executionTestID = "22222222-2222-2222-2222-222222222222"
const executionDetailID = "11111111-1111-1111-1111-111111111111"

func executionBody() ExecutionResult {
	return ExecutionResult{ExecutionStart: ExecutionStart{executionTestID, 12, executionDetailID, 1, time.Date(2026, 8, 1, 2, 0, 1, 123456000, time.UTC)}, FinishedAt: time.Date(2026, 8, 1, 2, 0, 9, 987654000, time.UTC), Environment: map[string]any{"host": "device-한글"}}
}
func resultKey(r ExecutionResult) string {
	return fmt.Sprintf("%s:%s:%d", r.ExecutionID, r.DetailID, r.Attempt)
}
func TestExecutionEnvironmentPythonVector(t *testing.T) {
	raw, err := os.ReadFile("testdata/execution-environment.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Value     map[string]any `json:"value"`
		Canonical string         `json:"canonical"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err = decoder.Decode(&fixture); err != nil {
		t.Fatal(err)
	}
	result, err := canonicalEnvironment(fixture.Value)
	if err != nil || result == nil || *result != fixture.Canonical {
		t.Fatalf("canonical environment differs: %v %v", result, err)
	}
	if v, err := canonicalEnvironment(nil); v != nil || err != nil {
		t.Fatal("null changed")
	}
	if v, err := canonicalEnvironment(map[string]any{}); err != nil || *v != "{}" {
		t.Fatal("empty changed")
	}
	if _, err := canonicalEnvironment(map[string]any{"x": strings.Repeat("가", 11000)}); err == nil {
		t.Fatal("oversized environment")
	}
	if _, err := canonicalEnvironment(map[string]any{"x": json.Number("1e400")}); err == nil {
		t.Fatal("non-finite number accepted")
	}
}
func TestExecutionValidationWithoutStorage(t *testing.T) {
	e := &Execution{}
	for _, modify := range []func(*ExecutionResult){
		func(r *ExecutionResult) { r.Attempt = 0 }, func(r *ExecutionResult) { r.ExecutionID = "invalid" },
		func(r *ExecutionResult) { r.FinishedAt = r.StartedAt.Add(-time.Nanosecond) },
		func(r *ExecutionResult) { r.StartedAt = r.StartedAt.In(time.FixedZone("JST", 9*3600)) },
		func(r *ExecutionResult) { v := int64(4); r.ManualID = &v }, func(r *ExecutionResult) { r.ClaimToken = strptr(strings.Repeat("a", 64)) },
		func(r *ExecutionResult) {
			v := int64(4)
			r.ManualID = &v
			r.ClaimToken = strptr(strings.Repeat("A", 64))
		},
	} {
		r := executionBody()
		modify(&r)
		_, _, err := e.Result(context.Background(), DeviceIdentity{}, executionTestID, resultKey(r), r)
		requireCode(t, err, 422, "invalid_request")
	}
	r := executionBody()
	_, _, err := e.Result(context.Background(), DeviceIdentity{}, executionTestID, "wrong-key", r)
	requireCode(t, err, 422, "invalid_request")
	request := httptest.NewRequest("POST", "/", strings.NewReader(`{"execution_grp_id":"22222222-2222-2222-2222-222222222222","schedule_id":"12","detail_id":"11111111-1111-1111-1111-111111111111","attempt":1.0,"started_at":"2026-08-01T02:00:01Z"}`))
	var start ExecutionStart
	if err = decodeExecutionBody(httptest.NewRecorder(), request, &start, []string{"attempt", "schedule_id"}); err != nil || start.Attempt != 1 || start.ScheduleID != 12 {
		t.Fatal(start, err)
	}
	for _, body := range []string{`null`, `[]`, `{}`, `{"attempt":null}`, `{"attempt":1} {}`, `{"attempt":1e9999}`, `{"attempt":1.5}`, `{"attempt":9223372036854775808}`, strings.Repeat(" ", executionBodyMax+1)} {
		request = httptest.NewRequest("POST", "/", strings.NewReader(body))
		if decodeExecutionBody(httptest.NewRecorder(), request, &start, []string{"attempt"}) == nil {
			t.Fatal("invalid body accepted")
		}
	}
	// The loop is cancellable and construction never starts a background writer.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e.RunClaimSweeper(ctx, nil)
	if _, err = NewExecution(nil); err == nil {
		t.Fatal("nil store accepted")
	}
}

type executionFixture struct {
	runtimeFixture
	service *Execution
	handler *ExecutionHTTP
}

func newExecutionFixture(t *testing.T) executionFixture {
	t.Helper()
	return newExecutionFixtureOn(t, identityStore(t))
}

func newExecutionFixtureOn(t *testing.T, db *store.Store) executionFixture {
	t.Helper()
	f := newRuntimeFixtureOn(t, db)
	service, err := NewExecution(f.runtime.db)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewExecutionHTTP(f.auth, service)
	if err != nil {
		t.Fatal(err)
	}
	now, err := store.DBNow(context.Background(), db.DB, db.Dialect)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.DB.Exec(`INSERT INTO manual_execution(manual_id,detail_id,status,schedule_datetime) VALUES(41,?,'wait',?)`, bytes.Repeat([]byte{0x11}, 16), store.TimeValue(db.Dialect, now))
	if err != nil {
		t.Fatal(err)
	}
	return executionFixture{f, service, handler}
}
func TestMySQLExecutionConcurrencyAndReplay(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	const count = 20
	type claimed struct {
		data ClaimData
		err  error
	}
	claims := make(chan claimed, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); v, _, err := f.service.Claim(ctx, f.principal, 41); claims <- claimed{v, err} }()
	}
	wg.Wait()
	close(claims)
	wins := 0
	for claim := range claims {
		if claim.err == nil {
			wins++
			if !validClaimToken(claim.data.Token) || claim.data.ExecutionID != nil {
				t.Fatal("invalid claim")
			}
		} else {
			requireCode(t, claim.err, 409, "already_claimed")
		}
	}
	if wins != 1 {
		t.Fatalf("claim winners %d", wins)
	}
	r := executionBody()
	if _, err := f.service.Start(ctx, f.principal, r.ExecutionID, r.ExecutionStart); err != nil {
		t.Fatal(err)
	}
	type reported struct {
		data ResultData
		err  error
	}
	results := make(chan reported, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, _, err := f.service.Result(ctx, f.principal, r.ExecutionID, resultKey(r), r)
			results <- reported{v, err}
		}()
	}
	wg.Wait()
	close(results)
	wins = 0
	ids := map[int64]bool{}
	for report := range results {
		if report.err != nil {
			t.Fatal(report.err)
		}
		if !report.data.Duplicate {
			wins++
		}
		ids[report.data.ExecutionID] = true
	}
	if wins != 1 || len(ids) != 1 {
		t.Fatal("duplicate execution rows", wins, ids)
	}
	var rows, running int
	if err := f.runtime.db.DB.QueryRow("SELECT COUNT(*) FROM execution_log").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if err := f.runtime.db.DB.QueryRow("SELECT COUNT(*) FROM execution_running").Scan(&running); err != nil {
		t.Fatal(err)
	}
	if rows != 1 || running != 0 {
		t.Fatal(rows, running)
	}
	r.ResultMessage = strptr("different")
	_, _, err := f.service.Result(ctx, f.principal, r.ExecutionID, resultKey(r), r)
	requireCode(t, err, 422, "invalid_request")
	// Two sweepers cannot reclaim the same row twice; a future claim is retained.
	if _, err = f.runtime.db.DB.Exec(`UPDATE manual_execution SET claim_expires_at=UTC_TIMESTAMP()-INTERVAL 1 SECOND WHERE manual_id=41`); err != nil {
		t.Fatal(err)
	}
	sweeps := make(chan int, 2)
	sweepErrors := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); n, e := f.service.SweepExpiredClaims(ctx); sweeps <- n; sweepErrors <- e }()
	}
	wg.Wait()
	if e := <-sweepErrors; e != nil {
		t.Fatal(e)
	}
	if e := <-sweepErrors; e != nil {
		t.Fatal(e)
	}
	if (<-sweeps)+(<-sweeps) != 1 {
		t.Fatal("sweep overlap")
	}
}
func TestMySQLExecutionManualTransitionsAndScope(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	db := f.runtime.db.DB
	claim, now, err := f.service.Claim(ctx, f.principal, 41)
	if err != nil {
		t.Fatal(err)
	}
	if claim.ExpiresAt.Sub(now) != claimTTL {
		t.Fatal("lease lifetime")
	}
	r := executionBody()
	m := int64(41)
	r.ManualID = &m
	r.ClaimToken = &claim.Token
	r.ResultCode = 1
	data, _, err := f.service.Result(ctx, f.principal, r.ExecutionID, resultKey(r), r)
	if err != nil || data.Duplicate || len(data.Transitions) != 1 || data.Transitions[0].Target != "manual_execution" || data.Transitions[0].Status != "failed" {
		t.Fatal(data, err)
	}
	// Completion clears the lease; duplicate must still succeed before lease checks.
	data, _, err = f.service.Result(ctx, f.principal, r.ExecutionID, resultKey(r), r)
	if err != nil || !data.Duplicate || len(data.Transitions) != 0 {
		t.Fatal(data, err)
	}
	var status string
	if err = db.QueryRow("SELECT status FROM schedule_group WHERE schedule_id=12").Scan(&status); err != nil || status != "active" {
		t.Fatal(status, err)
	}
	if _, err = db.Exec(`UPDATE manual_execution SET status='processing',claim_token=?,claim_expires_at=UTC_TIMESTAMP()-INTERVAL 1 SECOND WHERE manual_id=41`, claim.Token); err != nil {
		t.Fatal(err)
	}
	r.Attempt = 2
	_, _, err = f.service.Result(ctx, f.principal, r.ExecutionID, resultKey(r), r)
	requireCode(t, err, 410, "claim_expired")
	next, _, err := f.service.Claim(ctx, f.principal, 41)
	if err != nil || next.Token == claim.Token {
		t.Fatal("expired claim not rotated", err)
	}
	_, _, err = f.service.Result(ctx, f.principal, r.ExecutionID, resultKey(r), r)
	requireCode(t, err, 410, "claim_expired")
	r.ClaimToken = &next.Token
	r.ResultCode = 0
	data, _, err = f.service.Result(ctx, f.principal, r.ExecutionID, resultKey(r), r)
	if err != nil || data.Transitions[0].Status != "done" {
		t.Fatal(data, err)
	}
	other := f.principal
	other.DeviceID++
	_, err = f.service.Start(ctx, other, r.ExecutionID, r.ExecutionStart)
	requireCode(t, err, 404, "not_found")
	_, _, err = f.service.Result(ctx, other, r.ExecutionID, resultKey(r), r)
	requireCode(t, err, 404, "not_found")
	_, _, err = f.service.Claim(ctx, other, 41)
	requireCode(t, err, 404, "not_found")
	// Soft-deleted details accept delayed outbox results, but cannot start/claim.
	if _, err = db.Exec("UPDATE schedule_detail SET deleted_at=UTC_TIMESTAMP() WHERE schedule_id=12"); err != nil {
		t.Fatal(err)
	}
	_, err = f.service.Start(ctx, f.principal, r.ExecutionID, r.ExecutionStart)
	requireCode(t, err, 404, "not_found")
	_, _, err = f.service.Claim(ctx, f.principal, 41)
	requireCode(t, err, 404, "not_found")
	r.ManualID = nil
	r.ClaimToken = nil
	r.Attempt = 3
	r.ResultCode = 0
	if _, _, err = f.service.Result(ctx, f.principal, r.ExecutionID, resultKey(r), r); err != nil {
		t.Fatal(err)
	}
}
func TestMySQLExecutionHTTPAndErrorStop(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	base := "/timeweaver_svr/api/agent/v1"
	for _, target := range []string{"schedule_group", "schedule_detail", "none"} {
		groupStop, detailStop := 0, 0
		if target == "schedule_group" {
			groupStop, detailStop = 1, 1
		}
		if target == "schedule_detail" {
			detailStop = 1
		}
		if _, err := f.runtime.db.DB.Exec("UPDATE schedule_group SET status='active',is_error_stop=? WHERE schedule_id=12", groupStop); err != nil {
			t.Fatal(err)
		}
		if _, err := f.runtime.db.DB.Exec("UPDATE schedule_detail SET status='active',is_error_stop=? WHERE schedule_id=12", detailStop); err != nil {
			t.Fatal(err)
		}
		r := executionBody()
		r.ExecutionID, _ = newJTI()
		r.ResultCode = 3
		data, _, err := f.service.Result(ctx, f.principal, r.ExecutionID, resultKey(r), r)
		if err != nil {
			t.Fatal(err)
		}
		if target == "none" {
			if len(data.Transitions) != 0 {
				t.Fatal(data)
			}
		} else if len(data.Transitions) != 1 || data.Transitions[0].Target != target {
			t.Fatal(data)
		}
	}
	r := executionBody()
	raw, _ := json.Marshal(r)
	path := base + "/executions/" + r.ExecutionID + "/results"
	for _, suffix := range []string{"", "/"} {
		response := runtimeRequest(f.handler, "POST", path+suffix, string(raw), f.token.AccessToken, map[string]string{"Idempotency-Key": resultKey(r)})
		if response.Code != 200 || response.Header().Get("Location") != "" {
			t.Fatal(response.Code, response.Body)
		}
	}
	response := runtimeRequest(f.handler, "POST", path, string(raw), f.token.AccessToken, nil)
	if response.Code != 422 {
		t.Fatal(response.Code)
	}
	response = runtimeRequest(f.handler, "POST", path, string(raw), "", nil)
	if response.Code != 401 {
		t.Fatal(response.Code)
	}
	response = runtimeRequest(f.handler, "GET", path, "", f.token.AccessToken, nil)
	if response.Code != 405 {
		t.Fatal(response.Code)
	}
	response = runtimeRequest(f.handler, "POST", base+"/manual-runs/not-an-int/claim", "", f.token.AccessToken, nil)
	if response.Code != 422 {
		t.Fatal(response.Code)
	}
	event := `{"event_type":"sync_error","severity":"error","occurred_at":"2026-08-01T02:00:00Z","message":"검증"}`
	response = runtimeRequest(f.handler, "POST", base+"/events/", event, f.token.AccessToken, nil)
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body)
	}
	response = runtimeRequest(f.handler, "POST", base+"/events", strings.Replace(event, "sync_error", "unknown", 1), f.token.AccessToken, nil)
	if response.Code != 422 {
		t.Fatal(response.Code)
	}
	if _, err := f.runtime.db.DB.Exec("UPDATE devices SET status='inactive' WHERE device_id=?", f.principal.DeviceID); err != nil {
		t.Fatal(err)
	}
	response = runtimeRequest(f.handler, "POST", path, string(raw), f.token.AccessToken, map[string]string{"Idempotency-Key": resultKey(r)})
	if response.Code != 403 {
		t.Fatal(response.Code)
	}
}
func TestPythonExecutionInterop(t *testing.T) {
	if os.Getenv("TIMEWEAVER_EXECUTION_INTEROP") != "1" {
		t.Skip("requires frozen Python execution dependencies")
	}
	db := identityStore(t)
	identity := identityService(t, db, "")
	auth := httpService(t, identity)
	service, err := NewExecution(db)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewExecutionHTTP(auth, service)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimSuffix(r.URL.Path, "/")
		if strings.HasSuffix(path, "/enroll") || strings.HasSuffix(path, "/token") {
			auth.ServeHTTP(w, r)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	var database string
	if err = db.DB.QueryRow("SELECT DATABASE()").Scan(&database); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("python", filepath.Join("..", "..", "scripts", "test-go-execution-compat.py"), server.URL, database)
	cmd.Env = append(os.Environ(), "TW_INTEROP_SECRET="+testSecret, "TW_INTEROP_AGENT_SECRET=")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("execution interop failed: %v\n%s", err, out)
	}
	t.Log(strings.TrimSpace(string(out)))
}

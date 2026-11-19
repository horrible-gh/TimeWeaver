package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/horrible-gh/TimeWeaver/internal/access"
	"github.com/horrible-gh/TimeWeaver/internal/dashboard"
)

func manualExec(t *testing.T, f *deviceFixture, q string, args ...any) {
	t.Helper()
	if _, err := fixtureExec(t, f.db, q, args...); err != nil {
		t.Fatal(err)
	}
}

func queueManualTask(t *testing.T, f *deviceFixture, detail string) int64 {
	t.Helper()
	p := f.sessions["timeweaver"]
	ids, err := f.dashboard.QueueManual(context.Background(), p.token, p.csrf, dashboard.ManualInput{DetailID: detail, Immediate: true})
	if err != nil || len(ids) != 1 {
		t.Fatal("queue fixture", ids, err)
	}
	return ids[0]
}

func manualCount(t *testing.T, f *deviceFixture) int {
	t.Helper()
	var n int
	if err := fixtureRow(t, f.db, "SELECT COUNT(*) FROM manual_execution").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestMySQLManualScopeFanoutAndForcedActor(t *testing.T) {
	f := deviceFixtureFor(t)
	ctx := context.Background()
	op, viewer := f.sessions["operator"], f.sessions["viewer"]
	a, b := createSchedule(t, f, 7, "Seven requests"), createSchedule(t, f, 8, "Private eight")
	first, second, inactive := createTask(t, f, a), createTask(t, f, a), createTask(t, f, a)
	hidden := createTask(t, f, b)
	manualExec(t, f, "UPDATE schedule_detail SET status='inactive' WHERE detail_id=UNHEX(REPLACE(?,'-',''))", inactive)
	in := dashboard.ManualInput{ScheduleID: a, Immediate: true, Creator: "forged", Modifier: "forged"}
	_, err := f.dashboard.QueueManual(ctx, viewer.token, viewer.csrf, in)
	expectCode(t, err, "forbidden")
	_, err = f.dashboard.QueueManual(ctx, op.token, "wrong", in)
	expectCode(t, err, "csrf_failed")
	ids, err := f.dashboard.QueueManual(ctx, op.token, op.csrf, in)
	if err != nil || len(ids) != 2 || ids[0] == ids[1] {
		t.Fatal("schedule fanout must enqueue exactly its active details", ids, err)
	}
	var n int
	if err = fixtureRow(t, f.db, "SELECT COUNT(*) FROM manual_execution WHERE creator='operator' AND status='wait' AND detail_id IN (UNHEX(REPLACE(?,'-','')),UNHEX(REPLACE(?,'-','')))", first, second).Scan(&n); err != nil || n != 2 {
		t.Fatal("actor or selected task changed", n, err)
	}
	if err = fixtureRow(t, f.db, "SELECT COUNT(*) FROM tw_audit WHERE action='manual_create' AND actor='operator'").Scan(&n); err != nil || n != 1 {
		t.Fatal("batch audit not attributable", n, err)
	}
	for _, request := range []dashboard.ManualInput{{ScheduleID: b, Immediate: true}, {DetailID: hidden, Immediate: true}} {
		_, err = f.dashboard.QueueManual(ctx, op.token, op.csrf, request)
		expectCode(t, err, "forbidden")
	}
	_, err = f.dashboard.QueueManual(ctx, op.token, op.csrf, dashboard.ManualInput{ScheduleID: a, DetailID: hidden, Immediate: true})
	expectCode(t, err, "invalid_request")
	empty := createSchedule(t, f, 7, "Empty")
	_, err = f.dashboard.QueueManual(ctx, op.token, op.csrf, dashboard.ManualInput{ScheduleID: empty, Immediate: true})
	expectCode(t, err, "no_runnable_tasks")
	_, err = f.dashboard.QueueManual(ctx, op.token, op.csrf, dashboard.ManualInput{DetailID: inactive, Immediate: true})
	expectCode(t, err, "task_inactive")
	hiddenID := queueManualTask(t, f, hidden)
	expectCode(t, f.dashboard.CancelManual(ctx, op.token, op.csrf, hiddenID), "forbidden")
	_, err = f.dashboard.UpdateManual(ctx, op.token, op.csrf, dashboard.ManualInput{ManualID: hiddenID, Immediate: true})
	expectCode(t, err, "forbidden")
	// The recorded target device has an independent scope even for a legacy
	// schedule whose group and target device do not agree.
	otherDevice := scheduleInput(t, f, 8, "").DeviceID
	manualExec(t, f, "UPDATE schedule_group SET target_device=? WHERE schedule_id=?", otherDevice, a)
	_, err = f.dashboard.QueueManual(ctx, op.token, op.csrf, dashboard.ManualInput{DetailID: first, Immediate: true})
	expectCode(t, err, "forbidden")
	expectCode(t, f.dashboard.CancelManual(ctx, op.token, op.csrf, ids[0]), "forbidden")
	rows, err := f.dashboard.ManualRuns(ctx, viewer.token)
	if err != nil || len(rows) != 0 {
		t.Fatal("manual list leaked a foreign target", rows, err)
	}
	manualExec(t, f, "UPDATE schedule_group SET target_device=? WHERE schedule_id=?", scheduleInput(t, f, 7, "").DeviceID, a)
	rows, err = f.dashboard.ManualRuns(ctx, viewer.token)
	if err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
	for _, row := range rows {
		if row.CanEdit || row.CanCancel || row.EditEnable != "0" || row.ScheduleID != a {
			t.Fatal("viewer received a mutation hint", row)
		}
	}
	rows, err = f.dashboard.ManualRuns(ctx, op.token)
	if err != nil || len(rows) != 2 || !rows[0].CanEdit || !rows[0].CanCancel || rows[0].EditEnable != "1" {
		t.Fatal("operator hints", rows, err)
	}
}

func TestMySQLManualTimingStateAndPendingCancellation(t *testing.T) {
	f := deviceFixtureFor(t)
	ctx := context.Background()
	p := f.sessions["operator"]
	a := createSchedule(t, f, 7, "Reservations")
	detail := createTask(t, f, a)
	var future time.Time
	if err := fixtureRow(t, f.db, "SELECT UTC_TIMESTAMP()+INTERVAL 2 HOUR").Scan(&future); err != nil {
		t.Fatal(err)
	}
	in := dashboard.ManualInput{DetailID: detail, ScheduleDatetime: future.In(time.FixedZone("test", 9*3600)).Format(time.RFC3339), Status: "wait", Creator: "forged"}
	ids, err := f.dashboard.QueueManual(ctx, p.token, p.csrf, in)
	if err != nil || len(ids) != 1 {
		t.Fatal(ids, err)
	}
	var due time.Time
	var immediate bool
	if err = fixtureRow(t, f.db, "SELECT schedule_datetime,is_immediate FROM manual_execution WHERE manual_id=?", ids[0]).Scan(&due, &immediate); err != nil || immediate || !due.Equal(future) {
		t.Fatal("reservation timezone changed", due, err)
	}
	for _, value := range []string{"", "2000-01-01T00:00:00Z", "2099-01-01T09:00:00", "2099-02-30T09:00:00Z"} {
		in.ScheduleDatetime = value
		_, err = f.dashboard.QueueManual(ctx, p.token, p.csrf, in)
		expectCode(t, err, "invalid_datetime")
	}
	for _, status := range []string{"active", "inactive", "processing", "done", "failed"} {
		_, err = f.dashboard.QueueManual(ctx, p.token, p.csrf, dashboard.ManualInput{DetailID: detail, Immediate: true, Status: status})
		expectCode(t, err, "invalid_request")
	}
	_, err = f.dashboard.UpdateManual(ctx, p.token, p.csrf, dashboard.ManualInput{ManualID: ids[0], ScheduleID: a, Immediate: true})
	expectCode(t, err, "invalid_request")
	_, err = f.dashboard.UpdateManual(ctx, p.token, p.csrf, dashboard.ManualInput{ManualID: ids[0], Immediate: true, Status: "done"})
	expectCode(t, err, "invalid_request")
	for _, status := range []string{"processing", "done", "failed", "inactive"} {
		id := queueManualTask(t, f, detail)
		manualExec(t, f, "UPDATE manual_execution SET status=? WHERE manual_id=?", status, id)
		expectCode(t, f.dashboard.CancelManual(ctx, p.token, p.csrf, id), "manual_not_pending")
		_, err = f.dashboard.UpdateManual(ctx, p.token, p.csrf, dashboard.ManualInput{ManualID: id, Immediate: true})
		expectCode(t, err, "manual_not_pending")
	}
	legacyPending := queueManualTask(t, f, detail)
	manualExec(t, f, "UPDATE manual_execution SET status='active',schedule_datetime=NULL,claim_token='stale',claim_expires_at=UTC_TIMESTAMP() WHERE manual_id=?", legacyPending)
	manualExec(t, f, "UPDATE devices SET status='inactive' WHERE group_id=7")
	manualExec(t, f, "UPDATE `groups` SET status='inactive' WHERE group_id=7")
	manualExec(t, f, "UPDATE schedule_detail SET deleted_at=UTC_TIMESTAMP(),status='inactive' WHERE detail_id=UNHEX(REPLACE(?,'-',''))", detail)
	manualExec(t, f, "DELETE FROM task_detail WHERE detail_id=UNHEX(REPLACE(?,'-',''))", detail)
	rows, err := f.dashboard.ManualRuns(ctx, p.token)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range rows {
		if row.ID == legacyPending {
			found = true
			if row.CanEdit || !row.CanCancel || row.TaskName != "Task" || row.Due != nil {
				t.Fatal("pending tombstone must remain cancelable", row)
			}
		}
	}
	if !found {
		t.Fatal("pending tombstone disappeared")
	}
	if err = f.dashboard.CancelManual(ctx, p.token, p.csrf, legacyPending); err != nil {
		t.Fatal("disabled target blocked cancellation", err)
	}
	var status, modifier string
	var claim sql.NullString
	var expiry sql.NullTime
	if err = fixtureRow(t, f.db, "SELECT status,modifier,claim_token,claim_expires_at FROM manual_execution WHERE manual_id=?", legacyPending).Scan(&status, &modifier, &claim, &expiry); err != nil || status != "failed" || modifier != "operator" || claim.Valid || expiry.Valid {
		t.Fatal("cancellation changed ownership or retained a lease", status, modifier, claim, expiry, err)
	}
	expectCode(t, f.dashboard.CancelManual(ctx, p.token, p.csrf, legacyPending), "manual_not_pending")
}

func TestMySQLManualAuditFailureRollsBackBatchAndReplacement(t *testing.T) {
	f := deviceFixtureFor(t)
	ctx := context.Background()
	p := f.sessions["operator"]
	a := createSchedule(t, f, 7, "Atomic requests")
	detail := createTask(t, f, a)
	createTask(t, f, a)
	id := queueManualTask(t, f, detail)
	before := manualCount(t, f)
	manualExec(t, f, "RENAME TABLE tw_audit TO tw_audit_offline")
	defer manualExec(t, f, "RENAME TABLE tw_audit_offline TO tw_audit")
	ids, err := f.dashboard.QueueManual(ctx, p.token, p.csrf, dashboard.ManualInput{ScheduleID: a, Immediate: true})
	expectCode(t, err, "unavailable")
	if len(ids) != 0 || manualCount(t, f) != before {
		t.Fatal("failed audit left a partial fanout", ids)
	}
	replacement, err := f.dashboard.UpdateManual(ctx, p.token, p.csrf, dashboard.ManualInput{ManualID: id, Immediate: true})
	expectCode(t, err, "unavailable")
	if replacement != 0 || manualCount(t, f) != before {
		t.Fatal("failed audit left a replacement request", replacement)
	}
	expectCode(t, f.dashboard.CancelManual(ctx, p.token, p.csrf, id), "unavailable")
	var status string
	var modifier sql.NullString
	if err = fixtureRow(t, f.db, "SELECT status,modifier FROM manual_execution WHERE manual_id=?", id).Scan(&status, &modifier); err != nil || status != "wait" || modifier.Valid {
		t.Fatal("failed audit retired the original request", status, modifier, err)
	}
}

func insertManualHistory(t *testing.T, f *deviceFixture, schedule int64, detail, message string) int64 {
	t.Helper()
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	r, err := fixtureExec(t, f.db, "INSERT INTO execution_log(execution_grp_id,schedule_id,detail_id,start_time,end_time,result_code,result_message,environment_info) VALUES(?,?,UNHEX(REPLACE(?,'-','')),'2030-02-01 01:00:00','2030-02-01 01:00:01',0,?,'not-json')", raw, schedule, detail, message)
	if err != nil {
		t.Fatal(err)
	}
	id, err := r.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestMySQLManualHistoryRecordedScopesAndTombstones(t *testing.T) {
	f := deviceFixtureFor(t)
	ctx := context.Background()
	a, b, cross := createSchedule(t, f, 7, "Original seven"), createSchedule(t, f, 8, "Private eight"), createSchedule(t, f, 7, "Cross target")
	deleted, movedOut, movedIn, hidden, crossTask := createTask(t, f, a), createTask(t, f, a), createTask(t, f, b), createTask(t, f, b), createTask(t, f, cross)
	tombstoneID := insertManualHistory(t, f, a, deleted, "tombstone")
	movedID := insertManualHistory(t, f, a, movedOut, "moved-out")
	insertManualHistory(t, f, b, movedIn, "private-original")
	insertManualHistory(t, f, b, hidden, "private")
	insertManualHistory(t, f, cross, crossTask, "foreign-device")
	insertManualHistory(t, f, -1, "00000000-0000-0000-0000-000000000000", "system")
	insertManualHistory(t, f, 999999, "00000000-0000-0000-0000-000000000001", "unresolved")
	p := f.sessions["timeweaver"]
	if err := f.dashboard.DeleteTask(ctx, p.token, p.csrf, deleted); err != nil {
		t.Fatal(err)
	}
	// Simulate pre-migration moves the new service itself forbids once history
	// exists. Current task fields must not redefine historical ownership.
	manualExec(t, f, "UPDATE schedule_detail SET schedule_id=?,schedule_name='private moved task' WHERE detail_id=UNHEX(REPLACE(?,'-',''))", b, movedOut)
	manualExec(t, f, "UPDATE task_detail SET command='secret-command',source_path='/secret/source',destination_path='/secret/destination' WHERE detail_id=UNHEX(REPLACE(?,'-',''))", movedOut)
	manualExec(t, f, "UPDATE schedule_detail SET schedule_id=? WHERE detail_id=UNHEX(REPLACE(?,'-',''))", a, movedIn)
	manualExec(t, f, "UPDATE schedule_group SET target_device=? WHERE schedule_id=?", scheduleInput(t, f, 8, "").DeviceID, cross)
	tasks, err := f.dashboard.Tasks(ctx, f.sessions["global-editor"].token)
	if err != nil {
		t.Fatal(err)
	}
	movedTasks := 0
	for _, task := range tasks {
		if task["detail_id"] == movedOut || task["detail_id"] == movedIn {
			movedTasks++
			if task["lastest_start_time"] != nil {
				t.Fatal("destination task borrowed the old parent's latest run", task)
			}
		}
	}
	if movedTasks != 2 {
		t.Fatal("moved task fixture missing", movedTasks)
	}
	const from, to = "2030-02-01T00:00:00Z", "2030-02-02T00:00:00Z"
	for _, tc := range []struct {
		user string
		want int
	}{{"viewer", 2}, {"global-editor", 5}, {"timeweaver", 7}} {
		rows, err := f.dashboard.ExecutionHistory(ctx, f.sessions[tc.user].token, from, to)
		if err != nil || len(rows) != tc.want {
			t.Fatal(tc.user, len(rows), err)
		}
		if tc.user == "viewer" {
			foundTombstone, foundMoved := false, false
			for _, row := range rows {
				if row["execution_id"] == tombstoneID {
					foundTombstone = true
					if row["schedule_name"] != "Task" || row["sg_name"] != "Original seven" || row["command"] != nil || row["start_time"] != "2030-02-01T01:00:00Z" {
						t.Fatal("tombstone lost its name or UTC time", row)
					}
				}
				if row["execution_id"] == movedID {
					foundMoved = true
					if row["command"] != nil || row["source_path"] != nil || row["destination_path"] != nil || row["task_type"] != nil {
						t.Fatal("current foreign task definition leaked", row)
					}
				}
				encoded, _ := json.Marshal(row)
				if strings.Contains(string(encoded), "private") || strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), "foreign-device") {
					t.Fatal("history visibility followed the current task", string(encoded))
				}
			}
			if !foundTombstone || !foundMoved {
				t.Fatal("missing original records")
			}
		}
	}
	rows, err := f.dashboard.ExecutionHistory(ctx, f.sessions["viewer"].token, "2030-02-01T10:00:00+09:00", "2030-02-01T10:00:00+09:00")
	if err != nil || len(rows) != 2 {
		t.Fatal("inclusive UTC bounds", rows, err)
	}
	_, err = f.dashboard.ExecutionHistory(ctx, p.token, to, from)
	expectCode(t, err, "invalid_date_range")
}

func TestMySQLManualHistoryRejectsTruncatedResults(t *testing.T) {
	f := deviceFixtureFor(t)
	schedule := createSchedule(t, f, 7, "History bound")
	detail := createTask(t, f, schedule)
	const from, to = "2030-02-01T00:00:00Z", "2030-02-02T00:00:00Z"
	tx, err := f.db.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 1; i <= 1000; i++ {
		if _, err = tx.Exec(fixtureSQL(f.db.Dialect, "INSERT INTO execution_log(execution_grp_id,schedule_id,detail_id,start_time,result_code) VALUES(UNHEX(LPAD(HEX(?),32,'0')),?,UNHEX(REPLACE(?,'-','')),'2030-02-01 01:00:00',0)"), i, schedule, detail); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	p := f.sessions["viewer"]
	rows, err := f.dashboard.ExecutionHistory(context.Background(), p.token, from, to)
	if err != nil || len(rows) != 1000 {
		t.Fatal("exact bound rejected", len(rows), err)
	}
	insertManualHistory(t, f, schedule, detail, "overflow")
	rows, err = f.dashboard.ExecutionHistory(context.Background(), p.token, from, to)
	expectCode(t, err, "history_too_large")
	if rows != nil {
		t.Fatal("partial history returned with overflow", len(rows))
	}
}

func TestMySQLManualHTTPContractAndBoundaries(t *testing.T) {
	f := deviceFixtureFor(t)
	a := createSchedule(t, f, 7, "HTTP manual")
	detail := createTask(t, f, a)
	guard, err := access.NewHTTP(f.auth, f.settings.Context, access.HTTPOptions{Origin: "http://127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	h := dashboard.NewHTTP(f.auth, guard, f.settings.Context)
	valid := fmt.Sprintf(`{"detail_id":%q,"is_immediate":true,"creator":"forged"}`, detail)
	for _, tc := range []struct {
		name, method, path, body, user, csrf, media, origin, authorization string
		want                                                               int
	}{
		{"create", "POST", "/dashboard/tasks/insert_manual_task/", valid, "operator", "ok", "application/json", "", "", 200},
		{"read", "GET", "/dashboard/manual_execution/get_manual_execution_list/?group_id=8", "", "viewer", "", "", "", "", 200},
		{"history", "GET", "/dashboard/schedule/execution_history/", "", "viewer", "", "", "", "", 200},
		{"method", "POST", "/dashboard/schedule/execution_history", "{}", "operator", "ok", "application/json", "", "", 405},
		{"anonymous", "GET", "/dashboard/manual_execution/get_manual_execution_list", "", "", "", "", "", "", 401},
		{"bearer with cookie", "GET", "/dashboard/manual_execution/get_manual_execution_list", "", "operator", "", "", "", "Bearer device-token", 401},
		{"viewer mutation", "POST", "/dashboard/tasks/insert_manual_task", valid, "viewer", "ok", "application/json", "", "", 403},
		{"csrf", "POST", "/dashboard/tasks/insert_manual_task", valid, "operator", "bad", "application/json", "", "", 403},
		{"foreign origin", "POST", "/dashboard/tasks/insert_manual_task", valid, "operator", "ok", "application/json", "https://other.invalid", "", 403},
		{"display field", "POST", "/dashboard/tasks/insert_manual_task", fmt.Sprintf(`{"detail_id":%q,"is_immediate":true,"can_run":true}`, detail), "operator", "ok", "application/json", "", "", 422},
		{"claim injection", "POST", "/dashboard/tasks/insert_manual_task", fmt.Sprintf(`{"detail_id":%q,"is_immediate":true,"claim_token":"forged"}`, detail), "operator", "ok", "application/json", "", "", 422},
		{"mixed targets", "POST", "/dashboard/tasks/insert_manual_task", fmt.Sprintf(`{"detail_id":%q,"schedule_id":%d,"is_immediate":true}`, detail, a), "operator", "ok", "application/json", "", "", 422},
		{"wrong route", "POST", "/dashboard/schedule/insert_manual_schedule", valid, "operator", "ok", "application/json", "", "", 422},
		{"typed ID", "POST", "/dashboard/schedule/insert_manual_schedule", fmt.Sprintf(`{"schedule_id":"%d","is_immediate":true}`, a), "operator", "ok", "application/json", "", "", 422},
		{"extra JSON", "POST", "/dashboard/tasks/insert_manual_task", valid + " {}", "operator", "ok", "application/json", "", "", 422},
		{"media", "POST", "/dashboard/tasks/insert_manual_task", valid, "operator", "ok", "text/plain", "", "", 415},
		{"cancel status injection", "PUT", "/dashboard/manual_execution/abandon_manual_execution", `{"manual_id":1,"status":"done"}`, "operator", "ok", "application/json", "", "", 422},
		{"no offset", "GET", "/dashboard/schedule/execution_history?start_date=2030-02-01T00:00:00", "", "viewer", "", "", "", "", 422},
		{"double slash", "GET", "/dashboard/manual_execution/get_manual_execution_list//", "", "viewer", "", "", "", "", 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := f.sessions[tc.user]
			r := httptest.NewRequest(tc.method, "http://127.0.0.1"+f.settings.Context+tc.path, strings.NewReader(tc.body))
			r.RemoteAddr = "127.0.0.1:42"
			origin := tc.origin
			if origin == "" {
				origin = "http://127.0.0.1"
			}
			r.Header.Set("Origin", origin)
			r.Header.Set("Content-Type", tc.media)
			r.Header.Set("Authorization", tc.authorization)
			if tc.csrf == "ok" {
				r.Header.Set("X-CSRF-Token", p.csrf)
			} else {
				r.Header.Set("X-CSRF-Token", tc.csrf)
			}
			if tc.user != "" {
				r.AddCookie(&http.Cookie{Name: "tw_session", Value: p.token})
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want || w.Header().Get("Location") != "" {
				t.Fatal(w.Code, w.Body.String())
			}
			if tc.name == "create" {
				var created struct {
					OK    bool    `json:"ok"`
					IDs   []int64 `json:"manual_ids"`
					Count int     `json:"count"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil || !created.OK || created.Count != 1 || len(created.IDs) != 1 {
					t.Fatal("invalid create response", w.Body.String(), err)
				}
			}
		})
	}
	if manualCount(t, f) != 1 {
		t.Fatal("rejected HTTP requests inserted manual work")
	}
}

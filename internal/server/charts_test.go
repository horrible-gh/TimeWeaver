package server

import (
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/horrible-gh/TimeWeaver/internal/access"
	"github.com/horrible-gh/TimeWeaver/internal/dashboard"
)

func chartsFixtureFor(t *testing.T) *deviceFixture {
	t.Helper()
	f := deviceFixtureFor(t)
	manualExec(t, f, "UPDATE devices SET last_heartbeat_at=UTC_TIMESTAMP() WHERE device_id>0")
	return f
}

func chartsHandler(t *testing.T, f *deviceFixture) http.Handler {
	t.Helper()
	guard, err := access.NewHTTP(f.auth, f.settings.Context, access.HTTPOptions{Origin: "http://127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	return dashboard.NewHTTP(f.auth, guard, f.settings.Context)
}

func chartsRequest(t *testing.T, f *deviceFixture, h http.Handler, user, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "http://127.0.0.1"+f.settings.Context+path, nil)
	r.RemoteAddr = "127.0.0.1:42"
	r.Header.Set("Origin", "http://127.0.0.1")
	if user != "" {
		p := f.sessions[user]
		r.AddCookie(&http.Cookie{Name: "tw_session", Value: p.token})
		r.Header.Set("X-CSRF-Token", p.csrf)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Header().Get("Location") != "" {
		t.Fatalf("chart endpoint redirected: %s", path)
	}
	return w
}

func chartCounts(t *testing.T, f *deviceFixture, h http.Handler, user, path string) map[string]int64 {
	t.Helper()
	w := chartsRequest(t, f, h, user, "GET", path)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("chart %s: %d %s", path, w.Code, w.Body.String())
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	out := map[string]int64{}
	for key, value := range raw {
		if string(value) == "null" || len(value) == 0 || value[0] == '"' {
			t.Fatalf("chart count must be a JSON integer, %s=%s", key, value)
		}
		var n int64
		if err := json.Unmarshal(value, &n); err != nil || n < 0 {
			t.Fatalf("invalid count %s=%s: %v", key, value, err)
		}
		out[key] = n
	}
	return out
}

func latestChartRows(t *testing.T, f *deviceFixture, h http.Handler, user string) []map[string]any {
	t.Helper()
	w := chartsRequest(t, f, h, user, "GET", "/dashboard/lastest-schedules")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	var out []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out == nil {
		t.Fatal("latest schedules must be an array", w.Body.String(), err)
	}
	return out
}

func chartsNow(t *testing.T, f *deviceFixture) time.Time {
	t.Helper()
	var now time.Time
	if err := fixtureRow(t, f.db, "SELECT UTC_TIMESTAMP()").Scan(&now); err != nil {
		t.Fatal(err)
	}
	return now.UTC()
}

func chartsLog(t *testing.T, f *deviceFixture, schedule int64, detail string, when time.Time, code int64, message string, instance []byte) int64 {
	t.Helper()
	if instance == nil {
		instance = make([]byte, 16)
		if _, err := rand.Read(instance); err != nil {
			t.Fatal(err)
		}
	}
	result, err := fixtureExec(t, f.db, `INSERT INTO execution_log(execution_grp_id,schedule_id,detail_id,start_time,end_time,result_code,result_message) VALUES(?,?,UNHEX(REPLACE(?,'-','')),?,?,?,?)`, instance, schedule, detail, when, when.Add(time.Second), code, message)
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func chartsRunning(t *testing.T, f *deviceFixture, schedule int64, detail string, when time.Time) {
	t.Helper()
	manualExec(t, f, `INSERT INTO execution_running(schedule_id,detail_id,execution_grp_id,attempt,start_time) VALUES(?,UNHEX(REPLACE(?,'-','')),UNHEX(REPEAT('ef',16)),1,?)`, schedule, detail, when)
}

func wantChart(t *testing.T, got, want map[string]int64) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("chart counts: got %v, want %v", got, want)
	}
}

func TestMySQLChartsScopesHTTPAndEmptyResults(t *testing.T) {
	f := chartsFixtureFor(t)
	h := chartsHandler(t, f)
	now := chartsNow(t, f).Add(-time.Minute)
	visible := createSchedule(t, f, 7, "chart-seven")
	private := createSchedule(t, f, 8, "chart-eight-private")
	cross := createSchedule(t, f, 7, "cross-device-private")
	a, b := createTask(t, f, visible), createTask(t, f, private)
	createTask(t, f, cross)
	chartsLog(t, f, visible, a, now, 0, "visible", nil)
	chartsLog(t, f, private, b, now.Add(time.Second), 7, "private", nil)
	manualExec(t, f, "UPDATE schedule_group SET target_device=? WHERE schedule_id=?", scheduleInput(t, f, 8, "").DeviceID, cross)
	manualExec(t, f, "INSERT INTO schedule_group(schedule_id,group_id,name,target_device) VALUES(-1,0,'System sentinel',-1),(999999,7,'Sentinel target',-1)")
	for _, user := range []string{"viewer", "operator", "editor"} {
		for _, query := range []string{"", "?group_id=8", "?group_id=0", "?group_id=%25", "?group_id=7&group_id=8"} {
			wantChart(t, chartCounts(t, f, h, user, "/dashboard/charts/devices"+query), map[string]int64{"active_count": 1, "error_count": 0, "inactive_count": 0})
			wantChart(t, chartCounts(t, f, h, user, "/dashboard/charts/schedules"+query), map[string]int64{"active_count": 1, "error_count": 0, "inactive_count": 0})
			wantChart(t, chartCounts(t, f, h, user, "/dashboard/charts/tasks"+query), map[string]int64{"pending_count": 0, "in_progress_count": 0, "completed_count": 1, "error_count": 0})
			w := chartsRequest(t, f, h, user, "GET", "/dashboard/lastest-schedules"+query)
			if w.Code != 200 || strings.Contains(w.Body.String(), "private") || strings.Contains(w.Body.String(), "sentinel") {
				t.Fatal("recent scope leak", user, w.Code, w.Body.String())
			}
		}
		rows := latestChartRows(t, f, h, user)
		if len(rows) != 1 || rows[0]["sg_name"] != "chart-seven" {
			t.Fatal("recent visible group missing", rows)
		}
	}
	for _, user := range []string{"timeweaver", "global-editor"} {
		wantChart(t, chartCounts(t, f, h, user, "/dashboard/charts/devices"), map[string]int64{"active_count": 2, "error_count": 0, "inactive_count": 0})
		wantChart(t, chartCounts(t, f, h, user, "/dashboard/charts/schedules"), map[string]int64{"active_count": 2, "error_count": 1, "inactive_count": 0})
		wantChart(t, chartCounts(t, f, h, user, "/dashboard/charts/tasks"), map[string]int64{"pending_count": 1, "in_progress_count": 0, "completed_count": 1, "error_count": 1})
		if rows := latestChartRows(t, f, h, user); len(rows) != 2 {
			t.Fatal("global scope excluded resolved records", rows)
		}
	}
	paths := []string{"/dashboard/charts/devices", "/dashboard/charts/schedules", "/dashboard/charts/tasks", "/dashboard/lastest-schedules"}
	for _, path := range paths {
		if w := chartsRequest(t, f, h, "", "GET", path); w.Code != 401 {
			t.Fatal("unauthenticated chart", path, w.Code)
		}
		if w := chartsRequest(t, f, h, "viewer", "GET", path+"/"); w.Code != 200 {
			t.Fatal("trailing slash contract", path, w.Code)
		}
		for _, method := range []string{"POST", "PUT", "DELETE"} {
			if w := chartsRequest(t, f, h, "viewer", method, path); w.Code != 405 || w.Header().Get("Allow") != "GET" {
				t.Fatal("chart method", path, method, w.Code)
			}
		}
	}
	manualExec(t, f, "DELETE FROM tw_group_member WHERE user_id='viewer'")
	for _, path := range paths[:3] {
		counts := chartCounts(t, f, h, "viewer", path)
		if len(counts) < 3 {
			t.Fatal("empty scope lost numeric fields", counts)
		}
		for _, n := range counts {
			if n != 0 {
				t.Fatal("revoked scope retained a count", path, counts)
			}
		}
	}
	if rows := latestChartRows(t, f, h, "viewer"); len(rows) != 0 {
		t.Fatal("revoked scope retained latest records", rows)
	}
}

func TestMySQLChartDeviceLivenessAndLegacyScheduleRules(t *testing.T) {
	f := chartsFixtureFor(t)
	h := chartsHandler(t, f)
	now := chartsNow(t, f)
	manualExec(t, f, `INSERT INTO devices(group_id,device_name,status,last_heartbeat_at,last_login_at) VALUES
 (7,'inactive-fresh','inactive',?,NULL),(7,'heartbeat-stale','active',?,?),
 (7,'login-stale','active',NULL,?),(7,'login-fresh','active',NULL,?),(7,'never-seen','active',NULL,NULL)`, now, now.Add(-6*time.Minute), now, now.Add(-48*time.Hour), now)
	wantChart(t, chartCounts(t, f, h, "viewer", "/dashboard/charts/devices"), map[string]int64{"active_count": 3, "error_count": 2, "inactive_count": 1})
	createSchedule(t, f, 7, "fresh")
	stale := createSchedule(t, f, 7, "stale")
	inactive := createSchedule(t, f, 7, "inactive")
	errorSchedule := createSchedule(t, f, 7, "error-without-log")
	deviceOff := createSchedule(t, f, 7, "disabled-device-fresh")
	heartbeatStale := createSchedule(t, f, 7, "six-minute-heartbeat")
	oldError := createSchedule(t, f, 7, "old-error")
	recentError := createSchedule(t, f, 7, "recent-error")
	oldTask, recentTask := createTask(t, f, oldError), createTask(t, f, recentError)
	chartsLog(t, f, oldError, oldTask, now.Add(-48*time.Hour), -1, "old", nil)
	chartsLog(t, f, recentError, recentTask, now.Add(-time.Hour), 9, "recent", nil)
	manualExec(t, f, "UPDATE schedule_group SET target_device=(SELECT device_id FROM devices WHERE device_name='login-stale') WHERE schedule_id=?", stale)
	manualExec(t, f, "UPDATE schedule_group SET target_device=(SELECT device_id FROM devices WHERE device_name='inactive-fresh') WHERE schedule_id=?", deviceOff)
	manualExec(t, f, "UPDATE schedule_group SET target_device=(SELECT device_id FROM devices WHERE device_name='heartbeat-stale') WHERE schedule_id=?", heartbeatStale)
	manualExec(t, f, "UPDATE schedule_group SET status='inactive' WHERE schedule_id=?", inactive)
	manualExec(t, f, "UPDATE schedule_group SET status='error' WHERE schedule_id=?", errorSchedule)
	// Legacy schedule charts use one-day last-seen and execution outcomes, not
	// direct device.status or schedule.status=error by itself.
	wantChart(t, chartCounts(t, f, h, "viewer", "/dashboard/charts/schedules"), map[string]int64{"active_count": 5, "error_count": 1, "inactive_count": 2})
	manualExec(t, f, "UPDATE `groups` SET status='inactive' WHERE group_id=7")
	wantChart(t, chartCounts(t, f, h, "viewer", "/dashboard/charts/schedules"), map[string]int64{"active_count": 5, "error_count": 1, "inactive_count": 2})
}

func TestMySQLChartTaskWindowsRunningAndRecordedParent(t *testing.T) {
	f := chartsFixtureFor(t)
	h := chartsHandler(t, f)
	now := chartsNow(t, f)
	a, b := createSchedule(t, f, 7, "task-chart"), createSchedule(t, f, 8, "private-chart")
	createTask(t, f, a) // pending
	running, complete, failed := createTask(t, f, a), createTask(t, f, a), createTask(t, f, a)
	staleRunning, old, tied := createTask(t, f, a), createTask(t, f, a), createTask(t, f, a)
	moved := createTask(t, f, b)
	chartsLog(t, f, a, running, now.Add(-time.Hour), 1, "overridden-by-running", nil)
	chartsLog(t, f, a, complete, now.Add(-time.Hour), 0, "complete", nil)
	chartsLog(t, f, a, failed, now.Add(-time.Hour), 3, "error", nil)
	chartsLog(t, f, a, old, now.Add(-48*time.Hour), 4, "expired-history", nil)
	chartsLog(t, f, a, tied, now.Add(-time.Hour), 0, "earlier-id", nil)
	chartsLog(t, f, a, tied, now.Add(-time.Hour), 8, "later-id", nil)
	chartsLog(t, f, b, moved, now.Add(-time.Hour), 9, "other-parent", nil)
	chartsRunning(t, f, a, running, now.Add(-time.Minute))
	chartsRunning(t, f, a, staleRunning, now.Add(-2*time.Hour))
	manualExec(t, f, "UPDATE schedule_detail SET schedule_id=? WHERE detail_id=UNHEX(REPLACE(?,'-',''))", a, moved)
	for _, status := range []string{"inactive", "manual"} {
		id := createTask(t, f, a)
		manualExec(t, f, "UPDATE schedule_detail SET status=? WHERE detail_id=UNHEX(REPLACE(?,'-',''))", status, id)
	}
	deleted := createTask(t, f, a)
	manualExec(t, f, "UPDATE schedule_detail SET deleted_at=UTC_TIMESTAMP() WHERE detail_id=UNHEX(REPLACE(?,'-',''))", deleted)
	for _, status := range []string{"inactive", "manual"} {
		sid := createSchedule(t, f, 7, "excluded-parent-"+status)
		createTask(t, f, sid)
		manualExec(t, f, "UPDATE schedule_group SET status=? WHERE schedule_id=?", status, sid)
	}
	wantChart(t, chartCounts(t, f, h, "viewer", "/dashboard/charts/tasks"), map[string]int64{"pending_count": 4, "in_progress_count": 1, "completed_count": 1, "error_count": 2})
	// The device's active flag and last-seen window still determine task-chart
	// membership, independently of the legacy schedule-chart classification.
	manualExec(t, f, "UPDATE devices SET status='inactive' WHERE group_id=7")
	wantChart(t, chartCounts(t, f, h, "viewer", "/dashboard/charts/tasks"), map[string]int64{"pending_count": 0, "in_progress_count": 0, "completed_count": 0, "error_count": 0})
	manualExec(t, f, "UPDATE devices SET status='active',last_heartbeat_at=? WHERE group_id=7", now.Add(-48*time.Hour))
	wantChart(t, chartCounts(t, f, h, "viewer", "/dashboard/charts/tasks"), map[string]int64{"pending_count": 0, "in_progress_count": 0, "completed_count": 0, "error_count": 0})
	manualExec(t, f, "UPDATE devices SET last_heartbeat_at=NULL,last_login_at=NULL WHERE group_id=7")
	wantChart(t, chartCounts(t, f, h, "viewer", "/dashboard/charts/tasks"), map[string]int64{"pending_count": 0, "in_progress_count": 0, "completed_count": 0, "error_count": 0})
}

func TestMySQLLatestChartsLimitOrderingFailuresAndTombstones(t *testing.T) {
	f := chartsFixtureFor(t)
	h := chartsHandler(t, f)
	now := chartsNow(t, f)
	old := createSchedule(t, f, 7, "old-visible")
	oldTask := createTask(t, f, old)
	chartsLog(t, f, old, oldTask, now.Add(-72*time.Hour), 0, "older-than-day", nil)
	warning := createSchedule(t, f, 7, "warning-visible")
	one, two := createTask(t, f, warning), createTask(t, f, warning)
	instance := []byte("chart-instance01")
	start := now.Add(-2 * time.Hour)
	firstFailure := chartsLog(t, f, warning, one, start, 7, "Zulu first\nfailure", instance)
	chartsLog(t, f, warning, two, start.Add(time.Second), -1, "Alpha later failure", instance)
	tied := createSchedule(t, f, 7, "tied-visible")
	tiedTask := createTask(t, f, tied)
	chartsLog(t, f, tied, tiedTask, now.Add(-time.Hour), 0, "first-instance", nil)
	chartsLog(t, f, tied, tiedTask, now.Add(-time.Hour), 3, "second-instance", nil)
	deleted := createSchedule(t, f, 7, "all-tombstones")
	deletedTask := createTask(t, f, deleted)
	chartsLog(t, f, deleted, deletedTask, now.Add(-time.Minute), 0, "removed", nil)
	manualExec(t, f, "UPDATE schedule_detail SET deleted_at=UTC_TIMESTAMP() WHERE detail_id=UNHEX(REPLACE(?,'-',''))", deletedTask)
	for i, name := range []string{"private-newest-one", "private-newest-two", "private-newest-three", "private-newest-four"} {
		sid := createSchedule(t, f, 8, name)
		detail := createTask(t, f, sid)
		chartsLog(t, f, sid, detail, now.Add(-time.Duration(i+2)*time.Minute), 0, "private", nil)
	}
	rows := latestChartRows(t, f, h, "viewer")
	if len(rows) != 3 || rows[0]["sg_name"] != "tied-visible" || rows[1]["sg_name"] != "warning-visible" || rows[2]["sg_name"] != "old-visible" {
		t.Fatal("scope must precede limit; old records have no daily cutoff", rows)
	}
	if rows[0]["custom_status"] != "warning" || rows[0]["error_summary"] != "second-instance" {
		t.Fatal("latest execution instance tie must use execution id", rows[0])
	}
	if rows[1]["custom_status"] != "warning" || rows[1]["error_summary"] != "Zulu first failure" || rows[1]["task_count"] != float64(2) {
		t.Fatal("first chronological nonzero failure or task count changed", rows[1])
	}
	if rows[1]["group_start_time"] != start.Format(time.RFC3339) || rows[2]["group_start_time"] != now.Add(-72*time.Hour).Format(time.RFC3339) {
		t.Fatal("recent timestamps were not UTC", rows)
	}
	if all := latestChartRows(t, f, h, "timeweaver"); len(all) != 3 || all[0]["sg_name"] != "private-newest-one" {
		t.Fatal("admin recent limit/order", all)
	}
	manualExec(t, f, "UPDATE execution_log SET result_message=NULL WHERE execution_id=?", firstFailure)
	manualExec(t, f, "UPDATE execution_log SET end_time=NULL WHERE schedule_id=?", warning)
	rows = latestChartRows(t, f, h, "viewer")
	if rows[1]["custom_status"] != "warning" || rows[1]["error_summary"] != nil || rows[1]["group_end_time"] != nil {
		t.Fatal("a first failure without text must not become success or borrow later text", rows[1])
	}
	manualExec(t, f, "UPDATE execution_log SET result_message='Zulu first failure' WHERE execution_id=?", firstFailure)
	// A remaining live task keeps historical results for a deleted sibling in
	// the latest execution, while the current task count excludes its tombstone.
	manualExec(t, f, "UPDATE schedule_detail SET deleted_at=UTC_TIMESTAMP() WHERE detail_id=UNHEX(REPLACE(?,'-',''))", two)
	rows = latestChartRows(t, f, h, "viewer")
	if rows[1]["task_count"] != float64(1) || rows[1]["error_summary"] != "Zulu first failure" {
		t.Fatal("partial tombstones lost latest history", rows[1])
	}
	manualExec(t, f, "UPDATE schedule_group SET status=NULL WHERE schedule_id=?", old)
	rows = latestChartRows(t, f, h, "viewer")
	if rows[2]["group_status"] != nil || rows[2]["sg_name"] != "old-visible" {
		t.Fatal("nullable legacy group status broke the recent response", rows[2])
	}
}

func TestMySQLChartsDatabaseFailureIsNotZero(t *testing.T) {
	f := chartsFixtureFor(t)
	h := chartsHandler(t, f)
	manualExec(t, f, "RENAME TABLE execution_log TO chart_execution_log_offline")
	defer manualExec(t, f, "RENAME TABLE chart_execution_log_offline TO execution_log")
	for _, path := range []string{"/dashboard/charts/schedules", "/dashboard/charts/tasks", "/dashboard/lastest-schedules"} {
		w := chartsRequest(t, f, h, "viewer", "GET", path)
		if w.Code != 503 || !strings.Contains(w.Body.String(), `"code":"unavailable"`) || strings.Contains(w.Body.String(), "_count") || strings.Contains(w.Body.String(), "chart_execution_log") {
			t.Fatal("database failure became a chart count or leaked SQL", path, w.Code, w.Body.String())
		}
	}
}

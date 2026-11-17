package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/horrible-gh/TimeWeaver/internal/access"
	"github.com/horrible-gh/TimeWeaver/internal/agentapi"
	"github.com/horrible-gh/TimeWeaver/internal/dashboard"
)

func scheduleInput(t *testing.T, f *deviceFixture, group int64, name string) dashboard.ScheduleInput {
	t.Helper()
	var device int64
	if err := f.db.DB.QueryRow("SELECT device_id FROM devices WHERE group_id=? ORDER BY device_id LIMIT 1", group).Scan(&device); err != nil {
		t.Fatal(err)
	}
	return dashboard.ScheduleInput{GroupID: group, DeviceID: device, Name: name, Status: "active", Creator: "forged"}
}
func createSchedule(t *testing.T, f *deviceFixture, group int64, name string) int64 {
	t.Helper()
	p := f.sessions["timeweaver"]
	id, err := f.dashboard.SaveSchedule(context.Background(), p.token, p.csrf, scheduleInput(t, f, group, name), true)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func taskInput(schedule int64) dashboard.TaskInput {
	command := "echo contract"
	return dashboard.TaskInput{ScheduleID: schedule, Name: "Task", Status: "active", Type: "command", Command: &command, MissingError: true, Creator: "forged"}
}
func createTask(t *testing.T, f *deviceFixture, schedule int64) string {
	t.Helper()
	p := f.sessions["timeweaver"]
	id, err := f.dashboard.SaveTask(context.Background(), p.token, p.csrf, taskInput(schedule), true)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func TestMySQLScheduleTaskScopeAndLifecycle(t *testing.T) {
	f := deviceFixtureFor(t)
	ctx := context.Background()
	p := f.sessions["editor"]
	admin := f.sessions["timeweaver"]
	for _, role := range []string{"viewer", "operator"} {
		r := f.sessions[role]
		_, err := f.dashboard.SaveSchedule(ctx, r.token, r.csrf, scheduleInput(t, f, 7, "no"), true)
		expectCode(t, err, "forbidden")
	}
	_, err := f.dashboard.SaveSchedule(ctx, p.token, p.csrf, scheduleInput(t, f, 8, "no"), true)
	expectCode(t, err, "forbidden")
	in := scheduleInput(t, f, 7, "스케줄-日本語")
	in.ErrorStop = false
	a, err := f.dashboard.SaveSchedule(ctx, p.token, p.csrf, in, true)
	if err != nil {
		t.Fatal(err)
	}
	b := createSchedule(t, f, 8, "private-eight")
	empty := createSchedule(t, f, 7, "empty")
	var actor string
	if err = f.db.DB.QueryRow("SELECT creator FROM schedule_group WHERE schedule_id=?", a).Scan(&actor); err != nil || actor != "editor" {
		t.Fatal(actor, err)
	}
	rows, err := f.dashboard.Schedules(ctx, f.sessions["viewer"].token)
	if err != nil || len(rows) != 2 || rows[0].CanEdit {
		t.Fatal(rows, err)
	}
	for _, row := range rows {
		if row.GroupID != 7 {
			t.Fatal("scope leaked")
		}
	}
	hidden := scheduleInput(t, f, 8, "forged")
	hidden.ID = b
	hidden.GroupID = 7
	_, err = f.dashboard.SaveSchedule(ctx, p.token, p.csrf, hidden, false)
	expectCode(t, err, "forbidden")
	ti := taskInput(a)
	ti.ErrorStop = false
	ti.Sequence = 3
	ti.Retry = 2
	id, err := f.dashboard.SaveTask(ctx, p.token, p.csrf, ti, true)
	if err != nil {
		t.Fatal(err)
	}
	createTask(t, f, b)
	tasks, err := f.dashboard.Tasks(ctx, f.sessions["viewer"].token)
	if err != nil || len(tasks) != 1 || tasks[0]["detail_id"] != id || tasks[0]["can_edit"] != false {
		t.Fatal(tasks, err)
	}
	ti.ID = id
	ti.ScheduleID = b
	_, err = f.dashboard.SaveTask(ctx, p.token, p.csrf, ti, false)
	expectCode(t, err, "forbidden")
	ti.ScheduleID = empty
	if _, err = f.dashboard.SaveTask(ctx, p.token, p.csrf, ti, false); err != nil {
		t.Fatal(err)
	}
	in.ID = empty
	in.DeviceID = scheduleInput(t, f, 8, "").DeviceID
	in.GroupID = 8
	_, err = f.dashboard.SaveSchedule(ctx, admin.token, admin.csrf, in, false)
	expectCode(t, err, "schedule_in_use")
	// Recursive housekeeping must be explicitly supported by the target agent.
	days := int64(4)
	path := "/tmp/tw-contract-only"
	hk := dashboard.TaskInput{ScheduleID: empty, Name: "HK", Status: "active", Type: "housekeep", Destination: &path, KeepDays: &days, Recursive: true}
	_, err = f.dashboard.SaveTask(ctx, p.token, p.csrf, hk, true)
	expectCode(t, err, "agent_upgrade_required")
	if _, err = f.db.DB.Exec("UPDATE devices SET agent_capabilities='housekeep_recursive_v1' WHERE group_id=7"); err != nil {
		t.Fatal(err)
	}
	hkid, err := f.dashboard.SaveTask(ctx, p.token, p.csrf, hk, true)
	if err != nil {
		t.Fatal(err)
	}
	rt, _ := agentapi.NewRuntime(f.db)
	snapshot, err := rt.Snapshot(ctx, agentapi.DeviceIdentity{DeviceID: scheduleInput(t, f, 7, "").DeviceID})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(snapshot.Data)
	if !strings.Contains(string(raw), id) || !strings.Contains(string(raw), "housekeep_recursive_v1") && !strings.Contains(string(raw), `"house_keep_recursive":true`) {
		t.Fatal("created task absent from snapshot", string(raw))
	}
	if err = f.dashboard.DeleteTask(ctx, p.token, p.csrf, hkid); err != nil {
		t.Fatal(err)
	}
	if err = f.dashboard.DeleteTask(ctx, p.token, p.csrf, id); err != nil {
		t.Fatal(err)
	}
	var deleted sql.NullTime
	if err = f.db.DB.QueryRow("SELECT deleted_at FROM schedule_detail WHERE detail_id=UNHEX(REPLACE(?,'-',''))", id).Scan(&deleted); err != nil || !deleted.Valid {
		t.Fatal("tombstone missing", err)
	}
	expectCode(t, f.dashboard.DeleteSchedule(ctx, p.token, p.csrf, empty), "schedule_in_use")
	if err = f.dashboard.DeleteSchedule(ctx, p.token, p.csrf, a); err != nil {
		t.Fatal(err)
	}
}
func TestMySQLScheduleTaskAtomicityAndHistory(t *testing.T) {
	f := deviceFixtureFor(t)
	ctx := context.Background()
	p := f.sessions["editor"]
	a := createSchedule(t, f, 7, "A")
	b := createSchedule(t, f, 7, "B")
	id := createTask(t, f, a)
	_, err := f.db.DB.Exec("INSERT INTO manual_execution(detail_id,status) VALUES(UNHEX(REPLACE(?,'-','')),'wait')", id)
	if err != nil {
		t.Fatal(err)
	}
	expectCode(t, f.dashboard.DeleteTask(ctx, p.token, p.csrf, id), "task_in_use")
	ti := taskInput(b)
	ti.ID = id
	_, err = f.dashboard.SaveTask(ctx, p.token, p.csrf, ti, false)
	expectCode(t, err, "task_in_use")
	if _, err = f.db.DB.Exec("UPDATE manual_execution SET status='done' WHERE detail_id=UNHEX(REPLACE(?,'-',''))", id); err != nil {
		t.Fatal(err)
	}
	_, err = f.dashboard.SaveTask(ctx, p.token, p.csrf, ti, false)
	expectCode(t, err, "task_in_use")
	// Audit failure rolls back both task rows as well as every schedule mutation.
	if _, err = f.db.DB.Exec("RENAME TABLE tw_audit TO tw_audit_offline"); err != nil {
		t.Fatal(err)
	}
	_, err = f.dashboard.SaveTask(ctx, p.token, p.csrf, taskInput(b), true)
	expectCode(t, err, "unavailable")
	var n int
	if err = f.db.DB.QueryRow("SELECT COUNT(*) FROM schedule_detail WHERE schedule_id=?", b).Scan(&n); err != nil || n != 0 {
		t.Fatal("partial insert", n, err)
	}
	ti = taskInput(a)
	ti.ID = id
	ti.Name = "should rollback"
	_, err = f.dashboard.SaveTask(ctx, p.token, p.csrf, ti, false)
	expectCode(t, err, "unavailable")
	var name string
	if err = f.db.DB.QueryRow("SELECT schedule_name FROM schedule_detail WHERE detail_id=UNHEX(REPLACE(?,'-',''))", id).Scan(&name); err != nil || name != "Task" {
		t.Fatal("update committed", name, err)
	}
	expectCode(t, f.dashboard.DeleteTask(ctx, p.token, p.csrf, id), "unavailable")
	expectCode(t, f.dashboard.DeleteSchedule(ctx, p.token, p.csrf, b), "unavailable")
	if _, err = f.db.DB.Exec("RENAME TABLE tw_audit_offline TO tw_audit"); err != nil {
		t.Fatal(err)
	}
	if err = f.dashboard.DeleteTask(ctx, p.token, p.csrf, id); err != nil {
		t.Fatal(err)
	}
	if err = f.db.DB.QueryRow("SELECT COUNT(*) FROM manual_execution WHERE detail_id=UNHEX(REPLACE(?,'-',''))", id).Scan(&n); err != nil || n != 1 {
		t.Fatal("history removed", n, err)
	}
}
func TestMySQLScheduleCanBePausedWithInactiveDevice(t *testing.T) {
	f := deviceFixtureFor(t)
	p := f.sessions["editor"]
	ctx := context.Background()
	in := scheduleInput(t, f, 7, "pause")
	id := createSchedule(t, f, 7, "pause")
	if _, err := f.db.DB.Exec("UPDATE devices SET status='inactive' WHERE device_id=?", in.DeviceID); err != nil {
		t.Fatal(err)
	}
	in.ID = id
	_, err := f.dashboard.SaveSchedule(ctx, p.token, p.csrf, in, false)
	expectCode(t, err, "device_inactive")
	in.Status = "inactive"
	if _, err = f.dashboard.SaveSchedule(ctx, p.token, p.csrf, in, false); err != nil {
		t.Fatal(err)
	}
	in.ID = 0
	_, err = f.dashboard.SaveSchedule(ctx, p.token, p.csrf, in, true)
	expectCode(t, err, "device_inactive")
}

func TestMySQLScheduleTaskHTTP(t *testing.T) {
	f := deviceFixtureFor(t)
	p := f.sessions["editor"]
	base := f.settings.Context
	guard, err := access.NewHTTP(f.auth, base, access.HTTPOptions{Origin: "http://127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	h := dashboard.NewHTTP(f.auth, guard, base)
	a := createSchedule(t, f, 7, "seven-schedule")
	createSchedule(t, f, 8, "eight-secret")
	id := createTask(t, f, a)
	for _, tc := range []struct {
		method, path, body string
		want               int
	}{
		{"GET", "/dashboard/schedule/get_schedule_groups?group_id=8", "", 200},
		{"GET", "/dashboard/tasks/get_schedule_groups?group_id=%25", "", 200},
		{"GET", "/dashboard/tasks/get_tasks/", "", 200},
		{"POST", "/dashboard/tasks/insert_task", fmt.Sprintf(`{"schedule_id":%d,"task_name":"one","command":"ok","task_type":"command","can_edit":true}`, a), 422},
		{"POST", "/dashboard/tasks/insert_task", fmt.Sprintf(`{"schedule_id":%d,"task_name":"one","command":"ok","task_type":"command","retry_count":101}`, a), 422},
		{"PUT", "/dashboard/tasks/get_tasks", "{}", 405},
		{"DELETE", "/dashboard/tasks/remove_task/not-uuid", "", 422},
		{"DELETE", "/dashboard/schedule/remove_schedule/-1", "", 422},
		{"POST", "/dashboard/schedule/insert_manual_schedule", "{}", 422},
	} {
		r := httptest.NewRequest(tc.method, "http://127.0.0.1"+base+tc.path, strings.NewReader(tc.body))
		r.RemoteAddr = "127.0.0.1:42"
		r.Header.Set("Origin", "http://127.0.0.1")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", p.csrf)
		r.AddCookie(&http.Cookie{Name: "tw_session", Value: p.token})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want || w.Header().Get("Location") != "" || strings.Contains(w.Body.String(), "eight-secret") {
			t.Fatal(tc.path, w.Code, w.Body.String())
		}
		if strings.Contains(tc.path, "get_tasks") && tc.method == "GET" && !strings.Contains(w.Body.String(), id) {
			t.Fatal("task missing")
		}
	}
}

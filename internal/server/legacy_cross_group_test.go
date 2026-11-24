package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/horrible-gh/TimeWeaver/internal/agentapi"
	"github.com/horrible-gh/TimeWeaver/internal/dashboard"
)

// Model an existing Python installation, including Unknown schedules that
// target agents in named groups. A task does not assign a different device.
func TestMySQLLegacyCrossGroupTaskSave(t *testing.T) {
	f := deviceFixtureFor(t)
	ctx := context.Background()
	admin := f.sessions["timeweaver"]
	editor := f.sessions["editor"]
	device := scheduleInput(t, f, 7, "").DeviceID
	sid := createSchedule(t, f, 7, "legacy-unknown")
	if _, err := fixtureExec(t, f.db, "UPDATE schedule_group SET group_id=0 WHERE schedule_id=?", sid); err != nil {
		t.Fatal(err)
	}
	days := int64(1)
	path := "/tmp/timeweaver-contract-only"
	hk := dashboard.TaskInput{ScheduleID: sid, Name: "legacy housekeep", Status: "active", Type: "housekeep", Destination: &path, KeepDays: &days, Recursive: true, Sequence: 20, MissingError: true}
	_, err := f.dashboard.SaveTask(ctx, editor.token, editor.csrf, hk, true)
	expectCode(t, err, "forbidden")
	_, err = f.dashboard.SaveTask(ctx, admin.token, admin.csrf, hk, true)
	expectCode(t, err, "agent_upgrade_required")
	if _, err = fixtureExec(t, f.db, "UPDATE devices SET agent_capabilities='housekeep_recursive_v1' WHERE device_id=?", device); err != nil {
		t.Fatal(err)
	}
	hkid, err := f.dashboard.SaveTask(ctx, admin.token, admin.csrf, hk, true)
	if err != nil {
		t.Fatal("existing cross-group schedule rejected a valid task", err)
	}
	hk.ID = hkid
	hk.Name = "edited housekeep"
	if _, err = f.dashboard.SaveTask(ctx, admin.token, admin.csrf, hk, false); err != nil {
		t.Fatal(err)
	}
	// All other task types have the same inherited-target behavior.
	command := taskInput(sid)
	copyTask := dashboard.TaskInput{ScheduleID: sid, Name: "copy", Status: "active", Type: "copy", Source: &path, Destination: &path}
	archive := copyTask
	archive.Name, archive.Type, archive.Archive = "archive", "archive", dname("zip")
	for _, task := range []dashboard.TaskInput{command, copyTask, archive} {
		if _, err = f.dashboard.SaveTask(ctx, admin.token, admin.csrf, task, true); err != nil {
			t.Fatal(task.Type, err)
		}
	}
	var group, target int64
	if err = fixtureRow(t, f.db, "SELECT group_id,target_device FROM schedule_group WHERE schedule_id=?", sid).Scan(&group, &target); err != nil || group != 0 || target != device {
		t.Fatal("legacy target was changed", group, target, err)
	}
	runtime, err := agentapi.NewRuntime(f.db)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := runtime.Snapshot(ctx, agentapi.DeviceIdentity{DeviceID: device})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(snapshot.Data)
	if !strings.Contains(string(raw), hkid) || !strings.Contains(string(raw), `"house_keep_recursive":true`) {
		t.Fatal("housekeeping definition missing from the assigned device", string(raw))
	}
	otherDevice := scheduleInput(t, f, 8, "").DeviceID
	other, err := runtime.Snapshot(ctx, agentapi.DeviceIdentity{DeviceID: otherDevice})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(other.Data)
	if strings.Contains(string(raw), hkid) {
		t.Fatal("task leaked to a different device")
	}
	// The existing pair can be edited or disabled, but a new mismatch cannot
	// be created or introduced by retargeting.
	in := scheduleInput(t, f, 7, "legacy renamed")
	in.GroupID = 0
	_, err = f.dashboard.SaveSchedule(ctx, admin.token, admin.csrf, in, true)
	expectCode(t, err, "device_group_mismatch")
	in.ID = sid
	if _, err = f.dashboard.SaveSchedule(ctx, admin.token, admin.csrf, in, false); err != nil {
		t.Fatal("unchanged legacy target could not be edited", err)
	}
	in.DeviceID = otherDevice
	_, err = f.dashboard.SaveSchedule(ctx, admin.token, admin.csrf, in, false)
	expectCode(t, err, "device_group_mismatch")
	in.DeviceID = device
	if _, err = fixtureExec(t, f.db, "UPDATE `groups` SET status='inactive' WHERE group_id=7"); err != nil {
		t.Fatal(err)
	}
	_, err = f.dashboard.SaveTask(ctx, admin.token, admin.csrf, command, true)
	expectCode(t, err, "group_inactive")
	_, err = f.dashboard.SaveSchedule(ctx, admin.token, admin.csrf, in, false)
	expectCode(t, err, "group_inactive")
	in.Status = "inactive"
	if _, err = f.dashboard.SaveSchedule(ctx, admin.token, admin.csrf, in, false); err != nil {
		t.Fatal("legacy target could not be paused", err)
	}
	// Nonzero mismatches must still check both groups, not just the owner.
	if _, err = fixtureExec(t, f.db, "UPDATE `groups` SET status='active' WHERE group_id=7"); err != nil {
		t.Fatal(err)
	}
	cross := createSchedule(t, f, 7, "legacy named groups")
	if _, err = fixtureExec(t, f.db, "UPDATE schedule_group SET target_device=? WHERE schedule_id=?", otherDevice, cross); err != nil {
		t.Fatal(err)
	}
	_, err = f.dashboard.SaveTask(ctx, editor.token, editor.csrf, taskInput(cross), true)
	expectCode(t, err, "forbidden")
	if _, err = f.dashboard.SaveTask(ctx, admin.token, admin.csrf, taskInput(cross), true); err != nil {
		t.Fatal("authorized legacy named-group save", err)
	}
}

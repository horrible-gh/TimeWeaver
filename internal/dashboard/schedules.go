package dashboard

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"github.com/horrible-gh/TimeWeaver/internal/access"
	"github.com/horrible-gh/TimeWeaver/internal/store"
)

var businessQueries, businessQueryError = store.Queries("mysql")

func businessExec(ctx context.Context, tx *sql.Tx, key string, args ...any) (sql.Result, error) {
	if businessQueryError != nil {
		return nil, fail(503, "unavailable")
	}
	q, err := businessQueries.Get(key)
	if err != nil {
		return nil, fail(503, "unavailable")
	}
	result, err := tx.ExecContext(ctx, q, args...)
	if err != nil {
		return nil, dbError(err)
	}
	return result, nil
}

type Schedule struct {
	ScheduleInput
	GroupName  string `json:"group_name"`
	DeviceName string `json:"device_name"`
	CanEdit    bool   `json:"can_edit"`
}

// Both resource and execution-device scopes must be visible. This also protects
// legacy rows whose group and target device were configured inconsistently.
func scheduleScope(u access.User) (string, []any, error) {
	a, args, err := u.GroupFilter("sg.group_id", "viewer")
	if err != nil {
		return "", nil, err
	}
	b, more, err := u.GroupFilter("d.group_id", "viewer")
	if err != nil {
		return "", nil, err
	}
	return "(" + a + ") AND (" + b + ")", append(args, more...), nil
}
func (s *Service) Schedules(ctx context.Context, token string) ([]Schedule, error) {
	out := []Schedule{}
	err := s.access.WithRead(ctx, token, func(tx *sql.Tx, u access.User) error {
		scope, args, err := scheduleScope(u)
		if err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT sg.group_id,sg.schedule_id,sg.name,
   COALESCE(sg.year,'*'),COALESCE(sg.month,'*'),COALESCE(sg.day_of_week,'*'),COALESCE(sg.day,'*'),COALESCE(sg.hour,'*'),COALESCE(sg.minute,'*'),COALESCE(sg.second,'*'),
   COALESCE(sg.is_error_stop,0),sg.status,sg.target_device,d.device_name,d.group_id,g.group_name
   FROM schedule_group sg JOIN devices d ON d.device_id=sg.target_device JOIN `+"`groups`"+` g ON g.group_id=sg.group_id
   WHERE sg.schedule_id>0 AND `+scope+` ORDER BY sg.schedule_id`, args...)
		if err != nil {
			return dbError(err)
		}
		defer rows.Close()
		for rows.Next() {
			var v Schedule
			var stop bool
			var dg int64
			if err = rows.Scan(&v.GroupID, &v.ID, &v.Name, &v.Year, &v.Month, &v.DayOfWeek, &v.Day, &v.Hour, &v.Minute, &v.Second, &stop, &v.Status, &v.DeviceID, &v.DeviceName, &dg, &v.GroupName); err != nil {
				return dbError(err)
			}
			v.ErrorStop = FormBool(stop)
			v.CanEdit = u.Can(v.GroupID, "editor") && u.Can(dg, "editor")
			out = append(out, v)
		}
		if err = rows.Err(); err != nil {
			return dbError(err)
		}
		return nil
	})
	return out, err
}

type scheduleLock struct {
	group, device, deviceGroup int64
	capabilities               string
}

func lockSchedule(ctx context.Context, tx *sql.Tx, u access.User, id int64) (scheduleLock, error) {
	var v scheduleLock
	if !validID(id, false) {
		return v, fail(422, "invalid_request")
	}
	err := tx.QueryRowContext(ctx, "SELECT group_id,target_device FROM schedule_group WHERE schedule_id=? FOR UPDATE", id).Scan(&v.group, &v.device)
	if errors.Is(err, sql.ErrNoRows) {
		return v, fail(404, "not_found")
	}
	if err != nil {
		return v, dbError(err)
	}
	if !u.Can(v.group, "editor") {
		return v, fail(403, "forbidden")
	}
	err = tx.QueryRowContext(ctx, "SELECT group_id,agent_capabilities FROM devices WHERE device_id=? FOR UPDATE", v.device).Scan(&v.deviceGroup, &v.capabilities)
	if errors.Is(err, sql.ErrNoRows) {
		return v, fail(409, "device_not_found")
	}
	if err != nil {
		return v, dbError(err)
	}
	if !u.Can(v.deviceGroup, "editor") {
		return v, fail(403, "forbidden")
	}
	return v, nil
}
func requireActiveGroup(ctx context.Context, tx *sql.Tx, id int64) error {
	var status string
	err := tx.QueryRowContext(ctx, "SELECT status FROM `groups` WHERE group_id=? FOR UPDATE", id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return fail(422, "group_not_found")
	}
	if err != nil {
		return dbError(err)
	}
	if status != "active" {
		return fail(422, "group_inactive")
	}
	return nil
}
func recursiveCap(value string) bool {
	for _, s := range strings.Split(value, ",") {
		if strings.TrimSpace(s) == "housekeep_recursive_v1" {
			return true
		}
	}
	return false
}
func countGuard(ctx context.Context, tx *sql.Tx, q, code string, args ...any) error {
	var n int64
	if err := tx.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
		return dbError(err)
	}
	if n > 0 {
		return fail(409, code)
	}
	return nil
}
func (s *Service) SaveSchedule(ctx context.Context, token, csrf string, in ScheduleInput, create bool) (int64, error) {
	id := in.ID
	err := s.access.WithMutation(ctx, token, csrf, func(tx *sql.Tx, u access.User) error {
		if err := in.normalize(create); err != nil {
			return err
		}
		var old scheduleLock
		if !create {
			var err error
			old, err = lockSchedule(ctx, tx, u, id)
			if err != nil {
				return err
			}
		}
		if !u.Can(in.GroupID, "editor") {
			return fail(403, "forbidden")
		}
		var deviceGroup int64
		var status, caps string
		err := tx.QueryRowContext(ctx, "SELECT group_id,status,agent_capabilities FROM devices WHERE device_id=? FOR UPDATE", in.DeviceID).Scan(&deviceGroup, &status, &caps)
		if errors.Is(err, sql.ErrNoRows) {
			return fail(422, "device_not_found")
		}
		if err != nil {
			return dbError(err)
		}
		if !u.Can(deviceGroup, "editor") {
			return fail(403, "forbidden")
		}
		if deviceGroup != in.GroupID {
			return fail(422, "device_group_mismatch")
		}
		// Pausing an existing schedule must remain possible after its device or
		// group was disabled. This exception cannot move or activate anything.
		pausing := !create && old.group == in.GroupID && old.device == in.DeviceID && in.Status == "inactive"
		if status != "active" && !pausing {
			return fail(422, "device_inactive")
		}
		if !pausing {
			if err = requireActiveGroup(ctx, tx, in.GroupID); err != nil {
				return err
			}
		}
		if !create && (old.group != in.GroupID || old.device != in.DeviceID) {
			// Retargeting existing children would transfer executable commands and their
			// history. Require a new empty schedule instead of silently moving them.
			if err = countGuard(ctx, tx, "SELECT COUNT(*) FROM schedule_detail WHERE schedule_id=?", "schedule_in_use", id); err != nil {
				return err
			}
			if err = countGuard(ctx, tx, "SELECT COUNT(*) FROM execution_log WHERE schedule_id=?", "schedule_in_use", id); err != nil {
				return err
			}
		}
		if !create && !pausing && !recursiveCap(caps) {
			if err = countGuard(ctx, tx, "SELECT COUNT(*) FROM schedule_detail sd JOIN task_detail td ON td.detail_id=sd.detail_id WHERE sd.schedule_id=? AND sd.deleted_at IS NULL AND td.task_type='housekeep' AND td.house_keep_recursive=1", "agent_upgrade_required", id); err != nil {
				return err
			}
		}
		args := append([]any{in.GroupID, in.Name}, in.CronInput.args()...)
		args = append(args, bool(in.ErrorStop), in.Status, in.DeviceID, u.ID)
		key, action := "schedules.insert_schedule", "schedule_create"
		if !create {
			key = "schedules.update_schedule"
			action = "schedule_update"
			args = append(args, id)
		}
		result, err := businessExec(ctx, tx, key, args...)
		if err != nil {
			return err
		}
		if create {
			id, err = result.LastInsertId()
			if err != nil {
				return dbError(err)
			}
		}
		return audit(ctx, tx, u.ID, action, strconv.FormatInt(id, 10), map[string]any{"group_id": in.GroupID, "target_device": in.DeviceID})
	})
	if err != nil {
		return 0, err
	}
	return id, nil
}
func (s *Service) DeleteSchedule(ctx context.Context, token, csrf string, id int64) error {
	return s.access.WithMutation(ctx, token, csrf, func(tx *sql.Tx, u access.User) error {
		if _, err := lockSchedule(ctx, tx, u, id); err != nil {
			return err
		}
		for _, table := range []string{"schedule_detail", "execution_log", "execution_running"} {
			if err := countGuard(ctx, tx, "SELECT COUNT(*) FROM "+table+" WHERE schedule_id=?", "schedule_in_use", id); err != nil {
				return err
			}
		}
		if _, err := businessExec(ctx, tx, "schedules.remove_schedule", id); err != nil {
			return err
		}
		return audit(ctx, tx, u.ID, "schedule_delete", strconv.FormatInt(id, 10), nil)
	})
}

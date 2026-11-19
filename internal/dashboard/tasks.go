package dashboard

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/horrible-gh/TimeWeaver/internal/access"
)

// Maps retain the existing task-list field names, including lastest_start_time.
// Column names and queries are code-owned; client filters never become SQL.
func (s *Service) Tasks(ctx context.Context, token string) ([]map[string]any, error) {
	out := []map[string]any{}
	err := s.access.WithRead(ctx, token, func(tx *sql.Tx, u access.User) error {
		q, err := s.query("tasks.get_tasks")
		if err != nil {
			return fail(503, "unavailable")
		}
		scope, args, err := scheduleScope(u)
		if err != nil {
			return err
		}
		if strings.Count(q, "WHERE sd.deleted_at IS NULL") != 1 || strings.Count(q, "LEFT JOIN task_detail td") != 1 || strings.Count(q, "sg.name schedule_group_name") != 1 || strings.Count(q, "SELECT el.detail_id,") != 1 || strings.Count(q, "GROUP BY el.detail_id") != 1 || strings.Count(q, "ON el.detail_id = td.detail_id") != 1 {
			return fail(503, "unavailable")
		}
		// A legacy task move must not disclose the old parent's last execution.
		q = strings.Replace(q, "SELECT el.detail_id,", "SELECT el.schedule_id, el.detail_id,", 1)
		q = strings.Replace(q, "GROUP BY el.detail_id", "GROUP BY el.schedule_id, el.detail_id", 1)
		q = strings.Replace(q, "ON el.detail_id = td.detail_id", "ON el.detail_id = td.detail_id AND el.schedule_id = sd.schedule_id", 1)
		q = strings.Replace(q, "sg.name schedule_group_name", "sg.group_id, d.group_id AS device_group_id, (sg.status='active' AND d.status='active' AND g.status='active' AND dg.status='active' AND sd.status='active' AND td.detail_id IS NOT NULL) AS runnable, sg.name schedule_group_name", 1)
		q = strings.Replace(q, "LEFT JOIN task_detail td", "JOIN devices d ON d.device_id=sg.target_device JOIN `groups` g ON g.group_id=sg.group_id JOIN `groups` dg ON dg.group_id=d.group_id LEFT JOIN task_detail td", 1)
		q = strings.Replace(q, "WHERE sd.deleted_at IS NULL", "WHERE sd.deleted_at IS NULL AND ("+scope+")", 1)
		rows, err := tx.QueryContext(ctx, q, args...)
		if err != nil {
			return dbError(err)
		}
		defer rows.Close()
		cols, err := rows.Columns()
		if err != nil {
			return dbError(err)
		}
		for rows.Next() {
			values := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range values {
				ptrs[i] = &values[i]
			}
			if err = rows.Scan(ptrs...); err != nil {
				return dbError(err)
			}
			row := map[string]any{}
			for i, c := range cols {
				value, err := jsonDateColumn(c, values[i])
				if err != nil {
					return err
				}
				switch v := value.(type) {
				case []byte:
					row[c] = string(v)
				case time.Time:
					row[c] = v.UTC().Format(time.RFC3339Nano)
				default:
					row[c] = v
				}
			}
			group, ok := row["group_id"].(int64)
			dg, ok2 := row["device_group_id"].(int64)
			if !ok || !ok2 {
				return fail(503, "unavailable")
			}
			row["can_edit"] = u.Can(group, "editor") && u.Can(dg, "editor")
			row["can_run"] = u.Can(group, "operator") && u.Can(dg, "operator") && row["runnable"] == int64(1)
			delete(row, "device_group_id")
			delete(row, "runnable")
			out = append(out, row)
		}
		if err = rows.Err(); err != nil {
			return dbError(err)
		}
		return nil
	})
	return out, err
}

// Peek, lock parent schedules in ID order, then lock/recheck the detail. This
// matches the agent's schedule -> detail -> manual order, including task moves.
func (s *Service) lockTask(ctx context.Context, tx *sql.Tx, u access.User, id []byte, destination int64) (int64, scheduleLock, error) {
	var source int64
	var target scheduleLock
	err := tx.QueryRowContext(ctx, "SELECT schedule_id FROM schedule_detail WHERE detail_id=? AND deleted_at IS NULL", id).Scan(&source)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, target, fail(404, "not_found")
	}
	if err != nil {
		return 0, target, dbError(err)
	}
	ids := []int64{source}
	if destination != 0 && destination != source {
		ids = append(ids, destination)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, sid := range ids {
		v, err := s.lockSchedule(ctx, tx, u, sid)
		if err != nil {
			return 0, target, err
		}
		if sid == destination || destination == 0 {
			target = v
		}
	}
	var actual int64
	err = tx.QueryRowContext(ctx, "SELECT schedule_id FROM schedule_detail WHERE detail_id=? AND deleted_at IS NULL"+s.lock(), id).Scan(&actual)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && actual != source) {
		return 0, target, fail(409, "resource_changed")
	}
	if err != nil {
		return 0, target, dbError(err)
	}
	return source, target, nil
}
func (s *Service) SaveTask(ctx context.Context, token, csrf string, in TaskInput, create bool) (string, error) {
	id := in.ID
	err := s.access.WithMutation(ctx, token, csrf, func(tx *sql.Tx, u access.User) error {
		if err := in.normalize(create); err != nil {
			return err
		}
		var parent scheduleLock
		var err error
		if create {
			parent, err = s.lockSchedule(ctx, tx, u, in.ScheduleID)
			if err != nil {
				return err
			}
			raw := make([]byte, 16)
			if _, err = rand.Read(raw); err != nil {
				return fail(503, "unavailable")
			}
			raw[6] = (raw[6] & 15) | 64
			raw[8] = (raw[8] & 63) | 128
			id = uuidString(raw)
		} else {
			raw, _ := parseUUID(id)
			var source int64
			source, parent, err = s.lockTask(ctx, tx, u, raw, in.ScheduleID)
			if err != nil {
				return err
			}
			if source != in.ScheduleID {
				for _, table := range []string{"execution_log", "execution_running", "manual_execution"} {
					if err = countGuard(ctx, tx, "SELECT COUNT(*) FROM "+table+" WHERE detail_id=?", "task_in_use", raw); err != nil {
						return err
					}
				}
			}
			// Never report an update as successful when the paired definition is absent.
			var present []byte
			err = tx.QueryRowContext(ctx, "SELECT detail_id FROM task_detail WHERE detail_id=?"+s.lock(), raw).Scan(&present)
			if errors.Is(err, sql.ErrNoRows) {
				return fail(409, "task_incomplete")
			}
			if err != nil {
				return dbError(err)
			}
		}
		if err = s.requireActiveGroup(ctx, tx, parent.group); err != nil {
			return err
		}
		if parent.deviceGroup != parent.group {
			return fail(422, "device_group_mismatch")
		}
		if in.Recursive && !recursiveCap(parent.capabilities) {
			return fail(409, "agent_upgrade_required")
		}
		detailArgs := append([]any{in.Name, in.ScheduleID}, in.CronInput.args()...)
		detailArgs = append(detailArgs, bool(in.ErrorStop), in.Sequence, in.Retry, in.Status, u.ID)
		taskArgs := []any{in.Command, in.Type, in.Archive, in.Source, bool(in.MissingError), in.Destination, in.DateFormat, in.TargetDateFormat, in.DestinationDateFormat, in.KeepDays, in.Recursive, u.ID}
		detailKey, taskKey, action := "tasks.insert_schedule_detail", "tasks.insert_task", "task_create"
		if create {
			detailArgs = append([]any{id}, detailArgs...)
			taskArgs = append([]any{id}, taskArgs...)
		} else {
			detailKey = "tasks.update_schedule_detail"
			taskKey = "tasks.update_task"
			action = "task_update"
			detailArgs = append(detailArgs, id)
			taskArgs = append(taskArgs, id)
		}
		if _, err = s.businessExec(ctx, tx, detailKey, detailArgs...); err != nil {
			return err
		}
		if _, err = s.businessExec(ctx, tx, taskKey, taskArgs...); err != nil {
			return err
		}
		return audit(ctx, tx, u.ID, action, id, map[string]any{"schedule_id": in.ScheduleID, "task_type": in.Type})
	})
	if err != nil {
		return "", err
	}
	return strings.ToLower(id), nil
}
func (s *Service) DeleteTask(ctx context.Context, token, csrf, id string) error {
	return s.access.WithMutation(ctx, token, csrf, func(tx *sql.Tx, u access.User) error {
		raw, err := parseUUID(id)
		if err != nil {
			return err
		}
		if _, _, err = s.lockTask(ctx, tx, u, raw, 0); err != nil {
			return err
		}
		if err = countGuard(ctx, tx, "SELECT COUNT(*) FROM execution_running WHERE detail_id=?", "task_in_use", raw); err != nil {
			return err
		}
		if err = countGuard(ctx, tx, "SELECT COUNT(*) FROM manual_execution WHERE detail_id=? AND status IN ('active','wait','processing')", "task_in_use", raw); err != nil {
			return err
		}
		if _, err = s.businessExec(ctx, tx, "tasks.remove_task", id); err != nil {
			return err
		}
		if _, err = s.businessExec(ctx, tx, "tasks.remove_schedule_detail", id); err != nil {
			return err
		}
		// Keep the detail tombstone and execution history for delayed result reports.
		return audit(ctx, tx, u.ID, "task_delete", strings.ToLower(id), nil)
	})
}

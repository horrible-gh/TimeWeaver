package dashboard

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/horrible-gh/TimeWeaver/internal/access"
)

// The agent consumes wait rows. Clients may choose when a request becomes due,
// but may not manufacture a processing/completed state or a claim lease.
type ManualInput struct {
	ScheduleID       int64    `json:"schedule_id"`
	DetailID         string   `json:"detail_id"`
	ManualID         int64    `json:"manual_id"`
	Immediate        FormBool `json:"is_immediate"`
	ScheduleDatetime string   `json:"schedule_datetime"`
	Status           string   `json:"status"`
	Creator          string   `json:"creator"`
	Modifier         string   `json:"modifier"`
}

func (in ManualInput) runTime(now time.Time) (time.Time, error) {
	if in.Status != "" && in.Status != "wait" {
		return time.Time{}, fail(422, "invalid_request")
	}
	if in.Immediate {
		return now.UTC().Truncate(time.Second), nil
	}
	t, err := parseDashboardTime(in.ScheduleDatetime)
	if err != nil {
		return time.Time{}, err
	}
	t = t.Truncate(time.Second)
	if !t.After(now) {
		return time.Time{}, fail(422, "invalid_datetime")
	}
	return t, nil
}

func parseDashboardTime(value string) (time.Time, error) {
	if len(value) > 64 {
		return time.Time{}, fail(422, "invalid_datetime")
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || t.UTC().Year() < 1000 || t.UTC().Year() > 9999 {
		return time.Time{}, fail(422, "invalid_datetime")
	}
	return t.UTC(), nil
}

func dashboardNow(ctx context.Context, tx *sql.Tx) (time.Time, error) {
	var now time.Time
	if err := tx.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP()").Scan(&now); err != nil {
		return now, dbError(err)
	}
	return now.UTC(), nil
}

type manualTarget struct {
	scheduleLock
	status, deviceStatus string
}

// Keep the agent's schedule -> detail -> manual lock order. Device/group locks
// occur while the parent is held, as in the existing schedule editing service.
func lockManualTarget(ctx context.Context, tx *sql.Tx, u access.User, id int64) (manualTarget, error) {
	var v manualTarget
	err := tx.QueryRowContext(ctx, "SELECT group_id,target_device,status FROM schedule_group WHERE schedule_id=? FOR UPDATE", id).Scan(&v.group, &v.device, &v.status)
	if errors.Is(err, sql.ErrNoRows) {
		return v, fail(404, "not_found")
	}
	if err != nil {
		return v, dbError(err)
	}
	if !u.Can(v.group, "operator") {
		return v, fail(403, "forbidden")
	}
	err = tx.QueryRowContext(ctx, "SELECT group_id,status,agent_capabilities FROM devices WHERE device_id=? FOR UPDATE", v.device).Scan(&v.deviceGroup, &v.deviceStatus, &v.capabilities)
	if errors.Is(err, sql.ErrNoRows) {
		return v, fail(409, "device_not_found")
	}
	if err != nil {
		return v, dbError(err)
	}
	if !u.Can(v.deviceGroup, "operator") {
		return v, fail(403, "forbidden")
	}
	return v, nil
}

func (v manualTarget) runnable(ctx context.Context, tx *sql.Tx) error {
	if v.status != "active" {
		return fail(409, "schedule_inactive")
	}
	if v.deviceStatus != "active" {
		return fail(409, "device_inactive")
	}
	if err := requireActiveGroup(ctx, tx, v.group); err != nil {
		return err
	}
	if v.deviceGroup != v.group {
		return requireActiveGroup(ctx, tx, v.deviceGroup)
	}
	return nil
}

type manualDetail struct {
	id         []byte
	schedule   int64
	status     string
	deleted    sql.NullTime
	definition []byte
	taskType   sql.NullString
	recursive  sql.NullBool
}

func (d manualDetail) runnable(target manualTarget) error {
	if d.deleted.Valid || d.status != "active" {
		return fail(409, "task_inactive")
	}
	if len(d.definition) != 16 {
		return fail(409, "task_incomplete")
	}
	if d.taskType.String == "housekeep" && d.recursive.Bool && !recursiveCap(target.capabilities) {
		return fail(409, "agent_upgrade_required")
	}
	return nil
}

const manualDetailSelect = `SELECT sd.detail_id,sd.schedule_id,sd.status,sd.deleted_at,td.detail_id,td.task_type,td.house_keep_recursive
 FROM schedule_detail sd LEFT JOIN task_detail td ON td.detail_id=sd.detail_id`

func scanManualDetail(row interface{ Scan(...any) error }) (manualDetail, error) {
	var d manualDetail
	err := row.Scan(&d.id, &d.schedule, &d.status, &d.deleted, &d.definition, &d.taskType, &d.recursive)
	return d, err
}

func (s *Service) QueueManual(ctx context.Context, token, csrf string, in ManualInput) ([]int64, error) {
	ids := []int64{}
	err := s.access.WithMutation(ctx, token, csrf, func(tx *sql.Tx, u access.User) error {
		if in.ManualID != 0 || (in.ScheduleID == 0) == (in.DetailID == "") || (in.ScheduleID != 0 && !validID(in.ScheduleID, false)) {
			return fail(422, "invalid_request")
		}
		schedule := in.ScheduleID
		var raw []byte
		var err error
		if in.DetailID != "" {
			raw, err = parseUUID(in.DetailID)
			if err != nil {
				return err
			}
			err = tx.QueryRowContext(ctx, "SELECT schedule_id FROM schedule_detail WHERE detail_id=? AND deleted_at IS NULL", raw).Scan(&schedule)
			if errors.Is(err, sql.ErrNoRows) {
				return fail(404, "not_found")
			}
			if err != nil {
				return dbError(err)
			}
		}
		target, err := lockManualTarget(ctx, tx, u, schedule)
		if err != nil {
			return err
		}
		if err = target.runnable(ctx, tx); err != nil {
			return err
		}
		q := manualDetailSelect + " WHERE sd.schedule_id=? AND sd.deleted_at IS NULL"
		args := []any{schedule}
		if len(raw) > 0 {
			q += " AND sd.detail_id=?"
			args = append(args, raw)
		} else {
			q += " AND sd.status='active'"
		}
		rows, err := tx.QueryContext(ctx, q+" ORDER BY sd.detail_id LIMIT 501 FOR UPDATE", args...)
		if err != nil {
			return dbError(err)
		}
		details := []manualDetail{}
		for rows.Next() {
			d, e := scanManualDetail(rows)
			if e != nil {
				rows.Close()
				return dbError(e)
			}
			details = append(details, d)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return dbError(err)
		}
		if len(details) > 500 {
			return fail(422, "too_many_tasks")
		}
		if len(details) == 0 {
			if len(raw) > 0 {
				return fail(409, "resource_changed")
			}
			return fail(409, "no_runnable_tasks")
		}
		now, err := dashboardNow(ctx, tx)
		if err != nil {
			return err
		}
		due, err := in.runTime(now)
		if err != nil {
			return err
		}
		for _, d := range details {
			if err = d.runnable(target); err != nil {
				return err
			}
			mid, e := insertManual(ctx, tx, in.Immediate, due, u.ID, d.id)
			if e != nil {
				return e
			}
			ids = append(ids, mid)
		}
		return audit(ctx, tx, u.ID, "manual_create", strconv.FormatInt(schedule, 10), map[string]any{"schedule_id": schedule, "manual_ids": ids, "is_immediate": bool(in.Immediate), "schedule_datetime": due.Format(time.RFC3339)})
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

func insertManual(ctx context.Context, tx *sql.Tx, immediate FormBool, due time.Time, actor string, raw []byte) (int64, error) {
	// Reuse the frozen insert with one prelocked, validated active detail.
	id := uuidString(raw)
	result, err := businessExec(ctx, tx, "manual_execution.insert_manual_execution", bool(immediate), bool(immediate), due, "wait", actor, nil, nil, id, id)
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	if err != nil || n != 1 {
		return 0, fail(409, "resource_changed")
	}
	mid, err := result.LastInsertId()
	if err != nil {
		return 0, dbError(err)
	}
	return mid, nil
}

// A candidate lookup never grants permission. Recheck both relationships after
// acquiring the parent/detail/manual locks, including legacy task tombstones.
func lockPendingManual(ctx context.Context, tx *sql.Tx, u access.User, id int64) (manualTarget, manualDetail, error) {
	var target manualTarget
	var detail manualDetail
	if !validID(id, false) {
		return target, detail, fail(422, "invalid_request")
	}
	var raw []byte
	var schedule int64
	err := tx.QueryRowContext(ctx, "SELECT me.detail_id,sd.schedule_id FROM manual_execution me JOIN schedule_detail sd ON sd.detail_id=me.detail_id WHERE me.manual_id=?", id).Scan(&raw, &schedule)
	if errors.Is(err, sql.ErrNoRows) {
		return target, detail, fail(404, "not_found")
	}
	if err != nil {
		return target, detail, dbError(err)
	}
	target, err = lockManualTarget(ctx, tx, u, schedule)
	if err != nil {
		return target, detail, err
	}
	detail, err = scanManualDetail(tx.QueryRowContext(ctx, manualDetailSelect+" WHERE sd.detail_id=? FOR UPDATE", raw))
	if errors.Is(err, sql.ErrNoRows) || (err == nil && detail.schedule != schedule) {
		return target, detail, fail(409, "resource_changed")
	}
	if err != nil {
		return target, detail, dbError(err)
	}
	var lockedRaw []byte
	var status string
	err = tx.QueryRowContext(ctx, "SELECT detail_id,status FROM manual_execution WHERE manual_id=? FOR UPDATE", id).Scan(&lockedRaw, &status)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !bytes.Equal(raw, lockedRaw)) {
		return target, detail, fail(409, "resource_changed")
	}
	if err != nil {
		return target, detail, dbError(err)
	}
	if status != "active" && status != "wait" {
		return target, detail, fail(409, "manual_not_pending")
	}
	return target, detail, nil
}

// A reschedule retires the old request and returns a new ID. Cached agent
// snapshots can therefore never claim the retired request after this commits.
func (s *Service) UpdateManual(ctx context.Context, token, csrf string, in ManualInput) (int64, error) {
	var replacement int64
	err := s.access.WithMutation(ctx, token, csrf, func(tx *sql.Tx, u access.User) error {
		if in.ScheduleID != 0 || in.DetailID != "" {
			return fail(422, "invalid_request")
		}
		target, d, err := lockPendingManual(ctx, tx, u, in.ManualID)
		if err != nil {
			return err
		}
		if err = target.runnable(ctx, tx); err != nil {
			return err
		}
		if err = d.runnable(target); err != nil {
			return err
		}
		now, err := dashboardNow(ctx, tx)
		if err != nil {
			return err
		}
		due, err := in.runTime(now)
		if err != nil {
			return err
		}
		replacement, err = insertManual(ctx, tx, in.Immediate, due, u.ID, d.id)
		if err != nil {
			return err
		}
		if _, err = businessExec(ctx, tx, "manual_execution.update_abandon_manual_execution", "failed", u.ID, in.ManualID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE manual_execution SET claim_token=NULL,claim_expires_at=NULL WHERE manual_id=?", in.ManualID); err != nil {
			return dbError(err)
		}
		return audit(ctx, tx, u.ID, "manual_update", strconv.FormatInt(in.ManualID, 10), map[string]any{"replacement_manual_id": replacement, "is_immediate": bool(in.Immediate), "schedule_datetime": due.Format(time.RFC3339)})
	})
	if err != nil {
		return 0, err
	}
	return replacement, nil
}

func (s *Service) CancelManual(ctx context.Context, token, csrf string, id int64) error {
	return s.access.WithMutation(ctx, token, csrf, func(tx *sql.Tx, u access.User) error {
		if _, _, err := lockPendingManual(ctx, tx, u, id); err != nil {
			return err
		}
		if _, err := businessExec(ctx, tx, "manual_execution.update_abandon_manual_execution", "failed", u.ID, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE manual_execution SET claim_token=NULL,claim_expires_at=NULL WHERE manual_id=?", id); err != nil {
			return dbError(err)
		}
		return audit(ctx, tx, u.ID, "manual_cancel", strconv.FormatInt(id, 10), nil)
	})
}

type ManualRun struct {
	ID         int64   `json:"manual_id"`
	ScheduleID int64   `json:"schedule_id"`
	DetailID   string  `json:"detail_id"`
	Name       string  `json:"name"`
	TaskName   string  `json:"schedule_name"`
	Immediate  bool    `json:"is_immediate"`
	Due        *string `json:"schedule_datetime"`
	Status     string  `json:"status"`
	EditEnable string  `json:"edit_enable"`
	CanEdit    bool    `json:"can_edit"`
	CanCancel  bool    `json:"can_cancel"`
}

func (s *Service) ManualRuns(ctx context.Context, token string) ([]ManualRun, error) {
	out := []ManualRun{}
	err := s.access.WithRead(ctx, token, func(tx *sql.Tx, u access.User) error {
		scope, args, err := scheduleScope(u)
		if err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT me.manual_id,sd.schedule_id,sd.detail_id,COALESCE(sg.name,''),COALESCE(sd.schedule_name,''),
 COALESCE(me.is_immediate,0),me.schedule_datetime,me.status,sg.group_id,d.group_id,sg.status,d.status,g.status,dg.status,sd.status,sd.deleted_at,td.detail_id,td.task_type,td.house_keep_recursive,d.agent_capabilities
 FROM manual_execution me JOIN schedule_detail sd ON sd.detail_id=me.detail_id
 JOIN schedule_group sg ON sg.schedule_id=sd.schedule_id JOIN devices d ON d.device_id=sg.target_device
 JOIN `+"`groups`"+` g ON g.group_id=sg.group_id JOIN `+"`groups`"+` dg ON dg.group_id=d.group_id
 LEFT JOIN task_detail td ON td.detail_id=sd.detail_id
 WHERE sg.schedule_id>0 AND `+scope+` ORDER BY me.manual_id DESC`, args...)
		if err != nil {
			return dbError(err)
		}
		defer rows.Close()
		for rows.Next() {
			var v ManualRun
			var d manualDetail
			var target manualTarget
			var due sql.NullTime
			var groupStatus, deviceGroupStatus string
			if err = rows.Scan(&v.ID, &v.ScheduleID, &d.id, &v.Name, &v.TaskName, &v.Immediate, &due, &v.Status, &target.group, &target.deviceGroup, &target.status, &target.deviceStatus, &groupStatus, &deviceGroupStatus, &d.status, &d.deleted, &d.definition, &d.taskType, &d.recursive, &target.capabilities); err != nil {
				return dbError(err)
			}
			v.DetailID, v.Due = uuidString(d.id), nullableDate(due)
			v.CanCancel = (v.Status == "active" || v.Status == "wait") && u.Can(target.group, "operator") && u.Can(target.deviceGroup, "operator")
			v.CanEdit = v.CanCancel && target.status == "active" && target.deviceStatus == "active" && groupStatus == "active" && deviceGroupStatus == "active" && d.runnable(target) == nil
			v.EditEnable = "0"
			if v.CanEdit {
				v.EditEnable = "1"
			}
			out = append(out, v)
		}
		if err = rows.Err(); err != nil {
			return dbError(err)
		}
		return nil
	})
	return out, err
}

package agentapi

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"sort"
	"time"
)

func (s *Runtime) Heartbeat(ctx context.Context, principal DeviceIdentity, request HeartbeatRequest, capabilities string) (HeartbeatData, error) {
	if !validHeartbeat(request) {
		return HeartbeatData{}, invalidRequest()
	}
	tx, err := s.db.DB.BeginTx(ctx, nil)
	if err != nil {
		return HeartbeatData{}, runtimeFailure()
	}
	defer tx.Rollback()
	var now time.Time
	if err = tx.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP()").Scan(&now); err != nil {
		return HeartbeatData{}, runtimeFailure()
	}
	cap := ""
	if supportsRecursive(capabilities) {
		cap = recursiveHousekeep
	}
	result, err := tx.ExecContext(ctx, `UPDATE devices SET last_heartbeat_at=?,version=?,applied_revision=?,agent_capabilities=? WHERE device_id=? AND status='active'`, now, request.AgentVersion, request.AppliedRevision, cap, principal.DeviceID)
	if err != nil {
		return HeartbeatData{}, runtimeFailure()
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return HeartbeatData{}, runtimeFailure()
	}
	if changed != 1 {
		// Two identical heartbeats in one DATETIME second can change zero rows.
		var id int64
		err = tx.QueryRowContext(ctx, `SELECT device_id FROM devices WHERE device_id=? AND status='active'`, principal.DeviceID).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return HeartbeatData{}, runtimeRevoked()
		}
		if err != nil {
			return HeartbeatData{}, runtimeFailure()
		}
	}
	if err = tx.Commit(); err != nil {
		return HeartbeatData{}, runtimeFailure()
	}
	return HeartbeatData{DeviceID: principal.DeviceID, DeviceStatus: "active", ServerTime: utcSecond(now)}, nil
}

type snapshotRead struct {
	conn     *sql.Conn
	finished bool
}

func (s *Runtime) beginSnapshot(ctx context.Context) (*snapshotRead, error) {
	conn, err := s.db.DB.Conn(ctx)
	if err != nil {
		return nil, runtimeFailure()
	}
	discard := func() { conn.Raw(func(any) error { return driver.ErrBadConn }); conn.Close() }
	// Use the same explicit non-locking view as the Python repository. Both
	// statements and every subsequent read stay on this one pooled connection.
	if _, err = conn.ExecContext(ctx, "SET TRANSACTION ISOLATION LEVEL REPEATABLE READ"); err != nil {
		discard()
		return nil, runtimeFailure()
	}
	if _, err = conn.ExecContext(ctx, "START TRANSACTION WITH CONSISTENT SNAPSHOT, READ ONLY"); err != nil {
		discard()
		return nil, runtimeFailure()
	}
	return &snapshotRead{conn: conn}, nil
}
func (r *snapshotRead) commit(ctx context.Context) error {
	if _, err := r.conn.ExecContext(ctx, "COMMIT"); err != nil {
		return runtimeFailure()
	}
	r.finished = true
	return nil
}
func (r *snapshotRead) close() {
	if !r.finished {
		// A canceled HTTP context must not leak an open transaction into the pool.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, err := r.conn.ExecContext(ctx, "ROLLBACK"); err != nil {
			r.conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}
	r.conn.Close()
}
func nullText(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}
func nullNumber(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	n := v.Int64
	return &n
}
func cronData(v [7]sql.NullString) CronData {
	parts := [7]string{}
	for i, field := range v {
		parts[i] = "*"
		if field.Valid {
			parts[i] = field.String
		}
	}
	return CronData{parts[0], parts[1], parts[2], parts[3], parts[4], parts[5], parts[6]}
}
func cronScan(v *[7]sql.NullString) []any {
	return []any{&v[0], &v[1], &v[2], &v[3], &v[4], &v[5], &v[6]}
}
func uuidText(raw []byte) (string, error) {
	if len(raw) != 16 {
		return "", inconsistentSnapshot()
	}
	text := hex.EncodeToString(raw)
	return text[:8] + "-" + text[8:12] + "-" + text[12:16] + "-" + text[16:20] + "-" + text[20:], nil
}
func (s *Runtime) loadSnapshot(ctx context.Context, conn *sql.Conn, deviceID int64) (SnapshotData, error) {
	data := SnapshotData{Schedules: []SnapshotSchedule{}, ManualRuns: []SnapshotManual{}}
	if err := conn.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP()").Scan(&data.GeneratedAt); err != nil {
		return data, runtimeFailure()
	}
	data.GeneratedAt = utcSecond(data.GeneratedAt)
	var version sql.NullString
	err := conn.QueryRowContext(ctx, `SELECT device_id,device_name,status,version FROM devices WHERE device_id=? AND status='active'`, deviceID).Scan(&data.Device.ID, &data.Device.Name, &data.Device.Status, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return data, runtimeRevoked()
	}
	if err != nil {
		return data, runtimeFailure()
	}
	data.Device.Version = nullText(version)
	if err = s.loadSchedules(ctx, conn, deviceID, &data); err != nil {
		return data, err
	}
	if err = s.loadDetails(ctx, conn, deviceID, &data); err != nil {
		return data, err
	}
	if err = s.loadManuals(ctx, conn, deviceID, &data); err != nil {
		return data, err
	}
	return data, nil
}
func (s *Runtime) loadSchedules(ctx context.Context, conn *sql.Conn, deviceID int64, data *SnapshotData) error {
	rows, err := conn.QueryContext(ctx, `SELECT schedule_id,name,year,month,day_of_week,day,hour,minute,second,is_error_stop FROM schedule_group WHERE target_device=? AND status='active'`, deviceID)
	if err != nil {
		return runtimeFailure()
	}
	defer rows.Close()
	for rows.Next() {
		group := SnapshotSchedule{Details: []SnapshotDetail{}}
		var cron [7]sql.NullString
		var stop sql.NullInt64
		args := append([]any{&group.ID, &group.Name}, cronScan(&cron)...)
		args = append(args, &stop)
		if err = rows.Scan(args...); err != nil {
			return runtimeFailure()
		}
		group.Cron = cronData(cron)
		group.ErrorStop = stop.Int64 != 0
		data.Schedules = append(data.Schedules, group)
	}
	if rows.Err() != nil {
		return runtimeFailure()
	}
	sort.Slice(data.Schedules, func(i, j int) bool { return data.Schedules[i].ID < data.Schedules[j].ID })
	return nil
}
func (s *Runtime) loadDetails(ctx context.Context, conn *sql.Conn, deviceID int64, data *SnapshotData) error {
	rows, err := conn.QueryContext(ctx, `SELECT sd.detail_id,sd.schedule_id,sd.schedule_name,
 sd.year,sd.month,sd.day_of_week,sd.day,sd.hour,sd.minute,sd.second,
 sd.is_error_stop,sd.sequence,sd.retry_count,td.detail_id,
 td.task_type,td.command,td.archive_type,td.source_path,td.error_on_missing_source,
 td.destination_path,td.date_format,td.target_date_format,td.destination_date_format,
 td.house_keep_days,td.house_keep_recursive
 FROM schedule_group sg JOIN schedule_detail sd ON sd.schedule_id=sg.schedule_id AND sd.status='active' AND sd.deleted_at IS NULL
 LEFT JOIN task_detail td ON td.detail_id=sd.detail_id WHERE sg.target_device=? AND sg.status='active'`, deviceID)
	if err != nil {
		return runtimeFailure()
	}
	defer rows.Close()
	byID := map[int64]int{}
	for i, g := range data.Schedules {
		byID[g.ID] = i
	}
	for rows.Next() {
		var d SnapshotDetail
		var detailID, taskID []byte
		var groupID int64
		var name sql.NullString
		var cron [7]sql.NullString
		var stop, retry, missing, days, recursive sql.NullInt64
		var kind, command, archive, source, destination, date, targetDate, destinationDate sql.NullString
		args := append([]any{&detailID, &groupID, &name}, cronScan(&cron)...)
		args = append(args, &stop, &d.Sequence, &retry, &taskID, &kind, &command, &archive, &source, &missing, &destination, &date, &targetDate, &destinationDate, &days, &recursive)
		if err = rows.Scan(args...); err != nil {
			return runtimeFailure()
		}
		index, ok := byID[groupID]
		if !ok {
			continue
		}
		if taskID == nil {
			return inconsistentSnapshot()
		}
		d.ID, err = uuidText(detailID)
		if err != nil {
			return err
		}
		d.Name = nullText(name)
		d.Cron = cronData(cron)
		d.ErrorStop = stop.Int64 != 0
		d.RetryCount = retry.Int64
		d.Task, err = normalizeTask(SnapshotTask{Type: kind.String, Command: nullText(command), ArchiveType: nullText(archive), SourcePath: nullText(source), ErrorOnMissingSource: missing.Int64 != 0, DestinationPath: nullText(destination), DateFormat: nullText(date), TargetDateFormat: nullText(targetDate), DestinationDateFormat: nullText(destinationDate), HouseKeepDays: nullNumber(days), HouseKeepRecursive: recursive.Int64 == 1})
		if err != nil {
			return err
		}
		data.Schedules[index].Details = append(data.Schedules[index].Details, d)
	}
	if rows.Err() != nil {
		return runtimeFailure()
	}
	for i := range data.Schedules {
		details := data.Schedules[i].Details
		sort.Slice(details, func(i, j int) bool {
			if details[i].Sequence == details[j].Sequence {
				return details[i].ID < details[j].ID
			}
			return details[i].Sequence < details[j].Sequence
		})
		var rank int64
		for j := range details {
			if j == 0 || details[j].Sequence != details[j-1].Sequence {
				rank++
			}
			details[j].ExecSequence = rank
		}
	}
	return nil
}
func (s *Runtime) loadManuals(ctx context.Context, conn *sql.Conn, deviceID int64, data *SnapshotData) error {
	rows, err := conn.QueryContext(ctx, `SELECT me.manual_id,sd.schedule_id,me.detail_id,me.status,me.is_immediate,me.schedule_datetime,me.claim_expires_at
 FROM schedule_group sg JOIN schedule_detail sd ON sd.schedule_id=sg.schedule_id AND sd.status='active' AND sd.deleted_at IS NULL
 JOIN manual_execution me ON me.detail_id=sd.detail_id WHERE sg.target_device=? AND sg.status='active' AND me.status IN ('wait','processing')`, deviceID)
	if err != nil {
		return runtimeFailure()
	}
	defer rows.Close()
	for rows.Next() {
		var m SnapshotManual
		var id []byte
		var immediate sql.NullInt64
		var claim sql.NullTime
		if err = rows.Scan(&m.ID, &m.ScheduleID, &id, &m.Status, &immediate, &m.ScheduledAt, &claim); err != nil {
			return runtimeFailure()
		}
		m.DetailID, err = uuidText(id)
		if err != nil {
			return err
		}
		m.Immediate = immediate.Int64 != 0
		m.ScheduledAt = utcSecond(m.ScheduledAt)
		m.Claimable = m.Status == "wait" || (m.Status == "processing" && claim.Valid && !claim.Time.After(data.GeneratedAt))
		data.ManualRuns = append(data.ManualRuns, m)
	}
	if rows.Err() != nil {
		return runtimeFailure()
	}
	sort.Slice(data.ManualRuns, func(i, j int) bool {
		a, b := data.ManualRuns[i], data.ManualRuns[j]
		if a.ScheduledAt.Equal(b.ScheduledAt) {
			return a.ID < b.ID
		}
		return a.ScheduledAt.Before(b.ScheduledAt)
	})
	return nil
}

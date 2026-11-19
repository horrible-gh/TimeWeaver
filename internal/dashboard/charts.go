package dashboard

import (
	"context"
	"database/sql"
	"strings"

	"github.com/horrible-gh/TimeWeaver/internal/access"
	"github.com/horrible-gh/TimeWeaver/internal/store"
)

// Typed counts keep empty aggregates numeric in JSON on both supported stores.
type StatusChart struct {
	Active   int64 `json:"active_count"`
	Error    int64 `json:"error_count"`
	Inactive int64 `json:"inactive_count"`
}

type TaskStatusChart struct {
	Pending    int64 `json:"pending_count"`
	InProgress int64 `json:"in_progress_count"`
	Completed  int64 `json:"completed_count"`
	Error      int64 `json:"error_count"`
}

type LatestSchedule struct {
	ID           int64   `json:"schedule_id"`
	Status       string  `json:"custom_status"`
	DeviceName   string  `json:"device_name"`
	Name         string  `json:"sg_name"`
	GroupStatus  *string `json:"group_status"`
	TaskCount    int64   `json:"task_count"`
	StartTime    *string `json:"group_start_time"`
	EndTime      *string `json:"group_end_time"`
	ErrorSummary *string `json:"error_summary"`
}

func chartQuery(kind string, u access.User) (string, []any, error) {
	return chartQueryDialect("mysql", kind, u)
}

func chartQueryDialect(dialect, kind string, u access.User) (string, []any, error) {
	catalog, catalogErr := businessCatalog(dialect)
	if catalogErr != nil {
		return "", nil, fail(503, "unavailable")
	}
	key := ""
	switch kind {
	case "devices":
		key = "charts.get_chart_devices_all"
	case "schedules":
		key = "charts.get_chart_schedules"
	case "tasks":
		key = "charts.get_chart_tasks"
	case "latest":
		key = "get_lastest_schedules"
	default:
		return "", nil, fail(503, "unavailable")
	}
	q, err := catalog.Get(key)
	if err != nil {
		return "", nil, fail(503, "unavailable")
	}
	return scopeChartQuery(kind, q, u)
}

func scopeChartQuery(kind, q string, u access.User) (string, []any, error) {
	// Derive from the frozen catalog, including Windows CRLF checkouts. Every
	// insertion is guarded so catalog drift cannot silently remove a boundary.
	q = strings.ReplaceAll(q, "\r\n", "\n")
	var scope string
	var args []any
	var err error
	if kind == "devices" {
		scope, args, err = u.GroupFilter("devices.group_id", "viewer")
	} else {
		scope, args, err = scheduleScope(u)
	}
	if err != nil {
		return "", nil, err
	}
	var replacements [][2]string
	switch kind {
	case "devices":
		replacements = [][2]string{
			{"WHERE device_id > 0", "WHERE device_id > 0 AND (" + scope + ")"},
		}
	case "schedules":
		replacements = [][2]string{
			{"FROM schedule_group sg", "FROM schedule_group sg\n    JOIN devices d ON d.device_id = sg.target_device\n    WHERE sg.schedule_id > 0 AND d.device_id > 0 AND (" + scope + ")"},
		}
	case "tasks":
		replacements = [][2]string{
			{"PARTITION BY el.detail_id ORDER BY el.start_time DESC", "PARTITION BY el.schedule_id, el.detail_id ORDER BY el.start_time DESC, el.execution_id DESC"},
			{"ON el.detail_id = sd.detail_id", "ON el.detail_id = sd.detail_id AND el.schedule_id = sd.schedule_id"},
			{"WHERE sd.deleted_at IS NULL", "WHERE sd.deleted_at IS NULL AND sg.schedule_id > 0 AND d.device_id > 0 AND (" + scope + ")"},
		}
	case "latest":
		replacements = [][2]string{
			{"MAX(start_time) AS group_latest_time", "MAX(start_time) AS group_latest_time,\n    MAX(execution_id) AS group_latest_id"},
			{"PARTITION BY schedule_id ORDER BY group_latest_time DESC", "PARTITION BY schedule_id ORDER BY group_latest_time DESC, group_latest_id DESC"},
			{"el.start_time\n  FROM execution_log el", "el.start_time,\n    ROW_NUMBER() OVER (PARTITION BY el.schedule_id ORDER BY el.start_time, el.execution_id) AS error_rank\n  FROM execution_log el"},
			{"WHERE el.result_code = -1\n  ORDER BY el.start_time ASC", "WHERE el.result_code != 0"},
			{"MIN(raw_error) AS raw_error\n  FROM first_error_last\n  GROUP BY schedule_id", "raw_error\n  FROM first_error_last\n  WHERE error_rank = 1"},
			{"fep.raw_error IS NOT NULL THEN 'warning'", "fep.schedule_id IS NOT NULL THEN 'warning'"},
			{"ORDER BY li.group_latest_time DESC\nLIMIT 3", "WHERE sg.schedule_id > 0 AND d.device_id > 0 AND (" + scope + ")\nORDER BY li.group_latest_time DESC, li.group_latest_id DESC, sg.schedule_id DESC\nLIMIT 3"},
		}
	default:
		return "", nil, fail(503, "unavailable")
	}
	for _, pair := range replacements {
		if strings.Count(q, pair[0]) != 1 {
			return "", nil, fail(503, "unavailable")
		}
		q = strings.Replace(q, pair[0], pair[1], 1)
	}
	return q, args, nil
}

func (s *Service) statusChart(ctx context.Context, token, kind string) (StatusChart, error) {
	var out StatusChart
	err := s.access.WithRead(ctx, token, func(tx *sql.Tx, u access.User) error {
		q, args, err := chartQueryDialect(s.access.Dialect(), kind, u)
		if err != nil {
			return err
		}
		if err = tx.QueryRowContext(ctx, q, args...).Scan(&out.Active, &out.Error, &out.Inactive); err != nil {
			return dbError(err)
		}
		return nil
	})
	return out, err
}

func (s *Service) DeviceChart(ctx context.Context, token string) (StatusChart, error) {
	return s.statusChart(ctx, token, "devices")
}

func (s *Service) ScheduleChart(ctx context.Context, token string) (StatusChart, error) {
	return s.statusChart(ctx, token, "schedules")
}

func (s *Service) TaskChart(ctx context.Context, token string) (TaskStatusChart, error) {
	var out TaskStatusChart
	err := s.access.WithRead(ctx, token, func(tx *sql.Tx, u access.User) error {
		q, args, err := chartQueryDialect(s.access.Dialect(), "tasks", u)
		if err != nil {
			return err
		}
		if err = tx.QueryRowContext(ctx, q, args...).Scan(&out.Pending, &out.InProgress, &out.Completed, &out.Error); err != nil {
			return dbError(err)
		}
		return nil
	})
	return out, err
}

func (s *Service) LatestSchedules(ctx context.Context, token string) ([]LatestSchedule, error) {
	out := []LatestSchedule{}
	err := s.access.WithRead(ctx, token, func(tx *sql.Tx, u access.User) error {
		q, args, err := chartQueryDialect(s.access.Dialect(), "latest", u)
		if err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, q, args...)
		if err != nil {
			return dbError(err)
		}
		defer rows.Close()
		for rows.Next() {
			var v LatestSchedule
			var start, end sql.NullTime
			var groupStatus, summary sql.NullString
			if err = rows.Scan(&v.ID, &v.Status, &v.DeviceName, &v.Name, &groupStatus, &v.TaskCount, store.ScanNullTime(&start), store.ScanNullTime(&end), &summary); err != nil {
				return dbError(err)
			}
			v.GroupStatus = nullableString(groupStatus)
			v.StartTime, v.EndTime, v.ErrorSummary = nullableDate(start), nullableDate(end), nullableString(summary)
			out = append(out, v)
		}
		if err = rows.Err(); err != nil {
			return dbError(err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

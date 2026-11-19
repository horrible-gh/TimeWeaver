package dashboard

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/horrible-gh/TimeWeaver/internal/access"
)

const historyLimit = 1000

func historyRange(start, end string, now time.Time) (time.Time, time.Time, error) {
	to := now.UTC()
	var err error
	if end != "" {
		to, err = parseDashboardTime(end)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	from := to.Add(-30 * 24 * time.Hour)
	if start != "" {
		from, err = parseDashboardTime(start)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	if from.After(to) || to.Sub(from) > 366*24*time.Hour || from.Year() < 1000 {
		return time.Time{}, time.Time{}, fail(422, "invalid_date_range")
	}
	return from, to, nil
}

func historyQuery(u access.User) (string, []any, error) {
	return historyQueryDialect("mysql", u)
}

func historyQueryDialect(dialect string, u access.User) (string, []any, error) {
	catalog, catalogErr := businessCatalog(dialect)
	if catalogErr != nil {
		return "", nil, fail(503, "unavailable")
	}
	q, err := catalog.Get("get_execution_logs")
	if err != nil {
		return "", nil, fail(503, "unavailable")
	}
	return scopeHistoryQueryDialect(dialect, q, u)
}

func scopeHistoryQuery(q string, u access.User) (string, []any, error) {
	return scopeHistoryQueryDialect("mysql", q, u)
}

func scopeHistoryQueryDialect(dialect, q string, u access.User) (string, []any, error) {
	// Windows checkouts can embed CRLF. Normalize the derived query in memory;
	// the frozen SQL source stays byte-for-byte unchanged on every platform.
	q = strings.ReplaceAll(q, "\r\n", "\n")
	// Derive the read from the frozen catalog without altering its bytes. Match
	// the recorded parent as well as detail ID, so a legacy move cannot reveal
	// another group's current command/path or make its old history visible.
	jsonValue := "JSON_VALUE(environment_info, '$.device_name')"
	guardedJSON := "CASE WHEN JSON_VALID(el.environment_info) THEN JSON_VALUE(el.environment_info, '$.device_name') ELSE NULL END"
	if dialect == "sqlite" {
		jsonValue = "json_extract(environment_info, '$.device_name')"
		guardedJSON = "CASE WHEN json_valid(el.environment_info) THEN json_extract(el.environment_info, '$.device_name') ELSE NULL END"
	}
	replacements := [][2]string{
		{"td.detail_id\n", "sd.detail_id\n        , sg.schedule_id\n"},
		{"el.execution_id\n", "el.execution_id\n    , el.execution_grp_id AS execution_uuid\n    , el.attempt\n    , el.manual_id\n    , el.schedule_id\n"},
		{"ON sh.detail_id = el.detail_id", "ON sh.detail_id = el.detail_id AND sh.schedule_id = el.schedule_id"},
		{jsonValue, guardedJSON},
		{"WHERE start_time >= ? AND start_time <= ?", "LEFT JOIN schedule_group scope_sg ON scope_sg.schedule_id=el.schedule_id\nLEFT JOIN devices scope_device ON scope_device.device_id=scope_sg.target_device\nWHERE start_time >= ? AND start_time <= ? AND "},
	}
	for _, pair := range replacements {
		if strings.Count(q, pair[0]) != 1 {
			return "", nil, fail(503, "unavailable")
		}
		q = strings.Replace(q, pair[0], pair[1], 1)
	}
	scope := "1=1"
	var args []any
	if u.Role != "admin" {
		a, first, e := u.GroupFilter("scope_sg.group_id", "viewer")
		if e != nil {
			return "", nil, e
		}
		b, second, e := u.GroupFilter("scope_device.group_id", "viewer")
		if e != nil {
			return "", nil, e
		}
		// Global memberships also require resolved resource/device ownership.
		// System and unresolved legacy records have no trustworthy group scope.
		scope = "(scope_sg.schedule_id>0 AND scope_device.device_id>0 AND (" + a + ") AND (" + b + "))"
		args = append(first, second...)
	}
	q = strings.Replace(q, "AND \nORDER BY", "AND "+scope+"\nORDER BY", 1)
	if strings.Contains(q, "AND \nORDER BY") || !strings.Contains(q, "AND "+scope+"\nORDER BY") {
		return "", nil, fail(503, "unavailable")
	}
	return strings.TrimSpace(q) + " LIMIT 1001", args, nil
}

func (s *Service) ExecutionHistory(ctx context.Context, token, start, end string) ([]map[string]any, error) {
	out := []map[string]any{}
	err := s.access.WithRead(ctx, token, func(tx *sql.Tx, u access.User) error {
		now, err := s.dashboardNow(ctx, tx)
		if err != nil {
			return err
		}
		from, to, err := historyRange(start, end, now)
		if err != nil {
			return err
		}
		q, args, err := historyQueryDialect(s.access.Dialect(), u)
		if err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, q, append([]any{s.timeValue(from), s.timeValue(to)}, args...)...)
		if err != nil {
			return dbError(err)
		}
		defer rows.Close()
		cols, err := rows.Columns()
		if err != nil {
			return dbError(err)
		}
		for rows.Next() {
			if len(out) == historyLimit {
				return fail(422, "history_too_large")
			}
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
					if c == "execution_uuid" {
						row[c] = uuidString(v)
					} else {
						row[c] = string(v)
					}
				case time.Time:
					row[c] = v.UTC().Format(time.RFC3339Nano)
				default:
					row[c] = v
				}
			}
			out = append(out, row)
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

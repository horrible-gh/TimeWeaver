package store

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
)

var sqliteRequiredColumns = map[string]string{
	"groups":                  "group_id group_name status creator created_at modifier modified_at",
	"users":                   "group_id user_id name password email role created_at",
	"devices":                 "group_id device_id device_name status version creator created_at modifier modified_at last_login_at last_heartbeat_at applied_revision agent_capabilities",
	"schedule_group":          "group_id schedule_id name year month day_of_week day hour minute second is_error_stop status target_device creator created_at modifier modified_at",
	"schedule_detail":         "detail_id schedule_name schedule_id year month day_of_week day hour minute second is_immediate is_error_stop sequence retry_count status creator created_at modifier modified_at deleted_at",
	"task_detail":             "detail_id command task_type archive_type source_path error_on_missing_source destination_path date_format target_date_format destination_date_format house_keep_days house_keep_recursive creator created_at modifier modified_at",
	"manual_execution":        "manual_id detail_id is_immediate schedule_datetime status claim_token claim_expires_at creator created_at modifier modified_at",
	"execution_log":           "execution_id execution_grp_id schedule_id detail_id attempt manual_id start_time end_time result_code result_message environment_info",
	"execution_running":       "schedule_id detail_id execution_grp_id attempt start_time",
	"agent_enrollment_token":  "enrollment_id token_hash device_name group_id created_at expires_at used_at used_by_device_id revoked_at",
	"agent_device_credential": "credential_id device_id token_hash issued_at expires_at revoked_at",
	"agent_event":             "event_id device_id event_type severity occurred_at message environment_info created_at",
}

type sqliteColumn struct {
	name, kind string
	notNull    bool
	def        sql.NullString
	pk         int
	hidden     int
}

func sqliteColumns(ctx context.Context, q queryer, table string) ([]sqliteColumn, error) {
	if err := validIdentifier(table); err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, "PRAGMA table_xinfo(`"+table+"`)")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []sqliteColumn{}
	for rows.Next() {
		var cid, notNull int
		var c sqliteColumn
		if err = rows.Scan(&cid, &c.name, &c.kind, &notNull, &c.def, &c.pk, &c.hidden); err != nil {
			return nil, err
		}
		c.notNull = notNull != 0
		out = append(out, c)
	}
	return out, rows.Err()
}

func inspectSQLiteCritical(ctx context.Context, q queryer) (SchemaReport, error) {
	result := SchemaReport{ReadOnly: true, Issues: []SchemaIssue{}, Repaired: []string{}}
	names := make([]string, 0, len(sqliteRequiredColumns))
	for name := range sqliteRequiredColumns {
		names = append(names, name)
	}
	sort.Strings(names)
	reference, err := sqliteReference()
	if err != nil {
		return result, dbError("load SQLite schema reference", err)
	}
	for _, name := range names {
		columns, err := sqliteColumns(ctx, q, name)
		if err != nil {
			return result, dbError("inspect SQLite schema", err)
		}
		if len(columns) == 0 {
			result.Issues = append(result.Issues, SchemaIssue{Target: name, Reason: "missing_table"})
			continue
		}
		present := map[string]bool{}
		for _, c := range columns {
			present[c.name] = true
		}
		missing := []string{}
		for _, column := range strings.Fields(sqliteRequiredColumns[name]) {
			if !present[column] {
				missing = append(missing, column)
			}
		}
		if len(missing) != 0 {
			result.Issues = append(result.Issues, SchemaIssue{Target: name, Missing: missing, Reason: "missing_columns"})
			continue
		}
		shape, err := sqliteTableShape(ctx, q, name)
		if err != nil || !exactSQLiteShape(shape, reference[name]) {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			result.Issues = append(result.Issues, SchemaIssue{Target: name, Reason: "unexpected_table_shape"})
		}
	}
	if len(result.Issues) != 0 {
		return result, nil
	}
	var name string
	err = q.QueryRowContext(ctx, "SELECT group_name FROM `groups` WHERE group_id=0").Scan(&name)
	if errors.Is(err, sql.ErrNoRows) || err == nil && name != "Unknown" {
		result.Issues = append(result.Issues, SchemaIssue{Target: "groups.0", Reason: "restore_unknown_group"})
	} else if err != nil {
		return result, dbError("inspect SQLite reserved group", err)
	}
	if err = checkSQLiteForeignKeys(ctx, q); err != nil {
		result.Issues = append(result.Issues, SchemaIssue{Target: "foreign_keys", Reason: "foreign_key_violation"})
	}
	return result, nil
}

func (s *Store) repairSQLiteCritical(ctx context.Context) (SchemaReport, error) {
	result := SchemaReport{Issues: []SchemaIssue{}, Repaired: []string{}}
	if s.readOnly {
		return result, errors.New("schema repair requires an explicitly writable database")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return result, dbError("lock SQLite schema repair", err)
	}
	defer tx.Rollback()
	catalog, err := Migrations("sqlite")
	if err != nil {
		return result, err
	}
	report, err := inspect(ctx, tx, "sqlite", catalog)
	if err != nil {
		return result, err
	}
	if report.Blocked || report.Pending != 0 || !report.HistoryExists {
		return result, errors.New("complete clean migration history is required before schema repair")
	}
	checked, err := inspectSQLiteCritical(ctx, tx)
	if err != nil {
		return result, err
	}
	restoreGroup := false
	for _, issue := range checked.Issues {
		if issue.Target == "groups.0" {
			restoreGroup = true
		}
	}
	for _, issue := range checked.Issues {
		if issue.Target != "groups.0" && !(restoreGroup && issue.Target == "foreign_keys") {
			return checked, errors.New("unexpected SQLite schema or data; manual review required")
		}
	}
	if restoreGroup {
		if _, err = tx.ExecContext(ctx, "INSERT INTO `groups`(group_id,group_name) VALUES(0,'Unknown') ON CONFLICT(group_id) DO UPDATE SET group_name='Unknown'"); err != nil {
			return result, dbError("restore SQLite reserved group", err)
		}
		result.Repaired = append(result.Repaired, "groups.0")
	}
	if err = checkSQLiteForeignKeys(ctx, tx); err != nil {
		return result, err
	}
	if err = tx.Commit(); err != nil {
		return result, dbError("commit SQLite schema repair", err)
	}
	return result, nil
}

package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
)

const sqliteFoundation = "go_sqlite_001.sql"
const sqliteSeedHash = "$pbkdf2-sha256$29000$UGptrTWGsPbeO2csJaTUWg$L0UlEAeIEwTtylnWYs2Jx/DgDdB174/k7ba7VeDj3xg"

func (s *Store) applySQLite(ctx context.Context, catalog []Migration) (MigrationReport, error) {
	return s.applySQLiteMode(ctx, catalog, false)
}

func (s *Store) applySQLiteMode(ctx context.Context, catalog []Migration, startup bool) (MigrationReport, error) {
	var empty MigrationReport
	if s.readOnly {
		return empty, errors.New("read-only database")
	}
	conn, err := s.DB.Conn(ctx)
	if err != nil {
		return empty, dbError("pin SQLite migration connection", err)
	}
	defer conn.Close()
	// SQLite's documented table-rebuild sequence requires this per-connection
	// setting before BEGIN. All copied rows are checked before COMMIT. This
	// connection is discarded on every exit, including cancellation, so the
	// normal pool can never inherit weakened foreign-key enforcement.
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(cleanup, "PRAGMA foreign_keys=ON")
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
	}()
	if _, err = conn.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		return empty, dbError("prepare SQLite migration", err)
	}
	tx, err := beginSQLiteMigration(ctx, conn) // Open configures BEGIN IMMEDIATE.
	if err != nil {
		return empty, dbError("lock SQLite migration", err)
	}
	defer tx.Rollback()
	report, err := inspect(ctx, tx, "sqlite", catalog)
	if err != nil {
		return report, err
	}
	if report.Blocked {
		return report, errors.New("migration blocked by unknown, changed or unfinished history; inspect the database before retrying")
	}
	if startup {
		if _, err := StartupMigrations(report); err != nil {
			return report, err
		}
	}
	if report.Pending == 0 {
		return report, nil
	}
	var businessTables int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name NOT IN ('migrations',?)", attemptTable).Scan(&businessTables); err != nil {
		return report, dbError("inspect SQLite installation", err)
	}
	var tracked int
	if report.HistoryExists {
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM migrations").Scan(&tracked); err != nil {
			return report, dbError("inspect SQLite history", err)
		}
	}
	fresh := businessTables == 0 && tracked == 0
	if !fresh && !report.HistoryExists {
		return report, errors.New("existing SQLite database has no migration history; refusing to bootstrap over data")
	}
	if !fresh {
		if tracked == 0 {
			return report, errors.New("existing SQLite schema is not a tracked installation")
		}
	}
	for _, ddl := range []string{
		"CREATE TABLE IF NOT EXISTS migrations(filename TEXT NOT NULL PRIMARY KEY,applied_at DATETIME DEFAULT CURRENT_TIMESTAMP)",
		"CREATE TABLE IF NOT EXISTS " + attemptTable + "(filename TEXT NOT NULL PRIMARY KEY,checksum TEXT NOT NULL,state TEXT NOT NULL,statement_index INTEGER NOT NULL DEFAULT 0,started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,finished_at DATETIME)",
	} {
		if _, err = tx.ExecContext(ctx, ddl); err != nil {
			return report, dbError("prepare SQLite migration history", err)
		}
	}
	states := map[string]string{}
	for _, item := range report.Migrations {
		states[item.Name] = item.State
	}
	for _, m := range catalog {
		if states[m.Name] == "applied" {
			continue
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO "+attemptTable+"(filename,checksum,state) VALUES(?,?,'running')", m.Name, m.SHA256); err != nil {
			return report, dbError("record SQLite migration", err)
		}
		if m.Name == sqliteFoundation && !fresh {
			err = upgradeSQLiteFoundation(ctx, tx, m)
		} else {
			for i, statement := range m.Statements {
				if _, err = tx.ExecContext(ctx, statement); err != nil {
					err = fmt.Errorf("SQLite migration %s statement %d rolled back: %w", m.Name, i+1, dbError("migration", err))
					break
				}
			}
		}
		if err != nil {
			return report, err
		}
		if m.Name == sqliteFoundation {
			if _, err = tx.ExecContext(ctx, "INSERT INTO `groups`(group_id,group_name) VALUES(0,'Unknown') ON CONFLICT(group_id) DO UPDATE SET group_name='Unknown'"); err != nil {
				return report, dbError("restore SQLite reserved group", err)
			}
			if fresh {
				if _, err = tx.ExecContext(ctx, "INSERT INTO devices(device_id,device_name,group_id) VALUES(-1,'%',0)"); err != nil {
					return report, dbError("seed SQLite device sentinel", err)
				}
				if _, err = tx.ExecContext(ctx, "INSERT INTO users(group_id,user_id,name,password,role) VALUES(0,'timeweaver','TimeWeaver Administrator',?,'admin')", sqliteSeedHash); err != nil {
					return report, dbError("seed SQLite administrator", err)
				}
			}
		}
		if err = checkSQLiteForeignKeys(ctx, tx); err != nil {
			return report, err
		}
		if m.Name == sqliteFoundation || m.Name == "go_sqlite_002.sql" {
			checked, checkErr := inspectSQLiteSchema(ctx, tx, m.Name != sqliteFoundation)
			if checkErr != nil {
				return report, checkErr
			}
			if len(checked.Issues) != 0 {
				return report, errors.New("SQLite migration did not produce the required schema; changes rolled back")
			}
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO migrations(filename) VALUES(?)", m.Name); err != nil {
			return report, dbError("record SQLite migration completion", err)
		}
		if _, err = tx.ExecContext(ctx, "UPDATE "+attemptTable+" SET state='applied',statement_index=?,finished_at=CURRENT_TIMESTAMP WHERE filename=?", len(m.Statements), m.Name); err != nil {
			return report, dbError("record SQLite migration checksum", err)
		}
	}
	result, err := inspect(ctx, tx, "sqlite", catalog)
	if err != nil {
		return report, err
	}
	if err = tx.Commit(); err != nil {
		return report, dbError("commit SQLite migration", err)
	}
	result.ReadOnly = false
	return result, nil
}

func checkSQLiteForeignKeys(ctx context.Context, q queryer) error {
	rows, err := q.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return dbError("check SQLite foreign keys", err)
	}
	defer rows.Close()
	if rows.Next() {
		return errors.New("SQLite foreign-key violations require review; migration rolled back")
	}
	return rows.Err()
}

func beginSQLiteMigration(ctx context.Context, conn *sql.Conn) (*sql.Tx, error) {
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		tx, err := conn.BeginTx(ctx, nil)
		if err == nil || !IsBusy(err) || time.Now().After(deadline) {
			return tx, err
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

// Only columns introduced by known additive migrations may be absent. A BLOB
// task identity is mandatory: numeric task IDs cannot be mapped to UUIDs safely.
var sqliteLegacyAdditions = map[string]string{
	"users":            "group_id",
	"devices":          "last_heartbeat_at applied_revision agent_capabilities",
	"schedule_detail":  "is_immediate deleted_at",
	"task_detail":      "house_keep_recursive",
	"manual_execution": "claim_token claim_expires_at",
	"execution_log":    "attempt manual_id",
}

func upgradeSQLiteFoundation(ctx context.Context, tx *sql.Tx, migration Migration) error {
	reference, err := sqliteReference()
	if err != nil {
		return err
	}
	var views int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_schema WHERE type='view'").Scan(&views); err != nil {
		return err
	}
	if views != 0 {
		return errors.New("legacy SQLite views require manual migration review")
	}
	names := make([]string, 0, len(reference))
	for name := range reference {
		names = append(names, name)
	}
	sort.Strings(names)
	old := map[string]sqliteShape{}
	for _, name := range names {
		shape, err := sqliteTableShape(ctx, tx, name)
		if err != nil {
			return fmt.Errorf("legacy SQLite %s metadata requires review: %w", name, err)
		}
		old[name] = shape
		if shape.ddl == "" {
			continue
		}
		if err = supportedSQLiteLegacyShape(name, shape, reference[name]); err != nil {
			return err
		}
		var triggers int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_schema WHERE type='trigger' AND tbl_name=?", name).Scan(&triggers); err != nil {
			return err
		}
		if triggers != 0 {
			return fmt.Errorf("legacy SQLite %s has custom triggers; manual review required", name)
		}
	}
	if err = validateSQLitePredecessorClaims(ctx, tx, old); err != nil {
		return err
	}
	for _, name := range names {
		want, prior := reference[name], old[name]
		if prior.ddl == "" {
			if _, err = tx.ExecContext(ctx, want.ddl); err != nil {
				return dbError("create missing SQLite table", err)
			}
			continue
		}
		shadow := "tw_rebuild_" + name
		var collision int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_schema WHERE name=?", shadow).Scan(&collision); err != nil {
			return err
		}
		if collision != 0 {
			return errors.New("SQLite rebuild table name is already in use")
		}
		var highwater int64
		if strings.Contains(strings.ToUpper(prior.ddl), "AUTOINCREMENT") {
			err = tx.QueryRowContext(ctx, "SELECT seq FROM sqlite_sequence WHERE name=?", name).Scan(&highwater)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		ddl := "CREATE TABLE `" + shadow + "` " + want.ddl[strings.Index(want.ddl, "("):]
		if _, err = tx.ExecContext(ctx, ddl); err != nil {
			return dbError("create SQLite replacement table", err)
		}
		columns := make([]string, len(prior.columns))
		for i, c := range prior.columns {
			columns[i] = "`" + c.name + "`"
		}
		joined := strings.Join(columns, ",")
		if _, err = tx.ExecContext(ctx, "INSERT INTO `"+shadow+"`("+joined+") SELECT "+joined+" FROM `"+name+"`"); err != nil {
			return fmt.Errorf("legacy SQLite %s data does not satisfy the supported schema; changes rolled back: %w", name, dbError("preserve SQLite rows", err))
		}
		if _, err = tx.ExecContext(ctx, "DROP TABLE `"+name+"`"); err != nil {
			return dbError("replace SQLite table", err)
		}
		if _, err = tx.ExecContext(ctx, "ALTER TABLE `"+shadow+"` RENAME TO `"+name+"`"); err != nil {
			return dbError("rename SQLite replacement", err)
		}
		if highwater > 0 {
			if _, err = tx.ExecContext(ctx, "UPDATE sqlite_sequence SET seq=MAX(seq,?) WHERE name=?", highwater, name); err != nil {
				return err
			}
		}
	}
	// Original, ordinary indexes remain useful and are preserved verbatim. Unique
	// constraints are rebuilt from their verified canonical definitions instead.
	for _, name := range names {
		for _, index := range old[name].indexes {
			if index.unique || index.ddl == "" {
				continue
			}
			statements, err := SplitSQL(index.ddl, "sqlite")
			if err != nil || len(statements) != 1 || !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(statements[0])), "CREATE INDEX ") {
				return errors.New("custom SQLite index requires manual review")
			}
			if _, err = tx.ExecContext(ctx, index.ddl); err != nil {
				return dbError("restore SQLite index", err)
			}
		}
	}
	for _, statement := range migration.Statements {
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(statement)), "CREATE INDEX ") {
			if _, err = tx.ExecContext(ctx, statement); err != nil {
				return dbError("create SQLite index", err)
			}
		}
	}
	for _, name := range names {
		if err = normalizeSQLiteDates(ctx, tx, name, reference[name]); err != nil {
			return err
		}
	}
	return nil
}

func supportedSQLiteLegacyShape(name string, got, want sqliteShape) error {
	fail := func() error {
		return fmt.Errorf("legacy SQLite %s has an unsupported schema; manual review required", name)
	}
	if got.pk != want.pk || got.autoIncrement != want.autoIncrement || !reflect.DeepEqual(sqlitePKKeys(got), sqlitePKKeys(want)) {
		return fail()
	}
	expected := map[string]sqliteColumn{}
	for _, c := range want.columns {
		expected[c.name] = c
	}
	for _, c := range got.columns {
		w, ok := expected[c.name]
		if !ok || c.hidden != 0 || !sqliteLegacyType(c.kind, w.kind) {
			return fail()
		}
		if c.notNull != w.notNull {
			// These three nullable declarations occur in the frozen SQLite
			// predecessors. Existing NULL ownership/identity still fails COPY.
			knownNullable := !c.notNull && (name == "users" && (c.name == "user_id" || c.name == "group_id") || name == "schedule_group" && c.name == "target_device")
			if !knownNullable {
				return fail()
			}
		}
		if sqliteDefault(c.def) != sqliteDefault(w.def) {
			legacyDefault := name == "schedule_group" && c.name == "target_device" && sqliteDefault(c.def) == "0" || name == "task_detail" && c.name == "date_format" && sqliteDefault(c.def) == "'%Y%m%d'"
			if !legacyDefault {
				return fail()
			}
		}
		delete(expected, c.name)
	}
	optional := " " + sqliteLegacyAdditions[name] + " "
	for missing := range expected {
		if !strings.Contains(optional, " "+missing+" ") {
			return fail()
		}
	}
	if len(got.fks) != 0 && !reflect.DeepEqual(got.fks, want.fks) {
		return fail()
	}
	allowedChecks := map[string]bool{}
	for _, check := range want.checks {
		allowedChecks[check] = true
	}
	for _, check := range []string{
		"statusin('active','inactive')", "statusin('active','inactive','error')", "archive_typein('zip')",
	} {
		allowedChecks[check] = true
	}
	for _, check := range got.checks {
		if !allowedChecks[check] {
			return fail()
		}
	}
	allowedUnique := map[string]bool{}
	for _, key := range sqliteUniqueKeys(want) {
		allowedUnique[key] = true
	}
	if name == "devices" {
		allowedUnique["device_name"] = true
	}
	if name == "execution_log" {
		allowedUnique["execution_id"] = true
	}
	for _, key := range sqliteUniqueKeys(got) {
		if !allowedUnique[key] {
			return fail()
		}
	}
	canonicalIndex := map[string]string{}
	for _, index := range want.indexes {
		if index.origin == "c" {
			canonicalIndex[index.name] = index.columns
		}
	}
	for _, index := range got.indexes {
		if index.partial || strings.Contains(index.columns, "<") {
			return fail()
		}
		if cols, ok := canonicalIndex[index.name]; ok && index.columns != cols {
			return fail()
		}
	}
	return nil
}

func sqliteDefault(value sql.NullString) string {
	if !value.Valid || strings.EqualFold(strings.TrimSpace(value.String), "NULL") {
		return ""
	}
	text := strings.TrimSpace(value.String)
	switch strings.ToUpper(text) {
	case "TRUE":
		return "1"
	case "FALSE":
		return "0"
	case "CURRENT_TIMESTAMP":
		return "CURRENT_TIMESTAMP"
	}
	return text
}

func sqliteLegacyType(got, want string) bool {
	got, want = strings.ToUpper(strings.TrimSpace(got)), strings.ToUpper(want)
	if got == want {
		return true
	}
	switch want {
	case "INTEGER":
		return strings.Contains(got, "INT") || got == "BOOLEAN" || got == "BOOL"
	case "TEXT":
		return strings.Contains(got, "CHAR") || strings.Contains(got, "TEXT") || got == "CLOB"
	case "DATETIME":
		return got == "TEXT" || got == "TIMESTAMP" || got == "DATE"
	case "BLOB":
		return got == "BINARY(16)" || got == "BINARY(32)"
	}
	return false
}

func validateSQLitePredecessorClaims(ctx context.Context, tx *sql.Tx, shapes map[string]sqliteShape) error {
	claims := map[string]string{
		"groups_001.sql": "groups", "groups_002.sql": "groups",
		"users_001.sql": "users", "users_002.sql": "users.group_id",
		"users_003_group_integrity.sql": "users.group_id devices.group_id agent_enrollment_token.group_id schedule_group.group_id",
		"timeweaver_server_014.sql":     "execution_running",
		"timeweaver_server_015.sql":     "devices.agent_capabilities task_detail.house_keep_recursive",
	}
	for filename, requirements := range claims {
		var applied int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM migrations WHERE filename=?", filename).Scan(&applied); err != nil {
			return err
		}
		if applied == 0 {
			continue
		}
		for _, requirement := range strings.Fields(requirements) {
			parts := strings.SplitN(requirement, ".", 2)
			shape := shapes[parts[0]]
			present := shape.ddl != ""
			if present && len(parts) == 2 {
				present = false
				for _, c := range shape.columns {
					if c.name == parts[1] {
						present = true
					}
				}
			}
			if !present {
				return errors.New("legacy SQLite migration history disagrees with its schema; manual review required")
			}
		}
	}
	return nil
}

// Normalize instants, never compare RFC3339/offset/SQL text lexically. Bounded
// batches keep historical execution logs from becoming an unbounded allocation.
func normalizeSQLiteDates(ctx context.Context, tx *sql.Tx, table string, shape sqliteShape) error {
	columns := []string{}
	for _, c := range shape.columns {
		if c.kind == "DATETIME" {
			columns = append(columns, c.name)
		}
	}
	if len(columns) == 0 {
		return nil
	}
	selects, updates := []string{}, []string{}
	for _, c := range columns {
		selects = append(selects, "CAST(`"+c+"` AS TEXT)")
		updates = append(updates, "`"+c+"`=?")
	}
	var last int64
	first := true
	for {
		query := "SELECT rowid," + strings.Join(selects, ",") + " FROM `" + table + "`"
		args := []any{}
		if !first {
			query += " WHERE rowid>?"
			args = append(args, last)
		}
		query += " ORDER BY rowid LIMIT 256"
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return dbError("read SQLite dates", err)
		}
		type changedRow struct {
			id     int64
			values []any
		}
		changed := []changedRow{}
		count := 0
		for rows.Next() {
			var id int64
			values := make([]sql.NullString, len(columns))
			dest := []any{&id}
			for i := range values {
				dest = append(dest, &values[i])
			}
			if err = rows.Scan(dest...); err != nil {
				rows.Close()
				return err
			}
			entry := changedRow{id: id, values: make([]any, len(values))}
			needsUpdate := false
			for i, value := range values {
				if !value.Valid {
					continue
				}
				t, parseErr := parseTimestamp(value.String)
				if parseErr != nil {
					rows.Close()
					return fmt.Errorf("legacy SQLite %s.%s contains an invalid timestamp; changes rolled back", table, columns[i])
				}
				canonical := TimeValue("sqlite", t).(string)
				entry.values[i] = canonical
				needsUpdate = needsUpdate || canonical != value.String
			}
			if needsUpdate {
				changed = append(changed, entry)
			}
			last, count = id, count+1
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, entry := range changed {
			args := append(entry.values, entry.id)
			if _, err = tx.ExecContext(ctx, "UPDATE `"+table+"` SET "+strings.Join(updates, ",")+" WHERE rowid=?", args...); err != nil {
				return dbError("normalize SQLite dates", err)
			}
		}
		if count < 256 {
			return nil
		}
		first = false
	}
}

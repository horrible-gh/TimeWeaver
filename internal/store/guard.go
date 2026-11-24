package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

type SchemaIssue struct {
	Target  string   `json:"target"`
	Missing []string `json:"missing,omitempty"`
	Reason  string   `json:"reason,omitempty"`
}
type SchemaReport struct {
	ReadOnly bool          `json:"read_only"`
	Issues   []SchemaIssue `json:"issues"`
	Repaired []string      `json:"repaired"`
}
type enumRule struct {
	table, column string
	required      []string
}

var criticalEnums = []enumRule{
	{"task_detail", "archive_type", []string{"null", "zip"}},
	{"schedule_detail", "status", []string{"active", "inactive", "error", "manual"}},
}

type enumColumn struct {
	kind, nullable, extra, comment string
	def, charset, collation        sql.NullString
}

var metadataName = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// CriticalSchema inspects the legacy invariants. A failure is explicit rather
// than silently declaring a broken schema healthy. No writes occur here.
func (s *Store) CriticalSchema(ctx context.Context) (SchemaReport, error) {
	if s.Dialect == "sqlite" {
		return inspectSQLiteCritical(ctx, s.DB)
	}
	return inspectCritical(ctx, s.DB)
}
func inspectCritical(ctx context.Context, q queryer) (SchemaReport, error) {
	result := SchemaReport{ReadOnly: true, Issues: []SchemaIssue{}, Repaired: []string{}}
	for _, rule := range criticalEnums {
		column, err := readEnumColumn(ctx, q, rule)
		if errors.Is(err, sql.ErrNoRows) {
			result.Issues = append(result.Issues, SchemaIssue{Target: rule.table + "." + rule.column, Reason: "missing_column"})
			continue
		}
		if err != nil {
			return result, dbError("inspect critical schema", err)
		}
		members, err := parseEnum(column.kind)
		if err != nil {
			result.Issues = append(result.Issues, SchemaIssue{Target: rule.table + "." + rule.column, Reason: "unexpected_column_type"})
			continue
		}
		missing := missingMembers(members, rule.required)
		if len(missing) > 0 {
			result.Issues = append(result.Issues, SchemaIssue{Target: rule.table + "." + rule.column, Missing: missing})
		}
	}
	var optionColumns int
	if err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='task_detail' AND COLUMN_NAME='house_keep_delete_empty_dirs' AND DATA_TYPE='tinyint' AND IS_NULLABLE='NO' AND COLUMN_DEFAULT='0'").Scan(&optionColumns); err != nil {
		return result, dbError("inspect housekeeping option", err)
	}
	if optionColumns != 1 {
		result.Issues = append(result.Issues, SchemaIssue{Target: "task_detail.house_keep_delete_empty_dirs", Reason: "missing_or_unexpected_column"})
	}
	var name string
	err := q.QueryRowContext(ctx, "SELECT group_name FROM `groups` WHERE group_id=0").Scan(&name)
	if errors.Is(err, sql.ErrNoRows) || err == nil && name != "Unknown" {
		result.Issues = append(result.Issues, SchemaIssue{Target: "groups.0", Reason: "restore_unknown_group"})
	} else if err != nil {
		return result, dbError("inspect hidden group", err)
	}
	return result, nil
}
func readEnumColumn(ctx context.Context, q queryer, rule enumRule) (enumColumn, error) {
	var c enumColumn
	err := q.QueryRowContext(ctx, `SELECT COLUMN_TYPE,IS_NULLABLE,COLUMN_DEFAULT,EXTRA,CHARACTER_SET_NAME,COLLATION_NAME,COLUMN_COMMENT
 FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND COLUMN_NAME=?`, rule.table, rule.column).Scan(&c.kind, &c.nullable, &c.def, &c.extra, &c.charset, &c.collation, &c.comment)
	return c, err
}
func missingMembers(members, required []string) []string {
	out := []string{}
	for _, r := range required {
		found := false
		for _, m := range members {
			if m == r {
				found = true
				break
			}
		}
		if !found {
			out = append(out, r)
		}
	}
	return out
}

// RepairCriticalSchema widens only the two legacy enums and restores group 0.
// It shares the migration lock, requires complete clean history, and preserves
// extra enum members and their ordinal order. It never narrows a column.
func (s *Store) RepairCriticalSchema(ctx context.Context) (SchemaReport, error) {
	result := SchemaReport{Issues: []SchemaIssue{}, Repaired: []string{}}
	if s.readOnly {
		return result, errors.New("schema repair requires an explicitly writable database")
	}
	if s.Dialect == "sqlite" {
		return s.repairSQLiteCritical(ctx)
	}
	conn, err := s.DB.Conn(ctx)
	if err != nil {
		return result, dbError("pin schema connection", err)
	}
	defer conn.Close()
	var database string
	if err = conn.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&database); err != nil {
		return result, dbError("identify database", err)
	}
	key := fmt.Sprintf("timeweaver:migrate:%x", sha256.Sum256([]byte(database)))[:63]
	var locked sql.NullInt64
	if err = conn.QueryRowContext(ctx, "SELECT GET_LOCK(?,5)", key).Scan(&locked); err != nil {
		return result, dbError("acquire schema lock", err)
	}
	if !locked.Valid || locked.Int64 != 1 {
		return result, errors.New("another migration or repair holds the database lock")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		conn.ExecContext(cleanup, "SELECT RELEASE_LOCK(?)", key)
		conn.Raw(func(any) error { return driver.ErrBadConn })
	}()
	catalog, err := Migrations("mysql")
	if err != nil {
		return result, err
	}
	history, err := inspect(ctx, conn, "mysql", catalog)
	if err != nil {
		return result, err
	}
	if history.Blocked || history.Pending != 0 || !history.HistoryExists {
		return result, errors.New("complete clean migration history is required before schema repair")
	}
	var mode string
	if err = conn.QueryRowContext(ctx, "SELECT @@SESSION.sql_mode").Scan(&mode); err != nil {
		return result, dbError("inspect session mode", err)
	}
	// Standardize string escaping on this disposable connection. It is never
	// returned to the pool, even if a repair fails halfway through.
	modes := []string{}
	for _, v := range strings.Split(mode, ",") {
		if v != "NO_BACKSLASH_ESCAPES" && v != "" {
			modes = append(modes, v)
		}
	}
	if _, err = conn.ExecContext(ctx, "SET SESSION sql_mode=?", strings.Join(modes, ",")); err != nil {
		return result, dbError("set repair session mode", err)
	}
	var version string
	if err = conn.QueryRowContext(ctx, "SELECT VERSION()").Scan(&version); err != nil {
		return result, dbError("inspect database dialect", err)
	}
	mariaDB := strings.Contains(version, "MariaDB")
	// Plan ALL enum changes before executing any; unexpected schema is not an
	// invitation to replace it with an assumed historical definition.
	type change struct{ target, sql string }
	changes := []change{}
	for _, rule := range criticalEnums {
		column, e := readEnumColumn(ctx, conn, rule)
		if e != nil {
			return result, dbError("inspect repair column", e)
		}
		members, e := parseEnum(column.kind)
		if e != nil {
			return result, errors.New("critical column is not a supported enum; manual review required")
		}
		missing := missingMembers(members, rule.required)
		if len(missing) == 0 {
			continue
		}
		if column.extra != "" || !column.charset.Valid || !column.collation.Valid || !metadataName.MatchString(column.charset.String) || !metadataName.MatchString(column.collation.String) {
			return result, errors.New("unexpected enum attributes; manual review required")
		}
		members = append(members, missing...)
		quoted := make([]string, len(members))
		for i, v := range members {
			quoted[i] = mysqlLiteral(v)
		}
		ddl := "ALTER TABLE `" + rule.table + "` MODIFY COLUMN `" + rule.column + "` ENUM(" + strings.Join(quoted, ",") + ") CHARACTER SET " + column.charset.String + " COLLATE " + column.collation.String
		if column.nullable == "YES" {
			ddl += " NULL"
		} else {
			ddl += " NOT NULL"
		}
		if column.def.Valid {
			def := column.def.String
			writeDefault := true
			// MariaDB exposes quoted defaults, MySQL exposes the literal value.
			if mariaDB && len(def) >= 2 && def[0] == '\'' {
				parsed, e := parseEnum("enum(" + def + ")")
				if e != nil || len(parsed) != 1 {
					return result, errors.New("unsupported enum default")
				}
				def = parsed[0]
			} else if mariaDB && def == "NULL" && column.nullable == "YES" {
				ddl += " DEFAULT NULL"
				writeDefault = false
			}
			if writeDefault {
				ddl += " DEFAULT " + mysqlLiteral(def)
			}
		} else if column.nullable == "YES" {
			ddl += " DEFAULT NULL"
		}
		ddl += " COMMENT " + mysqlLiteral(column.comment)
		changes = append(changes, change{rule.table + "." + rule.column, ddl})
	}
	checked, err := inspectCritical(ctx, conn)
	if err != nil {
		return result, err
	}
	for _, issue := range checked.Issues {
		if issue.Target == "task_detail.house_keep_delete_empty_dirs" {
			return checked, errors.New("unexpected housekeeping option schema; manual review required")
		}
	}
	for _, c := range changes {
		if _, err = conn.ExecContext(ctx, c.sql); err != nil {
			return result, dbError("repair "+c.target, err)
		}
		result.Repaired = append(result.Repaired, c.target)
	}
	var name string
	err = conn.QueryRowContext(ctx, "SELECT group_name FROM `groups` WHERE group_id=0").Scan(&name)
	if errors.Is(err, sql.ErrNoRows) || err == nil && name != "Unknown" {
		if _, err = conn.ExecContext(ctx, "SET SESSION sql_mode=CONCAT_WS(',',NULLIF(@@SESSION.sql_mode,''),'NO_AUTO_VALUE_ON_ZERO')"); err != nil {
			return result, dbError("preserve group zero", err)
		}
		if _, err = conn.ExecContext(ctx, "INSERT INTO `groups`(group_id,group_name) VALUES (0,'Unknown') ON DUPLICATE KEY UPDATE group_name='Unknown'"); err != nil {
			return result, dbError("restore hidden group", err)
		}
		result.Repaired = append(result.Repaired, "groups.0")
	} else if err != nil {
		return result, dbError("inspect hidden group", err)
	}
	checked, err := inspectCritical(ctx, conn)
	result.Issues = checked.Issues
	return result, err
}
func mysqlLiteral(s string) string {
	return "'" + strings.NewReplacer("\\", "\\\\", "'", "''", "\x00", "\\0", "\n", "\\n", "\r", "\\r").Replace(s) + "'"
}

// parseEnum preserves ordering and handles quoted commas and SQL escapes.
func parseEnum(kind string) ([]string, error) {
	bad := errors.New("invalid enum definition")
	kind = strings.TrimSpace(kind)
	if len(kind) < 7 || !strings.EqualFold(kind[:5], "enum(") || kind[len(kind)-1] != ')' {
		return nil, bad
	}
	s := kind[5 : len(kind)-1]
	out := []string{}
	for {
		s = strings.TrimSpace(s)
		if s == "" || s[0] != '\'' {
			return nil, bad
		}
		s = s[1:]
		var b strings.Builder
		closed := false
		for i := 0; i < len(s); i++ {
			if s[i] == '\\' {
				i++
				if i >= len(s) {
					return nil, bad
				}
				switch s[i] {
				case 'n':
					b.WriteByte('\n')
				case 'r':
					b.WriteByte('\r')
				case 't':
					b.WriteByte('\t')
				case '0':
					b.WriteByte(0)
				case 'b':
					b.WriteByte('\b')
				case 'Z':
					b.WriteByte(26)
				default:
					b.WriteByte(s[i])
				}
				continue
			}
			if s[i] == '\'' {
				if i+1 < len(s) && s[i+1] == '\'' {
					b.WriteByte('\'')
					i++
					continue
				}
				s = s[i+1:]
				closed = true
				break
			}
			b.WriteByte(s[i])
		}
		if !closed {
			return nil, bad
		}
		out = append(out, b.String())
		s = strings.TrimSpace(s)
		if s == "" {
			return out, nil
		}
		if s[0] != ',' {
			return nil, bad
		}
		s = s[1:]
	}
}

package access

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/horrible-gh/TimeWeaver/internal/store"
)

type accessCatalog interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

var sqliteAccessColumns = map[string][]string{
	"tw_user_auth":     {"user_id", "status", "version", "must_change_password", "password_changed_at", "last_login_at"},
	"tw_group_member":  {"user_id", "group_id", "role"},
	"tw_session":       {"digest", "user_id", "user_version", "csrf", "password_fingerprint", "created_at", "last_seen", "expires_at", "auth_method"},
	"tw_audit":         {"id", "at", "actor", "action", "result", "target", "detail"},
	"tw_login_attempt": {"key_hash", "count", "started"},
	"tw_user_identity": {"subject", "user_id", "os_name", "created_at"},
}

func sqliteAccessTable(ctx context.Context, q accessCatalog, name string) (bool, error) {
	var n int
	err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", name).Scan(&n)
	if err != nil {
		return false, unavailable()
	}
	return n == 1, nil
}

func sqliteAccessColumn(ctx context.Context, q accessCatalog, table, column string) (bool, error) {
	var n int
	err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_table_info(?) WHERE name=?", table, column).Scan(&n)
	if err != nil {
		return false, unavailable()
	}
	return n == 1, nil
}

func sqliteAccessStatus(ctx context.Context, q accessCatalog) (SchemaStatus, error) {
	r := SchemaStatus{State: "missing", ReadOnly: true}
	exists, err := sqliteAccessTable(ctx, q, "tw_access_schema")
	if err != nil || !exists {
		return r, err
	}
	err = q.QueryRowContext(ctx, "SELECT version,state FROM tw_access_schema WHERE id=1").Scan(&r.Version, &r.State)
	if errors.Is(err, sql.ErrNoRows) {
		r.State = "unfinished"
		return r, nil
	}
	if err != nil {
		return r, unavailable()
	}
	r.Ready = r.Version == schemaVersion && r.State == "ready"
	if r.Ready {
		for _, table := range accessTables {
			exists, err = sqliteAccessTable(ctx, q, table)
			if err != nil {
				return r, err
			}
			if !exists {
				r.Ready, r.State = false, "incomplete"
				continue
			}
			for _, column := range sqliteAccessColumns[table] {
				exists, err = sqliteAccessColumn(ctx, q, table, column)
				if err != nil {
					return r, err
				}
				if !exists {
					r.Ready, r.State = false, "incomplete"
				}
			}
		}
	}
	if r.Ready {
		valid, err := sqliteAccessKeys(ctx, q, r.Version)
		if err != nil {
			return r, err
		}
		if !valid {
			r.Ready, r.State = false, "incomplete"
		}
	}
	return r, nil
}

// Column names alone do not prove that a custom table can safely hold sessions
// or identity bindings. Validate the keys, their binary comparisons, and the
// references that prevent orphaned memberships and duplicate OS bindings.
func sqliteAccessKeys(ctx context.Context, q accessCatalog, version int) (bool, error) {
	keys := map[string]string{
		"tw_access_schema": "id", "tw_user_auth": "user_id", "tw_group_member": "user_id,group_id",
		"tw_session": "digest", "tw_audit": "id", "tw_login_attempt": "key_hash", "tw_user_identity": "subject",
	}
	tables := append([]string{"tw_access_schema"}, accessBaseTables...)
	if version >= 3 {
		tables = append(tables, "tw_user_identity")
	}
	for _, table := range tables {
		var ddl string
		if err := q.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&ddl); err != nil {
			return false, unavailable()
		}
		if !sqliteAccessBinaryCollations(ddl) {
			return false, nil
		}
		var key string
		if err := q.QueryRowContext(ctx, "SELECT COALESCE(group_concat(name,','),'') FROM (SELECT name FROM pragma_table_info(?) WHERE pk>0 ORDER BY pk)", table).Scan(&key); err != nil {
			return false, unavailable()
		}
		if key != keys[table] {
			return false, nil
		}
		var nonBinary int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_index_list(?) i JOIN pragma_index_xinfo(i.name) x WHERE i.origin='pk' AND x."key"=1 AND x.coll<>'BINARY'`, table).Scan(&nonBinary); err != nil {
			return false, unavailable()
		}
		if nonBinary != 0 {
			return false, nil
		}
	}
	references := []struct{ table, column, target, targetColumn string }{
		{"tw_user_auth", "user_id", "users", "user_id"},
		{"tw_group_member", "user_id", "users", "user_id"},
		{"tw_group_member", "group_id", "groups", "group_id"},
		{"tw_session", "user_id", "users", "user_id"},
	}
	if version >= 3 {
		references = append(references, struct{ table, column, target, targetColumn string }{"tw_user_identity", "user_id", "users", "user_id"})
	}
	for _, ref := range references {
		var n int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_foreign_key_list(?) WHERE "from"=? AND "table"=? AND "to"=?`, ref.table, ref.column, ref.target, ref.targetColumn).Scan(&n); err != nil {
			return false, unavailable()
		}
		if n != 1 {
			return false, nil
		}
	}
	if version >= 3 {
		var uniqueUsers int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM (
 SELECT i.name FROM pragma_index_list('tw_user_identity') i JOIN pragma_index_xinfo(i.name) x ON x."key"=1
 WHERE i."unique"=1 AND i.partial=0 GROUP BY i.name HAVING COUNT(*)=1 AND MIN(x.name)='user_id' AND MIN(x.coll)='BINARY')`).Scan(&uniqueUsers); err != nil {
			return false, unavailable()
		}
		if uniqueUsers == 0 {
			return false, nil
		}
	}
	return true, nil
}

// Index metadata alone misses a column declared NOCASE whose PRIMARY KEY
// explicitly overrides it with BINARY: ordinary equality still uses NOCASE.
// Known access schemas use only binary comparisons. Reject other collations
// and custom schema comments rather than adopting ambiguous identity policy.
func sqliteAccessBinaryCollations(ddl string) bool {
	needBinary := false
	for i := 0; i < len(ddl); {
		if strings.ContainsRune(" \t\r\n\f", rune(ddl[i])) {
			i++
			continue
		}
		if i+1 < len(ddl) && (ddl[i:i+2] == "--" || ddl[i:i+2] == "/*") {
			return false
		}
		if ddl[i] == '\'' || ddl[i] == '"' || ddl[i] == '`' || ddl[i] == '[' {
			start, end := i+1, ddl[i]
			if end == '[' {
				end = ']'
			}
			i++
			for i < len(ddl) {
				if ddl[i] != end {
					i++
					continue
				}
				if end != ']' && i+1 < len(ddl) && ddl[i+1] == end {
					i += 2
					continue
				}
				break
			}
			if i == len(ddl) || needBinary && !strings.EqualFold(ddl[start:i], "BINARY") {
				return false
			}
			needBinary = false
			i++
			continue
		}
		start := i
		for i < len(ddl) && (ddl[i] >= 'a' && ddl[i] <= 'z' || ddl[i] >= 'A' && ddl[i] <= 'Z' || ddl[i] >= '0' && ddl[i] <= '9' || ddl[i] == '_' || ddl[i] == '$' || ddl[i] >= 0x80) {
			i++
		}
		if start == i {
			if needBinary {
				return false
			}
			i++
			continue
		}
		word := ddl[start:i]
		if needBinary {
			if !strings.EqualFold(word, "BINARY") {
				return false
			}
			needBinary = false
		} else {
			needBinary = strings.EqualFold(word, "COLLATE")
		}
	}
	return !needBinary
}

const sqliteIdentityDDL = `CREATE TABLE tw_user_identity(
 subject TEXT COLLATE BINARY NOT NULL PRIMARY KEY,
 user_id TEXT COLLATE BINARY NOT NULL UNIQUE REFERENCES users(user_id),
 os_name TEXT COLLATE BINARY NOT NULL,created_at INTEGER NOT NULL)`

var sqliteAccessDDL = []string{
	`CREATE TABLE tw_access_schema(id INTEGER PRIMARY KEY CHECK(id=1),version INTEGER NOT NULL,state TEXT NOT NULL)`,
	`CREATE TABLE tw_user_auth(user_id TEXT COLLATE BINARY NOT NULL PRIMARY KEY REFERENCES users(user_id),status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','disabled')),version INTEGER NOT NULL DEFAULT 1,must_change_password INTEGER NOT NULL DEFAULT 0 CHECK(must_change_password IN (0,1)),password_changed_at INTEGER NULL,last_login_at INTEGER NULL)`,
	`CREATE TABLE tw_group_member(user_id TEXT COLLATE BINARY NOT NULL REFERENCES users(user_id),group_id INTEGER NOT NULL REFERENCES groups(group_id),role TEXT NOT NULL CHECK(role IN ('viewer','operator','editor')),PRIMARY KEY(user_id,group_id))`,
	`CREATE TABLE tw_session(digest TEXT COLLATE BINARY NOT NULL PRIMARY KEY,user_id TEXT COLLATE BINARY NOT NULL REFERENCES users(user_id),user_version INTEGER NOT NULL,csrf TEXT COLLATE BINARY NOT NULL,password_fingerprint TEXT COLLATE BINARY NOT NULL,created_at INTEGER NOT NULL,last_seen INTEGER NOT NULL,expires_at INTEGER NOT NULL,auth_method TEXT NOT NULL)`,
	`CREATE INDEX tw_session_user ON tw_session(user_id)`,
	`CREATE INDEX tw_session_expiry ON tw_session(expires_at)`,
	`CREATE TABLE tw_audit(id INTEGER PRIMARY KEY AUTOINCREMENT,at INTEGER NOT NULL,actor TEXT NOT NULL,action TEXT NOT NULL,result TEXT NOT NULL,target TEXT NOT NULL DEFAULT '',detail TEXT NULL)`,
	`CREATE TABLE tw_login_attempt(key_hash TEXT COLLATE BINARY NOT NULL PRIMARY KEY,count INTEGER NOT NULL,started INTEGER NOT NULL)`,
	`CREATE INDEX tw_attempt_expiry ON tw_login_attempt(started)`,
	sqliteIdentityDDL,
}

// SQLite DDL is transactional. The IMMEDIATE transaction serializes independent
// initializers and commits the schema and journal together. An existing running
// marker or an untracked table remains a hard failure, as it is on MySQL.
func initializeSQLiteAccess(ctx context.Context, db *store.Store) (SchemaStatus, error) {
	tx, err := db.DB.BeginTx(ctx, nil)
	if err != nil {
		return SchemaStatus{}, unavailable()
	}
	defer tx.Rollback()
	status, err := sqliteAccessStatus(ctx, tx)
	if err != nil {
		return status, err
	}
	if status.Ready {
		if err = tx.Commit(); err != nil {
			return status, unavailable()
		}
		return status, nil
	}
	if status.State == "missing" {
		for _, table := range accessTables {
			exists, err := sqliteAccessTable(ctx, tx, table)
			if err != nil {
				return status, err
			}
			if exists {
				return status, errors.New("untracked access tables exist; refusing to overwrite")
			}
		}
		for i, ddl := range sqliteAccessDDL {
			if _, err = tx.ExecContext(ctx, ddl); err != nil {
				return status, fmt.Errorf("SQLite access schema failed at step %d; transaction rolled back", i+1)
			}
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO tw_access_schema VALUES(1,?,'running')", schemaVersion); err != nil {
			return status, unavailable()
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO tw_user_auth(user_id,must_change_password) SELECT user_id,(password COLLATE BINARY=?) FROM users", legacySeedHash); err != nil {
			return status, unavailable()
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO tw_group_member(user_id,group_id,role) SELECT user_id,group_id,'editor' FROM users WHERE role='user'"); err != nil {
			return status, unavailable()
		}
	} else if status.State == "ready" && (status.Version == 1 || status.Version == 2) {
		if err = checkSQLiteAccessUpgrade(ctx, tx, status.Version); err != nil {
			return status, err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE tw_access_schema SET state='running' WHERE id=1"); err != nil {
			return status, unavailable()
		}
		if status.Version == 1 {
			for _, ddl := range []string{"ALTER TABLE tw_audit ADD COLUMN target TEXT NOT NULL DEFAULT ''", "ALTER TABLE tw_audit ADD COLUMN detail TEXT NULL"} {
				if _, err = tx.ExecContext(ctx, ddl); err != nil {
					return status, errors.New("SQLite audit upgrade failed; transaction rolled back")
				}
			}
		}
		if _, err = tx.ExecContext(ctx, sqliteIdentityDDL); err != nil {
			return status, errors.New("SQLite identity upgrade failed; transaction rolled back")
		}
	} else {
		return status, errors.New("access migration is unfinished or incompatible; restore/review it before retrying")
	}
	if _, err = tx.ExecContext(ctx, "UPDATE tw_access_schema SET version=?,state='ready' WHERE id=1", schemaVersion); err != nil {
		return status, unavailable()
	}
	if err = tx.Commit(); err != nil {
		return status, unavailable()
	}
	return SchemaStatus{Ready: true, Version: schemaVersion, State: "ready"}, nil
}

func checkSQLiteAccessUpgrade(ctx context.Context, tx *sql.Tx, version int) error {
	for _, table := range accessBaseTables {
		exists, err := sqliteAccessTable(ctx, tx, table)
		if err != nil {
			return err
		}
		if !exists {
			return errors.New("incomplete SQLite access schema; inspect before upgrading")
		}
		for _, column := range sqliteAccessColumns[table] {
			exists, err := sqliteAccessColumn(ctx, tx, table, column)
			if err != nil {
				return err
			}
			want := version != 1 || table != "tw_audit" || column != "target" && column != "detail"
			if exists != want {
				return errors.New("unexpected SQLite access schema; inspect before upgrading")
			}
		}
	}
	exists, err := sqliteAccessTable(ctx, tx, "tw_user_identity")
	if err != nil {
		return err
	}
	if exists {
		return errors.New("untracked OS identity table; inspect before upgrading")
	}
	valid, err := sqliteAccessKeys(ctx, tx, version)
	if err != nil {
		return err
	}
	if !valid {
		return errors.New("unexpected SQLite access keys; inspect before upgrading")
	}
	return nil
}

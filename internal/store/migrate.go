package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"sort"
	"time"
)

const attemptTable = "tw_migration_attempt"

type MigrationStatus struct {
	Name      string `json:"name"`
	SHA256    string `json:"sha256"`
	State     string `json:"state"`
	Statement int    `json:"statement,omitempty"`
}
type MigrationReport struct {
	Dialect        string            `json:"dialect"`
	ReadOnly       bool              `json:"read_only"`
	HistoryExists  bool              `json:"history_exists"`
	WriteSupported bool              `json:"write_supported"`
	Pending        int               `json:"pending"`
	Blocked        bool              `json:"blocked"`
	Migrations     []MigrationStatus `json:"migrations"`
	Unknown        []string          `json:"unknown_migrations"`
}
type attempt struct {
	hash, state string
	statement   int
}

// Status only reads the catalog and existing tracking tables. Unknown/dirty
// history is reported, never guessed to be safe or marked applied automatically.
func (s *Store) Status(ctx context.Context) (MigrationReport, error) {
	catalog, err := Migrations(s.Dialect)
	if err != nil {
		return MigrationReport{}, err
	}
	return inspect(ctx, s.DB, s.Dialect, catalog)
}
func inspect(ctx context.Context, q queryer, dialect string, catalog []Migration) (MigrationReport, error) {
	r := MigrationReport{Dialect: dialect, ReadOnly: true, WriteSupported: dialect == "mysql", Migrations: []MigrationStatus{}, Unknown: []string{}}
	exists, err := tableExists(ctx, q, dialect, "migrations")
	if err != nil {
		return r, err
	}
	r.HistoryExists = exists
	applied := map[string]bool{}
	if exists {
		rows, err := q.QueryContext(ctx, "SELECT filename FROM migrations")
		if err != nil {
			return r, dbError("read migration history", err)
		}
		for rows.Next() {
			var name string
			if err = rows.Scan(&name); err != nil {
				rows.Close()
				return r, dbError("read migration history", err)
			}
			applied[name] = true
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return r, dbError("read migration history", err)
		}
	}
	attempts := map[string]attempt{}
	exists, err = tableExists(ctx, q, dialect, attemptTable)
	if err != nil {
		return r, err
	}
	if exists {
		rows, err := q.QueryContext(ctx, "SELECT filename, checksum, state, statement_index FROM "+attemptTable)
		if err != nil {
			return r, dbError("read migration attempts", err)
		}
		for rows.Next() {
			var name string
			var a attempt
			if err = rows.Scan(&name, &a.hash, &a.state, &a.statement); err != nil {
				rows.Close()
				return r, dbError("read migration attempts", err)
			}
			attempts[name] = a
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return r, dbError("read migration attempts", err)
		}
	}
	known := map[string]bool{}
	for _, m := range catalog {
		known[m.Name] = true
		item := MigrationStatus{Name: m.Name, SHA256: m.SHA256, State: "pending"}
		if applied[m.Name] {
			item.State = "applied"
		} else {
			r.Pending++
		}
		if a, ok := attempts[m.Name]; ok {
			item.Statement = a.statement
			if a.hash != m.SHA256 {
				item.State = "checksum_mismatch"
			} else if a.state != "applied" || !applied[m.Name] {
				item.State = "blocked"
			}
			if item.State != "applied" {
				r.Blocked = true
			}
		}
		r.Migrations = append(r.Migrations, item)
	}
	unknown := map[string]bool{}
	for name := range applied {
		if !known[name] {
			unknown[name] = true
		}
	}
	for name := range attempts {
		if !known[name] {
			unknown[name] = true
		}
	}
	for name := range unknown {
		r.Unknown = append(r.Unknown, name)
	}
	sort.Strings(r.Unknown)
	if len(r.Unknown) > 0 {
		r.Blocked = true
	}
	return r, nil
}

// Migrate applies the frozen MySQL catalog on ONE pinned connection. The caller
// must stop the Python server and confirm a backup first; its migrator does not
// participate in this Go-only advisory lock. SQLite writes remain blocked until
// the missing bootstrap and query dialect have been implemented and verified.
func (s *Store) Migrate(ctx context.Context) (MigrationReport, error) {
	if s.readOnly {
		return MigrationReport{}, errors.New("migration requires an explicitly writable database")
	}
	if s.Dialect != "mysql" {
		return MigrationReport{}, ErrSQLiteIncomplete
	}
	catalog, err := Migrations(s.Dialect)
	if err != nil {
		return MigrationReport{}, err
	}
	return s.apply(ctx, catalog)
}
func (s *Store) apply(ctx context.Context, catalog []Migration) (MigrationReport, error) {
	var empty MigrationReport
	if s.readOnly {
		return empty, errors.New("read-only database")
	}
	conn, err := s.DB.Conn(ctx)
	if err != nil {
		return empty, dbError("pin migration connection", err)
	}
	defer conn.Close()
	var database string
	if err = conn.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&database); err != nil {
		return empty, dbError("identify database", err)
	}
	key := fmt.Sprintf("timeweaver:migrate:%x", sha256.Sum256([]byte(database)))[:63]
	var locked sql.NullInt64
	if err = conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 5)", key).Scan(&locked); err != nil {
		return empty, dbError("acquire migration lock", err)
	}
	if !locked.Valid || locked.Int64 != 1 {
		return empty, errors.New("another migration holds the database lock")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		conn.ExecContext(cleanup, "SELECT RELEASE_LOCK(?)", key)
		// Never return changed sql_mode, user variables or prepared statements to a pool.
		conn.Raw(func(any) error { return driver.ErrBadConn })
	}()
	report, err := inspect(ctx, conn, s.Dialect, catalog)
	if err != nil {
		return report, err
	}
	if report.Blocked {
		return report, errors.New("migration blocked by unknown, changed or unfinished history; inspect the database before retrying")
	}
	if report.Pending == 0 {
		return report, nil
	}
	if !report.HistoryExists {
		// An untracked pre-existing installation is not a new empty database.
		var count int
		if err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE()").Scan(&count); err != nil {
			return report, dbError("inspect empty database", err)
		}
		if count != 0 {
			return report, errors.New("existing database has no migration history; refusing to bootstrap over data")
		}
	}
	if _, err = conn.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS migrations (filename VARCHAR(255) PRIMARY KEY, applied_at DATETIME DEFAULT CURRENT_TIMESTAMP)"); err != nil {
		return report, dbError("create migration history", err)
	}
	if _, err = conn.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS "+attemptTable+" (filename VARCHAR(255) PRIMARY KEY, checksum CHAR(64) NOT NULL, state VARCHAR(16) NOT NULL, statement_index INT NOT NULL DEFAULT 0, started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, finished_at DATETIME NULL)"); err != nil {
		return report, dbError("create migration attempts", err)
	}
	for index, m := range catalog {
		if report.Migrations[index].State == "applied" {
			continue
		}
		if _, err = conn.ExecContext(ctx, "INSERT INTO "+attemptTable+" (filename,checksum,state) VALUES (?,?,'running')", m.Name, m.SHA256); err != nil {
			return report, dbError("record migration start", err)
		}
		if err = applyFile(ctx, conn, m); err != nil {
			return report, err
		}
	}
	result, err := inspect(ctx, conn, s.Dialect, catalog)
	result.ReadOnly = false
	return result, err
}
func applyFile(ctx context.Context, conn *sql.Conn, m Migration) error {
	fail := func(step int, cause error) error {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// If the connection died, the already-durable running marker still blocks replay.
		conn.ExecContext(cleanup, "UPDATE "+attemptTable+" SET state='failed',statement_index=? WHERE filename=? AND state='running'", step, m.Name)
		return fmt.Errorf("migration %s stopped at statement %d; partial changes may remain; %w", m.Name, step, dbError("migration", cause))
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fail(0, err)
	}
	for i, statement := range m.Statements {
		if _, err = tx.ExecContext(ctx, statement); err != nil {
			tx.Rollback()
			return fail(i+1, err)
		}
	}
	// MySQL DDL (and legacy 011's explicit COMMIT) can commit inside this file.
	// We intentionally do not claim atomic rollback of a whole migration file.
	if err = tx.Commit(); err != nil {
		return fail(len(m.Statements), err)
	}
	finish, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fail(len(m.Statements), err)
	}
	if _, err = finish.ExecContext(ctx, "INSERT INTO migrations (filename) VALUES (?)", m.Name); err != nil {
		finish.Rollback()
		return fail(len(m.Statements), err)
	}
	if _, err = finish.ExecContext(ctx, "UPDATE "+attemptTable+" SET state='applied',statement_index=?,finished_at=CURRENT_TIMESTAMP WHERE filename=?", len(m.Statements), m.Name); err != nil {
		finish.Rollback()
		return fail(len(m.Statements), err)
	}
	if err = finish.Commit(); err != nil {
		return fail(len(m.Statements), err)
	}
	return nil
}

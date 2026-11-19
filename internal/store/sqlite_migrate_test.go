package store

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/horrible-gh/TimeWeaver/internal/config"
)

func sqliteTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), config.Database{Type: "sqlite", Path: filepath.Join(t.TempDir(), "real.sqlite3")}, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func execSQLite(t *testing.T, s *Store, query string, args ...any) {
	t.Helper()
	if _, err := s.DB.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func legacySQLiteFixture(t *testing.T) *Store {
	t.Helper()
	s := sqliteTestStore(t)
	ddl, err := sqliteAssets.ReadFile("sqlite/migrations/" + sqliteFoundation)
	if err != nil {
		t.Fatal(err)
	}
	legacy := strings.NewReplacer(
		"user_id TEXT NOT NULL PRIMARY KEY", "user_id TEXT PRIMARY KEY",
		"is_immediate INTEGER", "is_immediate BOOLEAN",
		"is_error_stop INTEGER", "is_error_stop BOOLEAN",
		"error_on_missing_source INTEGER", "error_on_missing_source BOOLEAN",
		"house_keep_recursive INTEGER", "house_keep_recursive BOOLEAN",
		"target_device INTEGER NOT NULL DEFAULT -1", "target_device INTEGER DEFAULT 0",
		"UNIQUE (group_id,device_name)", "UNIQUE (device_name)",
		"    deleted_at DATETIME\n", "    deleted_at TEXT\n",
	).Replace(strings.ReplaceAll(string(ddl), "\r\n", "\n"))
	execSQLite(t, s, legacy)
	execSQLite(t, s, "CREATE TABLE migrations(filename TEXT PRIMARY KEY, applied_at TEXT)")
	files, _ := legacySQLiteMigrations()
	for _, file := range files {
		execSQLite(t, s, "INSERT INTO migrations VALUES(?,?)", file.Name, "2025-01-02T12:34:56+09:00")
	}
	execSQLite(t, s, "INSERT INTO groups(group_id,group_name) VALUES(0,'Unknown'),(7,'preserved-group')")
	execSQLite(t, s, "INSERT INTO users(group_id,user_id,name,password,role) VALUES(7,?,'legacy admin',?,'admin')", "사용자-long-id", "literal-PBKDF2-Hash+Case/Bytes")
	execSQLite(t, s, "INSERT INTO devices(group_id,device_id,device_name) VALUES(7,999,'deleted-highwater'),(7,42,'preserved-device')")
	execSQLite(t, s, "DELETE FROM devices WHERE device_id=999")
	execSQLite(t, s, "UPDATE devices SET last_heartbeat_at='2026-11-19T12:34:56.123456789+09:00' WHERE device_id=42")
	execSQLite(t, s, "INSERT INTO schedule_group(group_id,schedule_id,name,target_device,is_error_stop) VALUES(7,88,'preserved-schedule',42,NULL)")
	id := []byte("0123456789abcdef")
	grp := []byte("fedcba9876543210")
	execSQLite(t, s, "INSERT INTO schedule_detail(detail_id,schedule_id,schedule_name,is_immediate,is_error_stop,retry_count,deleted_at) VALUES(?,88,'retained tombstone',NULL,NULL,NULL,'2026-11-19T03:45:00Z')", id)
	execSQLite(t, s, "INSERT INTO task_detail(detail_id,command,error_on_missing_source,house_keep_recursive) VALUES(?,'preserved command',NULL,1)", id)
	execSQLite(t, s, "INSERT INTO manual_execution(manual_id,detail_id,is_immediate,schedule_datetime,status) VALUES(123,?,NULL,NULL,'done')", id)
	execSQLite(t, s, "INSERT INTO execution_log(execution_id,execution_grp_id,schedule_id,detail_id,attempt,manual_id,start_time,end_time,result_code,result_message) VALUES(456,?,88,?,3,123,'2026-11-19T12:30:00.100+09:00','2026-11-19 03:30:01.2 +0000 UTC',5,'preserved failure')", grp, id)
	execSQLite(t, s, "INSERT INTO agent_enrollment_token(enrollment_id,token_hash,group_id,expires_at) VALUES(?,?,7,'2026-11-20T00:00:00Z')", id, []byte(strings.Repeat("x", 32)))
	execSQLite(t, s, "CREATE INDEX site_schedule_status ON schedule_group(status)")
	execSQLite(t, s, "CREATE TABLE preserved_child(user_id TEXT REFERENCES users(user_id), token BLOB)")
	execSQLite(t, s, "INSERT INTO preserved_child VALUES(?,?)", "사용자-long-id", []byte{0, 1, 255})
	return s
}

func TestSQLiteTrackedLegacyPreservesDataAndNormalizesInstants(t *testing.T) {
	s := legacySQLiteFixture(t)
	ctx := context.Background()
	report, err := s.Migrate(ctx)
	if err != nil || report.Pending != 0 || report.Blocked || len(report.Migrations) != 8 {
		t.Fatal(report, err)
	}
	var group, id, attempt, manual int
	var user, password, heartbeat, deleted, started, ended, command string
	var detail, executionGroup, hash []byte
	if err = s.DB.QueryRow("SELECT group_id,user_id,password FROM users").Scan(&group, &user, &password); err != nil || group != 7 || user != "사용자-long-id" || password != "literal-PBKDF2-Hash+Case/Bytes" {
		t.Fatal(group, user, password, err)
	}
	if err = s.DB.QueryRow("SELECT CAST(last_heartbeat_at AS TEXT) FROM devices WHERE device_id=42").Scan(&heartbeat); err != nil || heartbeat != "2026-11-19 03:34:56.123456789" {
		t.Fatal(heartbeat, err)
	}
	if err = s.DB.QueryRow("SELECT detail_id,CAST(deleted_at AS TEXT) FROM schedule_detail").Scan(&detail, &deleted); err != nil || string(detail) != "0123456789abcdef" || deleted != "2026-11-19 03:45:00" {
		t.Fatal(detail, deleted, err)
	}
	if err = s.DB.QueryRow("SELECT execution_id,execution_grp_id,attempt,manual_id,CAST(start_time AS TEXT),CAST(end_time AS TEXT) FROM execution_log").Scan(&id, &executionGroup, &attempt, &manual, &started, &ended); err != nil || id != 456 || string(executionGroup) != "fedcba9876543210" || attempt != 3 || manual != 123 || started != "2026-11-19 03:30:00.1" || ended != "2026-11-19 03:30:01.2" {
		t.Fatal(id, attempt, manual, started, ended, err)
	}
	if err = s.DB.QueryRow("SELECT command FROM task_detail").Scan(&command); err != nil || command != "preserved command" {
		t.Fatal(command, err)
	}
	if err = s.DB.QueryRow("SELECT token_hash FROM agent_enrollment_token").Scan(&hash); err != nil || string(hash) != strings.Repeat("x", 32) {
		t.Fatal(hash, err)
	}
	for _, query := range []string{
		"SELECT COUNT(*) FROM schedule_group WHERE is_error_stop IS NULL",
		"SELECT COUNT(*) FROM schedule_detail WHERE is_immediate IS NULL AND is_error_stop IS NULL AND retry_count IS NULL",
		"SELECT COUNT(*) FROM manual_execution WHERE is_immediate IS NULL AND schedule_datetime IS NULL",
		"SELECT COUNT(*) FROM task_detail WHERE error_on_missing_source IS NULL AND house_keep_recursive=1",
		"SELECT COUNT(*) FROM preserved_child WHERE hex(token)='0001FF'",
		"SELECT COUNT(*) FROM sqlite_schema WHERE name='site_schedule_status'",
	} {
		var n int
		if err = s.DB.QueryRow(query).Scan(&n); err != nil || n != 1 {
			t.Fatal(query, n, err)
		}
	}
	var n int
	if err = s.DB.QueryRow("SELECT COUNT(*) FROM migrations WHERE applied_at='2025-01-02T12:34:56+09:00'").Scan(&n); err != nil || n != 7 {
		t.Fatal(n, err)
	}
	if err = s.DB.QueryRow("SELECT COUNT(*) FROM tw_migration_attempt").Scan(&n); err != nil || n != 1 {
		t.Fatal("predecessor attempts were invented", n, err)
	}
	if err = s.DB.QueryRow("SELECT COUNT(*) FROM users WHERE user_id='timeweaver'").Scan(&n); err != nil || n != 0 {
		t.Fatal("legacy seeded unexpectedly", n, err)
	}
	execSQLite(t, s, "INSERT INTO devices(group_id,device_name) VALUES(7,'next-device')")
	if err = s.DB.QueryRow("SELECT device_id FROM devices WHERE device_name='next-device'").Scan(&n); err != nil || n != 1000 {
		t.Fatal("AUTOINCREMENT highwater lost", n, err)
	}
	guard, err := s.CriticalSchema(ctx)
	if err != nil || len(guard.Issues) != 0 {
		t.Fatal(guard, err)
	}
	before := sqliteFingerprint(t, s)
	if _, err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if after := sqliteFingerprint(t, s); before != after {
		t.Fatal("second migration changed preserved data")
	}
}

func TestSQLiteUpgradeFromActualEarlyUserMigrations(t *testing.T) {
	s := sqliteTestStore(t)
	files, _ := legacySQLiteMigrations()
	execSQLite(t, s, "CREATE TABLE migrations(filename TEXT PRIMARY KEY,applied_at TEXT)")
	for _, file := range files {
		if file.Name != "groups_001.sql" && file.Name != "groups_002.sql" && file.Name != "users_001.sql" {
			continue
		}
		raw, err := assets.ReadFile("sql/migration/sqlite/" + file.Name)
		if err != nil {
			t.Fatal(err)
		}
		execSQLite(t, s, string(raw))
		execSQLite(t, s, "INSERT INTO migrations(filename) VALUES(?)", file.Name)
	}
	execSQLite(t, s, "INSERT INTO users(user_id,name,password,role) VALUES('before-groups','preserved','old-hash','admin')")
	if _, err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	var group int
	var password string
	if err := s.DB.QueryRow("SELECT group_id,password FROM users WHERE user_id='before-groups'").Scan(&group, &password); err != nil || group != 0 || password != "old-hash" {
		t.Fatal(group, password, err)
	}
}

func TestSQLiteLegacyRefusalIsAtomic(t *testing.T) {
	for _, tc := range []struct {
		name    string
		corrupt func(*testing.T, *Store)
	}{
		{"unknown_history", func(t *testing.T, s *Store) {
			execSQLite(t, s, "INSERT INTO migrations(filename) VALUES('site-unknown.sql')")
		}},
		{"dirty_attempt", func(t *testing.T, s *Store) {
			files, _ := legacySQLiteMigrations()
			execSQLite(t, s, "CREATE TABLE tw_migration_attempt(filename TEXT PRIMARY KEY,checksum TEXT,state TEXT,statement_index INTEGER)")
			execSQLite(t, s, "INSERT INTO tw_migration_attempt VALUES(?,?,'running',0)", files[0].Name, files[0].SHA256)
		}},
		{"checksum_mismatch", func(t *testing.T, s *Store) {
			execSQLite(t, s, "CREATE TABLE tw_migration_attempt(filename TEXT PRIMARY KEY,checksum TEXT,state TEXT,statement_index INTEGER)")
			execSQLite(t, s, "INSERT INTO tw_migration_attempt VALUES('groups_001.sql','changed','applied',1)")
		}},
		{"extra_column", func(t *testing.T, s *Store) {
			execSQLite(t, s, "ALTER TABLE users ADD COLUMN private_policy TEXT DEFAULT 'must survive'")
		}},
		{"hidden_generated_column", func(t *testing.T, s *Store) { execSQLite(t, s, "ALTER TABLE users ADD COLUMN derived TEXT AS (name)") }},
		{"custom_required_command", func(t *testing.T, s *Store) {
			replaceSQLiteTable(t, s, "task_detail", func(ddl string) string { return strings.Replace(ddl, "command TEXT", "command TEXT NOT NULL", 1) }, true)
		}},
		{"custom_group_default", func(t *testing.T, s *Store) {
			replaceSQLiteTable(t, s, "devices", func(ddl string) string {
				return strings.Replace(ddl, "group_id INTEGER NOT NULL DEFAULT 0", "group_id INTEGER NOT NULL DEFAULT 7", 1)
			}, true)
		}},
		{"custom_trigger", func(t *testing.T, s *Store) {
			execSQLite(t, s, "CREATE TRIGGER custom_policy BEFORE INSERT ON users BEGIN SELECT RAISE(ABORT,'policy'); END")
		}},
		{"custom_view", func(t *testing.T, s *Store) { execSQLite(t, s, "CREATE VIEW custom_view AS SELECT user_id FROM users") }},
		{"bad_timestamp", func(t *testing.T, s *Store) {
			execSQLite(t, s, "UPDATE execution_log SET start_time='not-a-timestamp'")
		}},
		{"orphan_group", func(t *testing.T, s *Store) {
			execSQLite(t, s, "PRAGMA foreign_keys=OFF")
			execSQLite(t, s, "UPDATE users SET group_id=99999")
			execSQLite(t, s, "PRAGMA foreign_keys=ON")
		}},
		{"numeric_task_identity", func(t *testing.T, s *Store) {
			replaceSQLiteTable(t, s, "task_detail", func(ddl string) string {
				return strings.Replace(ddl, "detail_id BLOB NOT NULL PRIMARY KEY CHECK (typeof(detail_id)='blob' AND length(detail_id)=16)", "detail_id INTEGER NOT NULL PRIMARY KEY", 1)
			}, false)
			execSQLite(t, s, "INSERT INTO task_detail(detail_id,command) VALUES(1,'legacy numeric')")
		}},
		{"duplicate_execution_identity", func(t *testing.T, s *Store) {
			replaceSQLiteTable(t, s, "execution_log", func(ddl string) string {
				return strings.Replace(ddl, ",\n    UNIQUE (execution_grp_id,detail_id,attempt)", "", 1)
			}, true)
			execSQLite(t, s, "INSERT INTO execution_log(execution_grp_id,schedule_id,detail_id,attempt,start_time,result_code) SELECT execution_grp_id,schedule_id,detail_id,attempt,start_time,result_code FROM execution_log")
		}},
		{"lying_history", func(t *testing.T, s *Store) { execSQLite(t, s, "DROP TABLE execution_running") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := legacySQLiteFixture(t)
			tc.corrupt(t, s)
			before := sqliteFingerprint(t, s)
			if _, err := s.Migrate(context.Background()); err == nil {
				t.Fatal("unsafe legacy shape accepted")
			}
			if after := sqliteFingerprint(t, s); before != after {
				t.Fatal("failed upgrade changed schema/data/history")
			}
			var fk int
			if err := s.DB.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil || fk != 1 {
				t.Fatal("migration leaked disabled foreign keys", fk, err)
			}
		})
	}
}

// Replacement fixture helper; only disposable files are touched. No production
// migration bypasses a shape check or changes the writable_schema pragma.
func replaceSQLiteTable(t *testing.T, s *Store, name string, change func(string) string, copyRows bool) {
	t.Helper()
	var ddl string
	if err := s.DB.QueryRow("SELECT sql FROM sqlite_schema WHERE name=?", name).Scan(&ddl); err != nil {
		t.Fatal(err)
	}
	// SQLite preserves the checkout's line endings in sqlite_schema. Fixture
	// mutation patterns use LF even when Windows embedded assets contain CRLF.
	ddl = strings.ReplaceAll(ddl, "\r\n", "\n")
	execSQLite(t, s, "PRAGMA foreign_keys=OFF")
	execSQLite(t, s, "ALTER TABLE `"+name+"` RENAME TO old_fixture")
	execSQLite(t, s, change(ddl))
	if copyRows {
		execSQLite(t, s, "INSERT INTO `"+name+"` SELECT * FROM old_fixture")
	}
	execSQLite(t, s, "DROP TABLE old_fixture")
	execSQLite(t, s, "PRAGMA foreign_keys=ON")
}

func TestSQLiteFixtureMutationsHandleStoredLineEndings(t *testing.T) {
	reference, err := sqliteReference()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, newline string }{{"LF", "\n"}, {"CRLF", "\r\n"}} {
		t.Run(tc.name, func(t *testing.T) {
			s := sqliteTestStore(t)
			ddl := strings.ReplaceAll(reference["execution_log"].ddl, "\r\n", "\n")
			execSQLite(t, s, strings.ReplaceAll(ddl, "\n", tc.newline))
			var stored string
			if err := s.DB.QueryRow("SELECT sql FROM sqlite_schema WHERE name='execution_log'").Scan(&stored); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(stored, "\r\n") != (tc.newline == "\r\n") {
				t.Fatal("fixture did not retain the requested line endings")
			}
			replaceSQLiteTable(t, s, "execution_log", func(ddl string) string {
				const constraint = ",\n    UNIQUE (execution_grp_id,detail_id,attempt)"
				if !strings.Contains(ddl, constraint) {
					t.Fatal("fixture constraint pattern missing after normalization")
				}
				return strings.Replace(ddl, constraint, "", 1)
			}, true)
			for range 2 {
				execSQLite(t, s, "INSERT INTO execution_log(execution_grp_id,schedule_id,detail_id,attempt,start_time,result_code) VALUES(?,1,?,1,'2026-11-19 00:00:00',0)", []byte("fedcba9876543210"), []byte("0123456789abcdef"))
			}
			var count int
			if err := s.DB.QueryRow("SELECT COUNT(*) FROM execution_log").Scan(&count); err != nil || count != 2 {
				t.Fatal("fixture did not remove the unique constraint", count, err)
			}
		})
	}
}

func TestSQLiteMigrationRollbackAndCrossStoreSerialization(t *testing.T) {
	t.Run("failure_rolls_back_tracking_and_ddl", func(t *testing.T) {
		s := sqliteTestStore(t)
		before := sqliteFingerprint(t, s)
		m := Migration{Name: "test.sql", SHA256: strings.Repeat("a", 64), Statements: []string{"CREATE TABLE partial(value TEXT)", "INSERT INTO missing_table VALUES(1)"}}
		if _, err := s.applySQLite(context.Background(), []Migration{m}); err == nil {
			t.Fatal("bad migration accepted")
		}
		if after := sqliteFingerprint(t, s); before != after {
			t.Fatal("partial migration was committed")
		}
	})
	t.Run("two_independent_stores", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "shared.sqlite3")
		cfg := config.Database{Type: "sqlite", Path: path}
		a, err := Open(context.Background(), cfg, false)
		if err != nil {
			t.Fatal(err)
		}
		defer a.Close()
		b, err := Open(context.Background(), cfg, false)
		if err != nil {
			t.Fatal(err)
		}
		defer b.Close()
		start := make(chan struct{})
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for _, s := range []*Store{a, b} {
			wg.Add(1)
			go func(s *Store) { defer wg.Done(); <-start; _, err := s.Migrate(context.Background()); errs <- err }(s)
		}
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		var n int
		if err = a.DB.QueryRow("SELECT COUNT(*) FROM migrations").Scan(&n); err != nil || n != 1 {
			t.Fatal(n, err)
		}
	})
	t.Run("cancelled_writer_restores_connection", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "cancel.sqlite3")
		cfg := config.Database{Type: "sqlite", Path: path}
		a, err := Open(context.Background(), cfg, false)
		if err != nil {
			t.Fatal(err)
		}
		defer a.Close()
		b, err := Open(context.Background(), cfg, false)
		if err != nil {
			t.Fatal(err)
		}
		defer b.Close()
		tx, err := a.DB.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		started := time.Now()
		if _, err = b.Migrate(ctx); err == nil {
			t.Fatal("cancelled migration accepted")
		}
		if time.Since(started) > 2*time.Second {
			t.Fatal("native busy wait ignored cancellation budget")
		}
		tx.Rollback()
		var fk int
		if err = b.DB.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil || fk != 1 {
			t.Fatal(fk, err)
		}
	})
}

func sqliteFingerprint(t *testing.T, s *Store) [32]byte {
	t.Helper()
	ctx := context.Background()
	var data strings.Builder
	rows, err := s.DB.Query("SELECT type,name,COALESCE(sql,'') FROM sqlite_schema ORDER BY type,name")
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for rows.Next() {
		var kind, name, ddl string
		if err = rows.Scan(&kind, &name, &ddl); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&data, "%q:%q:%q\n", kind, name, ddl)
		if kind == "table" {
			names = append(names, name)
		}
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	for _, name := range names {
		columns, err := sqliteColumns(ctx, s.DB, name)
		if err != nil {
			t.Fatal(err)
		}
		expressions := []string{}
		for _, c := range columns {
			expressions = append(expressions, "quote(`"+c.name+"`)")
		}
		rows, err = s.DB.Query("SELECT " + strings.Join(expressions, ",") + " FROM `" + name + "` ORDER BY rowid")
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			values := make([]string, len(columns))
			dest := make([]any, len(columns))
			for i := range values {
				dest[i] = &values[i]
			}
			if err = rows.Scan(dest...); err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(&data, "%s:%q\n", name, values)
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
	}
	return sha256.Sum256([]byte(data.String()))
}

func TestSQLiteCriticalSchemaRejectsConstraintDrift(t *testing.T) {
	for _, tc := range []struct{ name, table, old, new string }{
		{"user_primary_key", "users", "user_id TEXT NOT NULL PRIMARY KEY", "user_id TEXT NOT NULL"},
		{"user_group_fk", "users", " REFERENCES groups(group_id)", ""},
		{"user_group_cascade", "users", "REFERENCES groups(group_id)", "REFERENCES groups(group_id) ON DELETE CASCADE"},
		{"user_role_members", "users", "('admin','user')", "('admin','user','extra')"},
		{"result_idempotency", "execution_log", ",\n    UNIQUE (execution_grp_id,detail_id,attempt)", ""},
		{"task_blob_identity", "task_detail", " CHECK (typeof(detail_id)='blob' AND length(detail_id)=16)", ""},
		{"manual_status_members", "manual_execution", "'wait',", ""},
		{"nullable_flag", "schedule_group", "is_error_stop INTEGER DEFAULT 1", "is_error_stop INTEGER NOT NULL DEFAULT 1"},
		{"autoincrement_policy", "devices", " PRIMARY KEY AUTOINCREMENT", " PRIMARY KEY"},
		{"integer_primary_key_desc", "devices", " PRIMARY KEY AUTOINCREMENT", " PRIMARY KEY DESC"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := sqliteTestStore(t)
			if _, err := s.Migrate(context.Background()); err != nil {
				t.Fatal(err)
			}
			replaceSQLiteTable(t, s, tc.table, func(ddl string) string {
				if !strings.Contains(ddl, tc.old) {
					t.Fatal("fixture pattern missing")
				}
				return strings.Replace(ddl, tc.old, tc.new, 1)
			}, true)
			before := sqliteFingerprint(t, s)
			report, err := s.CriticalSchema(context.Background())
			if err != nil || len(report.Issues) == 0 {
				t.Fatal(report, err)
			}
			if _, err = s.RepairCriticalSchema(context.Background()); err == nil {
				t.Fatal("unsafe drift silently repaired")
			}
			if after := sqliteFingerprint(t, s); before != after {
				t.Fatal("guard changed database")
			}
		})
	}
}

func TestSQLiteReservedGroupRepairAndForeignKeyRejection(t *testing.T) {
	s := sqliteTestStore(t)
	if _, err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	execSQLite(t, s, "PRAGMA foreign_keys=OFF")
	execSQLite(t, s, "DELETE FROM groups WHERE group_id=0")
	execSQLite(t, s, "PRAGMA foreign_keys=ON")
	r, err := s.RepairCriticalSchema(context.Background())
	if err != nil || !reflect.DeepEqual(r.Repaired, []string{"groups.0"}) {
		t.Fatal(r, err)
	}
	execSQLite(t, s, "PRAGMA foreign_keys=OFF")
	execSQLite(t, s, "UPDATE users SET group_id=98765")
	execSQLite(t, s, "PRAGMA foreign_keys=ON")
	if _, err = s.RepairCriticalSchema(context.Background()); err == nil {
		t.Fatal("ordinary orphan group silently remapped")
	}
}

func TestSQLiteNativeJournalAndGeneratedColumnsFailClosed(t *testing.T) {
	for _, mutation := range []string{
		"DELETE FROM tw_migration_attempt WHERE filename='go_sqlite_001.sql'",
		"UPDATE tw_migration_attempt SET statement_index=0 WHERE filename='go_sqlite_001.sql'",
	} {
		t.Run(mutation[:6], func(t *testing.T) {
			s := sqliteTestStore(t)
			if _, err := s.Migrate(context.Background()); err != nil {
				t.Fatal(err)
			}
			execSQLite(t, s, mutation)
			before := sqliteFingerprint(t, s)
			report, err := s.Status(context.Background())
			if err != nil || !report.Blocked {
				t.Fatal(report, err)
			}
			if _, err = s.Migrate(context.Background()); err == nil {
				t.Fatal("corrupt native history accepted")
			}
			if before != sqliteFingerprint(t, s) {
				t.Fatal("corrupt history was rewritten")
			}
		})
	}
	s := sqliteTestStore(t)
	if _, err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	execSQLite(t, s, "ALTER TABLE users ADD COLUMN derived TEXT AS (name)")
	guard, err := s.CriticalSchema(context.Background())
	if err != nil || len(guard.Issues) == 0 {
		t.Fatal("generated column concealed", guard, err)
	}
}

func TestSQLiteUntrackedDataIsNeverAdopted(t *testing.T) {
	s := sqliteTestStore(t)
	execSQLite(t, s, "CREATE TABLE keep(value TEXT)")
	execSQLite(t, s, "INSERT INTO keep VALUES('preserve')")
	before := sqliteFingerprint(t, s)
	if _, err := s.Migrate(context.Background()); err == nil {
		t.Fatal("untracked database accepted")
	}
	if before != sqliteFingerprint(t, s) {
		t.Fatal("untracked database changed")
	}
}

func TestSQLiteLegacyTimestampNormalizationSpansBatches(t *testing.T) {
	s := legacySQLiteFixture(t)
	execSQLite(t, s, `WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<600)
INSERT INTO execution_log(execution_grp_id,schedule_id,detail_id,attempt,start_time,result_code,result_message)
SELECT X'66656463626139383736353433323130',88,X'30313233343536373839616263646566',100+x,'2026-11-19T12:00:00.123456789+09:00',0,'batch' FROM n`)
	if _, err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.DB.QueryRow("SELECT COUNT(*) FROM execution_log WHERE result_message='batch' AND CAST(start_time AS TEXT)='2026-11-19 03:00:00.123456789'").Scan(&n); err != nil || n != 600 {
		t.Fatal(n, err)
	}
}

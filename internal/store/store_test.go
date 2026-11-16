package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/horrible-gh/TimeWeaver/internal/config"
)

func TestReadOnlySQLiteNeverCreatesAndCannotWrite(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "database with space#%.sqlite3")
	cfg := config.Database{Type: "sqlite3", Path: path}
	if db, err := Open(ctx, cfg, true); err == nil {
		db.Close()
		t.Fatal("missing file opened")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("read-only inspection created a file")
	}
	rw, err := Open(ctx, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = rw.DB.Exec("CREATE TABLE preserved (value TEXT); INSERT INTO preserved VALUES ('keep')"); err != nil {
		t.Fatal(err)
	}
	rw.Close()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ro, err := Open(ctx, cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	report, err := ro.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report.HistoryExists || report.Pending != 7 || report.WriteSupported || !report.ReadOnly {
		t.Fatal(report)
	}
	if _, err = ro.DB.Exec("DELETE FROM preserved"); err == nil {
		t.Fatal("read-only connection allowed a write")
	}
	if _, err = ro.Migrate(ctx); err == nil {
		t.Fatal("read-only migration accepted")
	}
	ro.Close()
	after, _ := os.ReadFile(path)
	if sha256.Sum256(before) != sha256.Sum256(after) {
		t.Fatal("status changed database bytes")
	}
}
func TestSQLiteMigrationBlockedWithoutSchemaChanges(t *testing.T) {
	cfg := config.Database{Type: "sqlite3", Path: filepath.Join(t.TempDir(), "test.sqlite3")}
	s, err := Open(context.Background(), cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.Migrate(context.Background()); !errors.Is(err, ErrSQLiteIncomplete) {
		t.Fatal(err)
	}
	var count int
	if err = s.DB.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table'").Scan(&count); err != nil || count != 0 {
		t.Fatal("incomplete migration wrote schema", count, err)
	}
}
func TestUnknownAndUnfinishedHistory(t *testing.T) {
	s, err := Open(context.Background(), config.Database{Type: "sqlite3", Path: filepath.Join(t.TempDir(), "test.sqlite3")}, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, query := range []string{
		"CREATE TABLE migrations (filename TEXT PRIMARY KEY, applied_at TEXT)",
		"CREATE TABLE tw_migration_attempt (filename TEXT PRIMARY KEY, checksum TEXT, state TEXT, statement_index INTEGER)",
		"INSERT INTO migrations(filename) VALUES ('unknown.sql')",
	} {
		if _, err = s.DB.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	files, _ := Migrations("sqlite")
	s.DB.Exec("INSERT INTO tw_migration_attempt VALUES (?,?,?,?)", files[0].Name, files[0].SHA256, "running", 0)
	r, err := s.Status(context.Background())
	if err != nil || !r.Blocked || r.Migrations[0].State != "blocked" || len(r.Unknown) != 1 {
		t.Fatal(r, err)
	}
	s.DB.Exec("UPDATE tw_migration_attempt SET checksum='wrong',state='applied'")
	s.DB.Exec("INSERT INTO migrations(filename) VALUES (?)", files[0].Name)
	r, err = s.Status(context.Background())
	if err != nil || r.Migrations[0].State != "checksum_mismatch" || !r.Blocked {
		t.Fatal(r, err)
	}
}
func TestDatabaseErrorsAreRedacted(t *testing.T) {
	err := dbError("connect", &mysql.MySQLError{Number: 1045, Message: "secret-db-password and user"})
	if !strings.Contains(err.Error(), "1045") || strings.Contains(err.Error(), "password") {
		t.Fatal(err)
	}
	if got := dbError("connect", errors.New("sensitive DSN")); strings.Contains(got.Error(), "sensitive") {
		t.Fatal(got)
	}
}

// Tests below create ONLY uniquely named disposable CI databases, never DB_DATABASE.
func mysqlTestStore(t *testing.T) *Store {
	t.Helper()
	if os.Getenv("TIMEWEAVER_TEST_MYSQL_ALLOW_CREATE") != "1" {
		t.Skip("live MySQL test requires explicit disposable-database permission")
	}
	host := os.Getenv("TIMEWEAVER_TEST_MYSQL_HOST")
	if host == "" {
		t.Fatal("CI MySQL host missing")
	}
	c := mysql.NewConfig()
	c.User = "root"
	c.Passwd = os.Getenv("TIMEWEAVER_TEST_MYSQL_PASSWORD")
	c.Net = "tcp"
	c.Addr = host + ":3306"
	c.ParseTime = true
	connector, err := mysql.NewConnector(c)
	if err != nil {
		t.Fatal(err)
	}
	admin := sql.OpenDB(connector)
	name := "tw_go_test_" + hex.EncodeToString([]byte(t.Name()))
	// Keep identifiers under MySQL's 64-character limit and unique per invocation.
	digest := sha256.Sum256([]byte(t.TempDir() + name))
	name = "tw_go_test_" + hex.EncodeToString(digest[:12])
	if _, err = admin.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4"); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	s, err := Open(context.Background(), config.Database{Type: "mysql", Host: host, Port: 3306, User: c.User, Password: c.Passwd, Name: name}, false)
	if err != nil {
		admin.Exec("DROP DATABASE `" + name + "`")
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close(); admin.Exec("DROP DATABASE `" + name + "`"); admin.Close() })
	return s
}
func TestMySQLFullMigrationAndNoReplay(t *testing.T) {
	s := mysqlTestStore(t)
	ctx := context.Background()
	before, err := s.Status(ctx)
	if err != nil || before.Pending != 31 || before.HistoryExists {
		t.Fatal(before, err)
	}
	r, err := s.Migrate(ctx)
	if err != nil || r.Pending != 0 || r.Blocked {
		t.Fatal(r, err)
	}
	if _, err = s.DB.Exec("UPDATE users SET name='preserved-custom-name' WHERE user_id='timeweaver'"); err != nil {
		t.Fatal(err)
	}
	r, err = s.Migrate(ctx)
	if err != nil || r.Pending != 0 {
		t.Fatal(r, err)
	}
	var name string
	if err = s.DB.QueryRow("SELECT name FROM users WHERE user_id='timeweaver'").Scan(&name); err != nil || name != "preserved-custom-name" {
		t.Fatal(name, err)
	}
	var count int
	if err = s.DB.QueryRow("SELECT COUNT(*) FROM tw_migration_attempt WHERE state='applied'").Scan(&count); err != nil || count != 31 {
		t.Fatal(count, err)
	}
}
func TestMySQLPartialDDLBlocksReplay(t *testing.T) {
	s := mysqlTestStore(t)
	ctx := context.Background()
	m := Migration{Name: "001.sql", SHA256: strings.Repeat("a", 64), Statements: []string{"CREATE TABLE survived (n INT)", "INSERT INTO missing_table VALUES(1)"}}
	if _, err := s.apply(ctx, []Migration{m}); err == nil {
		t.Fatal("invalid migration succeeded")
	}
	var state string
	var step int
	if err := s.DB.QueryRow("SELECT state,statement_index FROM tw_migration_attempt WHERE filename='001.sql'").Scan(&state, &step); err != nil || state != "failed" || step != 2 {
		t.Fatal(state, step, err)
	}
	if yes, err := tableExists(ctx, s.DB, "mysql", "survived"); err != nil || !yes {
		t.Fatal("DDL partial-commit fixture missing", yes, err)
	}
	if _, err := s.apply(ctx, []Migration{m}); err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatal("partial migration replayed", err)
	}
}
func TestMySQLPinnedConnection(t *testing.T) {
	s := mysqlTestStore(t)
	m := Migration{Name: "001.sql", SHA256: strings.Repeat("b", 64), Statements: []string{
		"SET @tw_sql='CREATE TABLE pinned (n INT)'", "PREPARE tw_stmt FROM @tw_sql", "EXECUTE tw_stmt", "DEALLOCATE PREPARE tw_stmt",
		"SET @tw_value=42", "INSERT INTO pinned VALUES (@tw_value)",
	}}
	if _, err := s.apply(context.Background(), []Migration{m}); err != nil {
		t.Fatal(err)
	}
	var value int
	if err := s.DB.QueryRow("SELECT n FROM pinned").Scan(&value); err != nil || value != 42 {
		t.Fatal(value, err)
	}
}

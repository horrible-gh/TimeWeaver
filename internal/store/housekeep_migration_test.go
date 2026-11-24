package store

import (
	"context"
	"testing"
)

func verifyHousekeepUpgrade(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	report, err := s.MigrateForStartup(ctx)
	if err != nil || report.Pending != 0 || report.Blocked {
		t.Fatal(report, err)
	}
	var flag int
	var command string
	if err = s.DB.QueryRow("SELECT house_keep_delete_empty_dirs,command FROM task_detail WHERE detail_id=?", []byte("0123456789abcdef")).Scan(&flag, &command); err != nil || flag != 0 || command != "preserve existing task" {
		t.Fatal("migration enabled deletion or changed a task", flag, command, err)
	}
	if _, err = s.DB.Exec("UPDATE task_detail SET house_keep_delete_empty_dirs=1 WHERE detail_id=?", []byte("0123456789abcdef")); err != nil {
		t.Fatal(err)
	}
	if _, err = s.MigrateForStartup(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.DB.QueryRow("SELECT house_keep_delete_empty_dirs FROM task_detail WHERE detail_id=?", []byte("0123456789abcdef")).Scan(&flag); err != nil || flag != 1 {
		t.Fatal("repeat migration reset an opt-in flag", flag, err)
	}
}

func TestSQLiteHousekeepUpgradeDefaultsOff(t *testing.T) {
	s := sqliteTestStore(t)
	ctx := context.Background()
	catalog, err := Migrations("sqlite")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.applySQLite(ctx, catalog[:1]); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec("INSERT INTO task_detail(detail_id,command) VALUES(?,?)", []byte("0123456789abcdef"), "preserve existing task"); err != nil {
		t.Fatal(err)
	}
	verifyHousekeepUpgrade(t, s)
}

func TestMySQLHousekeepUpgradeDefaultsOff(t *testing.T) {
	s := mysqlTestStore(t)
	ctx := context.Background()
	if _, err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"ALTER TABLE task_detail DROP COLUMN house_keep_delete_empty_dirs", "DELETE FROM migrations WHERE filename='timeweaver_server_016.sql'", "DELETE FROM tw_migration_attempt WHERE filename='timeweaver_server_016.sql'"} {
		if _, err := s.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.DB.Exec("INSERT INTO task_detail(detail_id,command) VALUES(?,?)", []byte("0123456789abcdef"), "preserve existing task"); err != nil {
		t.Fatal(err)
	}
	verifyHousekeepUpgrade(t, s)
}

package store

import (
	"context"
	"testing"
)

func TestStartupMigrationPlanRefusesOfflineAndBlockedHistory(t *testing.T) {
	for _, dialect := range []string{"mysql", "sqlite"} {
		catalog, err := Migrations(dialect)
		if err != nil {
			t.Fatal(err)
		}
		applied := map[string]bool{}
		attempts := map[string]attempt{}
		approved := startupSafeMigrations[dialect]
		for _, m := range catalog {
			if !approved[m.Name] {
				applied[m.Name] = true
				attempts[m.Name] = attempt{m.SHA256, "applied", len(m.Statements)}
			}
		}
		report, err := classifyHistory(dialect, catalog, true, applied, attempts)
		if err != nil {
			t.Fatal(err)
		}
		if pending, err := StartupMigrations(report); err != nil || len(pending) != 1 || !approved[pending[0]] {
			t.Fatal("routine migration refused", pending, err)
		}
		delete(applied, catalog[0].Name)
		delete(attempts, catalog[0].Name)
		report, err = classifyHistory(dialect, catalog, true, applied, attempts)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = StartupMigrations(report); err == nil {
			t.Fatal("incomplete foundation accepted")
		}
		applied["unknown-site-migration.sql"] = true
		report, err = classifyHistory(dialect, catalog, true, applied, attempts)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = StartupMigrations(report); err == nil {
			t.Fatal("unknown history accepted")
		}
	}
}

func TestStartupMigrationNeverBootstrapsSQLite(t *testing.T) {
	s := sqliteTestStore(t)
	before := sqliteFingerprint(t, s)
	if _, err := s.MigrateForStartup(context.Background()); err == nil {
		t.Fatal("empty database was automatically initialized")
	}
	if before != sqliteFingerprint(t, s) {
		t.Fatal("refused startup modified the database")
	}
}

func TestStartupMigrationNeverBootstrapsMySQL(t *testing.T) {
	s := mysqlTestStore(t)
	if _, err := s.MigrateForStartup(context.Background()); err == nil {
		t.Fatal("empty database was automatically initialized")
	}
	var tables int
	if err := s.DB.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE()").Scan(&tables); err != nil || tables != 0 {
		t.Fatal("refused startup created tables", tables, err)
	}
}

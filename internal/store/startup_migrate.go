package store

import (
	"context"
	"errors"
	"fmt"
)

// Explicitly reviewed, additive, startup-safe migrations. Future releases
// register compatible incremental migrations here; foundation conversions,
// destructive changes and operator-dependent migrations require offline setup.
var startupSafeMigrations = map[string]map[string]bool{
	"mysql":  {"timeweaver_server_016.sql": true},
	"sqlite": {"go_sqlite_002.sql": true},
}

// StartupMigrations returns the pending automatic upgrade plan without writing.
// Every pending migration must be approved; never run a partial subset over
// an incomplete or unknown installation.
func StartupMigrations(report MigrationReport) ([]string, error) {
	if report.Blocked || len(report.Unknown) != 0 {
		return nil, errors.New("automatic migration blocked by unknown, changed or unfinished migration history")
	}
	if !report.HistoryExists {
		return nil, errors.New("initial database setup is required; automatic migration does not create a new installation")
	}
	approved, ok := startupSafeMigrations[report.Dialect]
	if !ok {
		return nil, errors.New("automatic migration is unavailable for this database dialect")
	}
	var pending []string
	for _, item := range report.Migrations {
		switch item.State {
		case "applied":
		case "pending":
			if !approved[item.Name] {
				return nil, fmt.Errorf("migration %s requires explicit offline setup", item.Name)
			}
			pending = append(pending, item.Name)
		default:
			return nil, errors.New("automatic migration requires a clean migration history")
		}
	}
	if len(pending) != report.Pending {
		return nil, errors.New("automatic migration plan does not match pending history")
	}
	return pending, nil
}

// MigrateForStartup rechecks the complete plan inside the same database lock
// or SQLite transaction used by the normal migrator. A concurrent update
// cannot turn the read-only preflight into permission to bootstrap or replay
// an unapproved migration.
func (s *Store) MigrateForStartup(ctx context.Context) (MigrationReport, error) {
	if s.readOnly {
		return MigrationReport{}, errors.New("automatic migration requires a writable existing database")
	}
	catalog, err := Migrations(s.Dialect)
	if err != nil {
		return MigrationReport{}, err
	}
	switch s.Dialect {
	case "sqlite":
		return s.applySQLiteMode(ctx, catalog, true)
	case "mysql":
		return s.applyMySQLMode(ctx, catalog, true)
	default:
		return MigrationReport{}, errors.New("unsupported automatic migration dialect")
	}
}

package server

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/horrible-gh/TimeWeaver/internal/config"
	"github.com/horrible-gh/TimeWeaver/internal/store"
)

func sqliteServerDatabase(t *testing.T) (*store.Store, *store.Store) {
	db, probe, _ := sqliteServerDatabaseWithConfig(t)
	return db, probe
}

func sqliteServerDatabaseWithConfig(t *testing.T) (*store.Store, *store.Store, config.Database) {
	t.Helper()
	c := config.Database{Type: "sqlite3", Path: filepath.Join(t.TempDir(), "surface.sqlite3")}
	db, err := store.Open(context.Background(), c, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err = db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	probe, err := store.Open(context.Background(), c, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { probe.Close() })
	return db, probe, c
}

// Only fixture SQL is translated. Application queries exercise the native
// SQLite catalog and explicit dialect helpers through the real HTTP/services.
var fixtureInterval = regexp.MustCompile(`UTC_TIMESTAMP\(\)\s*([+-])\s*INTERVAL\s+(\d+)\s+(HOUR|DAY|MINUTE|SECOND)`)
var fixtureRepeat = regexp.MustCompile(`REPEAT\('([a-z0-9]+)',\s*(\d+)\)`)
var fixtureRename = regexp.MustCompile(`^RENAME TABLE ([a-z_]+) TO ([a-z_]+)$`)

func fixtureSQL(dialect, q string) string {
	if dialect != "sqlite" {
		return q
	}
	q = fixtureInterval.ReplaceAllStringFunc(q, func(part string) string {
		m := fixtureInterval.FindStringSubmatch(part)
		return "datetime('now','" + m[1] + m[2] + " " + strings.ToLower(m[3]) + "')"
	})
	q = strings.ReplaceAll(q, "UTC_TIMESTAMP()", "CURRENT_TIMESTAMP")
	q = fixtureRepeat.ReplaceAllStringFunc(q, func(part string) string {
		m := fixtureRepeat.FindStringSubmatch(part)
		n, _ := strconv.Atoi(m[2])
		return "'" + strings.Repeat(m[1], n) + "'"
	})
	q = strings.ReplaceAll(q, "LPAD(HEX(?),32,'0')", "printf('%032x',?)")
	q = strings.ReplaceAll(q, "TIMESTAMPDIFF(HOUR,created_at,expires_at)", "CAST((unixepoch(expires_at)-unixepoch(created_at))/3600 AS INTEGER)")
	q = fixtureRename.ReplaceAllString(q, "ALTER TABLE $1 RENAME TO $2")
	return q
}

func fixtureArgs(db *store.Store, args []any) []any {
	bound := append([]any(nil), args...)
	for i, v := range bound {
		if stamp, ok := v.(time.Time); ok {
			bound[i] = store.TimeValue(db.Dialect, stamp)
		}
	}
	return bound
}

func fixtureExec(t *testing.T, db *store.Store, q string, args ...any) (sql.Result, error) {
	t.Helper()
	return db.DB.Exec(fixtureSQL(db.Dialect, q), fixtureArgs(db, args)...)
}

type fixtureScanner struct{ row *sql.Row }

func (r fixtureScanner) Scan(dest ...any) error {
	for i, v := range dest {
		switch p := v.(type) {
		case *time.Time:
			dest[i] = store.ScanTime(p)
		case *sql.NullTime:
			dest[i] = store.ScanNullTime(p)
		}
	}
	return r.row.Scan(dest...)
}
func fixtureRow(t *testing.T, db *store.Store, q string, args ...any) fixtureScanner {
	t.Helper()
	return fixtureScanner{db.DB.QueryRow(fixtureSQL(db.Dialect, q), fixtureArgs(db, args)...)}
}

// Reuse the established behavior assertions with real SQLite files. No MySQL
// service or opt-in environment is necessary for the SQLite regression gate.
func TestSQLiteDashboardFlows(t *testing.T) {
	t.Setenv("TIMEWEAVER_TEST_SQLITE", "1")
	for _, tc := range []struct {
		name string
		run  func(*testing.T)
	}{
		{"schedule-task-lifecycle", TestMySQLScheduleTaskScopeAndLifecycle},
		{"legacy-cross-group-task-save", TestMySQLLegacyCrossGroupTaskSave},
		{"schedule-task-atomicity-history", TestMySQLScheduleTaskAtomicityAndHistory},
		{"pause-offline-device", TestMySQLScheduleCanBePausedWithInactiveDevice},
		{"schedule-task-http", TestMySQLScheduleTaskHTTP},
		{"device-scopes", TestMySQLDeviceMutationScopesAndPreservation},
		{"enrollment-audit", TestMySQLEnrollmentLifecycleAndAuditRollback},
		{"enrollment-race", TestMySQLEnrollmentConcurrentConsumeOrRevoke},
		{"device-http", TestMySQLDeviceHTTPBoundaries},
		{"manual-scope-fanout", TestMySQLManualScopeFanoutAndForcedActor},
		{"manual-timing-cancellation", TestMySQLManualTimingStateAndPendingCancellation},
		{"manual-audit-rollback", TestMySQLManualAuditFailureRollsBackBatchAndReplacement},
		{"history-recorded-scope", TestMySQLManualHistoryRecordedScopesAndTombstones},
		{"history-bound", TestMySQLManualHistoryRejectsTruncatedResults},
		{"manual-http", TestMySQLManualHTTPContractAndBoundaries},
		{"manual-cached-claim", TestMySQLManualRescheduleRejectsCachedClaim},
		{"manual-after-claim", TestMySQLManualRescheduleAfterClaimHasNoReplacement},
		{"charts-http-scope-empty", TestMySQLChartsScopesHTTPAndEmptyResults},
		{"chart-liveness", TestMySQLChartDeviceLivenessAndLegacyScheduleRules},
		{"chart-recorded-parent", TestMySQLChartTaskWindowsRunningAndRecordedParent},
		{"chart-latest-order", TestMySQLLatestChartsLimitOrderingFailuresAndTombstones},
		{"chart-housekeep-warning", TestMySQLHousekeepWarningsAreNotErrors},
		{"chart-database-failure", TestMySQLChartsDatabaseFailureIsNotZero},
	} {
		t.Run(tc.name, tc.run)
	}
}

func sqliteFixtureEnabled() bool { return os.Getenv("TIMEWEAVER_TEST_SQLITE") == "1" }

package store

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// These digests are from the raw agent SQL at 55b4c97a27aafa90a50845dd4728d7c59f33180a,
// before 750d315 removed that directory. Keep an independent inventory so a
// wildcard or an accidentally edited predecessor cannot silently expand trust.
var historicalMySQLInventory = map[string]struct {
	hash       string
	statements int
}{
	"devices_001.sql":          {"29b715aa766febaf2994a0c5e9efb7d4c0182ced276d86282066447d610e3001", 1},
	"devices_002.sql":          {"4ca6eff85620bef2f3df9895ec1d361975c0b965e15d4a03355baee012e2d3a1", 1},
	"devices_003.sql":          {"ea4be5570896869240d0c39910029e2979d8c3b9e8d5f2f6fb26b6f956c3cd16", 2},
	"execution_log_001.sql":    {"f2aa776d3f459cdcc35284838375ba5b77972fd92d8340ae9f2cd3c6c3b270da", 2},
	"execution_log_002.sql":    {"8fd83812007ea9a692a91738ff341e9be153eead5c55a1f07d3fa36394c21dd2", 2},
	"execution_log_003.sql":    {"33e404afd9a0f0061402c6d0592079254e5d9811da4dd60bc4366a0e9eda4340", 2},
	"manual_execution_001.sql": {"a5798a47d9ff7159a7bca7050b71378df240414ad4c65e7ea33213ee4738d32f", 3},
	"manual_execution_002.sql": {"da9c17a3758026a04d2ed7bb334da957e403334a58d4bff55541264780218685", 2},
	"manual_execution_003.sql": {"cdeca613ee38f0e77ce0dda76225015c6e22f38e45790eaa42dcb419d45e7ec5", 1},
	"schedule_detail_001.sql":  {"1650f09ee481ee29dd852aea4106b5b4a95b161b355dec30e45fbbe7790a0f46", 2},
	"schedule_detail_002.sql":  {"27a43dda9da80fb0dd3b5752d3123a9100596cab6b7689c0f0afc9f5f64fc4fc", 1},
	"schedule_group_001.sql":   {"44a62801b41b7a9dc84c86ad080e056f2e050929707b419e897ff13e596daeee", 1},
	"schedule_group_002.sql":   {"8e2a059efb943aff666d3055644e0cca46b9d58e28bd89ad32b782c50e992329", 1},
}

func mysqlLegacyCatalogs(t *testing.T) ([]Migration, []Migration) {
	t.Helper()
	current, err := Migrations("mysql")
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := legacyMySQLMigrations()
	if err != nil {
		t.Fatal(err)
	}
	return current, legacy
}

func mysqlLegacyApplied(catalogs ...[]Migration) map[string]bool {
	applied := map[string]bool{}
	for _, catalog := range catalogs {
		for _, m := range catalog {
			applied[m.Name] = true
		}
	}
	return applied
}

func mysqlLegacyStatus(t *testing.T, r MigrationReport, name string) MigrationStatus {
	t.Helper()
	for _, m := range r.Migrations {
		if m.Name == name {
			return m
		}
	}
	t.Fatalf("migration %s missing from report", name)
	return MigrationStatus{}
}

func TestMySQLLegacyCatalogHasExactHistoricalBytes(t *testing.T) {
	current, legacy := mysqlLegacyCatalogs(t)
	if len(current) != 31 || len(legacy) != 13 {
		t.Fatalf("executable/historical inventory changed: %d/%d", len(current), len(legacy))
	}
	seen := map[string]bool{}
	for i, m := range legacy {
		want, ok := historicalMySQLInventory[m.Name]
		if !ok || seen[m.Name] || m.SHA256 != want.hash || len(m.Statements) != want.statements {
			t.Errorf("historical identity changed: name=%s hash=%s statements=%d", m.Name, m.SHA256, len(m.Statements))
		}
		if i > 0 && legacy[i-1].Name >= m.Name {
			t.Error("historical inventory is not in stable filename order")
		}
		seen[m.Name] = true
	}
	activeTaskDetail := false
	for _, m := range current {
		if seen[m.Name] {
			t.Errorf("historical migration became executable: %s", m.Name)
		}
		activeTaskDetail = activeTaskDetail || m.Name == "task_detail_001.sql"
	}
	if !activeTaskDetail || seen["task_detail_001.sql"] {
		t.Fatal("task_detail_001.sql must remain in the executable catalog only")
	}
}

func TestMySQLLegacyHistoryRecognizesReportedThirteenRows(t *testing.T) {
	current, legacy := mysqlLegacyCatalogs(t)
	r, err := classifyHistory("mysql", current, true, mysqlLegacyApplied(current, legacy), nil)
	if err != nil || r.Pending != 0 || r.Blocked || len(r.Unknown) != 0 || !r.HistoryExists || !r.WriteSupported || !r.ReadOnly {
		t.Fatal("complete Python history was not recognized", r, err)
	}
	if len(r.Migrations) != len(current)+13 {
		t.Fatal("reported predecessors disappeared or were duplicated", len(r.Migrations))
	}
	for i, m := range current {
		if r.Migrations[i].Name != m.Name {
			t.Fatal("historical statuses displaced executable migration indexes")
		}
	}
	for _, m := range append(current, legacy...) {
		item := mysqlLegacyStatus(t, r, m.Name)
		if item.State != "applied" || item.SHA256 != m.SHA256 || item.Statement != 0 {
			t.Errorf("Python history must be retained without inventing a Go attempt: %+v", item)
		}
	}
}

func TestMySQLLegacyHistoryDoesNotInventAppliedOrPendingRows(t *testing.T) {
	current, legacy := mysqlLegacyCatalogs(t)
	t.Run("fresh_database", func(t *testing.T) {
		r, err := classifyHistory("mysql", current, false, nil, nil)
		if err != nil || r.HistoryExists || r.Blocked || r.Pending != len(current) || len(r.Migrations) != len(current) || len(r.Unknown) != 0 {
			t.Fatal("fresh database acquired historical migrations", r, err)
		}
	})
	t.Run("only_present_predecessor_is_reported", func(t *testing.T) {
		applied := mysqlLegacyApplied(current)
		applied[legacy[0].Name] = true
		r, err := classifyHistory("mysql", current, true, applied, nil)
		if err != nil || r.Pending != 0 || r.Blocked || len(r.Migrations) != len(current)+1 || len(r.Unknown) != 0 {
			t.Fatal("absent historical rows were invented", r, err)
		}
		if got := r.Migrations[len(current)]; got.Name != legacy[0].Name || got.State != "applied" {
			t.Fatal(got)
		}
	})
	for _, missing := range []string{"timeweaver_server_015.sql", "core_bootstrap_001_devices.sql", "task_detail_001.sql"} {
		t.Run("ordinary_pending_"+missing, func(t *testing.T) {
			applied := mysqlLegacyApplied(current, legacy)
			delete(applied, missing)
			r, err := classifyHistory("mysql", current, true, applied, nil)
			if err != nil || r.Pending != 1 || r.Blocked || len(r.Unknown) != 0 || len(r.Migrations) != len(current)+len(legacy) {
				t.Fatal("historical rows concealed a pending executable migration", r, err)
			}
			if item := mysqlLegacyStatus(t, r, missing); item.State != "pending" {
				t.Fatal("missing current migration was adopted as applied", item)
			}
		})
	}
}

func TestMySQLLegacyHistoryRequiresCompletedServerConversion(t *testing.T) {
	current, legacy := mysqlLegacyCatalogs(t)
	for _, tc := range []struct {
		name    string
		applied map[string]bool
		pending int
	}{
		{"agent_only", mysqlLegacyApplied(legacy), len(current)},
		{"missing_server_conversion", mysqlLegacyApplied(current, legacy), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			delete(tc.applied, "timeweaver_server_001.sql")
			r, err := classifyHistory("mysql", current, true, tc.applied, nil)
			if err != nil || !r.Blocked || r.Pending != tc.pending || len(r.Unknown) != 0 {
				t.Fatal("unconverted agent schema was implicitly authorized", r, err)
			}
			if item := mysqlLegacyStatus(t, r, "timeweaver_server_001.sql"); item.State != "legacy_schema_requires_review" {
				t.Fatal("destructive server conversion was not identified", item)
			}
			if tc.applied["timeweaver_server_001.sql"] {
				t.Fatal("classification invented server conversion history")
			}
		})
	}
}

func TestMySQLLegacyAttemptRequiresCompleteMatchingHistory(t *testing.T) {
	current, legacy := mysqlLegacyCatalogs(t)
	m := legacy[2] // devices_003.sql has two statements, so index 1 is incomplete.
	for _, tc := range []struct {
		name    string
		applied bool
		attempt attempt
		state   string
	}{
		{"complete", true, attempt{m.SHA256, "applied", len(m.Statements)}, "applied"},
		{"running", true, attempt{m.SHA256, "running", 1}, "blocked"},
		{"failed", true, attempt{m.SHA256, "failed", 1}, "blocked"},
		{"unknown_state", true, attempt{m.SHA256, "complete", len(m.Statements)}, "blocked"},
		{"changed_checksum", true, attempt{strings.Repeat("0", 64), "applied", len(m.Statements)}, "checksum_mismatch"},
		{"empty_checksum", true, attempt{"", "applied", len(m.Statements)}, "checksum_mismatch"},
		{"incomplete", true, attempt{m.SHA256, "applied", len(m.Statements) - 1}, "blocked"},
		{"not_started", true, attempt{m.SHA256, "applied", 0}, "blocked"},
		{"past_end", true, attempt{m.SHA256, "applied", len(m.Statements) + 1}, "blocked"},
		{"negative_index", true, attempt{m.SHA256, "applied", -1}, "blocked"},
		{"orphan_applied_attempt", false, attempt{m.SHA256, "applied", len(m.Statements)}, "blocked"},
		{"orphan_running_attempt", false, attempt{m.SHA256, "running", 0}, "blocked"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			applied := mysqlLegacyApplied(current)
			if tc.applied {
				applied[m.Name] = true
			}
			r, err := classifyHistory("mysql", current, true, applied, map[string]attempt{m.Name: tc.attempt})
			if err != nil || r.Blocked != (tc.state != "applied") || r.Pending != 0 || len(r.Unknown) != 0 || len(r.Migrations) != len(current)+1 {
				t.Fatal(r, err)
			}
			if item := mysqlLegacyStatus(t, r, m.Name); item.State != tc.state || item.Statement != tc.attempt.statement {
				t.Fatal("unsafe historical journal was accepted or concealed", item)
			}
		})
	}
}

func TestMySQLLegacyHistoryStillRejectsUnknownAndForeignNames(t *testing.T) {
	current, legacy := mysqlLegacyCatalogs(t)
	for _, name := range []string{
		"devices_999.sql", "devices_001.sql.bak", "devices_001.sql ", "DEVICES_001.sql",
		"schedule_detail_000.sql", "go_sqlite_001.sql", "users_003_group_integrity.sql",
	} {
		for _, source := range []string{"history", "attempt"} {
			t.Run(source+"/"+name, func(t *testing.T) {
				applied := mysqlLegacyApplied(current, legacy)
				attempts := map[string]attempt{}
				if source == "history" {
					applied[name] = true
				} else {
					attempts[name] = attempt{legacy[0].SHA256, "applied", 1}
				}
				r, err := classifyHistory("mysql", current, true, applied, attempts)
				if err != nil || !r.Blocked || !reflect.DeepEqual(r.Unknown, []string{name}) {
					t.Fatal("unknown migration was accepted by a prefix or dialect fallback", r, err)
				}
			})
		}
	}
	t.Run("mysql_predecessors_remain_unknown_to_sqlite", func(t *testing.T) {
		sqlite, err := Migrations("sqlite")
		if err != nil {
			t.Fatal(err)
		}
		applied := mysqlLegacyApplied(sqlite, legacy)
		attempts := map[string]attempt{}
		for _, m := range sqlite {
			attempts[m.Name] = attempt{m.SHA256, "applied", len(m.Statements)}
		}
		want := []string{}
		for _, m := range legacy {
			want = append(want, m.Name)
		}
		sort.Strings(want)
		r, err := classifyHistory("sqlite", sqlite, true, applied, attempts)
		if err != nil || !r.Blocked || r.Pending != 0 || !reflect.DeepEqual(r.Unknown, want) || len(r.Migrations) != len(sqlite) {
			t.Fatal("MySQL historical allowlist leaked into SQLite", r, err)
		}
	})
}

// Real-engine fixtures below are deliberately gated by mysqlTestStore. They run
// against disposable MySQL/MariaDB databases in CI and never touch DB_DATABASE.
func mysqlLegacyHistoryFixture(t *testing.T) *Store {
	t.Helper()
	s := mysqlTestStore(t)
	if _, err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Python recorded filename/applied_at only. Reproduce that history without
	// implying that the Go executable has ever run the retired agent SQL.
	for _, query := range []string{
		"DROP TABLE tw_migration_attempt",
		"UPDATE migrations SET applied_at='2001-02-03 04:05:06'",
		"UPDATE users SET name='retained legacy user',password='retained legacy password' WHERE user_id='timeweaver'",
		"INSERT INTO devices(device_id,device_name,version) VALUES (991,'retained legacy device','legacy-version')",
		"INSERT INTO schedule_group(schedule_id,name,target_device) VALUES (991,'retained legacy schedule',991)",
		"INSERT INTO schedule_detail(detail_id,schedule_id,schedule_name,status) VALUES (UNHEX(REPEAT('19',16)),991,'retained legacy detail','manual')",
		"INSERT INTO task_detail(detail_id,command,archive_type) VALUES (UNHEX(REPEAT('19',16)),'retained legacy command','null')",
		"INSERT INTO manual_execution(manual_id,detail_id,status) VALUES (991,UNHEX(REPEAT('19',16)),'done')",
		"INSERT INTO execution_log(execution_grp_id,schedule_id,detail_id,start_time,result_code,result_message) VALUES (UNHEX(REPEAT('29',16)),991,UNHEX(REPEAT('19',16)),'2002-03-04 05:06:07',0,'retained legacy result')",
	} {
		if _, err := s.DB.Exec(query); err != nil {
			t.Fatal("prepare historical fixture", err)
		}
	}
	for name := range historicalMySQLInventory {
		if _, err := s.DB.Exec("INSERT INTO migrations(filename,applied_at) VALUES (?,'2000-01-02 03:04:05')", name); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func mysqlLegacyRows(t *testing.T, s *Store, query string) string {
	t.Helper()
	rows, err := s.DB.Query(query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	for rows.Next() {
		values := make([]sql.RawBytes, len(columns))
		dest := make([]any, len(values))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			t.Fatal(err)
		}
		for _, value := range values {
			if value == nil {
				out.WriteString("NULL;")
			} else {
				fmt.Fprintf(&out, "%q;", []byte(value))
			}
		}
		out.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func mysqlLegacyBusinessRows(t *testing.T, s *Store) map[string]string {
	t.Helper()
	out := map[string]string{}
	for table, order := range map[string]string{
		"users": "user_id", "devices": "device_id", "schedule_group": "schedule_id",
		"schedule_detail": "detail_id", "task_detail": "detail_id", "manual_execution": "manual_id",
		"execution_log": "execution_id",
	} {
		out[table] = mysqlLegacyRows(t, s, "SELECT * FROM "+table+" ORDER BY "+order)
	}
	return out
}

func TestMySQLLegacyLiveFreshDatabaseDoesNotRecordPredecessors(t *testing.T) {
	s := mysqlTestStore(t)
	ctx := context.Background()
	r, err := s.Migrate(ctx)
	if err != nil || r.Blocked || r.Pending != 0 || len(r.Migrations) != 31 {
		t.Fatal(r, err)
	}
	for _, table := range []string{"migrations", "tw_migration_attempt"} {
		rows, err := s.DB.Query("SELECT filename FROM " + table)
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			if _, historical := historicalMySQLInventory[name]; historical {
				t.Errorf("fresh %s invented historical row %s", table, name)
			}
			count++
		}
		err = rows.Err()
		rows.Close()
		if err != nil || count != 31 {
			t.Fatalf("fresh %s inventory: count=%d error=%v", table, count, err)
		}
	}
}

func TestMySQLLegacyLiveHistoryPreservedAcrossStatusAndMigrate(t *testing.T) {
	s := mysqlLegacyHistoryFixture(t)
	ctx := context.Background()
	const historyQuery = "SELECT filename,applied_at FROM migrations ORDER BY filename"
	history := mysqlLegacyRows(t, s, historyQuery)
	business := mysqlLegacyBusinessRows(t, s)
	for _, operation := range []struct {
		name string
		run  func(context.Context) (MigrationReport, error)
	}{{"status", s.Status}, {"migrate", s.Migrate}, {"repeat_migrate", s.Migrate}} {
		t.Run(operation.name, func(t *testing.T) {
			r, err := operation.run(ctx)
			if err != nil || r.Blocked || r.Pending != 0 || len(r.Unknown) != 0 || len(r.Migrations) != 44 {
				t.Fatal("complete legacy installation was blocked", r, err)
			}
			for name := range historicalMySQLInventory {
				if item := mysqlLegacyStatus(t, r, name); item.State != "applied" {
					t.Fatal("historical migration was queued for replay", item)
				}
			}
			if got := mysqlLegacyRows(t, s, historyQuery); got != history {
				t.Fatal("inspection/no-op migration changed historical rows or applied_at")
			}
			if got := mysqlLegacyBusinessRows(t, s); !reflect.DeepEqual(got, business) {
				t.Fatal("inspection/no-op migration changed business rows")
			}
			if exists, err := tableExists(ctx, s.DB, "mysql", attemptTable); err != nil || exists {
				t.Fatal("no-op invented a Go migration journal", exists, err)
			}
		})
	}

	// A later executable migration remains runnable. Remove its empty table as
	// well as its row so this proves that current DDL really executes.
	const pending = "timeweaver_server_014.sql"
	for _, query := range []string{"DROP TABLE execution_running", "DELETE FROM migrations WHERE filename='" + pending + "'"} {
		if _, err := s.DB.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	history = mysqlLegacyRows(t, s, historyQuery)
	r, err := s.Status(ctx)
	if err != nil || r.Blocked || r.Pending != 1 || mysqlLegacyStatus(t, r, pending).State != "pending" {
		t.Fatal("historical rows concealed a current pending migration", r, err)
	}
	r, err = s.Migrate(ctx)
	if err != nil || r.Blocked || r.Pending != 0 || len(r.Unknown) != 0 || len(r.Migrations) != 44 {
		t.Fatal("ordinary current migration did not complete", r, err)
	}
	if exists, err := tableExists(ctx, s.DB, "mysql", "execution_running"); err != nil || !exists {
		t.Fatal("current migration was marked applied without executing", exists, err)
	}
	if got := mysqlLegacyRows(t, s, "SELECT filename,applied_at FROM migrations WHERE filename<>'"+pending+"' ORDER BY filename"); got != history {
		t.Fatal("current migration changed pre-existing history or timestamps")
	}
	if got := mysqlLegacyBusinessRows(t, s); !reflect.DeepEqual(got, business) {
		t.Fatal("current migration replayed predecessor SQL or changed business rows")
	}
	var count int
	if err := s.DB.QueryRow("SELECT COUNT(*) FROM tw_migration_attempt").Scan(&count); err != nil || count != 1 {
		t.Fatal("current migration invented attempts for already-applied history", count, err)
	}
	var name, state string
	if err := s.DB.QueryRow("SELECT filename,state FROM tw_migration_attempt").Scan(&name, &state); err != nil || name != pending || state != "applied" {
		t.Fatal("journal contains a historical replay", name, state, err)
	}
}

func TestMySQLLegacyLiveUnsafeHistoryRefusesWithoutChanges(t *testing.T) {
	s := mysqlLegacyHistoryFixture(t)
	_, legacy := mysqlLegacyCatalogs(t)
	m := legacy[2]
	if _, err := s.DB.Exec("CREATE TABLE tw_migration_attempt (filename VARCHAR(255) PRIMARY KEY, checksum CHAR(64) NOT NULL, state VARCHAR(16) NOT NULL, statement_index INT NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, filename string
		journal        attempt
		removeHistory  bool
		historyOnly    bool
	}{
		{"running", m.Name, attempt{m.SHA256, "running", 1}, false, false},
		{"failed", m.Name, attempt{m.SHA256, "failed", 1}, false, false},
		{"changed_checksum", m.Name, attempt{strings.Repeat("0", 64), "applied", len(m.Statements)}, false, false},
		{"incomplete", m.Name, attempt{m.SHA256, "applied", len(m.Statements) - 1}, false, false},
		{"orphan_attempt", m.Name, attempt{m.SHA256, "applied", len(m.Statements)}, true, false},
		{"unknown_prefix_history", "devices_999.sql", attempt{}, false, true},
		{"unknown_prefix_attempt", "devices_999.sql", attempt{m.SHA256, "applied", len(m.Statements)}, false, false},
		{"foreign_dialect_attempt", "go_sqlite_001.sql", attempt{m.SHA256, "applied", len(m.Statements)}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.removeHistory {
				if _, err := s.DB.Exec("DELETE FROM migrations WHERE filename=?", tc.filename); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if _, err := s.DB.Exec("INSERT INTO migrations(filename,applied_at) VALUES (?,'2000-01-02 03:04:05')", tc.filename); err != nil {
						t.Error(err)
					}
				})
			}
			if tc.historyOnly {
				if _, err := s.DB.Exec("INSERT INTO migrations(filename,applied_at) VALUES (?,'2000-01-02 03:04:05')", tc.filename); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if _, err := s.DB.Exec("DELETE FROM migrations WHERE filename=?", tc.filename); err != nil {
						t.Error(err)
					}
				})
			} else {
				if _, err := s.DB.Exec("INSERT INTO tw_migration_attempt(filename,checksum,state,statement_index) VALUES (?,?,?,?)", tc.filename, tc.journal.hash, tc.journal.state, tc.journal.statement); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if _, err := s.DB.Exec("DELETE FROM tw_migration_attempt WHERE filename=?", tc.filename); err != nil {
						t.Error(err)
					}
				})
			}
			mysqlLegacyAssertRefusedWithoutChanges(t, s)
		})
	}
	t.Run("unconverted_agent_schema", func(t *testing.T) {
		if _, err := s.DB.Exec("DELETE FROM migrations WHERE filename='timeweaver_server_001.sql'"); err != nil {
			t.Fatal(err)
		}
		mysqlLegacyAssertRefusedWithoutChanges(t, s)
	})
}

func mysqlLegacyAssertRefusedWithoutChanges(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	const historyQuery = "SELECT filename,applied_at FROM migrations ORDER BY filename"
	const journalQuery = "SELECT * FROM tw_migration_attempt ORDER BY filename"
	history := mysqlLegacyRows(t, s, historyQuery)
	journal := mysqlLegacyRows(t, s, journalQuery)
	business := mysqlLegacyBusinessRows(t, s)
	if r, err := s.Status(ctx); err != nil || !r.Blocked {
		t.Fatal("unsafe history did not block status", r, err)
	}
	if r, err := s.Migrate(ctx); err == nil || !r.Blocked {
		t.Fatal("unsafe history did not refuse migration", r, err)
	}
	if got := mysqlLegacyRows(t, s, historyQuery); got != history {
		t.Fatal("refusal changed historical rows or applied_at")
	}
	if got := mysqlLegacyRows(t, s, journalQuery); got != journal {
		t.Fatal("refusal rewrote the evidence of an unsafe attempt")
	}
	if got := mysqlLegacyBusinessRows(t, s); !reflect.DeepEqual(got, business) {
		t.Fatal("refused migration changed business rows")
	}
}

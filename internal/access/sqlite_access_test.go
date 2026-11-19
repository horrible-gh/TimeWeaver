package access

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/horrible-gh/TimeWeaver/internal/config"
	"github.com/horrible-gh/TimeWeaver/internal/store"
)

func sqliteAccessStore(t *testing.T) *store.Store {
	t.Helper()
	db := openSQLiteAccessStore(t, filepath.Join(t.TempDir(), "human access #test.sqlite3"))
	ctx := context.Background()
	if _, err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RepairCriticalSchema(ctx); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		"INSERT INTO `groups`(group_id,group_name) VALUES(7,'Seven'),(8,'Eight')",
		"INSERT INTO users(user_id,name,password,role,group_id) VALUES('reader','Reader','unused','user',7),('global','Global','unused','user',0)",
	} {
		if _, err := db.DB.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.DB.Exec("UPDATE users SET password=? WHERE user_id IN ('reader','global')", testHash); err != nil {
		t.Fatal(err)
	}
	return db
}

func openSQLiteAccessStore(t *testing.T, filename string) *store.Store {
	t.Helper()
	db, err := store.Open(context.Background(), config.Database{Type: "sqlite3", Path: filename}, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func sqliteAccessPath(t *testing.T, db *store.Store) string {
	t.Helper()
	var filename string
	if err := db.DB.QueryRow("SELECT file FROM pragma_database_list WHERE name='main'").Scan(&filename); err != nil || filename == "" {
		t.Fatal("expected a real SQLite file", filename, err)
	}
	return filename
}

func sqliteInitialized(t *testing.T) (*store.Store, *Service) {
	t.Helper()
	db := sqliteAccessStore(t)
	if _, err := Initialize(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	s, err := New(context.Background(), db, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return db, s
}

func sqliteAdminFixture(t *testing.T) (*Service, Principal, string) {
	t.Helper()
	db, s := sqliteInitialized(t)
	if _, err := db.DB.Exec("UPDATE users SET password=? WHERE user_id='timeweaver'", testHash); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("UPDATE tw_user_auth SET must_change_password=FALSE WHERE user_id='timeweaver'"); err != nil {
		t.Fatal(err)
	}
	p, token, err := s.Login(context.Background(), "timeweaver", testPassword, "127.0.0.1", "")
	if err != nil {
		t.Fatal(err)
	}
	return s, p, token
}

func sqliteLinkedOSFixture(t *testing.T) (*Service, Principal, string, *linkedOSProvider) {
	t.Helper()
	local, admin, token := sqliteAdminFixture(t)
	provider := newLinkedOSProvider()
	s, err := NewWithOS(context.Background(), local.db, 30*time.Minute, provider)
	if err != nil {
		t.Fatal(err)
	}
	return s, admin, token, provider
}

func TestSQLiteAccessConcurrentInitialization(t *testing.T) {
	db := sqliteAccessStore(t)
	other := openSQLiteAccessStore(t, sqliteAccessPath(t, db))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start, results := make(chan struct{}), make(chan error, 2)
	for _, target := range []*store.Store{db, other} {
		go func(target *store.Store) {
			<-start
			status, err := Initialize(ctx, target)
			if err == nil && (!status.Ready || status.Version != schemaVersion) {
				err = errors.New("initializer returned an incomplete schema")
			}
			results <- err
		}(target)
	}
	close(start)
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	var users, grants int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM tw_user_auth").Scan(&users); err != nil || users != 3 {
		t.Fatal("concurrent initialization lost or duplicated users", users, err)
	}
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM tw_group_member").Scan(&grants); err != nil || grants != 2 {
		t.Fatal("concurrent initialization changed initial grants", grants, err)
	}
}

func TestSQLiteAccessSchemaRollbackAndFailClosed(t *testing.T) {
	t.Run("atomic-install", func(t *testing.T) {
		db := sqliteAccessStore(t)
		// Force a real DDL error after tables have been created in the transaction.
		if _, err := db.DB.Exec("CREATE INDEX tw_session_user ON users(name)"); err != nil {
			t.Fatal(err)
		}
		if _, err := Initialize(context.Background(), db); err == nil {
			t.Fatal("conflicting schema unexpectedly initialized")
		}
		for _, table := range append([]string{"tw_access_schema"}, accessTables...) {
			exists, err := sqliteAccessTable(context.Background(), db.DB, table)
			if err != nil || exists {
				t.Fatal("failed SQLite DDL left a partial access table", table, err)
			}
		}
		if _, err := db.DB.Exec("DROP INDEX tw_session_user"); err != nil {
			t.Fatal(err)
		}
		if status, err := Initialize(context.Background(), db); err != nil || !status.Ready {
			t.Fatal("rolled-back installation could not be retried", status, err)
		}
	})
	t.Run("untracked-table", func(t *testing.T) {
		db := sqliteAccessStore(t)
		if _, err := db.DB.Exec("CREATE TABLE tw_session(preserved TEXT)"); err != nil {
			t.Fatal(err)
		}
		if _, err := Initialize(context.Background(), db); err == nil {
			t.Fatal("untracked access table was overwritten")
		}
		status, err := Status(context.Background(), db)
		if err != nil || status.State != "missing" {
			t.Fatal("refused installation changed the journal", status, err)
		}
	})
	t.Run("incomplete-ready-schema", func(t *testing.T) {
		db, _ := sqliteInitialized(t)
		if _, err := db.DB.Exec("ALTER TABLE tw_user_identity DROP COLUMN os_name"); err != nil {
			t.Fatal(err)
		}
		status, err := Status(context.Background(), db)
		if err != nil || status.Ready || status.State != "incomplete" {
			t.Fatal("incomplete access schema reported ready", status, err)
		}
		if _, err := New(context.Background(), db, 30*time.Minute); err == nil {
			t.Fatal("incomplete access schema enabled sessions")
		}
		if _, err := Initialize(context.Background(), db); err == nil {
			t.Fatal("incomplete ready schema was silently repaired")
		}
	})
	for _, drift := range []struct {
		name string
		ddl  []string
	}{
		{"identity-comparison", []string{"CREATE TABLE tw_user_identity(subject TEXT COLLATE NOCASE NOT NULL PRIMARY KEY,user_id TEXT COLLATE BINARY NOT NULL UNIQUE REFERENCES users(user_id),os_name TEXT NOT NULL,created_at INTEGER NOT NULL)"}},
		{"identity-column-comparison", []string{`CREATE TABLE tw_user_identity(subject TEXT COLLATE "NOCASE" NOT NULL,user_id TEXT COLLATE BINARY NOT NULL UNIQUE REFERENCES users(user_id),os_name TEXT NOT NULL,created_at INTEGER NOT NULL,PRIMARY KEY(subject COLLATE BINARY))`}},
		{"identity-commented-comparison", []string{`CREATE TABLE tw_user_identity(subject TEXT COLLATE/**/[NOCASE] NOT NULL,user_id TEXT COLLATE BINARY NOT NULL UNIQUE REFERENCES users(user_id),os_name TEXT NOT NULL,created_at INTEGER NOT NULL,PRIMARY KEY(subject COLLATE BINARY))`}},
		{"identity-user-comparison", []string{"CREATE TABLE tw_user_identity(subject TEXT COLLATE BINARY NOT NULL PRIMARY KEY,user_id TEXT COLLATE NOCASE NOT NULL REFERENCES users(user_id),os_name TEXT NOT NULL,created_at INTEGER NOT NULL,UNIQUE(user_id COLLATE BINARY))"}},
		{"identity-uniqueness", []string{"CREATE TABLE tw_user_identity(subject TEXT COLLATE BINARY NOT NULL PRIMARY KEY,user_id TEXT NOT NULL REFERENCES users(user_id),os_name TEXT NOT NULL,created_at INTEGER NOT NULL)"}},
		{"identity-partial-uniqueness", []string{
			"CREATE TABLE tw_user_identity(subject TEXT COLLATE BINARY NOT NULL PRIMARY KEY,user_id TEXT COLLATE BINARY NOT NULL REFERENCES users(user_id),os_name TEXT NOT NULL,created_at INTEGER NOT NULL)",
			"CREATE UNIQUE INDEX tw_identity_partial ON tw_user_identity(user_id) WHERE created_at>0",
		}},
		{"identity-reference", []string{"CREATE TABLE tw_user_identity(subject TEXT COLLATE BINARY NOT NULL PRIMARY KEY,user_id TEXT COLLATE BINARY NOT NULL UNIQUE,os_name TEXT NOT NULL,created_at INTEGER NOT NULL)"}},
	} {
		t.Run(drift.name, func(t *testing.T) {
			db, _ := sqliteInitialized(t)
			for _, query := range append([]string{"DROP TABLE tw_user_identity"}, drift.ddl...) {
				if _, err := db.DB.Exec(query); err != nil {
					t.Fatal(err)
				}
			}
			switch drift.name {
			case "identity-column-comparison":
				// The primary-key index is binary, but an unqualified subject
				// lookup aliases distinct accounts through the column collation.
				if _, err := db.DB.Exec("INSERT INTO tw_user_identity VALUES('SubjectA','reader','one',0),('subjecta','global','two',0)"); err != nil {
					t.Fatal(err)
				}
				var matches int
				if err := db.DB.QueryRow("SELECT COUNT(*) FROM tw_user_identity WHERE subject='SubjectA'").Scan(&matches); err != nil || matches != 2 {
					t.Fatal("fixture did not reproduce the declared-collation alias", matches, err)
				}
			case "identity-partial-uniqueness":
				// Rows outside the partial predicate can bind two identities to
				// one user, despite the index reporting unique user_id keys.
				if _, err := db.DB.Exec("INSERT INTO tw_user_identity VALUES('SubjectA','reader','one',0),('SubjectB','reader','two',0)"); err != nil {
					t.Fatal(err)
				}
				var matches int
				if err := db.DB.QueryRow("SELECT COUNT(*) FROM tw_user_identity WHERE user_id='reader'").Scan(&matches); err != nil || matches != 2 {
					t.Fatal("fixture did not reproduce the duplicate identity binding", matches, err)
				}
			}
			status, err := Status(context.Background(), db)
			if err != nil || status.Ready || status.State != "incomplete" {
				t.Fatal("custom identity schema reported ready", status, err)
			}
			if _, err := New(context.Background(), db, 30*time.Minute); err == nil {
				t.Fatal("custom identity schema enabled sessions")
			}
			if _, err := Initialize(context.Background(), db); err == nil {
				t.Fatal("custom identity schema was silently adopted")
			}
		})
	}
	t.Run("binary-declarations", func(t *testing.T) {
		db, _ := sqliteInitialized(t)
		for _, query := range []string{
			"DROP TABLE tw_user_identity",
			`CREATE TABLE tw_user_identity(subject TEXT COLLATE "binary" NOT NULL,user_id TEXT COLLATE [BINARY] NOT NULL UNIQUE REFERENCES users(user_id),os_name TEXT NOT NULL DEFAULT 'COLLATE NOCASE',created_at INTEGER NOT NULL,PRIMARY KEY(subject COLLATE BINARY))`,
		} {
			if _, err := db.DB.Exec(query); err != nil {
				t.Fatal(err)
			}
		}
		status, err := Status(context.Background(), db)
		if err != nil || !status.Ready {
			t.Fatal("binary declarations or an inert string literal were rejected", status, err)
		}
		if _, err := New(context.Background(), db, 30*time.Minute); err != nil {
			t.Fatal("binary schema could not enable sessions", err)
		}
	})
}

func TestSQLiteAccessReadOnlyInspection(t *testing.T) {
	db, _ := sqliteInitialized(t)
	filename := sqliteAccessPath(t, db)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	ro, err := store.Open(context.Background(), config.Database{Type: "sqlite3", Path: filename}, true)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	status, err := Status(context.Background(), ro)
	if err != nil || !status.Ready || !status.ReadOnly {
		t.Fatal(status, err)
	}
	if _, err := Initialize(context.Background(), ro); err == nil {
		t.Fatal("read-only access initialization accepted")
	}
	if _, err := New(context.Background(), ro, 30*time.Minute); err == nil {
		t.Fatal("read-only connection used for session writes")
	}
	if err := ro.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filename)
	if err != nil || sha256.Sum256(before) != sha256.Sum256(after) {
		t.Fatal("read-only access inspection changed the database", err)
	}
}

func TestSQLiteAccessLastAdministratorAcrossIndependentStores(t *testing.T) {
	for _, withOSAdmin := range []bool{false, true} {
		name := "last-admin"
		if withOSAdmin {
			name = "last-local-admin"
		}
		t.Run(name, func(t *testing.T) {
			s, admin, token, provider := sqliteLinkedOSFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if withOSAdmin {
				linkedOSCreate(t, s, admin, token, "os-admin", "host-one", "admin", nil)
			}
			if _, err := s.db.DB.Exec("INSERT INTO users(user_id,name,password,role,group_id) VALUES('second','Second',?,'admin',0)", testHash); err != nil {
				t.Fatal(err)
			}
			if _, err := s.db.DB.Exec("INSERT INTO tw_user_auth(user_id) VALUES('second')"); err != nil {
				t.Fatal(err)
			}
			otherDB := openSQLiteAccessStore(t, sqliteAccessPath(t, s.db))
			other, err := NewWithOS(ctx, otherDB, 30*time.Minute, provider)
			if err != nil {
				t.Fatal(err)
			}
			second, secondToken, err := other.Login(ctx, "second", testPassword, "127.0.0.2", "")
			if err != nil {
				t.Fatal(err)
			}
			start, results := make(chan struct{}), make(chan error, 2)
			for _, v := range []struct {
				s     *Service
				p     Principal
				token string
			}{{s, admin, token}, {other, second, secondToken}} {
				go func(s *Service, p Principal, token string) {
					<-start
					_, err := s.UpdateUser(ctx, token, p.CSRF, UserInput{ID: p.User.ID, Name: p.User.Name, Role: "user", Status: "active", Version: p.User.version})
					results <- err
				}(v.s, v.p, v.token)
			}
			close(start)
			succeeded, protected := 0, 0
			for i := 0; i < 2; i++ {
				if err := <-results; err == nil {
					succeeded++
				} else {
					code := "last_admin"
					if withOSAdmin {
						code = "last_local_admin"
					}
					requireError(t, err, code)
					protected++
				}
			}
			if succeeded != 1 || protected != 1 {
				t.Fatal("independent stores did not preserve a local recovery admin", succeeded, protected)
			}
		})
	}
}

func TestSQLiteAccessAuthorizationAndRevocationSerialize(t *testing.T) {
	s, admin, token := sqliteAdminFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	reader, readerToken, err := s.Login(ctx, "reader", testPassword, "127.0.0.2", "")
	if err != nil {
		t.Fatal(err)
	}
	other, err := New(ctx, openSQLiteAccessStore(t, sqliteAccessPath(t, s.db)), 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	// A separate connection can validate the durable session after restart.
	if _, err = other.Session(ctx, readerToken, false); err != nil {
		t.Fatal("session was not durable across independent stores", err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	writeDone, revokeDone := make(chan error, 1), make(chan error, 1)
	go func() {
		writeDone <- s.WithGroup(ctx, readerToken, reader.CSRF, 7, "editor", func(tx *sql.Tx, u User) error {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
			_, err := tx.ExecContext(ctx, "INSERT INTO devices(group_id,device_name) VALUES(7,'authorized-before-revocation')")
			return err
		})
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("authorized transaction did not start")
	}
	go func() {
		_, err := other.UpdateUser(ctx, token, admin.CSRF, UserInput{ID: "reader", Name: "Reader", Role: "user", Status: "disabled", Version: reader.User.version, Groups: []Membership{{7, "editor"}}})
		revokeDone <- err
	}()
	select {
	case err := <-revokeDone:
		close(release)
		<-writeDone
		t.Fatal("revocation completed before the authorized transaction released its lock", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-writeDone; err != nil {
		t.Fatal(err)
	}
	if err := <-revokeDone; err != nil {
		t.Fatal(err)
	}
	called := false
	err = s.WithGroup(ctx, readerToken, reader.CSRF, 7, "editor", func(*sql.Tx, User) error { called = true; return nil })
	requireError(t, err, "unauthenticated")
	if called {
		t.Fatal("revoked authorization reached the mutation callback")
	}
}

func TestSQLiteOSRateLimitsAcrossIndependentStores(t *testing.T) {
	s, admin, token, provider := sqliteLinkedOSFixture(t)
	linkedOSCreate(t, s, admin, token, "os-linked", "host-one", "user", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	other, err := NewWithOS(ctx, openSQLiteAccessStore(t, sqliteAccessPath(t, s.db)), 30*time.Minute, provider)
	if err != nil {
		t.Fatal(err)
	}
	start, results := make(chan struct{}), make(chan error, 12)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			target, name := s, "host-one"
			if i%2 == 1 {
				target, name = other, "host-alias"
			}
			_, _, err := target.LoginOS(ctx, name, "wrong", "127.0.0.2", "")
			results <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	checked, limited := 0, 0
	for err := range results {
		var e *Error
		if !errors.As(err, &e) {
			t.Fatal("unexpected login result", err)
		}
		switch e.Code {
		case "invalid_credentials":
			checked++
		case "login_limited":
			limited++
		default:
			t.Fatal("concurrent login failed outside the rate policy", err)
		}
	}
	if checked != 5 || limited != 7 || provider.calls.Load() != 5 {
		t.Fatal("independent stores bypassed the canonical identity limit", checked, limited, provider.calls.Load())
	}
}

func TestSQLiteAttemptCancellationIsNotBlockedBySiblingAdmission(t *testing.T) {
	db, service := sqliteInitialized(t)
	other := openSQLiteAccessStore(t, sqliteAccessPath(t, db))
	holder, err := other.DB.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	firstDone := make(chan error, 1)
	go func() { firstDone <- service.attempt(ctx, "waiting-peer", 5) }()
	deadline := time.Now().Add(2 * time.Second)
	for db.DB.Stats().InUse == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if db.DB.Stats().InUse == 0 {
		t.Fatal("first rate request never reached database admission")
	}
	canceled, stop := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer stop()
	started := time.Now()
	err = service.attempt(canceled, "canceled-peer", 5)
	elapsed := time.Since(started)
	requireError(t, err, "unavailable")
	if elapsed > 2*time.Second || canceled.Err() == nil {
		t.Error("canceled rate request waited behind a sibling admission", elapsed)
	}
	if err = holder.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err = <-firstDone; err != nil {
		t.Fatal("first rate request did not recover after writer release", err)
	}
	var count int
	if err = db.DB.QueryRow("SELECT count FROM tw_login_attempt WHERE key_hash=?", digest("waiting-peer")).Scan(&count); err != nil || count != 1 {
		t.Fatal("waiting rate bucket was not applied exactly once", count, err)
	}
	if err = db.DB.QueryRow("SELECT COUNT(*) FROM tw_login_attempt WHERE key_hash=?", digest("canceled-peer")).Scan(&count); err != nil || count != 0 {
		t.Fatal("canceled rate request changed its bucket", count, err)
	}
}

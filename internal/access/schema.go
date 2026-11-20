package access

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/horrible-gh/TimeWeaver/internal/store"
)

const schemaVersion = 4

// This journal is separate from Python's frozen migrations(filename) contract.
// A failed MySQL DDL installation is deliberately not auto-replayed.
var accessBaseTables = []string{"tw_user_auth", "tw_group_member", "tw_session", "tw_audit", "tw_login_attempt"}
var accessTables = append(append([]string{}, accessBaseTables...), "tw_user_identity")

type SchemaStatus struct {
	Ready    bool   `json:"ready"`
	Version  int    `json:"version"`
	State    string `json:"state"`
	ReadOnly bool   `json:"read_only"`
}

func Status(ctx context.Context, db *store.Store) (SchemaStatus, error) {
	r := SchemaStatus{State: "missing", ReadOnly: true}
	if db == nil || db.DB == nil || (db.Dialect != "mysql" && db.Dialect != "sqlite") {
		return r, errors.New("human authentication requires MySQL/MariaDB or SQLite")
	}
	if db.Dialect == "sqlite" {
		return sqliteAccessStatus(ctx, db.DB)
	}
	var n int
	if err := db.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name='tw_access_schema'").Scan(&n); err != nil {
		return r, unavailable()
	}
	if n == 0 {
		return r, nil
	}
	err := db.DB.QueryRowContext(ctx, "SELECT version,state FROM tw_access_schema WHERE id=1").Scan(&r.Version, &r.State)
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
			if err = db.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name=?", table).Scan(&n); err != nil {
				return r, unavailable()
			}
			if n != 1 {
				r.Ready = false
				r.State = "incomplete"
			}
		}
	}
	if r.Ready {
		if err = db.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='tw_audit' AND column_name IN ('target','detail')").Scan(&n); err != nil {
			return r, unavailable()
		}
		if n != 2 {
			r.Ready = false
			r.State = "incomplete"
		}
	}
	if r.Ready {
		if err = db.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='tw_user_identity' AND column_name IN ('subject','user_id','os_name','created_at')").Scan(&n); err != nil {
			return r, unavailable()
		}
		if n != 4 {
			r.Ready, r.State = false, "incomplete"
		}
	}
	if r.Ready {
		valid, err := mysqlRotationShape(ctx, db.DB)
		if err != nil {
			return r, err
		}
		if !valid {
			r.Ready, r.State = false, "incomplete"
		}
	}
	return r, nil
}

// Initialize requires explicit CLI approval, a completed legacy migration, a
// stopped legacy server and a backup. It never changes existing users/groups.
func Initialize(ctx context.Context, db *store.Store) (SchemaStatus, error) {
	empty := SchemaStatus{}
	if db == nil || db.DB == nil || (db.Dialect != "mysql" && db.Dialect != "sqlite") || db.ReadOnly() {
		return empty, errors.New("access initialization requires an explicitly writable MySQL/MariaDB or SQLite database")
	}
	baseline, err := db.Status(ctx)
	if err != nil {
		return empty, err
	}
	if baseline.Blocked || baseline.Pending != 0 {
		return empty, errors.New("complete and inspect the legacy migrations before initializing human access")
	}
	guard, err := db.CriticalSchema(ctx)
	if err != nil {
		return empty, err
	}
	if len(guard.Issues) > 0 {
		return empty, errors.New("repair the critical legacy schema before initializing human access")
	}
	if db.Dialect == "sqlite" {
		return initializeSQLiteAccess(ctx, db)
	}
	conn, err := db.DB.Conn(ctx)
	if err != nil {
		return empty, unavailable()
	}
	defer conn.Close()
	var database string
	if err = conn.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&database); err != nil {
		return empty, unavailable()
	}
	key := "timeweaver:access:" + digest(database)[:40]
	var locked sql.NullInt64
	if err = conn.QueryRowContext(ctx, "SELECT GET_LOCK(?,5)", key).Scan(&locked); err != nil || !locked.Valid || locked.Int64 != 1 {
		return empty, errors.New("cannot acquire access migration lock")
	}
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		conn.ExecContext(c, "SELECT RELEASE_LOCK(?)", key)
		conn.Raw(func(any) error { return driver.ErrBadConn })
	}()
	status, err := Status(ctx, db)
	if err != nil {
		return empty, err
	}
	if status.Ready {
		return status, nil
	}
	if status.Version == 1 && status.State == "ready" {
		status, err = upgradeAudit(ctx, conn)
		if err != nil {
			return empty, err
		}
	}
	if status.Version == 2 && status.State == "ready" {
		status, err = upgradeIdentity(ctx, conn)
		if err != nil {
			return empty, err
		}
	}
	if status.Version == 3 && status.State == "ready" {
		return upgradeSessions(ctx, conn)
	}
	if status.State != "missing" {
		return status, errors.New("access migration is unfinished or incompatible; restore/review it before retrying")
	}
	var count int
	for _, table := range accessTables {
		if err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name=?", table).Scan(&count); err != nil {
			return empty, unavailable()
		}
		if count != 0 {
			return empty, errors.New("untracked access tables exist; refusing to overwrite")
		}
	}
	// Match the actual legacy user identifier collation for foreign keys, rather
	// than assuming the database default still matches a pre-existing users table.
	userType, err := userIdentifierType(ctx, conn)
	if err != nil {
		return empty, err
	}
	if _, err = conn.ExecContext(ctx, `CREATE TABLE tw_access_schema(id INT PRIMARY KEY,version INT NOT NULL,state VARCHAR(16) NOT NULL) ENGINE=InnoDB`); err != nil {
		return empty, unavailable()
	}
	if _, err = conn.ExecContext(ctx, `INSERT INTO tw_access_schema VALUES(1,?,'running')`, schemaVersion); err != nil {
		return empty, unavailable()
	}
	ddls := []string{
		`CREATE TABLE tw_user_auth(user_id ` + userType + ` PRIMARY KEY,status VARCHAR(16) NOT NULL DEFAULT 'active',version BIGINT NOT NULL DEFAULT 1,must_change_password BOOLEAN NOT NULL DEFAULT FALSE,password_changed_at BIGINT NULL,last_login_at BIGINT NULL,FOREIGN KEY(user_id) REFERENCES users(user_id),CHECK(status IN ('active','disabled'))) ENGINE=InnoDB`,
		`CREATE TABLE tw_group_member(user_id ` + userType + ` NOT NULL,group_id INT NOT NULL,role VARCHAR(16) NOT NULL,PRIMARY KEY(user_id,group_id),FOREIGN KEY(user_id) REFERENCES users(user_id),FOREIGN KEY(group_id) REFERENCES ` + "`groups`" + `(group_id),CHECK(role IN ('viewer','operator','editor'))) ENGINE=InnoDB`,
		mysqlSessionDDL(userType),
		`CREATE TABLE tw_audit(id BIGINT AUTO_INCREMENT PRIMARY KEY,at BIGINT NOT NULL,actor VARCHAR(50) NOT NULL,action VARCHAR(40) NOT NULL,result VARCHAR(16) NOT NULL,target VARCHAR(255) NOT NULL DEFAULT '',detail TEXT NULL) ENGINE=InnoDB`,
		`CREATE TABLE tw_login_attempt(key_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,count INT NOT NULL,started BIGINT NOT NULL,INDEX tw_attempt_expiry(started)) ENGINE=InnoDB`,
		identityDDL(userType),
	}
	for i, ddl := range ddls {
		if _, err = conn.ExecContext(ctx, ddl); err != nil {
			return empty, fmt.Errorf("access schema stopped at step %d; partial DDL remains; restore/review before retrying", i+1)
		}
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return empty, unavailable()
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO tw_user_auth(user_id,must_change_password) SELECT user_id,(BINARY password=BINARY ?) FROM users`, legacySeedHash); err != nil {
		return empty, unavailable()
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO tw_group_member(user_id,group_id,role) SELECT user_id,group_id,'editor' FROM users WHERE role='user'`); err != nil {
		return empty, unavailable()
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tw_access_schema SET state='ready' WHERE id=1`); err != nil {
		return empty, unavailable()
	}
	if err = tx.Commit(); err != nil {
		return empty, unavailable()
	}
	return SchemaStatus{true, schemaVersion, "ready", false}, nil
}

// Upgrade only the known v1 shape. MySQL DDL commits implicitly; record running
// first, never silently retry partial DDL, and do not recreate memberships.
func upgradeAudit(ctx context.Context, conn *sql.Conn) (SchemaStatus, error) {
	empty := SchemaStatus{}
	var n int
	for _, table := range accessBaseTables {
		if conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name=?", table).Scan(&n) != nil {
			return empty, unavailable()
		}
		if n != 1 {
			return empty, errors.New("incomplete v1 access schema; inspect before upgrading")
		}
	}

	if conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='tw_audit' AND column_name IN ('target','detail')").Scan(&n) != nil {
		return empty, unavailable()
	}
	if n != 0 {
		return empty, errors.New("unexpected v1 audit shape; inspect before upgrading")
	}
	if _, err := conn.ExecContext(ctx, "UPDATE tw_access_schema SET version=2,state='running' WHERE id=1 AND version=1 AND state='ready'"); err != nil {
		return empty, unavailable()
	}
	if _, err := conn.ExecContext(ctx, "ALTER TABLE tw_audit ADD COLUMN target VARCHAR(255) NOT NULL DEFAULT '', ADD COLUMN detail TEXT NULL"); err != nil {
		return empty, errors.New("audit upgrade stopped; partial DDL must be inspected before retrying")
	}
	if _, err := conn.ExecContext(ctx, "UPDATE tw_access_schema SET state='ready' WHERE id=1 AND version=2"); err != nil {
		return empty, unavailable()
	}
	return SchemaStatus{false, 2, "ready", false}, nil
}

func userIdentifierType(ctx context.Context, conn *sql.Conn) (string, error) {
	var charset, collation, kind string
	if err := conn.QueryRowContext(ctx, `SELECT CHARACTER_SET_NAME,COLLATION_NAME,COLUMN_TYPE FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='users' AND column_name='user_id'`).Scan(&charset, &collation, &kind); err != nil {
		return "", unavailable()
	}
	validName := regexp.MustCompile(`^[A-Za-z0-9_]+$`)
	if !validName.MatchString(charset) || !validName.MatchString(collation) || strings.ToLower(kind) != "varchar(50)" {
		return "", errors.New("unexpected legacy user identifier shape")
	}
	return "VARCHAR(50) CHARACTER SET " + charset + " COLLATE " + collation, nil
}

func identityDDL(userType string) string {
	return `CREATE TABLE tw_user_identity(subject VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,user_id ` + userType + ` NOT NULL,os_name VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,created_at BIGINT NOT NULL,UNIQUE KEY tw_identity_user(user_id),FOREIGN KEY(user_id) REFERENCES users(user_id)) ENGINE=InnoDB`
}

// The v3 addition never reconstructs grants, changes passwords, or discards local
// sessions. Unknown or partial DDL must be reviewed instead of being replayed.
func upgradeIdentity(ctx context.Context, conn *sql.Conn) (SchemaStatus, error) {
	empty := SchemaStatus{}
	var n int
	for _, table := range accessBaseTables {
		if conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name=?", table).Scan(&n) != nil {
			return empty, unavailable()
		}
		if n != 1 {
			return empty, errors.New("incomplete v2 access schema; inspect before upgrading")
		}
	}
	if conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name='tw_user_identity'").Scan(&n) != nil {
		return empty, unavailable()
	}
	if n != 0 {
		return empty, errors.New("untracked OS identity table; inspect before upgrading")
	}
	if conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='tw_audit' AND column_name IN ('target','detail')").Scan(&n) != nil {
		return empty, unavailable()
	}
	if n != 2 {
		return empty, errors.New("incomplete v2 audit shape; inspect before upgrading")
	}
	userType, err := userIdentifierType(ctx, conn)
	if err != nil {
		return empty, err
	}
	if _, err = conn.ExecContext(ctx, "UPDATE tw_access_schema SET version=3,state='running' WHERE id=1 AND version=2 AND state='ready'"); err != nil {
		return empty, unavailable()
	}
	if _, err = conn.ExecContext(ctx, identityDDL(userType)); err != nil {
		return empty, errors.New("OS identity upgrade stopped; partial DDL must be inspected before retrying")
	}
	if _, err = conn.ExecContext(ctx, "UPDATE tw_access_schema SET state='ready' WHERE id=1 AND version=3"); err != nil {
		return empty, unavailable()
	}
	return SchemaStatus{false, 3, "ready", false}, nil
}

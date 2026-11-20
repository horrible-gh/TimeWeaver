package access

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

var rotationColumns = []string{"token_digest", "previous_digest", "previous_until", "rotation_seed", "rotated_at", "remembered"}

func rotationColumn(name string) bool {
	for _, column := range rotationColumns {
		if column == name {
			return true
		}
	}
	return false
}

const mysqlRotationColumns = `token_digest CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,previous_digest CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,previous_until BIGINT NOT NULL DEFAULT 0,rotation_seed CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,rotated_at BIGINT NOT NULL DEFAULT 0,remembered BOOLEAN NOT NULL DEFAULT FALSE`

func mysqlSessionDDL(userType string) string {
	return `CREATE TABLE tw_session(digest CHAR(64) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,user_id ` + userType + ` NOT NULL,user_version BIGINT NOT NULL,csrf CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,password_fingerprint CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,created_at BIGINT NOT NULL,last_seen BIGINT NOT NULL,expires_at BIGINT NOT NULL,auth_method VARCHAR(16) NOT NULL,` + mysqlRotationColumns + `,INDEX tw_session_user(user_id),INDEX tw_session_expiry(expires_at),UNIQUE KEY tw_session_token(token_digest),UNIQUE KEY tw_session_previous(previous_digest),FOREIGN KEY(user_id) REFERENCES users(user_id)) ENGINE=InnoDB`
}

func mysqlRotationShape(ctx context.Context, q accessCatalog) (bool, error) {
	for _, column := range rotationColumns {
		var dataType, columnType, nullable string
		var defaultValue sql.NullString
		err := q.QueryRowContext(ctx, "SELECT data_type,column_type,is_nullable,column_default FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='tw_session' AND column_name=?", column).Scan(&dataType, &columnType, &nullable, &defaultValue)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, unavailable()
		}
		switch column {
		case "token_digest", "previous_digest", "rotation_seed":
			// MariaDB versions can represent a nullable SQL default as the
			// metadata text NULL; a quoted string 'NULL' is not equivalent.
			if dataType != "char" || strings.ToLower(columnType) != "char(64)" || nullable != "YES" || defaultValue.Valid && !strings.EqualFold(defaultValue.String, "NULL") {
				return false, nil
			}
		case "previous_until", "rotated_at":
			if dataType != "bigint" || strings.Contains(strings.ToLower(columnType), "unsigned") || nullable != "NO" || !defaultValue.Valid || defaultValue.String != "0" {
				return false, nil
			}
		case "remembered":
			if dataType != "tinyint" || strings.ToLower(columnType) != "tinyint(1)" || nullable != "NO" || !defaultValue.Valid || defaultValue.String != "0" {
				return false, nil
			}
		}
	}
	var n int
	if err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='tw_session' AND column_name IN ('token_digest','previous_digest','rotation_seed') AND data_type='char' AND character_maximum_length=64 AND collation_name='ascii_bin'").Scan(&n); err != nil {
		return false, unavailable()
	}
	if n != 3 {
		return false, nil
	}
	for _, column := range []string{"token_digest", "previous_digest"} {
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM (
 SELECT index_name FROM information_schema.statistics
 WHERE table_schema=DATABASE() AND table_name='tw_session' AND non_unique=0
 GROUP BY index_name HAVING COUNT(*)=1 AND MIN(column_name)=? AND MAX(sub_part) IS NULL
) unique_keys`, column).Scan(&n); err != nil {
			return false, unavailable()
		}
		if n == 0 {
			return false, nil
		}
	}
	return true, nil
}

func sqliteRotationShape(ctx context.Context, q accessCatalog) (bool, error) {
	for _, column := range rotationColumns {
		var kind string
		var notNull int
		var defaultValue sql.NullString
		err := q.QueryRowContext(ctx, `SELECT type,"notnull",dflt_value FROM pragma_table_info('tw_session') WHERE name=?`, column).Scan(&kind, &notNull, &defaultValue)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, unavailable()
		}
		switch column {
		case "token_digest", "previous_digest", "rotation_seed":
			if !strings.EqualFold(kind, "TEXT") || notNull != 0 || defaultValue.Valid && !strings.EqualFold(defaultValue.String, "NULL") {
				return false, nil
			}
		case "previous_until", "rotated_at", "remembered":
			if !strings.EqualFold(kind, "INTEGER") || notNull != 1 || !defaultValue.Valid || defaultValue.String != "0" {
				return false, nil
			}
		}
	}
	return true, nil
}

// The v4 journal covers every implicit MySQL DDL commit. Existing credentials,
// CSRF values, deadlines, grants and idle timestamps are copied without renewal.
func upgradeSessions(ctx context.Context, conn *sql.Conn) (SchemaStatus, error) {
	empty := SchemaStatus{}
	var n int
	for _, table := range accessTables {
		if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name=?", table).Scan(&n); err != nil {
			return empty, unavailable()
		}
		if n != 1 {
			return empty, errors.New("incomplete v3 access schema; inspect before upgrading")
		}
	}
	for table, columns := range map[string][]string{
		"tw_session":       {"digest", "user_id", "user_version", "csrf", "password_fingerprint", "created_at", "last_seen", "expires_at", "auth_method"},
		"tw_audit":         {"target", "detail"},
		"tw_user_identity": {"subject", "user_id", "os_name", "created_at"},
	} {
		for _, column := range columns {
			if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name=? AND column_name=?", table, column).Scan(&n); err != nil {
				return empty, unavailable()
			}
			if n != 1 {
				return empty, errors.New("incomplete v3 access shape; inspect before upgrading")
			}
		}
	}
	for _, column := range rotationColumns {
		if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='tw_session' AND column_name=?", column).Scan(&n); err != nil {
			return empty, unavailable()
		}
		if n != 0 {
			return empty, errors.New("unexpected v3 session shape; inspect before upgrading")
		}
	}
	if _, err := conn.ExecContext(ctx, "UPDATE tw_access_schema SET version=4,state='running' WHERE id=1 AND version=3 AND state='ready'"); err != nil {
		return empty, unavailable()
	}
	if _, err := conn.ExecContext(ctx, `ALTER TABLE tw_session ADD COLUMN token_digest CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,ADD COLUMN previous_digest CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,ADD COLUMN previous_until BIGINT NOT NULL DEFAULT 0,ADD COLUMN rotation_seed CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,ADD COLUMN rotated_at BIGINT NOT NULL DEFAULT 0,ADD COLUMN remembered BOOLEAN NOT NULL DEFAULT FALSE,ADD UNIQUE KEY tw_session_token(token_digest),ADD UNIQUE KEY tw_session_previous(previous_digest)`); err != nil {
		return empty, errors.New("session upgrade stopped; partial DDL must be inspected before retrying")
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return empty, unavailable()
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "UPDATE tw_session SET token_digest=digest,rotated_at=created_at"); err != nil {
		return empty, unavailable()
	}
	if _, err = tx.ExecContext(ctx, "UPDATE tw_access_schema SET state='ready' WHERE id=1 AND version=4"); err != nil {
		return empty, unavailable()
	}
	if err = tx.Commit(); err != nil {
		return empty, unavailable()
	}
	return SchemaStatus{Ready: true, Version: schemaVersion, State: "ready"}, nil
}

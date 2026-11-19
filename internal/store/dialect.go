package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

// LockClause is deliberately explicit at each query call site. SQLite writable
// connections use BEGIN IMMEDIATE, so their transaction already owns the one
// writer reservation; pretending to implement row locks would be misleading.
func LockClause(dialect string, skipLocked bool) string {
	if dialect == "sqlite" {
		return ""
	}
	if skipLocked {
		return " FOR UPDATE SKIP LOCKED"
	}
	return " FOR UPDATE"
}

// NowExpr uses the database clock in UTC, at whole-second precision on both
// supported backends. SQLite's CURRENT_TIMESTAMP is UTC, unlike MySQL's NOW().
func NowExpr(dialect string) string {
	if dialect == "sqlite" {
		return "CURRENT_TIMESTAMP"
	}
	return "UTC_TIMESTAMP()"
}

func DBNow(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, dialect string) (time.Time, error) {
	var now time.Time
	err := q.QueryRowContext(ctx, "SELECT "+NowExpr(dialect)).Scan(ScanTime(&now))
	return now, err
}

// TimeValue keeps SQLite date values lexically comparable to CURRENT_TIMESTAMP.
// It omits a zero fractional part and never emits a timezone or Go's zone name.
// Callers retain native time.Time values for MySQL's binary/date encoding.
func TimeValue(dialect string, value time.Time) any {
	if dialect == "sqlite" {
		return value.UTC().Format("2006-01-02 15:04:05.999999999")
	}
	return value.UTC()
}

type timestampScanner struct {
	target *time.Time
	null   *sql.NullTime
}

// ScanTime accepts both native driver dates and the historical SQLite text
// forms. A timezone-less SQL value is interpreted as UTC on both backends.
func ScanTime(target *time.Time) sql.Scanner { return timestampScanner{target: target} }

func ScanNullTime(target *sql.NullTime) sql.Scanner { return timestampScanner{null: target} }

func (s timestampScanner) Scan(value any) error {
	if value == nil {
		if s.null == nil {
			return errors.New("NULL database timestamp is not allowed")
		}
		*s.null = sql.NullTime{}
		return nil
	}
	t, err := parseTimestamp(value)
	if err != nil {
		return err
	}
	if s.null != nil {
		*s.null = sql.NullTime{Time: t, Valid: true}
	} else if s.target != nil {
		*s.target = t
	} else {
		return errors.New("database timestamp destination is missing")
	}
	return nil
}

func parseTimestamp(value any) (time.Time, error) {
	if t, ok := value.(time.Time); ok {
		return t.UTC(), nil
	}
	var text string
	switch v := value.(type) {
	case string:
		text = v
	case []byte:
		text = string(v)
	default:
		return time.Time{}, errors.New("unsupported database timestamp type")
	}
	text = strings.TrimSpace(text)
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05.999999999 -0700 MST",
		"2006-01-02 15:04:05.999999999 -0700",
		"2006-01-02 15:04:05.999999999 -07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02T15:04:05.999999999",
		"2006-01-02 15:04",
		"2006-01-02T15:04",
		"2006-01-02",
	} {
		if t, err := time.ParseInLocation(layout, text, time.UTC); err == nil {
			return t.UTC(), nil
		}
	}
	// Never include a persisted value in diagnostics.
	return time.Time{}, errors.New("invalid database timestamp")
}

func sqliteCode(err error) (int, bool) {
	var coded interface{ Code() int }
	if errors.As(err, &coded) {
		return coded.Code(), true
	}
	return 0, false
}

func IsUniqueViolation(err error) bool {
	var my *mysql.MySQLError
	if errors.As(err, &my) {
		return my.Number == 1062
	}
	code, ok := sqliteCode(err)
	return ok && (code == 1555 || code == 2067) // PRIMARYKEY, UNIQUE
}

func IsForeignKeyViolation(err error) bool {
	var my *mysql.MySQLError
	if errors.As(err, &my) {
		return my.Number == 1451 || my.Number == 1452
	}
	code, ok := sqliteCode(err)
	return ok && code == 787 // SQLITE_CONSTRAINT_FOREIGNKEY
}

func IsBusy(err error) bool {
	var my *mysql.MySQLError
	if errors.As(err, &my) {
		return my.Number == 1205 || my.Number == 1213
	}
	code, ok := sqliteCode(err)
	return ok && (code&255 == 5 || code&255 == 6) // BUSY/LOCKED, including extended codes
}

// validIdentifier is for schema metadata controlled by the program, never for
// interpolating a user supplied identifier into SQL.
func validIdentifier(name string) error {
	if !metadataName.MatchString(name) {
		return fmt.Errorf("unsupported schema identifier")
	}
	return nil
}

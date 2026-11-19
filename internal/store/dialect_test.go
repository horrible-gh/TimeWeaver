package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func TestDatabaseTimestampScannersPreserveUTCInstants(t *testing.T) {
	want := time.Date(2026, 11, 19, 3, 4, 5, 123456789, time.UTC)
	for _, input := range []any{
		want.In(time.FixedZone("KST", 9*3600)),
		"2026-11-19T12:04:05.123456789+09:00",
		"2026-11-19 03:04:05.123456789Z",
		"2026-11-19 03:04:05.123456789",
		[]byte("2026-11-19T03:04:05.123456789Z"),
		"2026-11-19 12:04:05.123456789 +0900 KST",
		"2026-11-19 12:04:05.123456789 +0900",
		"2026-11-19 12:04:05.123456789 +09:00",
	} {
		var got time.Time
		if err := ScanTime(&got).Scan(input); err != nil || !got.Equal(want) || got.Location() != time.UTC {
			t.Fatal(got, err)
		}
		if text := TimeValue("sqlite", got); text != "2026-11-19 03:04:05.123456789" {
			t.Fatal(text)
		}
	}
	for _, input := range []string{"2026-11-19", "2026-11-19 03:04", "2026-11-19T03:04"} {
		var got time.Time
		if err := ScanTime(&got).Scan(input); err != nil {
			t.Fatal(input, err)
		}
	}
	var optional sql.NullTime
	if err := ScanNullTime(&optional).Scan(want); err != nil || !optional.Valid {
		t.Fatal(optional, err)
	}
	if err := ScanNullTime(&optional).Scan(nil); err != nil || optional.Valid {
		t.Fatal(optional, err)
	}
	for _, input := range []any{nil, "sensitive-invalid-timestamp", int64(1), "2026-02-30 00:00:00"} {
		var got time.Time
		if err := ScanTime(&got).Scan(input); err == nil || strings.Contains(err.Error(), "sensitive") {
			t.Fatal("invalid timestamp accepted or exposed", err)
		}
	}
	if v := TimeValue("sqlite", want.Truncate(time.Second)); v != "2026-11-19 03:04:05" {
		t.Fatal(v)
	}
	if v, ok := TimeValue("mysql", want).(time.Time); !ok || v.Location() != time.UTC || !v.Equal(want) {
		t.Fatal(v)
	}
}

func TestSQLiteClockAndConstraintClassification(t *testing.T) {
	s := sqliteTestStore(t)
	ctx := context.Background()
	if _, err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now, err := DBNow(ctx, s.DB, "sqlite")
	if err != nil || time.Since(now) < -time.Second || time.Since(now) > 2*time.Second {
		t.Fatal(now, err)
	}
	_, err = s.DB.Exec("INSERT INTO groups(group_id,group_name) VALUES(0,'duplicate')")
	if !IsUniqueViolation(fmt.Errorf("wrapped: %w", err)) || IsForeignKeyViolation(err) || IsBusy(err) {
		t.Fatal(err)
	}
	_, err = s.DB.Exec("INSERT INTO devices(group_id,device_name) VALUES(98765,'orphan')")
	if !IsForeignKeyViolation(err) || IsUniqueViolation(err) || IsBusy(err) {
		t.Fatal(err)
	}
	_, err = s.DB.Exec("INSERT INTO devices(group_id,device_name) VALUES(0,'%')")
	if !IsUniqueViolation(err) {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		code             uint16
		unique, fk, busy bool
	}{{1062, true, false, false}, {1451, false, true, false}, {1452, false, true, false}, {1205, false, false, true}, {1213, false, false, true}, {1045, false, false, false}} {
		err := fmt.Errorf("wrapped: %w", &mysql.MySQLError{Number: tc.code})
		if IsUniqueViolation(err) != tc.unique || IsForeignKeyViolation(err) != tc.fk || IsBusy(err) != tc.busy {
			t.Fatal(tc)
		}
	}
	for _, err := range []error{nil, errors.New("constraint"), context.Canceled} {
		if IsUniqueViolation(err) || IsForeignKeyViolation(err) || IsBusy(err) {
			t.Fatal(err)
		}
	}
	if LockClause("sqlite", false) != "" || LockClause("sqlite", true) != "" || LockClause("mysql", false) != " FOR UPDATE" || LockClause("mysql", true) != " FOR UPDATE SKIP LOCKED" {
		t.Fatal("dialect lock clauses changed")
	}
}

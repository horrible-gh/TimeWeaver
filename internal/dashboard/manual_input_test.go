package dashboard

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/horrible-gh/TimeWeaver/internal/access"
)

func requireManualInputCode(t *testing.T, err error, code string) {
	t.Helper()
	var got *access.Error
	if !errors.As(err, &got) || got.Status != 422 || got.Code != code {
		t.Fatalf("wanted 422/%s, got %v", code, err)
	}
}

func TestManualTimeRequiresAnOffsetAndServerOwnedState(t *testing.T) {
	now := time.Date(2026, 11, 18, 1, 2, 3, 0, time.UTC)
	in := ManualInput{ScheduleDatetime: "2026-11-18T12:34:56.789+09:00", Status: "wait"}
	due, err := in.runTime(now)
	if err != nil || due.Format(time.RFC3339Nano) != "2026-11-18T03:34:56Z" || due.Location() != time.UTC {
		t.Fatal("reservation lost its offset or database precision", due, err)
	}
	in.Immediate, in.ScheduleDatetime = true, "old modal value"
	due, err = in.runTime(now.Add(123 * time.Millisecond))
	if err != nil || !due.Equal(now) {
		t.Fatal("immediate run used a caller supplied date", due, err)
	}
	for _, status := range []string{"active", "processing", "done", "failed", "inactive", "WAIT"} {
		in.Status = status
		_, err = in.runTime(now)
		requireManualInputCode(t, err, "invalid_request")
	}
	for _, value := range []string{"", "2026-11-18T12:34:56", "2026-11-18 12:34:56", "2026-02-30T12:00:00Z", "2026-11-18T01:02:03Z", "2026-11-17T12:00:00Z", "1000-01-01T00:00:00+01:00", "9999-12-31T23:59:59-01:00", strings.Repeat("x", 65)} {
		_, err = (ManualInput{ScheduleDatetime: value}).runTime(now)
		requireManualInputCode(t, err, "invalid_datetime")
	}
}

func TestHistoryRangeUsesUTCAndBoundedInclusiveDates(t *testing.T) {
	now := time.Date(2026, 11, 18, 10, 0, 0, 0, time.FixedZone("test", 9*3600))
	from, to, err := historyRange("", "", now)
	if err != nil || !to.Equal(now) || from.Location() != time.UTC || to.Sub(from) != 30*24*time.Hour {
		t.Fatal("default history interval", from, to, err)
	}
	from, to, err = historyRange("2025-01-01T09:00:00+09:00", "2026-01-02T00:00:00Z", now)
	if err != nil || from.Format(time.RFC3339) != "2025-01-01T00:00:00Z" || to.Sub(from) != 366*24*time.Hour {
		t.Fatal("history must accept exactly 366 days", from, to, err)
	}
	if _, _, err = historyRange("2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z", now); err != nil {
		t.Fatal("inclusive equal bounds", err)
	}
	for _, bounds := range [][2]string{{"2026-01-02T00:00:00Z", "2026-01-01T00:00:00Z"}, {"2025-01-01T00:00:00Z", "2026-01-02T00:00:01Z"}, {"", "1000-01-01T00:00:00Z"}} {
		_, _, err = historyRange(bounds[0], bounds[1], now)
		requireManualInputCode(t, err, "invalid_date_range")
	}
	_, _, err = historyRange("2026-01-01T00:00", "", now)
	requireManualInputCode(t, err, "invalid_datetime")
}

func TestHistoryQueryPreservesRecordedIdentityAndFailsClosed(t *testing.T) {
	for _, role := range []string{"admin", "user"} {
		q, args, err := historyQuery(access.User{Role: role})
		if err != nil {
			t.Fatal("frozen catalog query no longer matches", err)
		}
		for _, fragment := range []string{"sd.detail_id", "sh.schedule_id = el.schedule_id", "scope_sg.schedule_id=el.schedule_id", "scope_device.device_id=scope_sg.target_device", "JSON_VALID(el.environment_info)", "el.execution_grp_id AS execution_uuid", "el.attempt", "el.manual_id", "LIMIT 1001"} {
			if !strings.Contains(q, fragment) {
				t.Fatalf("missing historical ownership invariant %q", fragment)
			}
		}
		if strings.Contains(q, "deleted_at IS NULL") || strings.Contains(q, "AND \nORDER BY") || len(args) != 0 || strings.Count(q, "?") != 2 {
			t.Fatal("query discarded tombstones or left an unbound clause", q, args)
		}
		if role == "user" && (!strings.Contains(q, "scope_sg.schedule_id>0 AND scope_device.device_id>0") || !strings.Contains(q, "(1=0)")) {
			t.Fatal("unresolved ownership or an unauthenticated identity could grant a scope", q)
		}
	}
}

func TestHistoryQueryIsIdenticalForWindowsAndUnixCheckouts(t *testing.T) {
	raw, err := businessQueries.Get("get_execution_logs")
	if err != nil {
		t.Fatal(err)
	}
	lf := strings.ReplaceAll(raw, "\r\n", "\n")
	crlf := strings.ReplaceAll(lf, "\n", "\r\n")
	for _, role := range []string{"admin", "user"} {
		u := access.User{Role: role}
		want, wantArgs, err := scopeHistoryQuery(lf, u)
		if err != nil {
			t.Fatal(err)
		}
		got, args, err := scopeHistoryQuery(crlf, u)
		if err != nil || got != want || !reflect.DeepEqual(args, wantArgs) {
			t.Fatal("Windows line endings changed the scoped history query", err)
		}
	}
}

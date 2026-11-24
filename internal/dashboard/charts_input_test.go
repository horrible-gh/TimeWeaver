package dashboard

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/horrible-gh/TimeWeaver/internal/access"
)

var chartCatalogCases = []struct {
	kind, key string
	guard     string
}{
	{"devices", "charts.get_chart_devices_all", "WHERE device_id > 0"},
	{"schedules", "charts.get_chart_schedules", "FROM schedule_group sg"},
	{"tasks", "charts.get_chart_tasks", "WHERE sd.deleted_at IS NULL"},
	{"latest", "get_lastest_schedules", "ORDER BY li.group_latest_time DESC\nLIMIT 3"},
}

func chartCatalog(t *testing.T, key string) string {
	t.Helper()
	if businessQueryError != nil {
		t.Fatal(businessQueryError)
	}
	q, err := businessQueries.Get(key)
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(q, "\r\n", "\n")
}

func requireChartUnavailable(t *testing.T, err error) {
	t.Helper()
	var got *access.Error
	if !errors.As(err, &got) || got.Status != 503 || got.Code != "unavailable" {
		t.Fatalf("expected fail-closed 503/unavailable, got %v", err)
	}
}

func TestChartQueriesDoNotGrantScopeFromPublicUserClaims(t *testing.T) {
	// Active identities are created by the access service and covered by the
	// database integration tests. Public role/group fields alone cannot create
	// an active principal or cause these readers to bypass GroupFilter.
	principals := []access.User{
		{},
		{Role: "admin"},
		{Role: "user", Groups: []access.Membership{{GroupID: 0, Role: "editor"}}},
		{Role: "user", Groups: []access.Membership{{GroupID: 7, Role: "viewer"}, {GroupID: 8, Role: "editor"}}},
		{Role: "admin", MustChangePassword: true},
	}
	for _, tc := range chartCatalogCases {
		t.Run(tc.kind, func(t *testing.T) {
			for _, u := range principals {
				q, args, err := chartQuery(tc.kind, u)
				if err != nil || len(args) != 0 || strings.Contains(q, "1=1") || strings.Contains(q, "?") {
					t.Fatal("unauthenticated claims produced a grant or unbound query", args, err)
				}
				if tc.kind == "devices" {
					if !strings.Contains(q, "device_id > 0 AND (1=0)") {
						t.Fatal("device scope or sentinel guard missing")
					}
				} else if !strings.Contains(q, "sg.schedule_id > 0 AND d.device_id > 0") || !strings.Contains(q, "(1=0) AND (1=0)") {
					t.Fatal("both recorded resource and target-device scopes are required")
				}
			}
		})
	}
}

func TestChartQueriesMatchWindowsAndUnixCatalogs(t *testing.T) {
	for _, tc := range chartCatalogCases {
		t.Run(tc.kind, func(t *testing.T) {
			lf := chartCatalog(t, tc.key)
			crlf := strings.ReplaceAll(lf, "\n", "\r\n")
			want, wantArgs, err := scopeChartQuery(tc.kind, lf, access.User{})
			if err != nil {
				t.Fatal("frozen catalog no longer matches its guarded reader", err)
			}
			got, args, err := scopeChartQuery(tc.kind, crlf, access.User{})
			if err != nil || got != want || !reflect.DeepEqual(args, wantArgs) || strings.Contains(got, "\r") {
				t.Fatal("Windows line endings changed authorization or aggregation", err)
			}
		})
	}
}

func TestChartQueryRejectsMissingOrAmbiguousScopeInsertion(t *testing.T) {
	for _, tc := range chartCatalogCases {
		t.Run(tc.kind, func(t *testing.T) {
			raw := chartCatalog(t, tc.key)
			if strings.Count(raw, tc.guard) != 1 {
				t.Fatal("test no longer exercises a unique catalog boundary")
			}
			for _, broken := range []string{
				strings.Replace(raw, tc.guard, "/* changed catalog boundary */", 1),
				raw + "\n" + tc.guard,
				"",
			} {
				q, args, err := scopeChartQuery(tc.kind, broken, access.User{})
				requireChartUnavailable(t, err)
				if q != "" || args != nil {
					t.Fatal("failed guard returned an executable partial query")
				}
			}
		})
	}
	for _, kind := range []string{"", "device", "LATEST", "tasks; SELECT 1"} {
		_, _, err := chartQuery(kind, access.User{})
		requireChartUnavailable(t, err)
		_, _, err = scopeChartQuery(kind, "SELECT 1", access.User{})
		requireChartUnavailable(t, err)
	}
}

func TestTaskChartHistoryCannotFollowAMovedDetail(t *testing.T) {
	q, _, err := chartQuery("tasks", access.User{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(q, "PARTITION BY el.schedule_id, el.detail_id ORDER BY el.start_time DESC, el.execution_id DESC") || !strings.Contains(q, "ON el.detail_id = sd.detail_id AND el.schedule_id = sd.schedule_id") {
		t.Fatal("task history lost its recorded parent or deterministic latest-result order")
	}
	raw := chartCatalog(t, "charts.get_chart_tasks")
	for _, guard := range []string{"PARTITION BY el.detail_id ORDER BY el.start_time DESC", "ON el.detail_id = sd.detail_id"} {
		for _, broken := range []string{strings.Replace(raw, guard, "/* changed history relation */", 1), raw + "\n" + guard} {
			_, _, err = scopeChartQuery("tasks", broken, access.User{})
			requireChartUnavailable(t, err)
		}
	}
}

func TestLatestScheduleScopePrecedesLimitAndFailureIsChronological(t *testing.T) {
	q, _, err := chartQuery("latest", access.User{})
	if err != nil {
		t.Fatal(err)
	}
	scopeAt := strings.LastIndex(q, "WHERE sg.schedule_id > 0 AND d.device_id > 0")
	orderAt := strings.LastIndex(q, "ORDER BY li.group_latest_time DESC")
	limitAt := strings.LastIndex(q, "LIMIT 3")
	if scopeAt < 0 || orderAt <= scopeAt || limitAt <= orderAt {
		t.Fatal("unseen schedules could consume the recent-result limit")
	}
	for _, fragment := range []string{
		"PARTITION BY schedule_id ORDER BY group_latest_time DESC, group_latest_id DESC",
		"PARTITION BY el.schedule_id ORDER BY CASE WHEN el.result_code != 0 THEN 0 ELSE 1 END, el.start_time, el.execution_id",
		"WHERE el.result_code != 0 OR " + housekeepWarningSQL,
		"WHERE error_rank = 1",
		"fep.schedule_id IS NOT NULL THEN 'warning'",
	} {
		if !strings.Contains(q, fragment) {
			t.Fatalf("missing instance/failure invariant %q", fragment)
		}
	}
	if strings.Contains(q, "MIN(raw_error)") || strings.Contains(q, "WHERE el.result_code = -1") || strings.Contains(q, "fep.raw_error IS NOT NULL THEN 'warning'") || strings.Contains(q, "el.start_time >=") {
		t.Fatal("latest instance borrowed a legacy failure bug or a one-day chart cutoff")
	}
}

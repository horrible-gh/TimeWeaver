package dashboard

import (
	"context"
	"database/sql"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/horrible-gh/TimeWeaver/internal/access"
)

type historyPageInput struct {
	start, end, group, schedule, sort, direction string
	page                                         int64
	size                                         int
	code                                         *int64
	options                                      bool
}

func parseHistoryPage(v url.Values) (historyPageInput, error) {
	in := historyPageInput{start: v.Get("start_date"), end: v.Get("end_date"), group: v.Get("group"), schedule: v.Get("schedule"), sort: v.Get("sort"), direction: v.Get("direction"), page: 1, size: 20}
	var err error
	if raw := v.Get("page"); raw != "" {
		in.page, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || in.page < 1 {
			return in, fail(422, "invalid_request")
		}
	}
	if raw := v.Get("page_size"); raw != "" {
		in.size, err = strconv.Atoi(raw)
		if err != nil || in.size < 1 || in.size > 100 {
			return in, fail(422, "invalid_request")
		}
	}
	if in.sort == "" {
		in.sort = "execution_id"
	}
	switch in.sort {
	case "execution_id", "sg_name", "schedule_name", "start_time", "end_time", "result_code", "result_message":
	default:
		return in, fail(422, "invalid_request")
	}
	if in.direction == "" {
		in.direction = "desc"
	}
	if in.direction != "asc" && in.direction != "desc" {
		return in, fail(422, "invalid_request")
	}
	for _, value := range []string{in.group, in.schedule} {
		if len(value) > 1024 || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
			return in, fail(422, "invalid_request")
		}
	}
	if raw := v.Get("result_code"); raw != "" {
		code, err := strconv.ParseInt(raw, 10, 32)
		if err != nil {
			return in, fail(422, "invalid_request")
		}
		in.code = &code
	}
	switch v.Get("include_options") {
	case "", "0":
	case "1":
		in.options = true
	default:
		return in, fail(422, "invalid_request")
	}
	return in, nil
}

type historyOption struct {
	Group    string `json:"sg_name"`
	Schedule string `json:"schedule_name"`
	Code     *int64 `json:"result_code"`
}

type historyPage struct {
	Items   []map[string]any `json:"items"`
	Total   int64            `json:"total"`
	Page    int64            `json:"page"`
	Size    int              `json:"page_size"`
	Options []historyOption  `json:"options,omitempty"`
}

// Reuse the authorization- and recorded-parent-aware query, not a separate
// unscoped execution_log query. Remove only the legacy array's final bound.
func historyPageCTE(query string) (string, error) {
	base, ok := strings.CutSuffix(query, "\nORDER BY el.execution_id DESC LIMIT 1001")
	if !ok {
		return "", fail(503, "unavailable")
	}
	const boundary = "\nSELECT\n    el.execution_id"
	if strings.Count(base, boundary) != 1 {
		return "", fail(503, "unavailable")
	}
	prefix, tail, _ := strings.Cut(base, boundary)
	return prefix + ", history_rows AS (" + boundary + tail + "\n)\n", nil
}

func (s *Service) executionHistoryPage(ctx context.Context, token string, in historyPageInput) (historyPage, error) {
	out := historyPage{Items: []map[string]any{}, Page: in.page, Size: in.size}
	err := s.access.WithRead(ctx, token, func(tx *sql.Tx, u access.User) error {
		now, err := s.dashboardNow(ctx, tx)
		if err != nil {
			return err
		}
		from, to, err := historyRange(in.start, in.end, now)
		if err != nil {
			return err
		}
		q, scopeArgs, err := historyQueryDialect(s.access.Dialect(), u)
		if err != nil {
			return err
		}
		cte, err := historyPageCTE(q)
		if err != nil {
			return err
		}
		baseArgs := append([]any{s.timeValue(from), s.timeValue(to)}, scopeArgs...)
		where := " FROM history_rows WHERE 1=1"
		args := append([]any(nil), baseArgs...)
		if in.group != "" {
			where += " AND sg_name=?"
			args = append(args, in.group)
		}
		if in.schedule != "" {
			where += " AND schedule_name=?"
			args = append(args, in.schedule)
		}
		if in.code != nil {
			where += " AND result_code=?"
			args = append(args, *in.code)
		}
		if err = tx.QueryRowContext(ctx, cte+"SELECT COUNT(*)"+where, args...).Scan(&out.Total); err != nil {
			return dbError(err)
		}
		last := int64(1)
		if out.Total > 0 {
			last = (out.Total-1)/int64(in.size) + 1
		}
		if out.Page > last {
			out.Page = last
		}
		// Sort fields and directions are validated against a fixed allowlist.
		// The execution ID tie-breaker makes pages deterministic.
		order := " ORDER BY " + in.sort + " " + in.direction
		if in.sort != "execution_id" {
			order += ", execution_id DESC"
		}
		pageArgs := append(args, in.size, (out.Page-1)*int64(in.size))
		out.Items, err = readHistoryRows(ctx, tx, cte+"SELECT *"+where+order+" LIMIT ? OFFSET ?", pageArgs, in.size)
		if err != nil {
			return err
		}
		if !in.options {
			return nil
		}
		// Only distinct filter values are returned for the entire authorized
		// date range, never an unbounded collection of execution logs.
		rows, err := tx.QueryContext(ctx, cte+"SELECT DISTINCT sg_name,schedule_name,result_code FROM history_rows ORDER BY sg_name,schedule_name,result_code", baseArgs...)
		if err != nil {
			return dbError(err)
		}
		defer rows.Close()
		for rows.Next() {
			var g, name sql.NullString
			var code sql.NullInt64
			if err = rows.Scan(&g, &name, &code); err != nil {
				return dbError(err)
			}
			option := historyOption{Group: g.String, Schedule: name.String}
			if code.Valid {
				value := code.Int64
				option.Code = &value
			}
			out.Options = append(out.Options, option)
		}
		if err = rows.Err(); err != nil {
			return dbError(err)
		}
		return nil
	})
	return out, err
}

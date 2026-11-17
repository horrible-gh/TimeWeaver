package dashboard

import (
	"net/http"

	"github.com/horrible-gh/TimeWeaver/internal/access"
)

func (s *Service) manualHTTP(w http.ResponseWriter, r *http.Request, path string) bool {
	method := ""
	switch path {
	case "/dashboard/manual_execution/get_manual_execution_list", "/dashboard/schedule/execution_history":
		method = "GET"
	case "/dashboard/schedule/insert_manual_schedule", "/dashboard/tasks/insert_manual_task":
		method = "POST"
	case "/dashboard/manual_execution/update_manual_execution", "/dashboard/manual_execution/abandon_manual_execution":
		method = "PUT"
	}
	if method == "" {
		return false
	}
	if r.Method != method {
		w.Header().Set("Allow", method)
		access.ReplyError(w, fail(405, "method_not_allowed"))
		return true
	}
	token, csrf := access.Credentials(r)
	var out any
	var err error
	switch path {
	case "/dashboard/manual_execution/get_manual_execution_list":
		out, err = s.ManualRuns(r.Context(), token)
	case "/dashboard/schedule/execution_history":
		out, err = s.ExecutionHistory(r.Context(), token, r.URL.Query().Get("start_date"), r.URL.Query().Get("end_date"))
	case "/dashboard/manual_execution/abandon_manual_execution":
		var in struct {
			ID       int64  `json:"manual_id"`
			Modifier string `json:"modifier"`
		}
		if err = decode(w, r, &in); err == nil {
			err = s.CancelManual(r.Context(), token, csrf, in.ID)
			out = map[string]any{"ok": true, "manual_id": in.ID}
		}
	default:
		var in ManualInput
		if err = decode(w, r, &in); err != nil {
			break
		}
		if path == "/dashboard/manual_execution/update_manual_execution" {
			var id int64
			id, err = s.UpdateManual(r.Context(), token, csrf, in)
			out = map[string]any{"ok": true, "manual_id": id, "previous_manual_id": in.ManualID}
		} else if (path == "/dashboard/schedule/insert_manual_schedule" && (in.ScheduleID == 0 || in.DetailID != "")) || (path == "/dashboard/tasks/insert_manual_task" && (in.DetailID == "" || in.ScheduleID != 0)) {
			err = fail(422, "invalid_request")
		} else {
			var ids []int64
			ids, err = s.QueueManual(r.Context(), token, csrf, in)
			out = map[string]any{"ok": true, "manual_ids": ids, "count": len(ids)}
		}
	}
	if err != nil {
		access.ReplyError(w, err)
	} else {
		reply(w, 200, out)
	}
	return true
}

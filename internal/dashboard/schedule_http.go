package dashboard

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/horrible-gh/TimeWeaver/internal/access"
)

// Called inside the existing human transport/Origin guard. Each service method
// authenticates again inside the read/write transaction; no JWT fallback exists.
func (s *Service) scheduleHTTP(w http.ResponseWriter, r *http.Request, path string) bool {
	method := ""
	switch path {
	case "/dashboard/schedule/get_schedule_groups", "/dashboard/tasks/get_schedule_groups", "/dashboard/tasks/get_tasks":
		method = "GET"
	case "/dashboard/schedule/insert_schedule", "/dashboard/tasks/insert_task":
		method = "POST"
	case "/dashboard/schedule/update_schedule", "/dashboard/tasks/update_task":
		method = "PUT"
	default:
		if strings.HasPrefix(path, "/dashboard/schedule/remove_schedule/") || strings.HasPrefix(path, "/dashboard/tasks/remove_task/") {
			method = "DELETE"
		}
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
	case "/dashboard/schedule/get_schedule_groups", "/dashboard/tasks/get_schedule_groups":
		out, err = s.Schedules(r.Context(), token)
	case "/dashboard/tasks/get_tasks":
		out, err = s.Tasks(r.Context(), token)
	case "/dashboard/schedule/insert_schedule", "/dashboard/schedule/update_schedule":
		in := ScheduleInput{Status: "active"}
		if err = decode(w, r, &in); err == nil {
			var id int64
			id, err = s.SaveSchedule(r.Context(), token, csrf, in, method == "POST")
			out = map[string]any{"ok": true, "schedule_id": id}
		}
	case "/dashboard/tasks/insert_task", "/dashboard/tasks/update_task":
		in := TaskInput{Status: "active", Type: "command", MissingError: true}
		if err = decode(w, r, &in); err == nil {
			var id string
			id, err = s.SaveTask(r.Context(), token, csrf, in, method == "POST")
			out = map[string]any{"ok": true, "detail_id": id}
		}
	default:
		if strings.HasPrefix(path, "/dashboard/schedule/remove_schedule/") {
			var id int64
			id, err = strconv.ParseInt(strings.TrimPrefix(path, "/dashboard/schedule/remove_schedule/"), 10, 32)
			if err != nil {
				err = fail(422, "invalid_request")
			} else {
				err = s.DeleteSchedule(r.Context(), token, csrf, id)
			}
		} else {
			err = s.DeleteTask(r.Context(), token, csrf, strings.TrimPrefix(path, "/dashboard/tasks/remove_task/"))
		}
		out = map[string]bool{"ok": true}
	}
	if err != nil {
		access.ReplyError(w, err)
	} else {
		reply(w, 200, out)
	}
	return true
}

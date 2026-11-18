package dashboard

import (
	"net/http"

	"github.com/horrible-gh/TimeWeaver/internal/access"
)

func (s *Service) chartHTTP(w http.ResponseWriter, r *http.Request, path string) bool {
	switch path {
	case "/dashboard/charts/devices", "/dashboard/charts/schedules", "/dashboard/charts/tasks", "/dashboard/lastest-schedules":
	default:
		return false
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		access.ReplyError(w, fail(405, "method_not_allowed"))
		return true
	}
	token, _ := access.Credentials(r)
	var out any
	var err error
	switch path {
	case "/dashboard/charts/devices":
		out, err = s.DeviceChart(r.Context(), token)
	case "/dashboard/charts/schedules":
		out, err = s.ScheduleChart(r.Context(), token)
	case "/dashboard/charts/tasks":
		out, err = s.TaskChart(r.Context(), token)
	case "/dashboard/lastest-schedules":
		out, err = s.LatestSchedules(r.Context(), token)
	}
	if err != nil {
		access.ReplyError(w, err)
	} else {
		reply(w, http.StatusOK, out)
	}
	return true
}

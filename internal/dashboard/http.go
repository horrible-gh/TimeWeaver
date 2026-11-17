// Package dashboard exposes human-authorized business data, never device JWTs.
package dashboard

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/horrible-gh/TimeWeaver/internal/access"
)

func decode(w http.ResponseWriter, r *http.Request, out any) error {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return fail(415, "unsupported_media_type")
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err = dec.Decode(out); err != nil {
		return fail(422, "invalid_request")
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return fail(422, "invalid_request")
	}
	return nil
}
func reply(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func envelope(value any) any {
	return struct {
		Version string    `json:"schema_version"`
		Time    time.Time `json:"server_time"`
		Data    any       `json:"data"`
	}{"1", time.Now().UTC(), value}
}
func NewHTTP(service *access.Service, guard *access.HTTP, base string) http.Handler {
	s := New(service)
	return guard.Guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(strings.TrimSuffix(r.URL.Path, "/"), base)
		allowed := ""
		switch path {
		case "/dashboard/devices/get_devices", "/dashboard/schedule/get_devices":
			allowed = "GET"
		case "/dashboard/devices/insert_device":
			allowed = "POST"
		case "/dashboard/devices/update_device":
			allowed = "PUT"
		case "/dashboard/agent-enrollment-tokens":
			allowed = "GET, POST"
		default:
			if strings.HasPrefix(path, "/dashboard/devices/remove_device/") || strings.HasPrefix(path, "/dashboard/agent-enrollment-tokens/") {
				allowed = "DELETE"
			}
		}
		if allowed == "" {
			access.ReplyError(w, fail(404, "not_found"))
			return
		}
		if !strings.Contains(", "+allowed+", ", ", "+r.Method+", ") {
			w.Header().Set("Allow", allowed)
			access.ReplyError(w, fail(405, "method_not_allowed"))
			return
		}
		token, csrf := access.Credentials(r)
		ctx := r.Context()
		var result any
		var err error
		status := 200
		switch path {
		case "/dashboard/devices/get_devices", "/dashboard/schedule/get_devices":
			result, err = s.Devices(ctx, token)
		case "/dashboard/devices/insert_device", "/dashboard/devices/update_device":
			in := DeviceInput{Status: "active"}
			if err = decode(w, r, &in); err == nil {
				var id int64
				id, err = s.SaveDevice(ctx, token, csrf, in, r.Method == "POST")
				result = map[string]any{"ok": true, "device_id": id}
			}
		case "/dashboard/agent-enrollment-tokens":
			if r.Method == "GET" {
				var group *int64
				if values, ok := r.URL.Query()["group_id"]; ok {
					var n int64
					if len(values) != 1 {
						err = fail(422, "invalid_request")
					} else {
						n, err = strconv.ParseInt(values[0], 10, 32)
						if err != nil || n < 0 {
							err = fail(422, "invalid_request")
						} else {
							group = &n
						}
					}
				}
				if err == nil {
					var items []EnrollmentItem
					items, err = s.Enrollments(ctx, token, group)
					result = envelope(map[string]any{"items": items})
				}
			} else {
				in := EnrollmentInput{TTLHours: 24}
				if err = decode(w, r, &in); err == nil {
					var issue EnrollmentIssue
					issue, err = s.IssueEnrollment(ctx, token, csrf, in)
					result = envelope(issue)
					status = 201
				}
			}
		default:
			if strings.HasPrefix(path, "/dashboard/devices/remove_device/") {
				var id int64
				id, err = strconv.ParseInt(strings.TrimPrefix(path, "/dashboard/devices/remove_device/"), 10, 32)
				if err != nil {
					err = fail(422, "invalid_request")
				} else {
					err = s.DeleteDevice(ctx, token, csrf, id)
				}
				result = map[string]bool{"ok": true}
			} else {
				var revoke EnrollmentRevocation
				revoke, err = s.RevokeEnrollment(ctx, token, csrf, strings.TrimPrefix(path, "/dashboard/agent-enrollment-tokens/"))
				result = envelope(revoke)
			}
		}
		if err != nil {
			access.ReplyError(w, err)
			return
		}
		reply(w, status, result)
	}))
}

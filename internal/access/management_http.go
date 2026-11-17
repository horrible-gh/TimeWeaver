package access

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Guard applies the same transport, Origin and admission rules as login.
// Downstream handlers MUST use WithRead/WithGroup/administrative service methods
// to authenticate and authorize inside their database transaction.
func (h *HTTP) Guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.transport(r) {
			writeError(w, failure(403, "secure_origin_required"))
			return
		}
		if !h.sameOrigin(r, r.Method != "GET" && r.Method != "HEAD") {
			writeError(w, failure(403, "origin_rejected"))
			return
		}
		if r.Header.Get("Authorization") != "" {
			writeError(w, failure(401, "unauthenticated"))
			return
		}
		select {
		case h.slots <- struct{}{}:
			defer func() { <-h.slots }()
		default:
			writeError(w, unavailable())
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
func Credentials(r *http.Request) (string, string) { return tokenFrom(r), r.Header.Get("X-CSRF-Token") }
func ReplyError(w http.ResponseWriter, err error)  { writeError(w, err) }
func decodeManagement(w http.ResponseWriter, r *http.Request, out any) error {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return failure(415, "unsupported_media_type")
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if dec.Decode(out) != nil {
		return failure(422, "invalid_request")
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return failure(422, "invalid_request")
	}
	return nil
}
func (h *HTTP) management(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimSuffix(r.URL.Path, "/")
	allowed := ""
	switch p {
	case h.base + "/dashboard/users":
		allowed = "GET, POST, PUT"
	case h.base + "/dashboard/users/password":
		allowed = "POST"
	case h.base + "/dashboard/groups/get_groups":
		allowed = "GET"
	case h.base + "/dashboard/groups/insert_group":
		allowed = "POST"
	case h.base + "/dashboard/groups/update_group":
		allowed = "PUT"
	default:
		if strings.HasPrefix(p, h.base+"/dashboard/groups/remove_group/") {
			allowed = "DELETE"
		}
	}
	if allowed == "" {
		writeError(w, failure(404, "not_found"))
		return
	}
	if !strings.Contains(", "+allowed+", ", ", "+r.Method+", ") {
		w.Header().Set("Allow", allowed)
		writeError(w, failure(405, "method_not_allowed"))
		return
	}
	h.Guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		token, csrf := Credentials(r)
		var result any = map[string]bool{"ok": true}
		var err error
		passwordWork := r.Method == "POST" && (p == h.base+"/dashboard/users" || p == h.base+"/dashboard/users/password")
		if passwordWork {
			select {
			case h.passwords <- struct{}{}:
				defer func() { <-h.passwords }()
			default:
				writeError(w, unavailable())
				return
			}
		}
		switch p {
		case h.base + "/dashboard/users":
			if r.Method == "GET" {
				result, err = h.service.ListUsers(ctx, token)
				break
			}
			var body UserInput
			if err = decodeManagement(w, r, &body); err != nil {
				break
			}
			if r.Method == "POST" {
				err = h.service.CreateUser(ctx, token, csrf, body)
			} else {
				var reauth bool
				reauth, err = h.service.UpdateUser(ctx, token, csrf, body)
				result = map[string]bool{"ok": true, "reauthenticate": reauth}
			}
		case h.base + "/dashboard/users/password":
			var body struct {
				ID       string `json:"user_id"`
				Password string `json:"password"`
				Version  int64  `json:"version"`
			}
			if err = decodeManagement(w, r, &body); err == nil {
				err = h.service.ResetPassword(ctx, token, csrf, body.ID, body.Password, body.Version)
			}
		case h.base + "/dashboard/groups/get_groups":
			result, err = h.service.ListGroups(ctx, token)
		case h.base + "/dashboard/groups/insert_group", h.base + "/dashboard/groups/update_group":
			body := GroupInput{Status: "active"}
			if err = decodeManagement(w, r, &body); err == nil {
				var id int64
				id, err = h.service.SaveGroup(ctx, token, csrf, body, r.Method == "POST")
				result = map[string]any{"ok": true, "group_id": id}
			}
		default:
			id, e := strconv.ParseInt(strings.TrimPrefix(p, h.base+"/dashboard/groups/remove_group/"), 10, 32)
			if e != nil {
				err = failure(422, "invalid_request")
			} else {
				err = h.service.DeleteGroup(ctx, token, csrf, id)
			}
		}
		if err != nil {
			writeError(w, err)
			return
		}
		jsonReply(w, 200, result)
	})).ServeHTTP(w, r)
}

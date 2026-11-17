// Package dashboard exposes human-authorized business data, never device JWTs.
package dashboard

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/horrible-gh/TimeWeaver/internal/access"
)

type Device struct {
	GroupID       int64   `json:"group_id"`
	ID            int64   `json:"device_id"`
	Name          string  `json:"device_name"`
	GroupName     string  `json:"group_name"`
	Status        string  `json:"status"`
	Version       *string `json:"version"`
	LastLogin     *string `json:"last_login_at"`
	LastHeartbeat *string `json:"last_heartbeat_at"`
	Revision      *string `json:"applied_revision"`
}

func nullableString(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	return &v.String
}
func nullableDate(v sql.NullTime) *string {
	if !v.Valid {
		return nil
	}
	s := v.Time.UTC().Format(time.RFC3339Nano)
	return &s
}
func NewHTTP(service *access.Service, guard *access.HTTP, base string) http.Handler {
	return guard.Guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimSuffix(r.URL.Path, "/")
		if path != base+"/dashboard/devices/get_devices" && path != base+"/dashboard/schedule/get_devices" {
			access.ReplyError(w, &access.Error{Status: 404, Code: "not_found"})
			return
		}
		if r.Method != "GET" {
			w.Header().Set("Allow", "GET")
			access.ReplyError(w, &access.Error{Status: 405, Code: "method_not_allowed"})
			return
		}
		token, _ := access.Credentials(r)
		items := []Device{}
		err := service.WithRead(r.Context(), token, func(tx *sql.Tx, u access.User) error {
			filter, args, err := u.GroupFilter("d.group_id", "viewer")
			if err != nil {
				return err
			}
			// Legacy group_id remains accepted but cannot enlarge the server scope.
			rows, err := tx.QueryContext(r.Context(), "SELECT d.group_id,d.device_id,d.device_name,g.group_name,d.status,d.version,d.last_login_at,d.last_heartbeat_at,d.applied_revision FROM devices d JOIN `groups` g ON g.group_id=d.group_id WHERE d.device_id>0 AND "+filter+" ORDER BY d.device_id", args...)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var d Device
				var version, revision sql.NullString
				var login, heartbeat sql.NullTime
				if err = rows.Scan(&d.GroupID, &d.ID, &d.Name, &d.GroupName, &d.Status, &version, &login, &heartbeat, &revision); err != nil {
					return err
				}
				d.Version = nullableString(version)
				d.Revision = nullableString(revision)
				d.LastLogin = nullableDate(login)
				d.LastHeartbeat = nullableDate(heartbeat)
				items = append(items, d)
			}
			return rows.Err()
		})
		if err != nil {
			access.ReplyError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_ = json.NewEncoder(w).Encode(items)
	}))
}

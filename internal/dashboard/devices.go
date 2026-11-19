package dashboard

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/horrible-gh/TimeWeaver/internal/access"
	"github.com/horrible-gh/TimeWeaver/internal/store"
)

type Service struct{ access *access.Service }

func New(service *access.Service) *Service { return &Service{access: service} }
func fail(status int, code string) error   { return &access.Error{Status: status, Code: code} }
func dbError(err error) error {
	if store.IsUniqueViolation(err) {
		return fail(409, "device_name_conflict")
	}
	if store.IsForeignKeyViolation(err) {
		return fail(409, "resource_in_use")
	}
	return fail(503, "unavailable")
}
func validName(name string) bool {
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 255 {
		return false
	}
	for _, c := range name {
		if unicode.IsControl(c) {
			return false
		}
	}
	return true
}
func validID(id int64, zero bool) bool { return id > 0 && id <= 2147483647 || zero && id == 0 }
func audit(ctx context.Context, tx *sql.Tx, actor, action, target string, detail any) error {
	raw, err := json.Marshal(detail)
	if err != nil {
		return fail(503, "unavailable")
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO tw_audit(at,actor,action,result,target,detail) VALUES(?,?,?,'success',?,?)", time.Now().Unix(), actor, action, target, string(raw))
	if err != nil {
		return dbError(err)
	}
	return nil
}

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
	CanEdit       bool    `json:"can_edit"`
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
func (s *Service) Devices(ctx context.Context, token string) ([]Device, error) {
	items := []Device{}
	err := s.access.WithRead(ctx, token, func(tx *sql.Tx, u access.User) error {
		filter, args, err := u.GroupFilter("d.group_id", "viewer")
		if err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, "SELECT d.group_id,d.device_id,d.device_name,g.group_name,d.status,d.version,d.last_login_at,d.last_heartbeat_at,d.applied_revision FROM devices d JOIN `groups` g ON g.group_id=d.group_id WHERE d.device_id>0 AND "+filter+" ORDER BY d.device_id", args...)
		if err != nil {
			return dbError(err)
		}
		defer rows.Close()
		for rows.Next() {
			var d Device
			var version, revision sql.NullString
			var login, heartbeat sql.NullTime
			if err = rows.Scan(&d.GroupID, &d.ID, &d.Name, &d.GroupName, &d.Status, &version, store.ScanNullTime(&login), store.ScanNullTime(&heartbeat), &revision); err != nil {
				return dbError(err)
			}
			d.Version = nullableString(version)
			d.Revision = nullableString(revision)
			d.LastLogin = nullableDate(login)
			d.LastHeartbeat = nullableDate(heartbeat)
			d.CanEdit = u.Can(d.GroupID, "editor")
			items = append(items, d)
		}
		if err = rows.Err(); err != nil {
			return dbError(err)
		}
		return nil
	})
	return items, err
}

type DeviceInput struct {
	ID       int64  `json:"device_id"`
	GroupID  *int64 `json:"group_id"`
	Name     string `json:"device_name"`
	Status   string `json:"status"`
	Creator  string `json:"creator"`
	Modifier string `json:"modifier"`
}

func (s *Service) deviceForEdit(ctx context.Context, tx *sql.Tx, u access.User, id int64) (int64, string, error) {
	if !validID(id, false) {
		return 0, "", fail(422, "invalid_request")
	}
	var group int64
	var name string
	err := tx.QueryRowContext(ctx, "SELECT group_id,device_name FROM devices WHERE device_id=?"+s.lock(), id).Scan(&group, &name)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", fail(404, "not_found")
	}
	if err != nil {
		return 0, "", dbError(err)
	}
	// Do not expose resource fields or trust a client-supplied group.
	if !u.Can(group, "editor") {
		return 0, "", fail(403, "forbidden")
	}
	return group, name, nil
}
func (s *Service) SaveDevice(ctx context.Context, token, csrf string, in DeviceInput, create bool) (int64, error) {
	id := in.ID
	err := s.access.WithMutation(ctx, token, csrf, func(tx *sql.Tx, u access.User) error {
		in.Name = strings.TrimSpace(in.Name)
		if !validName(in.Name) || (in.Status != "active" && in.Status != "inactive") {
			return fail(422, "invalid_request")
		}
		var group int64
		action := "device_create"
		if create {
			if in.ID != 0 || in.GroupID == nil || !validID(*in.GroupID, true) {
				return fail(422, "invalid_request")
			}
			group = *in.GroupID
			if !u.Can(group, "editor") {
				return fail(403, "forbidden")
			}
			var status string
			err := tx.QueryRowContext(ctx, "SELECT status FROM `groups` WHERE group_id=?"+s.lock(), group).Scan(&status)
			if errors.Is(err, sql.ErrNoRows) {
				return fail(422, "group_not_found")
			}
			if err != nil {
				return dbError(err)
			}
			if status != "active" {
				return fail(422, "group_inactive")
			}
			result, err := tx.ExecContext(ctx, "INSERT INTO devices(group_id,device_name,status,creator,created_at) VALUES(?,?,?,?,"+s.nowSQL()+")", group, in.Name, in.Status, u.ID)
			if err != nil {
				return dbError(err)
			}
			id, err = result.LastInsertId()
			if err != nil {
				return dbError(err)
			}
		} else {
			var err error
			group, _, err = s.deviceForEdit(ctx, tx, u, id)
			if err != nil {
				return err
			}
			// The legacy update API never moved devices. Preserve that contract rather
			// than letting a forged group_id change the authorization or agent scope.
			if in.GroupID != nil && *in.GroupID != group {
				return fail(409, "device_group_changed")
			}
			if _, err = tx.ExecContext(ctx, "UPDATE devices SET device_name=?,status=?,modifier=?,modified_at="+s.nowSQL()+" WHERE device_id=?", in.Name, in.Status, u.ID, id); err != nil {
				return dbError(err)
			}
			action = "device_update"
		}
		return audit(ctx, tx, u.ID, action, strconv.FormatInt(id, 10), map[string]any{"group_id": group, "device_name": in.Name, "status": in.Status})
	})
	if err != nil {
		return 0, err
	}
	return id, nil
}
func (s *Service) DeleteDevice(ctx context.Context, token, csrf string, id int64) error {
	return s.access.WithMutation(ctx, token, csrf, func(tx *sql.Tx, u access.User) error {
		group, name, err := s.deviceForEdit(ctx, tx, u, id)
		if err != nil {
			return err
		}
		// The legacy schema has no device foreign keys. Do not orphan schedules,
		// registration/credential history or events. Registered devices are disabled,
		// not destroyed; only unused manual entries can be removed here.
		checks := []struct {
			query string
			args  []any
		}{
			{"SELECT EXISTS(SELECT 1 FROM schedule_group WHERE target_device=?)", []any{id}},
			{"SELECT EXISTS(SELECT 1 FROM agent_device_credential WHERE device_id=?)", []any{id}},
			{"SELECT EXISTS(SELECT 1 FROM agent_event WHERE device_id=?)", []any{id}},
			{"SELECT EXISTS(SELECT 1 FROM agent_enrollment_token WHERE used_by_device_id=? OR (group_id=? AND device_name=? AND used_at IS NULL AND revoked_at IS NULL AND expires_at>" + s.nowSQL() + "))", []any{id, group, name}},
		}
		for _, c := range checks {
			var exists bool
			if err = tx.QueryRowContext(ctx, c.query, c.args...).Scan(&exists); err != nil {
				return dbError(err)
			}
			if exists {
				return fail(409, "device_in_use")
			}
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM devices WHERE device_id=?", id); err != nil {
			return dbError(err)
		}
		return audit(ctx, tx, u.ID, "device_delete", strconv.FormatInt(id, 10), map[string]any{"group_id": group, "device_name": name})
	})
}

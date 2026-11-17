package access

import (
	"context"
	"database/sql"
	"strconv"
)

type Group struct {
	ID     int64  `json:"group_id"`
	Name   string `json:"group_name"`
	Status string `json:"status"`
}
type GroupInput struct {
	ID       int64  `json:"group_id"`
	Name     string `json:"group_name"`
	Status   string `json:"status"`
	Creator  string `json:"creator,omitempty"`
	Modifier string `json:"modifier,omitempty"`
}

func (s *Service) ListGroups(ctx context.Context, token string) ([]Group, error) {
	out := []Group{}
	err := s.WithRead(ctx, token, func(tx *sql.Tx, u User) error {
		filter, args, err := u.GroupFilter("group_id", "viewer")
		if err != nil {
			return unavailable()
		}
		rows, err := tx.QueryContext(ctx, "SELECT group_id,group_name,status FROM `groups` WHERE group_id>0 AND "+filter+" ORDER BY group_id", args...)
		if err != nil {
			return unavailable()
		}
		defer rows.Close()
		for rows.Next() {
			var g Group
			if rows.Scan(&g.ID, &g.Name, &g.Status) != nil {
				return unavailable()
			}
			out = append(out, g)
		}
		if rows.Err() != nil {
			return unavailable()
		}
		return nil
	})
	return out, err
}
func (s *Service) SaveGroup(ctx context.Context, token, csrf string, in GroupInput, create bool) (int64, error) {
	id := in.ID
	err := s.withRequest(ctx, token, csrf, true, true, func(tx *sql.Tx, u User) error {
		if !cleanText(in.Name, 255, false) || (in.Status != "active" && in.Status != "inactive") {
			return failure(422, "invalid_request")
		}
		action := "group_create"
		if create {
			r, err := tx.ExecContext(ctx, "INSERT INTO `groups`(group_name,status,creator,created_at) VALUES(?,?,?,UTC_TIMESTAMP())", in.Name, in.Status, u.ID)
			if err != nil {
				return adminFailure(err)
			}
			id, err = r.LastInsertId()
			if err != nil {
				return unavailable()
			}
		} else {
			if id <= 0 {
				return failure(400, "group_reserved")
			}
			if err := requireGroup(ctx, tx, id); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE `groups` SET group_name=?,status=?,modifier=?,modified_at=UTC_TIMESTAMP() WHERE group_id=?", in.Name, in.Status, u.ID, id); err != nil {
				return unavailable()
			}
			action = "group_update"
		}
		return auditChange(ctx, tx, s.now().Unix(), u.ID, action, strconv.FormatInt(id, 10), map[string]string{"name": in.Name, "status": in.Status})
	})
	return id, err
}
func (s *Service) DeleteGroup(ctx context.Context, token, csrf string, id int64) error {
	return s.withRequest(ctx, token, csrf, true, true, func(tx *sql.Tx, actor User) error {
		if id <= 0 {
			return failure(400, "group_reserved")
		}
		// Policy writers hold the global gate. Lock affected users BEFORE resources,
		// matching dashboard's user -> resource order and invalidating old sessions.
		rows, err := tx.QueryContext(ctx, "SELECT u.user_id FROM users u WHERE u.group_id=? OR EXISTS(SELECT 1 FROM tw_group_member m WHERE m.user_id=u.user_id AND m.group_id=?) ORDER BY u.user_id", id, id)
		if err != nil {
			return unavailable()
		}
		ids := []string{}
		for rows.Next() {
			var uid string
			if rows.Scan(&uid) != nil {
				rows.Close()
				return unavailable()
			}
			ids = append(ids, uid)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return unavailable()
		}
		affected := []User{}
		for _, uid := range ids {
			u, err := loadUser(ctx, tx, uid)
			if err != nil {
				return err
			}
			affected = append(affected, u)
		}
		if err = requireGroup(ctx, tx, 0); err != nil {
			return err
		}
		if err = requireGroup(ctx, tx, id); err != nil {
			return err
		}
		// Keep legacy resource reassignment, but an unused enrollment secret must
		// not suddenly enroll a new device into the global/default group.
		if _, err = tx.ExecContext(ctx, "UPDATE agent_enrollment_token SET revoked_at=COALESCE(revoked_at,UTC_TIMESTAMP()) WHERE group_id=? AND used_at IS NULL", id); err != nil {
			return unavailable()
		}
		for _, table := range []string{"devices", "schedule_group", "agent_enrollment_token"} {
			if _, err = tx.ExecContext(ctx, "UPDATE "+table+" SET group_id=0 WHERE group_id=?", id); err != nil {
				return unavailable()
			}
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM tw_group_member WHERE group_id=?", id); err != nil {
			return unavailable()
		}
		for _, u := range affected {
			remaining := []Membership{}
			for _, m := range u.Groups {
				if m.GroupID != id {
					remaining = append(remaining, m)
				}
			}
			if _, err = tx.ExecContext(ctx, "UPDATE users SET group_id=? WHERE user_id=?", defaultGroup(u.GroupID, remaining), u.ID); err != nil {
				return unavailable()
			}
			if err = invalidateUser(ctx, tx, u.ID); err != nil {
				return err
			}
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM `groups` WHERE group_id=?", id); err != nil {
			return adminFailure(err)
		}
		return auditChange(ctx, tx, s.now().Unix(), actor.ID, "group_delete", strconv.FormatInt(id, 10), map[string]any{"resource_destination": 0, "revoked_users": len(affected)})
	})
}

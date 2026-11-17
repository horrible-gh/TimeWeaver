package access

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/mail"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"
)

type AdminUser struct {
	ID                 string       `json:"user_id"`
	Name               string       `json:"name"`
	Email              string       `json:"email"`
	Role               string       `json:"role"`
	Status             string       `json:"status"`
	GroupID            int64        `json:"group_id"`
	Version            int64        `json:"version"`
	MustChangePassword bool         `json:"must_change_password"`
	Groups             []Membership `json:"groups"`
}
type UserInput struct {
	ID       string       `json:"user_id"`
	Name     string       `json:"name"`
	Email    string       `json:"email"`
	Role     string       `json:"role"`
	Status   string       `json:"status"`
	Version  int64        `json:"version"`
	Groups   []Membership `json:"groups"`
	Password string       `json:"password,omitempty"`
}

func cleanText(s string, limit int, empty bool) bool {
	if !utf8.ValidString(s) || strings.TrimSpace(s) != s || (!empty && s == "") || utf8.RuneCountInString(s) > limit {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func validUserInput(in UserInput, create bool) error {
	if !cleanText(in.ID, 50, false) || !cleanText(in.Name, 255, false) || !cleanText(in.Email, 100, true) || (in.Role != "user" && in.Role != "admin") || (in.Status != "active" && in.Status != "disabled") || len(in.Groups) > 128 || (!create && (in.Version < 1 || in.Password != "")) {
		return failure(422, "invalid_request")
	}
	if in.Email != "" {
		a, err := mail.ParseAddress(in.Email)
		if err != nil || a.Address != in.Email {
			return failure(422, "invalid_request")
		}
	}
	seen := map[int64]bool{}
	for _, m := range in.Groups {
		if m.GroupID < 0 || m.GroupID > 2147483647 || rank(m.Role) == 0 || seen[m.GroupID] {
			return failure(422, "invalid_membership")
		}
		seen[m.GroupID] = true
	}
	return nil
}
func adminFailure(err error) error {
	var my *mysql.MySQLError
	if errors.As(err, &my) {
		if my.Number == 1062 {
			return failure(409, "already_exists")
		}
		if my.Number == 1451 || my.Number == 1452 {
			return failure(409, "resource_in_use")
		}
	}
	return unavailable()
}
func auditChange(ctx context.Context, tx *sql.Tx, now int64, actor, action, target string, detail any) error {
	data, err := json.Marshal(detail)
	if err != nil {
		return unavailable()
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO tw_audit(at,actor,action,result,target,detail) VALUES(?,?,?,'success',?,?)", now, actor, action, target, string(data))
	if err != nil {
		return unavailable()
	}
	return nil
}
func requireGroup(ctx context.Context, tx *sql.Tx, id int64) error {
	var found int64
	err := tx.QueryRowContext(ctx, "SELECT group_id FROM `groups` WHERE group_id=? FOR UPDATE", id).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return failure(422, "group_not_found")
	}
	if err != nil {
		return unavailable()
	}
	return nil
}
func replaceMemberships(ctx context.Context, tx *sql.Tx, id string, groups []Membership) error {
	copy := append([]Membership(nil), groups...)
	sort.Slice(copy, func(i, j int) bool { return copy[i].GroupID < copy[j].GroupID })
	for _, m := range copy {
		if err := requireGroup(ctx, tx, m.GroupID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM tw_group_member WHERE user_id=?", id); err != nil {
		return unavailable()
	}
	for _, m := range copy {
		if _, err := tx.ExecContext(ctx, "INSERT INTO tw_group_member(user_id,group_id,role) VALUES(?,?,?)", id, m.GroupID, m.Role); err != nil {
			return adminFailure(err)
		}
	}
	return nil
}

// Default-group metadata is not a grant. An empty membership list never gains
// the global group-zero membership, including after group deletion.
func defaultGroup(previous int64, groups []Membership) int64 {
	result := int64(0)
	for _, m := range groups {
		if m.GroupID == 0 {
			return 0
		}
		if result == 0 || m.GroupID < result {
			result = m.GroupID
		}
	}
	for _, m := range groups {
		if m.GroupID == previous {
			return previous
		}
	}
	return result
}
func invalidateUser(ctx context.Context, tx *sql.Tx, id string) error {
	if _, err := tx.ExecContext(ctx, "UPDATE tw_user_auth SET version=version+1 WHERE user_id=?", id); err != nil {
		return unavailable()
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM tw_session WHERE user_id=?", id); err != nil {
		return unavailable()
	}
	return nil
}
func targetUser(ctx context.Context, tx *sql.Tx, id string) (User, error) {
	u, err := loadUser(ctx, tx, id)
	var e *Error
	if errors.As(err, &e) && e.Code == "invalid_credentials" {
		return u, failure(404, "user_not_found")
	}
	return u, err
}
func (s *Service) ListUsers(ctx context.Context, token string) ([]AdminUser, error) {
	out := []AdminUser{}
	err := s.WithRead(ctx, token, func(tx *sql.Tx, u User) error {
		if u.Role != "admin" {
			return failure(403, "forbidden")
		}
		rows, err := tx.QueryContext(ctx, "SELECT u.user_id,u.name,COALESCE(u.email,''),u.role,u.group_id,a.status,a.version,a.must_change_password FROM users u JOIN tw_user_auth a ON a.user_id=u.user_id ORDER BY u.user_id")
		if err != nil {
			return unavailable()
		}
		for rows.Next() {
			v := AdminUser{Groups: []Membership{}}
			if rows.Scan(&v.ID, &v.Name, &v.Email, &v.Role, &v.GroupID, &v.Status, &v.Version, &v.MustChangePassword) != nil {
				rows.Close()
				return unavailable()
			}
			out = append(out, v)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return unavailable()
		}
		indices := map[string]int{}
		for i, v := range out {
			indices[v.ID] = i
		}
		rows, err = tx.QueryContext(ctx, "SELECT user_id,group_id,role FROM tw_group_member ORDER BY user_id,group_id")
		if err != nil {
			return unavailable()
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			var m Membership
			if rows.Scan(&id, &m.GroupID, &m.Role) != nil {
				return unavailable()
			}
			if i, ok := indices[id]; ok {
				out[i].Groups = append(out[i].Groups, m)
			}
		}
		if rows.Err() != nil {
			return unavailable()
		}
		return nil
	})
	return out, err
}
func (s *Service) checkAdmin(ctx context.Context, token, csrf string) error {
	p, err := s.Session(ctx, token, false)
	if err != nil {
		return err
	}
	if !equalSecret(p.CSRF, csrf) {
		return failure(403, "csrf_failed")
	}
	if p.User.MustChangePassword {
		return failure(403, "password_change_required")
	}
	if p.User.Role != "admin" {
		return failure(403, "forbidden")
	}
	return nil
}
func (s *Service) CreateUser(ctx context.Context, token, csrf string, in UserInput) error {
	if err := s.checkAdmin(ctx, token, csrf); err != nil {
		return err
	}
	if err := validUserInput(in, true); err != nil {
		return err
	}
	hash, err := hashPassword(in.Password)
	if err != nil {
		return err
	}
	return s.withRequest(ctx, token, csrf, true, true, func(tx *sql.Tx, actor User) error {
		// User/auth/memberships/audit are committed together or not at all.
		_, err := tx.ExecContext(ctx, "INSERT INTO users(user_id,name,email,password,role,group_id) VALUES(?,?,?,?,?,0)", in.ID, in.Name, in.Email, hash, in.Role)
		if err != nil {
			return adminFailure(err)
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO tw_user_auth(user_id,status,must_change_password) VALUES(?,?,TRUE)", in.ID, in.Status); err != nil {
			return unavailable()
		}
		if err = replaceMemberships(ctx, tx, in.ID, in.Groups); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE users SET group_id=? WHERE user_id=?", defaultGroup(0, in.Groups), in.ID); err != nil {
			return unavailable()
		}
		return auditChange(ctx, tx, s.now().Unix(), actor.ID, "user_create", in.ID, map[string]any{"role": in.Role, "status": in.Status, "groups": in.Groups})
	})
}
func (s *Service) UpdateUser(ctx context.Context, token, csrf string, in UserInput) (bool, error) {
	reauth := false
	err := s.withRequest(ctx, token, csrf, true, true, func(tx *sql.Tx, actor User) error {
		if err := validUserInput(in, false); err != nil {
			return err
		}
		old, err := targetUser(ctx, tx, in.ID)
		if err != nil {
			return err
		}
		if old.version != in.Version {
			return failure(409, "stale_user")
		}
		if old.Role == "admin" && old.status == "active" && !old.MustChangePassword && (in.Role != "admin" || in.Status != "active") {
			var remaining int
			if tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM users u JOIN tw_user_auth a ON a.user_id=u.user_id WHERE u.role='admin' AND a.status='active' AND a.must_change_password=FALSE AND BINARY u.password<>BINARY ? AND u.user_id<>?", legacySeedHash, old.ID).Scan(&remaining) != nil {
				return unavailable()
			}
			if remaining == 0 {
				return failure(409, "last_admin")
			}
		}
		if err = replaceMemberships(ctx, tx, old.ID, in.Groups); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE users SET name=?,email=?,role=?,group_id=? WHERE user_id=?", in.Name, in.Email, in.Role, defaultGroup(old.GroupID, in.Groups), old.ID); err != nil {
			return unavailable()
		}
		if _, err = tx.ExecContext(ctx, "UPDATE tw_user_auth SET status=? WHERE user_id=?", in.Status, old.ID); err != nil {
			return unavailable()
		}
		if err = invalidateUser(ctx, tx, old.ID); err != nil {
			return err
		}
		reauth = actor.ID == old.ID
		return auditChange(ctx, tx, s.now().Unix(), actor.ID, "user_update", old.ID, map[string]any{"role": in.Role, "status": in.Status, "groups": in.Groups})
	})
	return reauth, err
}
func (s *Service) ResetPassword(ctx context.Context, token, csrf, id, password string, version int64) error {
	if err := s.checkAdmin(ctx, token, csrf); err != nil {
		return err
	}
	if !cleanText(id, 50, false) || version < 1 {
		return failure(422, "invalid_request")
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	return s.withRequest(ctx, token, csrf, true, true, func(tx *sql.Tx, actor User) error {
		old, err := targetUser(ctx, tx, id)
		if err != nil {
			return err
		}
		if old.ID == actor.ID {
			return failure(409, "use_password_change")
		}
		if old.version != version {
			return failure(409, "stale_user")
		}
		if _, err = tx.ExecContext(ctx, "UPDATE users SET password=? WHERE user_id=?", hash, old.ID); err != nil {
			return unavailable()
		}
		if _, err = tx.ExecContext(ctx, "UPDATE tw_user_auth SET must_change_password=TRUE,password_changed_at=? WHERE user_id=?", s.now().Unix(), old.ID); err != nil {
			return unavailable()
		}
		if err = invalidateUser(ctx, tx, old.ID); err != nil {
			return err
		}
		return auditChange(ctx, tx, s.now().Unix(), actor.ID, "password_reset", old.ID, map[string]bool{"must_change_password": true})
	})
}

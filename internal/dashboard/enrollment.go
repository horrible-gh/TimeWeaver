package dashboard

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/horrible-gh/TimeWeaver/internal/access"
)

type EnrollmentInput struct {
	GroupID  *int64  `json:"group_id"`
	Name     *string `json:"device_name"`
	TTLHours int     `json:"ttl_hours"`
}
type EnrollmentIssue struct {
	ID      string    `json:"enrollment_id"`
	Token   string    `json:"token"`
	Name    *string   `json:"device_name"`
	GroupID int64     `json:"group_id"`
	Expires time.Time `json:"expires_at"`
}
type EnrollmentItem struct {
	ID      string    `json:"enrollment_id"`
	Name    *string   `json:"device_name"`
	GroupID int64     `json:"group_id"`
	Created time.Time `json:"created_at"`
	Expires time.Time `json:"expires_at"`
	Used    *string   `json:"used_at"`
	UsedBy  *int64    `json:"used_by_device_id"`
	Revoked *string   `json:"revoked_at"`
	Status  string    `json:"status"`
}
type EnrollmentRevocation struct {
	ID      string    `json:"enrollment_id"`
	Revoked time.Time `json:"revoked_at"`
}

func uuidString(b []byte) string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
func parseUUID(s string) ([]byte, error) {
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return nil, fail(422, "invalid_request")
	}
	b, err := hex.DecodeString(strings.ReplaceAll(s, "-", ""))
	if err != nil || len(b) != 16 {
		return nil, fail(422, "invalid_request")
	}
	return b, nil
}
func (s *Service) IssueEnrollment(ctx context.Context, token, csrf string, in EnrollmentInput) (EnrollmentIssue, error) {
	var out EnrollmentIssue
	err := s.access.WithMutation(ctx, token, csrf, func(tx *sql.Tx, u access.User) error {
		if u.Role != "admin" {
			return fail(403, "admin_required")
		}
		if in.GroupID == nil || !validID(*in.GroupID, true) || in.TTLHours < 1 || in.TTLHours > 168 {
			return fail(422, "invalid_request")
		}
		if in.Name != nil {
			v := strings.TrimSpace(*in.Name)
			if v == "" {
				in.Name = nil
			} else {
				if !validName(v) {
					return fail(422, "invalid_request")
				}
				in.Name = &v
			}
		}
		var status string
		err := tx.QueryRowContext(ctx, "SELECT status FROM `groups` WHERE group_id=? FOR UPDATE", *in.GroupID).Scan(&status)
		if errors.Is(err, sql.ErrNoRows) {
			return fail(422, "group_inactive")
		}
		if err != nil {
			return dbError(err)
		}
		if status != "active" {
			return fail(422, "group_inactive")
		}
		var now time.Time
		if err = tx.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP()").Scan(&now); err != nil {
			return dbError(err)
		}
		var id [16]byte
		var secret [32]byte
		if _, err = rand.Read(id[:]); err != nil {
			return fail(503, "unavailable")
		}
		if _, err = rand.Read(secret[:]); err != nil {
			return fail(503, "unavailable")
		}
		id[6] = id[6]&15 | 64
		id[8] = id[8]&63 | 128
		out = EnrollmentIssue{ID: uuidString(id[:]), Token: "enr_" + hex.EncodeToString(secret[:]), Name: in.Name, GroupID: *in.GroupID, Expires: now.UTC().Add(time.Duration(in.TTLHours) * time.Hour)}
		hash := sha256.Sum256([]byte(out.Token))
		if _, err = tx.ExecContext(ctx, "INSERT INTO agent_enrollment_token(enrollment_id,token_hash,device_name,group_id,created_at,expires_at) VALUES(?,?,?,?,?,?)", id[:], hash[:], out.Name, out.GroupID, now, out.Expires); err != nil {
			return dbError(err)
		}
		// Record only non-secret metadata; not even the digest belongs in the audit.
		return audit(ctx, tx, u.ID, "enrollment_create", out.ID, map[string]any{"group_id": out.GroupID, "device_name": out.Name, "ttl_hours": in.TTLHours})
	})
	// Never return a generated secret for a failed/uncertain commit.
	if err != nil {
		return EnrollmentIssue{}, err
	}
	return out, nil
}
func (s *Service) Enrollments(ctx context.Context, token string, group *int64) ([]EnrollmentItem, error) {
	out := []EnrollmentItem{}
	err := s.access.WithRead(ctx, token, func(tx *sql.Tx, u access.User) error {
		if u.Role != "admin" {
			return fail(403, "admin_required")
		}
		where := ""
		args := []any{}
		if group != nil {
			if !validID(*group, true) {
				return fail(422, "invalid_request")
			}
			where = " WHERE group_id=?"
			args = append(args, *group)
		}
		rows, err := tx.QueryContext(ctx, "SELECT enrollment_id,device_name,group_id,created_at,expires_at,used_at,used_by_device_id,revoked_at,UTC_TIMESTAMP() FROM agent_enrollment_token"+where+" ORDER BY created_at DESC,enrollment_id", args...)
		if err != nil {
			return dbError(err)
		}
		defer rows.Close()
		for rows.Next() {
			var v EnrollmentItem
			var id []byte
			var name sql.NullString
			var used, revoked sql.NullTime
			var usedBy sql.NullInt64
			var now time.Time
			if err = rows.Scan(&id, &name, &v.GroupID, &v.Created, &v.Expires, &used, &usedBy, &revoked, &now); err != nil {
				return dbError(err)
			}
			if len(id) != 16 {
				return fail(503, "unavailable")
			}
			v.ID = uuidString(id)
			v.Name = nullableString(name)
			v.Created = v.Created.UTC()
			v.Expires = v.Expires.UTC()
			v.Used = nullableDate(used)
			v.Revoked = nullableDate(revoked)
			if usedBy.Valid {
				value := usedBy.Int64
				v.UsedBy = &value
			}
			switch {
			case used.Valid:
				v.Status = "used"
			case revoked.Valid:
				v.Status = "revoked"
			case !v.Expires.After(now):
				v.Status = "expired"
			default:
				v.Status = "unused"
			}
			out = append(out, v)
		}
		if err = rows.Err(); err != nil {
			return dbError(err)
		}
		return nil
	})
	return out, err
}
func (s *Service) RevokeEnrollment(ctx context.Context, token, csrf, id string) (EnrollmentRevocation, error) {
	var out EnrollmentRevocation
	err := s.access.WithMutation(ctx, token, csrf, func(tx *sql.Tx, u access.User) error {
		if u.Role != "admin" {
			return fail(403, "admin_required")
		}
		b, err := parseUUID(id)
		if err != nil {
			return err
		}
		var used, revoked sql.NullTime
		var now time.Time
		err = tx.QueryRowContext(ctx, "SELECT used_at,revoked_at,UTC_TIMESTAMP() FROM agent_enrollment_token WHERE enrollment_id=? FOR UPDATE", b).Scan(&used, &revoked, &now)
		if errors.Is(err, sql.ErrNoRows) {
			return fail(404, "not_found")
		}
		if err != nil {
			return dbError(err)
		}
		if used.Valid {
			return fail(409, "already_used")
		}
		if !revoked.Valid {
			if _, err = tx.ExecContext(ctx, "UPDATE agent_enrollment_token SET revoked_at=? WHERE enrollment_id=?", now, b); err != nil {
				return dbError(err)
			}
			revoked = sql.NullTime{Time: now, Valid: true}
			if err = audit(ctx, tx, u.ID, "enrollment_revoke", uuidString(b), nil); err != nil {
				return err
			}
		}
		out = EnrollmentRevocation{ID: uuidString(b), Revoked: revoked.Time.UTC()}
		return nil
	})
	if err != nil {
		return EnrollmentRevocation{}, err
	}
	return out, nil
}

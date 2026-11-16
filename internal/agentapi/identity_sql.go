package agentapi

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

func dbNow(ctx context.Context, tx *sql.Tx) (time.Time, error) {
	var now time.Time
	err := tx.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP()").Scan(&now)
	return now.UTC(), err
}
func (s *Identity) enroll(ctx context.Context, token []byte, name string, version *string, refresh []byte) (credentialIssue, error) {
	var result credentialIssue
	tx, err := s.db.DB.BeginTx(ctx, nil)
	if err != nil {
		return result, failure("unavailable")
	}
	defer tx.Rollback()
	now, err := dbNow(ctx, tx)
	if err != nil {
		return result, failure("unavailable")
	}
	var enrollment []byte
	var expected sql.NullString
	var group int64
	var used, revoked sql.NullTime
	var expiry time.Time
	err = tx.QueryRowContext(ctx, `SELECT enrollment_id,device_name,group_id,used_at,expires_at,revoked_at
 FROM agent_enrollment_token WHERE token_hash=? FOR UPDATE`, token).Scan(&enrollment, &expected, &group, &used, &expiry, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return result, failure("enrollment_token_invalid")
	}
	if err != nil {
		return result, failure("unavailable")
	}
	if used.Valid || revoked.Valid || !expiry.After(now) {
		return result, failure("enrollment_token_invalid")
	}
	if expected.Valid && expected.String != name {
		e := failure("enrollment_token_invalid")
		e.Details = map[string]any{"reason": "device_name_mismatch", "expected_device_name": expected.String, "actual_device_name": name}
		return result, e
	}
	var status string
	err = tx.QueryRowContext(ctx, "SELECT status FROM `groups` WHERE group_id=? FOR UPDATE", group).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return result, failure("enrollment_token_invalid")
	}
	if err != nil {
		return result, failure("unavailable")
	}
	if status != "active" {
		return result, failure("enrollment_token_invalid")
	}
	var deviceID int64
	err = tx.QueryRowContext(ctx, "SELECT device_id,status FROM devices WHERE device_name=? AND group_id=? FOR UPDATE", name, group).Scan(&deviceID, &status)
	if err == nil {
		if status != "active" {
			return result, failure("device_inactive")
		}
		_, err = tx.ExecContext(ctx, "UPDATE devices SET version=?,last_login_at=? WHERE device_id=?", version, now, deviceID)
	} else if errors.Is(err, sql.ErrNoRows) {
		var res sql.Result
		res, err = tx.ExecContext(ctx, "INSERT INTO devices(group_id,device_name,status,version,created_at,last_login_at) VALUES (?,?,'active',?,?,?)", group, name, version, now, now)
		if err == nil {
			deviceID, err = res.LastInsertId()
		}
	}
	if err != nil {
		return result, failure("unavailable")
	}
	result, err = replaceCredential(ctx, tx, deviceID, name, refresh, now)
	if err != nil {
		return result, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE agent_enrollment_token SET used_at=?,used_by_device_id=?
 WHERE enrollment_id=? AND used_at IS NULL AND revoked_at IS NULL AND expires_at>?`, now, deviceID, enrollment, now)
	if err != nil {
		return credentialIssue{}, failure("unavailable")
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return credentialIssue{}, failure("unavailable")
	}
	if affected != 1 {
		return credentialIssue{}, failure("enrollment_token_invalid")
	}
	// Do not retry a failed commit: it may have succeeded on the server. Replaying
	// registration/rotation after an uncertain commit can invalidate the new token.
	if err = tx.Commit(); err != nil {
		return credentialIssue{}, failure("unavailable")
	}
	return result, nil
}
func (s *Identity) rotate(ctx context.Context, current, next []byte) (credentialIssue, error) {
	var result credentialIssue
	tx, err := s.db.DB.BeginTx(ctx, nil)
	if err != nil {
		return result, failure("unavailable")
	}
	defer tx.Rollback()
	now, err := dbNow(ctx, tx)
	if err != nil {
		return result, failure("unavailable")
	}
	var deviceID int64
	var expiry time.Time
	var revoked sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT device_id,expires_at,revoked_at FROM agent_device_credential WHERE token_hash=? FOR UPDATE`, current).Scan(&deviceID, &expiry, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return result, invalidRefresh()
	}
	if err != nil {
		return result, failure("unavailable")
	}
	if revoked.Valid || !expiry.After(now) {
		return result, invalidRefresh()
	}
	var name, status string
	err = tx.QueryRowContext(ctx, "SELECT device_name,status FROM devices WHERE device_id=? FOR UPDATE", deviceID).Scan(&name, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return result, failure("device_inactive")
	}
	if err != nil {
		return result, failure("unavailable")
	}
	if status != "active" {
		return result, failure("device_inactive")
	}
	result, err = replaceCredential(ctx, tx, deviceID, name, next, now)
	if err != nil {
		return result, err
	}
	if err = tx.Commit(); err != nil {
		return credentialIssue{}, failure("unavailable")
	}
	return result, nil
}
func invalidRefresh() *APIError {
	e := failure("invalid_token")
	e.Message = "Refresh token is invalid or expired"
	return e
}
func replaceCredential(ctx context.Context, tx *sql.Tx, deviceID int64, name string, hash []byte, now time.Time) (credentialIssue, error) {
	result := credentialIssue{DeviceIdentity: DeviceIdentity{DeviceID: deviceID, DeviceName: name}, IssuedAt: now, ExpiresAt: now.Add(refreshTTL)}
	if _, err := tx.ExecContext(ctx, "UPDATE agent_device_credential SET revoked_at=? WHERE device_id=? AND revoked_at IS NULL", now, deviceID); err != nil {
		return credentialIssue{}, failure("unavailable")
	}
	res, err := tx.ExecContext(ctx, "INSERT INTO agent_device_credential(device_id,token_hash,issued_at,expires_at) VALUES (?,?,?,?)", deviceID, hash, now, result.ExpiresAt)
	if err != nil {
		return credentialIssue{}, failure("unavailable")
	}
	result.CredentialID, err = res.LastInsertId()
	if err != nil {
		return credentialIssue{}, failure("unavailable")
	}
	return result, nil
}
func (s *Identity) lookup(ctx context.Context, deviceID, credentialID int64) (DeviceIdentity, error) {
	var name, status string
	var expiry, now time.Time
	var revoked sql.NullTime
	err := s.db.DB.QueryRowContext(ctx, `SELECT d.device_name,d.status,c.expires_at,c.revoked_at,UTC_TIMESTAMP()
 FROM agent_device_credential c JOIN devices d ON d.device_id=c.device_id WHERE c.credential_id=? AND c.device_id=?`, credentialID, deviceID).Scan(&name, &status, &expiry, &revoked, &now)
	if errors.Is(err, sql.ErrNoRows) {
		return DeviceIdentity{}, failure("device_revoked")
	}
	if err != nil {
		return DeviceIdentity{}, failure("unavailable")
	}
	if status != "active" || revoked.Valid || !expiry.After(now) {
		return DeviceIdentity{}, failure("device_revoked")
	}
	return DeviceIdentity{DeviceID: deviceID, DeviceName: name, CredentialID: credentialID}, nil
}

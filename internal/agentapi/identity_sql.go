package agentapi

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/horrible-gh/TimeWeaver/internal/store"
)

func (s *Identity) enrollOnce(ctx context.Context, token []byte, name string, version *string, refresh []byte) (credentialIssue, error) {
	var result credentialIssue
	tx, err := s.db.DB.BeginTx(ctx, nil)
	if err != nil {
		return result, failure("unavailable")
	}
	defer tx.Rollback()
	now, err := store.DBNow(ctx, tx, s.db.Dialect)
	if err != nil {
		return result, identityStatementError(err)
	}
	var enrollment []byte
	var expected sql.NullString
	var group int64
	var used, revoked sql.NullTime
	var expiry time.Time
	err = tx.QueryRowContext(ctx, `SELECT enrollment_id,device_name,group_id,used_at,expires_at,revoked_at
 FROM agent_enrollment_token WHERE token_hash=?`+store.LockClause(s.db.Dialect, false), token).Scan(&enrollment, &expected, &group, store.ScanNullTime(&used), store.ScanTime(&expiry), store.ScanNullTime(&revoked))
	if errors.Is(err, sql.ErrNoRows) {
		return result, failure("enrollment_token_invalid")
	}
	if err != nil {
		return result, identityStatementError(err)
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
	err = tx.QueryRowContext(ctx, "SELECT status FROM `groups` WHERE group_id=?"+store.LockClause(s.db.Dialect, false), group).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return result, failure("enrollment_token_invalid")
	}
	if err != nil {
		return result, identityStatementError(err)
	}
	if status != "active" {
		return result, failure("enrollment_token_invalid")
	}
	var deviceID int64
	err = tx.QueryRowContext(ctx, "SELECT device_id,status FROM devices WHERE device_name=? AND group_id=?"+store.LockClause(s.db.Dialect, false), name, group).Scan(&deviceID, &status)
	if err == nil {
		if status != "active" {
			return result, failure("device_inactive")
		}
		_, err = tx.ExecContext(ctx, "UPDATE devices SET version=?,last_login_at=? WHERE device_id=?", version, store.TimeValue(s.db.Dialect, now), deviceID)
	} else if errors.Is(err, sql.ErrNoRows) {
		var res sql.Result
		res, err = tx.ExecContext(ctx, "INSERT INTO devices(group_id,device_name,status,version,created_at,last_login_at) VALUES (?,?,'active',?,?,?)", group, name, version, store.TimeValue(s.db.Dialect, now), store.TimeValue(s.db.Dialect, now))
		if err == nil {
			deviceID, err = res.LastInsertId()
		}
	}
	if err != nil {
		return result, identityStatementError(err)
	}
	result, err = replaceCredential(ctx, tx, s.db.Dialect, deviceID, name, refresh, now)
	if err != nil {
		return result, err
	}
	expiryCheck := "expires_at>?"
	if s.db.Dialect == "sqlite" {
		expiryCheck = "julianday(expires_at)>julianday(?)"
	}
	res, err := tx.ExecContext(ctx, `UPDATE agent_enrollment_token SET used_at=?,used_by_device_id=?
 WHERE enrollment_id=? AND used_at IS NULL AND revoked_at IS NULL AND `+expiryCheck, store.TimeValue(s.db.Dialect, now), deviceID, enrollment, store.TimeValue(s.db.Dialect, now))
	if err != nil {
		return credentialIssue{}, identityStatementError(err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return credentialIssue{}, identityStatementError(err)
	}
	if affected != 1 {
		return credentialIssue{}, failure("enrollment_token_invalid")
	}
	// Do not retry a failed commit: it may have succeeded on the server. Replaying
	// registration/rotation after an uncertain commit can invalidate the new token.
	if err = identityCommitError(tx.Commit()); err != nil {
		return credentialIssue{}, err
	}
	return result, nil
}
func (s *Identity) rotateOnce(ctx context.Context, current, next []byte) (credentialIssue, error) {
	var result credentialIssue
	tx, err := s.db.DB.BeginTx(ctx, nil)
	if err != nil {
		return result, failure("unavailable")
	}
	defer tx.Rollback()
	now, err := store.DBNow(ctx, tx, s.db.Dialect)
	if err != nil {
		return result, identityStatementError(err)
	}
	var deviceID int64
	var expiry time.Time
	var revoked sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT device_id,expires_at,revoked_at FROM agent_device_credential WHERE token_hash=?`+store.LockClause(s.db.Dialect, false), current).Scan(&deviceID, store.ScanTime(&expiry), store.ScanNullTime(&revoked))
	if errors.Is(err, sql.ErrNoRows) {
		return result, invalidRefresh()
	}
	if err != nil {
		return result, identityStatementError(err)
	}
	if revoked.Valid || !expiry.After(now) {
		return result, invalidRefresh()
	}
	var name, status string
	err = tx.QueryRowContext(ctx, "SELECT device_name,status FROM devices WHERE device_id=?"+store.LockClause(s.db.Dialect, false), deviceID).Scan(&name, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return result, failure("device_inactive")
	}
	if err != nil {
		return result, identityStatementError(err)
	}
	if status != "active" {
		return result, failure("device_inactive")
	}
	result, err = replaceCredential(ctx, tx, s.db.Dialect, deviceID, name, next, now)
	if err != nil {
		return result, err
	}
	if err = identityCommitError(tx.Commit()); err != nil {
		return credentialIssue{}, err
	}
	return result, nil
}
func invalidRefresh() *APIError {
	e := failure("invalid_token")
	e.Message = "Refresh token is invalid or expired"
	return e
}
func replaceCredential(ctx context.Context, tx *sql.Tx, dialect string, deviceID int64, name string, hash []byte, now time.Time) (credentialIssue, error) {
	result := credentialIssue{DeviceIdentity: DeviceIdentity{DeviceID: deviceID, DeviceName: name}, IssuedAt: now, ExpiresAt: now.Add(refreshTTL)}
	if _, err := tx.ExecContext(ctx, "UPDATE agent_device_credential SET revoked_at=? WHERE device_id=? AND revoked_at IS NULL", store.TimeValue(dialect, now), deviceID); err != nil {
		return credentialIssue{}, identityStatementError(err)
	}
	res, err := tx.ExecContext(ctx, "INSERT INTO agent_device_credential(device_id,token_hash,issued_at,expires_at) VALUES (?,?,?,?)", deviceID, hash, store.TimeValue(dialect, now), store.TimeValue(dialect, result.ExpiresAt))
	if err != nil {
		return credentialIssue{}, identityStatementError(err)
	}
	result.CredentialID, err = res.LastInsertId()
	if err != nil {
		return credentialIssue{}, identityStatementError(err)
	}
	return result, nil
}
func (s *Identity) lookup(ctx context.Context, deviceID, credentialID int64) (DeviceIdentity, error) {
	var name, status string
	var expiry, now time.Time
	var revoked sql.NullTime
	err := s.db.DB.QueryRowContext(ctx, `SELECT d.device_name,d.status,c.expires_at,c.revoked_at,`+store.NowExpr(s.db.Dialect)+`
 FROM agent_device_credential c JOIN devices d ON d.device_id=c.device_id WHERE c.credential_id=? AND c.device_id=?`, credentialID, deviceID).Scan(&name, &status, store.ScanTime(&expiry), store.ScanNullTime(&revoked), store.ScanTime(&now))
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

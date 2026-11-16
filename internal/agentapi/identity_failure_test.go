package agentapi

import (
	"context"
	"testing"
)

func TestMySQLIdentityExpiryAndStorageFailure(t *testing.T) {
	s := identityService(t, identityStore(t), "")
	ctx := context.Background()
	name := "expiry-device"
	expired := enrollment(t, s, &name, 0)
	if _, err := s.db.DB.Exec("UPDATE agent_enrollment_token SET expires_at=UTC_TIMESTAMP()-INTERVAL 1 SECOND WHERE token_hash=?", digest(expired)); err != nil {
		t.Fatal(err)
	}
	_, err := s.Enroll(ctx, expired, name, nil)
	requireCode(t, err, 403, "enrollment_token_invalid")
	revoked := enrollment(t, s, &name, 0)
	if _, err = s.db.DB.Exec("UPDATE agent_enrollment_token SET revoked_at=UTC_TIMESTAMP() WHERE token_hash=?", digest(revoked)); err != nil {
		t.Fatal(err)
	}
	_, err = s.Enroll(ctx, revoked, name, nil)
	requireCode(t, err, 403, "enrollment_token_invalid")
	fresh, err := s.Enroll(ctx, enrollment(t, s, &name, 0), name, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.DB.Exec("UPDATE agent_device_credential SET expires_at=UTC_TIMESTAMP()-INTERVAL 1 SECOND WHERE device_id=?", fresh.DeviceID); err != nil {
		t.Fatal(err)
	}
	_, err = s.VerifyAccess(ctx, fresh.AccessToken)
	requireCode(t, err, 403, "device_revoked")
	_, err = s.Rotate(ctx, fresh.RefreshToken)
	requireCode(t, err, 401, "invalid_token")
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = s.Rotate(canceled, fresh.RefreshToken)
	requireCode(t, err, 503, "unavailable")
	s.db.Close()
	_, err = s.VerifyAccess(ctx, fresh.AccessToken)
	requireCode(t, err, 503, "unavailable")
}

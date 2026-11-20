package access

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"time"
)

const (
	normalSessionLifetime     = 12 * time.Hour
	rememberedSessionLifetime = 30 * 24 * time.Hour
	sessionRotationInterval   = 12 * time.Hour
	sessionRotationGrace      = time.Minute
)

func sessionInfo(remembered bool, expires, rotated, now int64, previous bool) SessionInfo {
	info := SessionInfo{Remembered: remembered, ExpiresAt: expires}
	if remembered {
		info.RefreshAfter = rotated + int64(sessionRotationInterval.Seconds())
		if previous {
			info.RefreshAfter = now
		}
		info.RefreshAfterSeconds = max(0, info.RefreshAfter-now)
	}
	return info
}

// A fresh random seed for each rotation prevents an old credential from
// calculating future credentials by itself. A predecessor presented during
// the fixed overlap can recover exactly the committed successor even when the
// first HTTP response was lost. The database stores no raw bearer credential.
func successorToken(seed, family, previous string) (string, error) {
	key, err := hex.DecodeString(seed)
	if err != nil || len(key) != 32 || !validToken(family) || !validToken(previous) {
		return "", unavailable()
	}
	h := hmac.New(sha256.New, key)
	h.Write([]byte("timeweaver/session-rotation/v1\x00"))
	h.Write([]byte(family))
	h.Write([]byte{0})
	h.Write([]byte(previous))
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Refresh only changes credentials after an explicit, CSRF-protected request.
// Ordinary sessions keep their original idle and absolute expiry policy. The
// returned token is empty when the caller already has the current credential.
func (s *Service) Refresh(ctx context.Context, token, csrf string) (Principal, string, error) {
	tx, p, err := s.sessionTx(ctx, token)
	if err != nil {
		return Principal{}, "", err
	}
	defer tx.Rollback()
	if !equalSecret(p.CSRF, csrf) {
		return Principal{}, "", failure(403, "csrf_failed")
	}
	now := s.now().Unix()
	var next string
	if p.previous {
		next, err = successorToken(p.rotationSeed, p.session, token)
		if err != nil || !equalSecret(digest(next), p.tokenDigest) {
			return Principal{}, "", unavailable()
		}
		// Recovery does not move rotated_at or previous_until. A retry cannot
		// stretch the overlap or create a second successor for sibling tabs.
		p.previous = false
		p.Session = sessionInfo(p.Session.Remembered, p.Session.ExpiresAt, p.rotatedAt, now, false)
	} else if p.Session.Remembered && p.Session.RefreshAfter <= now {
		seed, seedErr := randomToken()
		if seedErr != nil {
			return Principal{}, "", seedErr
		}
		next, err = successorToken(seed, p.session, token)
		if err != nil {
			return Principal{}, "", err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE tw_session SET token_digest=?,previous_digest=?,previous_until=?,rotation_seed=?,rotated_at=? WHERE digest=?`, digest(next), digest(token), now+int64(sessionRotationGrace.Seconds()), seed, now, p.session); err != nil {
			return Principal{}, "", unavailable()
		}
		p.tokenDigest, p.rotationSeed = digest(next), seed
		p.rotatedAt = now
		p.Session = sessionInfo(true, p.Session.ExpiresAt, now, now, false)
		if err = audit(ctx, tx, now, p.User.ID, "session_refresh"); err != nil {
			return Principal{}, "", err
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE tw_session SET last_seen=? WHERE digest=?", now, p.session); err != nil {
		return Principal{}, "", unavailable()
	}
	if tx.Commit() != nil {
		return Principal{}, "", unavailable()
	}
	return p, next, nil
}

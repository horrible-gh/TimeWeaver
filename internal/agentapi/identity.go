// Package agentapi preserves the existing device API, independently of human sessions.
package agentapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/horrible-gh/TimeWeaver/internal/config"
	"github.com/horrible-gh/TimeWeaver/internal/store"
)

const accessTTL = 15 * time.Minute
const refreshTTL = 90 * 24 * time.Hour

// APIError never carries SQL, connection strings or token values.
type APIError struct {
	Status  int            `json:"-"`
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"-"`
}

func (e *APIError) Error() string { return e.Code }
func failure(code string) *APIError {
	e := &APIError{Status: 503, Code: "unavailable", Message: "Identity storage unavailable"}
	switch code {
	case "enrollment_token_invalid":
		e.Status = 403
		e.Code = code
		e.Message = "Enrollment token invalid, expired, or revoked"
	case "invalid_token":
		e.Status = 401
		e.Code = code
		e.Message = "Invalid agent access token"
	case "token_expired":
		e.Status = 401
		e.Code = code
		e.Message = "Access token expired"
	case "device_inactive":
		e.Status = 403
		e.Code = code
		e.Message = "Device is not active"
	case "device_revoked":
		e.Status = 403
		e.Code = code
		e.Message = "Device credential revoked"
	}
	return e
}

type DeviceIdentity struct {
	DeviceID     int64  `json:"device_id"`
	DeviceName   string `json:"device_name"`
	CredentialID int64  `json:"credential_id"`
}
type TokenData struct {
	AccessToken    string    `json:"access_token"`
	AccessExpires  time.Time `json:"access_token_expires_at"`
	RefreshToken   string    `json:"refresh_token"`
	RefreshExpires time.Time `json:"refresh_token_expires_at"`
	TokenType      string    `json:"token_type"`
	DeviceID       int64     `json:"device_id,omitempty"`
	DeviceName     string    `json:"device_name,omitempty"`
}
type credentialIssue struct {
	DeviceIdentity
	IssuedAt, ExpiresAt time.Time
}

// Identity is intentionally MySQL/MariaDB-only until the SQLite schema and
// transaction dialect are complete. Constructing it never runs migrations.
type Identity struct {
	db  *store.Store
	key []byte
	now func() time.Time
}

func NewIdentity(db *store.Store, settings config.Settings) (*Identity, error) {
	if db == nil || db.DB == nil || db.Dialect != "mysql" || db.ReadOnly() {
		return nil, errors.New("identity API requires a writable MySQL/MariaDB store")
	}
	if len(settings.SecretKey) < 32 || (settings.AgentSecretKey != "" && len(settings.AgentSecretKey) < 32) {
		return nil, errors.New("identity signing configuration is invalid")
	}
	return &Identity{db: db, key: settings.AgentSigningKey(), now: time.Now}, nil
}
func randomSecret(prefix string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", failure("unavailable")
	}
	return prefix + hex.EncodeToString(b), nil
}
func newJTI() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", failure("unavailable")
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}
func digest(raw string) []byte { value := sha256.Sum256([]byte(raw)); return value[:] }
func (s *Identity) Enroll(ctx context.Context, token, name string, version *string) (TokenData, error) {
	// Generate entropy before committing credentials; an entropy failure must not
	// consume a one-time enrollment token or revoke a working credential.
	raw, err := randomSecret("rft_")
	if err != nil {
		return TokenData{}, err
	}
	jti, err := newJTI()
	if err != nil {
		return TokenData{}, err
	}
	issue, err := s.enroll(ctx, digest(token), name, version, digest(raw))
	if err != nil {
		return TokenData{}, err
	}
	data, err := s.issue(issue, raw, jti)
	data.DeviceID = issue.DeviceID
	data.DeviceName = issue.DeviceName
	return data, err
}
func (s *Identity) Rotate(ctx context.Context, token string) (TokenData, error) {
	raw, err := randomSecret("rft_")
	if err != nil {
		return TokenData{}, err
	}
	jti, err := newJTI()
	if err != nil {
		return TokenData{}, err
	}
	issue, err := s.rotate(ctx, digest(token), digest(raw))
	if err != nil {
		return TokenData{}, err
	}
	return s.issue(issue, raw, jti)
}
func (s *Identity) issue(v credentialIssue, refresh, jti string) (TokenData, error) {
	expiry := v.IssuedAt.Add(accessTTL)
	// MapClaims keeps aud a string, exactly as the Python issuer emits it.
	claims := jwt.MapClaims{"sub": "device:" + strconv.FormatInt(v.DeviceID, 10), "aud": "timeweaver-agent", "typ": "agent", "credential_id": v.CredentialID, "jti": jti, "iat": v.IssuedAt.Unix(), "exp": expiry.Unix()}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.key)
	if err != nil {
		return TokenData{}, failure("unavailable")
	}
	return TokenData{AccessToken: signed, AccessExpires: expiry.UTC(), RefreshToken: refresh, RefreshExpires: v.ExpiresAt.UTC(), TokenType: "bearer"}, nil
}

type agentClaims struct {
	jwt.RegisteredClaims
	Type         string `json:"typ"`
	CredentialID int64  `json:"credential_id"`
}

func (c agentClaims) Validate() error {
	id, err := strconv.ParseInt(strings.TrimPrefix(c.Subject, "device:"), 10, 64)
	if c.IssuedAt == nil || c.Type != "agent" || c.CredentialID <= 0 || !strings.HasPrefix(c.Subject, "device:") || err != nil || id <= 0 {
		return errors.New("invalid device claims")
	}
	return nil
}
func (s *Identity) VerifyAccess(ctx context.Context, raw string) (DeviceIdentity, error) {
	if len(raw) > 8192 {
		return DeviceIdentity{}, failure("invalid_token")
	}
	claims := &agentClaims{}
	_, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) { return s.key, nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithAudience("timeweaver-agent"), jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithTimeFunc(s.now))
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return DeviceIdentity{}, failure("token_expired")
		}
		return DeviceIdentity{}, failure("invalid_token")
	}
	id, _ := strconv.ParseInt(strings.TrimPrefix(claims.Subject, "device:"), 10, 64)
	return s.lookup(ctx, id, claims.CredentialID)
}

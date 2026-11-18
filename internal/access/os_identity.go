package access

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"
)

func (s *Service) OSAvailable() bool {
	return s.osAuth != nil && s.osAuth.Available()
}

func (s *Service) Methods() []string {
	methods := []string{"local"}
	if s.OSAvailable() {
		methods = append(methods, "os")
	}
	return methods
}

func validOSIdentity(v OSIdentity) bool {
	if len(v.Subject) > 255 || (!strings.HasPrefix(v.Subject, "linux:uid:") && !strings.HasPrefix(v.Subject, "windows:sid:")) || !cleanText(v.Name, 255, false) {
		return false
	}
	for _, c := range v.Subject {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == ':' || c == '-') {
			return false
		}
	}
	return true
}

func (s *Service) resolveIdentity(ctx context.Context, name string) (OSIdentity, error) {
	if !s.OSAvailable() {
		return OSIdentity{}, failure(503, "os_login_unavailable")
	}
	if !cleanText(name, 255, false) {
		return OSIdentity{}, failure(422, "invalid_request")
	}
	v, err := s.osAuth.Resolve(ctx, name)
	if errors.Is(err, ErrOSUnavailable) || ctx.Err() != nil {
		return OSIdentity{}, failure(503, "os_login_unavailable")
	}
	if err != nil || !validOSIdentity(v) {
		return OSIdentity{}, failure(422, "os_account_not_found")
	}
	return v, nil
}

func identityFailure(err error) error {
	var my *mysql.MySQLError
	if errors.As(err, &my) && my.Number == 1062 {
		return failure(409, "os_identity_in_use")
	}
	return adminFailure(err)
}

// OS authentication is an explicit method. Only a pre-linked identity can reach
// the native password verifier, and it never falls back to a local password.
func (s *Service) LoginOS(ctx context.Context, name, password, peer, oldToken string) (Principal, string, error) {
	if err := s.attempt(ctx, "peer:"+peer, 50); err != nil {
		return Principal{}, "", err
	}
	if !cleanText(name, 255, false) || len(password) == 0 || len(password) > 1024 || !utf8.ValidString(password) || strings.ContainsAny(password, "\x00\r\n") {
		return Principal{}, "", failure(401, "invalid_credentials")
	}
	if !s.OSAvailable() {
		return Principal{}, "", failure(503, "os_login_unavailable")
	}
	identity, err := s.osAuth.Resolve(ctx, name)
	if errors.Is(err, ErrOSUnavailable) || ctx.Err() != nil {
		return Principal{}, "", failure(503, "os_login_unavailable")
	}
	if err != nil || !validOSIdentity(identity) {
		if rateErr := s.attempt(ctx, "os-name:"+strings.ToLower(name), 5); rateErr != nil {
			return Principal{}, "", rateErr
		}
		return Principal{}, "", failure(401, "invalid_credentials")
	}
	key := "os-subject:" + identity.Subject
	if err = s.attempt(ctx, key, 5); err != nil {
		return Principal{}, "", err
	}
	var id string
	err = s.db.DB.QueryRowContext(ctx, "SELECT user_id FROM tw_user_identity WHERE subject=?", identity.Subject).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Principal{}, "", failure(401, "invalid_credentials")
	}
	if err != nil {
		return Principal{}, "", unavailable()
	}
	candidate, err := scanUser(s.db.DB.QueryRowContext(ctx, userSelect, id))
	if err != nil {
		return Principal{}, "", err
	}
	if candidate.AuthMethod != "os" || candidate.status != "active" || candidate.osSubject != identity.Subject {
		return Principal{}, "", failure(401, "invalid_credentials")
	}
	verified, err := s.osAuth.Authenticate(ctx, name, password)
	if errors.Is(err, ErrOSUnavailable) || ctx.Err() != nil {
		return Principal{}, "", failure(503, "os_login_unavailable")
	}
	if err != nil || !validOSIdentity(verified) || verified.Subject != identity.Subject {
		return Principal{}, "", failure(401, "invalid_credentials")
	}
	tx, err := s.db.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Principal{}, "", unavailable()
	}
	defer tx.Rollback()
	u, err := loadUser(ctx, tx, candidate.ID)
	if err != nil {
		return Principal{}, "", err
	}
	// The external operation ran without DB locks. Reject every intervening
	// disable/rebind/policy change before issuing a browser session.
	if !s.OSAvailable() || ctx.Err() != nil || u.status != "active" || u.AuthMethod != "os" || u.version != candidate.version || u.osSubject != candidate.osSubject || u.osSubject != verified.Subject {
		return Principal{}, "", failure(401, "invalid_credentials")
	}
	// A verified OS rename preserves identity and refreshes its display name.
	if _, err = tx.ExecContext(ctx, "UPDATE tw_user_identity SET os_name=? WHERE user_id=? AND subject=?", verified.Name, u.ID, verified.Subject); err != nil {
		return Principal{}, "", unavailable()
	}
	u.OSName = verified.Name
	p, token, err := s.issueSession(ctx, tx, u, oldToken)
	if err != nil {
		return Principal{}, "", err
	}
	if tx.Commit() != nil {
		return Principal{}, "", unavailable()
	}
	s.clearAccountAttempt(ctx, key)
	return p, token, nil
}

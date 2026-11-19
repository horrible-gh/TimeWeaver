package access

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/horrible-gh/TimeWeaver/internal/store"
)

type Service struct {
	db     *store.Store
	idle   time.Duration
	now    func() time.Time
	rateMu sync.Mutex
	osAuth OSAuthenticator
}

// Dialect lets authorized data services select SQL without exposing the database.
func (s *Service) Dialect() string { return s.db.Dialect }

func New(ctx context.Context, db *store.Store, idle time.Duration) (*Service, error) {
	return NewWithOS(ctx, db, idle, nil)
}

// A nil provider keeps the local-only embedding contract. Server startup chooses
// its native provider explicitly; clients can never supply an authentication implementation.
func NewWithOS(ctx context.Context, db *store.Store, idle time.Duration, provider OSAuthenticator) (*Service, error) {
	if db == nil || db.ReadOnly() || idle < time.Minute || idle > 30*24*time.Hour {
		return nil, errors.New("invalid human access settings")
	}
	status, err := Status(ctx, db)
	if err != nil {
		return nil, err
	}
	if !status.Ready {
		return nil, errors.New("human access schema is not ready")
	}
	return &Service{db: db, idle: idle, now: time.Now, osAuth: provider}, nil
}

const userSelect = `SELECT u.user_id,u.name,u.role,u.group_id,u.password,a.status,a.version,a.must_change_password,COALESCE(i.subject,''),COALESCE(i.os_name,'') FROM users u JOIN tw_user_auth a ON a.user_id=u.user_id LEFT JOIN tw_user_identity i ON i.user_id=u.user_id WHERE u.user_id=?`

func scanUser(row *sql.Row) (User, error) {
	u := User{Groups: []Membership{}}
	err := row.Scan(&u.ID, &u.Name, &u.Role, &u.GroupID, &u.password, &u.status, &u.version, &u.MustChangePassword, &u.osSubject, &u.OSName)
	if errors.Is(err, sql.ErrNoRows) {
		return u, failure(401, "invalid_credentials")
	}
	if err != nil {
		return u, unavailable()
	}
	// Even if an administrator accidentally removes the flag, a known seed hash
	// must never acquire unrestricted access through a normal login.
	u.MustChangePassword = u.MustChangePassword || equalSecret(u.password, legacySeedHash)
	u.AuthMethod = "local"
	if u.password == "!os" {
		u.AuthMethod = "os"
	}
	return u, nil
}
func (s *Service) loadUser(ctx context.Context, tx *sql.Tx, id string) (User, error) {
	u, err := scanUser(tx.QueryRowContext(ctx, userSelect+store.LockClause(s.db.Dialect, false), id))
	if err != nil {
		return u, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT group_id,role FROM tw_group_member WHERE user_id=? ORDER BY group_id", u.ID)
	if err != nil {
		return u, unavailable()
	}
	defer rows.Close()
	for rows.Next() {
		var m Membership
		if rows.Scan(&m.GroupID, &m.Role) != nil {
			return u, unavailable()
		}
		u.Groups = append(u.Groups, m)
	}
	if rows.Err() != nil {
		return u, unavailable()
	}
	return u, nil
}
func audit(ctx context.Context, tx *sql.Tx, now int64, id, action string) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO tw_audit(at,actor,action,result) VALUES(?,?,?,'success')", now, id, action)
	if err != nil {
		return unavailable()
	}
	return nil
}
func (s *Service) attempt(ctx context.Context, key string, limit int) error {
	s.rateMu.Lock()
	defer s.rateMu.Unlock()
	now := s.now().Unix()
	tx, err := s.db.DB.BeginTx(ctx, nil)
	if err != nil {
		return unavailable()
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "DELETE FROM tw_login_attempt WHERE started<=?", now-300); err != nil {
		return unavailable()
	}
	var count int
	var started int64
	err = tx.QueryRowContext(ctx, "SELECT count,started FROM tw_login_attempt WHERE key_hash=?"+store.LockClause(s.db.Dialect, false), digest(key)).Scan(&count, &started)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return unavailable()
	}
	if count >= limit {
		return failure(429, "login_limited")
	}
	if errors.Is(err, sql.ErrNoRows) {
		var total int
		if tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM tw_login_attempt").Scan(&total) != nil {
			return unavailable()
		}
		if total >= 10000 {
			return failure(429, "login_limited")
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO tw_login_attempt(key_hash,count,started) VALUES(?,1,?)", digest(key), now)
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE tw_login_attempt SET count=count+1 WHERE key_hash=?", digest(key))
	}
	if err != nil {
		return unavailable()
	}
	if tx.Commit() != nil {
		return unavailable()
	}
	return nil
}
func (s *Service) Login(ctx context.Context, id, password, peer, oldToken string) (Principal, string, error) {
	if err := s.attempt(ctx, "peer:"+peer, 50); err != nil {
		return Principal{}, "", err
	}
	if strings.TrimSpace(id) == "" || len(id) > 200 || len(password) == 0 || len(password) > 1024 || !utf8.ValidString(id) || !utf8.ValidString(password) {
		return Principal{}, "", failure(401, "invalid_credentials")
	}
	candidate, err := scanUser(s.db.DB.QueryRowContext(ctx, userSelect, id))
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Status == 503 {
			return Principal{}, "", err
		}
		if rateErr := s.attempt(ctx, "account:"+strings.ToLower(id), 5); rateErr != nil {
			return Principal{}, "", rateErr
		}
		dummyPassword(password)
		return Principal{}, "", failure(401, "invalid_credentials")
	}
	// Use the DB's canonical ID, so case/accent aliases cannot bypass limits.
	if err = s.attempt(ctx, "account:"+candidate.ID, 5); err != nil {
		return Principal{}, "", err
	}
	if !checkPassword(candidate.password, password) || candidate.status != "active" || candidate.AuthMethod != "local" {
		return Principal{}, "", failure(401, "invalid_credentials")
	}
	tx, err := s.db.DB.BeginTx(ctx, nil)
	if err != nil {
		return Principal{}, "", unavailable()
	}
	defer tx.Rollback()
	u, err := s.loadUser(ctx, tx, candidate.ID)
	if err != nil {
		return Principal{}, "", err
	}
	if u.status != "active" || u.version != candidate.version || !equalSecret(u.password, candidate.password) {
		return Principal{}, "", failure(401, "invalid_credentials")
	}
	p, token, err := s.issueSession(ctx, tx, u, oldToken)
	if err != nil {
		return Principal{}, "", err
	}
	if tx.Commit() != nil {
		return Principal{}, "", unavailable()
	}
	s.clearAccountAttempt(ctx, "account:"+u.ID)
	return p, token, nil
}

func sessionFingerprint(u User) string {
	if u.AuthMethod == "os" {
		return digest("os\x00" + u.osSubject)
	}
	return digest(u.password)
}

func (s *Service) issueSession(ctx context.Context, tx *sql.Tx, u User, oldToken string) (Principal, string, error) {
	token, err := randomToken()
	if err != nil {
		return Principal{}, "", err
	}
	csrf, err := randomToken()
	if err != nil {
		return Principal{}, "", err
	}
	now := s.now().Unix()
	p := Principal{u, csrf, digest(token)}
	if _, err = tx.ExecContext(ctx, `DELETE FROM tw_session WHERE expires_at<=? OR last_seen<=? OR digest=?`, now, now-int64(s.idle.Seconds()), digest(oldToken)); err != nil {
		return Principal{}, "", unavailable()
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO tw_session(digest,user_id,user_version,csrf,password_fingerprint,created_at,last_seen,expires_at,auth_method) VALUES(?,?,?,?,?,?,?,?,?)`, p.session, u.ID, u.version, csrf, sessionFingerprint(u), now, now, now+12*3600, u.AuthMethod); err != nil {
		return Principal{}, "", unavailable()
	}
	rows, err := tx.QueryContext(ctx, `SELECT digest FROM tw_session WHERE user_id=? ORDER BY (digest=?) DESC,created_at DESC,digest DESC`, u.ID, p.session)
	if err != nil {
		return Principal{}, "", unavailable()
	}
	var excess []string
	n := 0
	for rows.Next() {
		var d string
		if rows.Scan(&d) != nil {
			rows.Close()
			return Principal{}, "", unavailable()
		}
		n++
		if n > 10 {
			excess = append(excess, d)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return Principal{}, "", unavailable()
	}
	for _, d := range excess {
		if _, err = tx.ExecContext(ctx, `DELETE FROM tw_session WHERE digest=?`, d); err != nil {
			return Principal{}, "", unavailable()
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tw_user_auth SET last_login_at=? WHERE user_id=?`, now, u.ID); err != nil {
		return Principal{}, "", unavailable()
	}
	if err = audit(ctx, tx, now, u.ID, "login"); err != nil {
		return Principal{}, "", err
	}
	return p, token, nil
}

func (s *Service) clearAccountAttempt(ctx context.Context, key string) {
	// Only a successful login clears the account bucket; never the peer bucket.
	s.rateMu.Lock()
	_, _ = s.db.DB.ExecContext(ctx, "DELETE FROM tw_login_attempt WHERE key_hash=?", digest(key))
	s.rateMu.Unlock()
}
func validToken(raw string) bool {
	if len(raw) != 64 {
		return false
	}
	for _, c := range raw {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// sessionTx locks user before session, matching login/password operations.
// Future dashboard mutations must keep this authorization transaction open
// through their writes; a separate unchecked principal is not a write permit.
func (s *Service) sessionTx(ctx context.Context, token string) (*sql.Tx, Principal, error) {
	if !validToken(token) {
		return nil, Principal{}, failure(401, "unauthenticated")
	}
	tx, err := s.db.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, Principal{}, unavailable()
	}
	p, err := s.principalTx(ctx, tx, token)
	if err != nil {
		tx.Rollback()
		return nil, Principal{}, err
	}
	return tx, p, nil
}

// principalTx uses the caller's transaction so administrative operations can
// take the global policy lock BEFORE any user or session row locks.
func (s *Service) principalTx(ctx context.Context, tx *sql.Tx, token string) (Principal, error) {
	empty := Principal{}
	if !validToken(token) {
		return empty, failure(401, "unauthenticated")
	}
	hash := digest(token)
	var id string
	err := tx.QueryRowContext(ctx, `SELECT user_id FROM tw_session WHERE digest=?`, hash).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return empty, failure(401, "unauthenticated")
	}
	if err != nil {
		return empty, unavailable()
	}
	u, err := s.loadUser(ctx, tx, id)
	if err != nil {
		return empty, err
	}
	var version, last, expires int64
	var csrf, fingerprint, method string
	err = tx.QueryRowContext(ctx, `SELECT user_version,csrf,password_fingerprint,last_seen,expires_at,auth_method FROM tw_session WHERE digest=? AND user_id=?`+store.LockClause(s.db.Dialect, false), hash, u.ID).Scan(&version, &csrf, &fingerprint, &last, &expires, &method)
	if errors.Is(err, sql.ErrNoRows) {
		return empty, failure(401, "unauthenticated")
	}
	if err != nil {
		return empty, unavailable()
	}
	now := s.now().Unix()
	if method != u.AuthMethod || (method != "local" && method != "os") || method == "os" && (!s.OSAvailable() || u.osSubject == "") || u.status != "active" || version != u.version || !equalSecret(fingerprint, sessionFingerprint(u)) || now >= expires || now-last >= int64(s.idle.Seconds()) || now < last {
		return empty, failure(401, "unauthenticated")
	}
	return Principal{u, csrf, hash}, nil
}
func (s *Service) Session(ctx context.Context, token string, touch bool) (Principal, error) {
	tx, p, err := s.sessionTx(ctx, token)
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	if touch {
		if _, err = tx.ExecContext(ctx, `UPDATE tw_session SET last_seen=? WHERE digest=?`, s.now().Unix(), p.session); err != nil {
			return Principal{}, unavailable()
		}
	}
	if tx.Commit() != nil {
		return Principal{}, unavailable()
	}
	return p, nil
}
func (s *Service) Logout(ctx context.Context, token, csrf string) error {
	tx, p, err := s.sessionTx(ctx, token)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if !equalSecret(p.CSRF, csrf) {
		return failure(403, "csrf_failed")
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM tw_session WHERE digest=?`, p.session); err != nil {
		return unavailable()
	}
	if err = audit(ctx, tx, s.now().Unix(), p.User.ID, "logout"); err != nil {
		return err
	}
	if tx.Commit() != nil {
		return unavailable()
	}
	return nil
}
func (s *Service) ChangePassword(ctx context.Context, token, csrf, current, next string) error {
	// Verify before expensive hashing, then recheck under the mutation lock.
	p, err := s.Session(ctx, token, false)
	if err != nil {
		return err
	}
	if !equalSecret(p.CSRF, csrf) {
		return failure(403, "csrf_failed")
	}
	if p.User.AuthMethod == "os" {
		return failure(409, "os_password_managed")
	}
	if err = s.attempt(ctx, "password:"+p.User.ID, 5); err != nil {
		return err
	}
	if !checkPassword(p.User.password, current) {
		return failure(401, "invalid_credentials")
	}
	if current == next {
		return failure(422, "password_unchanged")
	}
	hash, err := hashPassword(next)
	if err != nil {
		return err
	}
	tx, fresh, err := s.sessionTx(ctx, token)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if !equalSecret(fresh.User.password, p.User.password) || !equalSecret(fresh.CSRF, csrf) {
		return failure(401, "unauthenticated")
	}
	if _, err = tx.ExecContext(ctx, `UPDATE users SET password=? WHERE user_id=?`, hash, p.User.ID); err != nil {
		return unavailable()
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tw_user_auth SET version=version+1,must_change_password=FALSE,password_changed_at=? WHERE user_id=?`, s.now().Unix(), p.User.ID); err != nil {
		return unavailable()
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM tw_session WHERE user_id=?`, p.User.ID); err != nil {
		return unavailable()
	}
	if err = audit(ctx, tx, s.now().Unix(), p.User.ID, "password_change"); err != nil {
		return err
	}
	if tx.Commit() != nil {
		return unavailable()
	}
	return nil
}

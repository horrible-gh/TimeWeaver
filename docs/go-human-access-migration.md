# Human sessions and group authorization

> Historical implementation slice. The unified executable now supports `serve`;
> see [Go server setup and operation](go-server.md) for current startup and
> upgrade instructions. Status statements below describe this slice when introduced.

Branch: `feature/go-server-unified`, following `c17fec3`. This slice does not
unlock production `serve`. The deployed server, main branch, original Python
sources/SQL and separate agent repository are unchanged.

This document records the local-session foundation. The subsequent
[OS account slice](go-os-access-migration.md) adds native Linux/Windows login and
access schema v3. [Remembered sessions](go-remembered-session-migration.md)
subsequently add schema v4, explicit browser persistence and credential renewal;
the local password and group-permission rules continue to apply.

## Implemented

- `internal/access` verifies existing passlib PBKDF2-SHA256 passwords without
  rewriting them at login. New passwords use passlib-compatible 600,000-round
  SHA-256, random 16-byte salts and bounded input/cost. It uses Go's standard
  `crypto/pbkdf2`; no new production dependency is added.
- `timeweaver access status` is read-only; `access init --allow-write` requires a
  completed clean legacy migration and critical schema. It creates only `tw_`
  auxiliary tables, with its own journal and advisory lock. Existing users,
  groups, password hashes and Python migration history remain unchanged.
  DDL failure is marked unfinished and is not automatically replayed. A backup
  and stopping the old server/migrator are prerequisites, not optional advice.
- Initial membership maps existing non-admin users to editor in their existing
  group (explicit group 0 grants global access). Initialization runs once; it
  never recreates grants an administrator removed. Admins keep their role.
- DB-backed 32-byte random session credentials are stored only as SHA-256
  digests. Cookies are HttpOnly, SameSite=Strict, scoped to the API context and
  Secure for HTTPS. Ordinary sessions use the existing configured idle timeout
  and a 12-hour absolute expiry. The subsequent remembered-session slice adds
  an explicit fixed 30-day opt-in and 12-hour credential rotation.
  At most ten sessions are retained per user. GET polling does not prolong
  idle sessions; `/auth/me` and authorized mutations count as activity.
- Logout, password change, session expiry, user version/status and external
  password-hash changes invalidate sessions. Password change increments the
  user version and removes all that user's sessions in the same transaction as
  the password update and audit record. Login always issues a fresh token.
- A known seeded hash is detected both during migration and authentication.
  A restricted session may inspect itself, change its password or log out, but
  cannot pass the general group-authorization boundary until changed.
- `User.Can`/`GroupFilter` implement viewer/operator/editor and global admin.
  `WithGroup` checks session, CSRF and group rights inside the same transaction
  as a future dashboard mutation. Callers must resolve resource groups in that
  transaction and check every source/target, not trust a request's group ID.
- Human endpoints: form POST `/login`; GET `/auth/me` and `/auth/methods`;
  POST `/logout`, JSON POST `/auth/password`, and the subsequent CSRF-protected
  POST `/auth/refresh`. Single trailing-slash aliases
  work without redirects. Browser endpoints reject all Authorization headers;
  device JWTs and old human JWTs never fall back to cookies.
- Human HTTP composition requires an explicit canonical public origin. Remote
  plaintext login is rejected. A forwarded HTTPS scheme/peer is accepted only
  from configured trusted proxies. A proxy forwarding remote HTTP to loopback
  is not mistaken for a local user. Writes require the exact Origin and CSRF
  (login uses Origin without an existing session); cross-site fetches fail.
- Account/peer attempt limits (5/50 per five minutes) are DB-persistent, bounded,
  and checked before password work. Canonical DB user IDs prevent collation
  aliases from bypassing account limits. Admission bounds concurrent requests
  and expensive password work; storage failures fail closed.

## Vue integration

Only embedded builds use cookies. Existing standalone Vue builds keep the
Python JWT contract. Embedded clients discard old access tokens, hold CSRF in
memory, restore it via `/auth/me` after reload, and send it on mutations. The
router verifies the server session instead of trusting localStorage. Cached
user data is display-only and never grants server permissions.

The existing login form now supports forced password changes, with compact
English/Korean/Japanese labels. Duplicate submission is blocked, credentials
are not logged, and the profile logout button waits for server revocation.
No signup/recovery placeholders are shown in the embedded login. The existing
clock favicon is retained.

## Verification

Node tests exercise CSRF lifetime, reload, concurrent bootstrap, failed bootstrap,
stale in-flight responses and legacy token removal. Go tests cover password
vectors, scope construction, migration idempotence, seeded-password restriction,
durable sessions/attempt limits, status/version/expiry revocation, session caps,
CSRF/origin/proxy rejection and password/logout atomic rollback on audit failure.
CI runs real disposable MySQL 8.4/MariaDB 11.4 databases only on loopback, and
passlib independently verifies a newly generated Go password hash.

`TestBrowserSessionFlow` serves the real embedded Vue build and drives Chromium
through login -> forced password change -> login -> reload -> profile logout.
It checks HttpOnly cookies, absence of token/CSRF in web storage, stale JWT
removal and server-side logout. External font/icon requests are blocked during
this test. This is the authentication flow, not a complete dashboard test.

## Remaining gates

The [migration overview](go-server-migration.md) records the current status of
subsequent administration, dashboard, OS-account, SQLite and remembered-session
slices. Installable startup, native Windows end-to-end/browser deployment,
production inventory and cutover validation remain unfinished. No production
server launch command is enabled by this slice. Python rollback still ignores new RBAC/status
restrictions and must not be treated as a permission-safe rollback.

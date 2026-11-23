# Remembered browser sessions and renewal

> Historical implementation slice. The unified executable now supports `serve`;
> see [Go server setup and operation](go-server.md) for current startup and
> upgrade instructions. Status statements below describe this slice when introduced.

Branch: `feature/go-server-unified`, following SQLite baseline `1bcda04`.
This slice adds browser-session continuity to both local and pre-linked OS
accounts. `serve` remains gated. It does not change the running Python server,
the independent agent, production databases, or the main branch.

## Browser behavior

The embedded login form offers **Keep me signed in / 로그인 유지 /
ログイン状態を保持**, initially unchecked. The server records the choice;
cookie attributes alone are not the policy.

| Login choice | Browser cookie | Server lifetime | Renewal |
| --- | --- | --- | --- |
| Unchecked or omitted | Browser-session cookie | Existing configured idle timeout and 12-hour absolute deadline | No extension |
| Checked | Persistent cookie with `Max-Age` and `Expires` | Fixed 30 days from password login; idle time does not invalidate it | Credential rotation after 12 hours, keeping the original deadline |
| Initial/reset password change required | Browser-session cookie even if checked | Existing restricted session | Change password, then sign in again |

The 30-day deadline is not a sliding inactivity limit. Using the application or
renewing its credential never moves it. A browser reopen or server restart with
the same database retains a remembered session. Clearing browser data, reaching
the deadline, signing out, or revoking the account still requires a new login.
Browser session-restore features can retain an ordinary session cookie; the
server still enforces that session's idle and absolute deadlines.

The client restores its session on login/dashboard entry and visible-page
return, and renews a due credential before protected requests. It uses a
server-relative renewal delay rather than trusting the browser's wall clock.
There is no periodic keep-alive that extends an unattended ordinary session.
Background dashboard GET polling still does not count as activity.

The login page resumes a valid session directly, retaining the initial-password
change gate when required. Successful logout or password change invalidates
other tabs through a browser event; the server revokes the session independently
of that event. Late session responses cannot restore a cleared client state.
Temporary network/database failures do not erase credentials or pretend a
logout succeeded. Application mutations are not automatically replayed.

Only embedded builds use this contract. Standalone Vue continues to send the
existing Python login payload and use its JWT behavior. Session credentials and
CSRF secrets are not stored in localStorage or sessionStorage; cached user data
is display-only.

## HTTP contract

All paths are relative to the configured API context, including
`/timeweaver_svr`. Existing single-trailing-slash aliases work without redirects.

| Endpoint | Request | Response behavior |
| --- | --- | --- |
| `POST /login` | Existing form fields plus optional `remember=true` or `remember=false` | Fresh session, persisted choice, matching cookie lifetime |
| `GET /auth/me` | HttpOnly session cookie | User, stable CSRF token, session metadata; no credential rotation or `Set-Cookie` |
| `POST /auth/refresh` | Cookie, exact Origin and `X-CSRF-Token`; empty body | Rotate when due or recover the same successor during overlap; otherwise no cookie update |
| `POST /logout` | Existing cookie and CSRF checks | Delete the whole session family, then clear the cookie |

Duplicate or malformed `remember` values fail rather than silently opting a
browser into persistence. Login, session lookup and refresh return the existing
`user` and `csrf_token`, plus:

```json
{
  "session": {
    "remembered": true,
    "expires_at": 1800000000,
    "refresh_after": 1797451200,
    "refresh_after_seconds": 43200
  }
}
```

The timestamps are Unix seconds; the values above illustrate the shape only.
`refresh_after_seconds` is nonnegative and relative to server evaluation.
Remembered sessions with a due or overlap credential report zero. Ordinary
sessions report zero for both renewal fields and `remembered=false`; clients
must not attempt to turn them into remembered sessions. These fields contain
no bearer token. The server remains authoritative for expiration and access.

Refresh uses the same canonical-origin, trusted-proxy, HTTPS, SameSite=Strict,
HttpOnly and API-context cookie rules as login. It requires the existing CSRF
secret. A reload first reads `/auth/me` to recover CSRF in memory, then refreshes
if needed. Human endpoints reject Authorization headers, including agent JWTs.
A GET request cannot rotate the credential. Errors do not clear cookies that
another tab may have just renewed.

## Rotation, overlap and revocation

`tw_session.digest` identifies a stable session family. It is not accepted as a
credential merely because it identifies that row. The current credential has a
separate SHA-256 digest. Authorization resolves that credential, locks the user
and session in the existing order, and rechecks policy in the same transaction
as an application mutation.

After 12 hours, an authorized refresh derives a successor using HMAC-SHA256 with
a fresh random per-rotation seed, a fixed domain separator, the family ID and
the presented current credential. Only credential digests and the seed are
stored. The database does not contain the raw current or previous credential.
Knowing an old credential alone does not reveal a future successor.

The previous credential is accepted for a fixed 60-second overlap. A concurrent
or retried refresh with that credential reconstructs the **same** successor and
verifies its digest; it cannot create a competing successor, extend the overlap,
or extend the absolute deadline. An overlap `/auth/me` response reports renewal
due so a reload can repair a lost first refresh response. Recovery after a lost
response is bounded by this overlap; a predecessor presented after it ends
requires login. An expired predecessor does not revoke the valid current family.

CSRF remains stable within the family, so one tab's renewal does not invalidate
another tab's pending mutation header. The ten-session cap counts families.
Logout removes the family, invalidating both current and overlap credentials.
Password changes, user-version/status changes, external password-hash changes,
OS-provider unavailability, and missing/rebound OS identity bindings retain their
existing revocation checks. Refresh never bypasses the forced-password-change or
group-authorization boundary.

An OS password is still verified only at login. Existing application sessions do
not retain OS passwords or invoke PAM/Windows on every request. An OS-side
password change or account disable alone is not immediate application-session
revocation; the application's user disable/rebind path remains authoritative.
See [OS-account limitations](go-os-access-migration.md).

## Access schema v4 and upgrade

The explicit access initializer upgrades the auxiliary schema from v3 to v4.
Earlier supported versions pass through their existing upgrades first. The
new fields retain the remembered flag, current/previous credential digests,
overlap deadline, rotation time and derivation seed. Existing v3 sessions copy
their digest into the current-credential field and preserve their original
timestamps, CSRF, user version, method and absolute deadline. They remain
ordinary sessions; users must log in with the checkbox selected to opt in.

The upgrade preserves users, password hashes, groups, removed grants, OS
bindings, business data and the legacy migration history. It does not replay
initial membership seeding. SQLite applies the upgrade transactionally. MySQL
and MariaDB retain the explicit `running`/`ready` journal and advisory lock;
unfinished DDL is refused for inspection, not automatically replayed.

Use the existing read-only status and explicit write commands against a copy:

```sh
go run ./cmd/timeweaver access status --env copied-server.env
go run ./cmd/timeweaver access init --env copied-server.env --allow-write
```

These commands do not authorize a production cutover. The existing backup and
stopped-writer prerequisites still apply to an operational database migration.
The implementation and automated tests use disposable databases only.

## Verification

The gates cover the service, composed HTTP listener and actual embedded UI:

- Access tests exercise ordinary/remembered deadlines, mandatory-password
  restriction, concurrent rotation, lost-response recovery, the fixed overlap,
  current/previous revocation, audit rollback and v3 preservation.
- Shared server tests exercise the route and cookie contract, CSRF/origin/bearer
  rejection, renewed credentials and a fresh listener over the same database.
- Node tests cover relative renewal scheduling, concurrent bootstrap/refresh,
  late responses, failed requests, state invalidation and the standalone login
  contract.
- `TestBrowserSessionFlow` uses a real persistent Chromium profile for checkbox
  login, long-idle return, close/reopen, server restart, multi-tab behavior,
  lost-response recovery, explicit logout and ordinary-session behavior.
- The SQLite, MySQL 8.4 and MariaDB 11.4 jobs retain all seven browser flows,
  native account tests remain on Linux/Windows, and the pinned real Go agent
  exercises the composed browser/agent listener. Existing Python agent-contract
  verification stays in the MySQL/MariaDB gates.

Consult the workflow run for the exact commit's executed results. Remaining
production gates are installable startup, deployment/database inventory,
native Windows browser/end-to-end deployment and rollback validation.

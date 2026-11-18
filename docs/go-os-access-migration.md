# OS account login and identity bindings

Branch: `feature/go-server-unified`, following chart checkpoint `d0865b3`.
This slice adds OS authentication to the unified Go browser surface. Production
`serve` remains gated. It does not change the frozen Python server, original
agent, embedded legacy SQL, or the separate agent repository.

The preceding contracts remain applicable:
[human sessions](go-human-access-migration.md),
[administration](go-administration-migration.md),
[unified surface](go-surface-migration.md), and the
[overall migration checklist](go-server-migration.md). The OS-login item in the
earlier human-session document describes that earlier checkpoint; the behavior
below supersedes it for this slice.

## Authentication and administration contract

An OS account can authenticate only after an application administrator creates
an explicit identity binding. There is no automatic user creation, OS-group
import, administrator inference, or permission assignment from the operating
system. Application roles and group memberships remain authoritative.

| Surface | Behavior |
| --- | --- |
| `OS_LOGIN_ENABLED` | Configuration boolean, default `true` when loaded through the normal configuration loader. `false` omits the native provider. A capable host and usable native dependencies are still required. |
| `GET /auth/methods` | Returns `{"methods":["local","os"]}` when the configured provider is available; otherwise returns `{"methods":["local"]}`. |
| Form `POST /login` | Accepts `username`, `password`, and optional `method=local\|os`. Omitted/empty method preserves local-login compatibility. Duplicate or unknown methods are rejected. |
| `GET /auth/me` | Uses the existing cookie/CSRF contract and includes the user's `auth_method` and, for OS users, `os_name`. |
| Administrator user creation | `auth_method:"os"` requires `os_name` and rejects a supplied password. The server resolves the stable subject. An omitted application `user_id` becomes `os_` plus 32 random hexadecimal digits, independent of the OS subject. |
| Administrator user update | The authentication method is immutable. Existing OS users can be rebound using another `os_name` and the current optimistic version. Empty/unchanged `os_name` preserves the binding, including during provider unavailability. |
| Administrator user list | Includes `auth_method` and canonical `os_name`; an unavailable provider does not hide existing users or prevent ordinary role/status edits. |
| Password changes/resets | OS users receive `409 os_password_managed`. Their passwords must be managed through the operating system. |

The internal application ID remains stable across an OS rebind. Random IDs avoid
the collision that would result from deriving the application ID from the
original subject, rebinding that user, and then linking the original OS account
to a new application user. The separate subject uniqueness constraint prevents
two application users from simultaneously owning the same OS account.

Creation and rebinding resolve account names on the server. A client cannot
submit a subject, UID, SID, native token, or an OS password as an administrator's
binding input. An OS account may resolve while its OS login is disabled or
expired; actual authentication still applies OS policy. Changing an existing
local user into an OS user, the reverse conversion, and dual local/OS credentials
for one application user are outside this slice.

OS login never retries with the local password verifier. Unknown/unlinked
subjects, disabled application users, wrong passwords, and rejected OS account
policy return the existing generic credential failure. Provider unavailability
returns `503 os_login_unavailable`; invalid administrator account resolution
returns `422 os_account_not_found`; duplicate bindings return
`409 os_identity_in_use`; authentication-method conversion returns
`409 auth_method_immutable`.

The existing transport boundary applies to both login methods: HTTPS at the
canonical origin, or a genuine direct loopback HTTP client; exact Origin
validation; narrowly trusted forwarding proxies; no human/device JWT fallback;
bounded request bodies; and no credential logging. Browser mutations continue
to require CSRF. The standalone Python UI keeps its original login payload and
does not query the Go-only OS authentication methods.

The embedded login form defaults to local authentication, offers OS login only
when advertised, and clears the password when the selected method changes.
The user modal exposes OS account linking and rebinding, while omitting local
password fields and reset actions for OS users.

## Provider boundary and resource limits

`internal/access` defines `OSIdentity{Subject, Name}` and an `OSAuthenticator`
interface with `Available`, `Resolve`, and `Authenticate`. The native provider
is selected at build time for Linux or Windows; other platforms return
`ErrOSUnavailable`. `NewWithOS` injects a provider into the access service.
The existing `New` constructor remains local-only for compatibility, and
`NewBrowserSurface` supplies the native provider when configuration enables it.
No production test-provider switch or environment-based credential bypass is
added.

Each native provider admits at most four concurrent operations. Every operation
has a 15-second provider deadline, shortened by its caller's deadline; the human
HTTP boundary currently has a 10-second deadline. A cancelled call retains its
admission slot until the underlying native work actually returns. This matters
on Windows, where synchronous account lookup and `LogonUserW` cannot be forcibly
cancelled. Cancellation can return promptly to the request without allowing an
unbounded number of abandoned native workers. A late successful native result
cannot issue a session for an ended request.

The existing persistent peer limit remains 50 attempts per five minutes.
Resolved OS accounts use five attempts per stable subject in that interval, so
local aliases and account renames do not create fresh password-attempt buckets.
Unresolved names have a separate normalized-name bucket. The server checks the
binding and application status before calling the native password verifier.

## Linux behavior

The Linux implementation supports local `/etc/passwd` accounts. It reads that
file directly even in a development build with cgo enabled. Directory-only
LDAP/NSS identities are not supported by this provider. The Go release build
remains `CGO_ENABLED=0`; no PAM shared-library binding or shadow-password reader
is introduced.

OS login is unavailable when either the real or effective server UID is zero.
This prevents PAM's root trust for `su` from being mistaken for a checked
password. The provider requires root-owned executables at `/usr/bin/su`,
`/usr/bin/id`, and `/bin/sh`, with no group/world write permission; `su` must also
have its set-user-ID bit. The supported `su` implementation is util-linux with
the host's PAM configuration. Configuration files read by the provider must be
regular root-owned files without group/world write permission, and reads have
explicit size bounds.

Authentication executes only the following fixed probe:

```text
/usr/bin/su --shell /bin/sh --command "exec /usr/bin/id -u" -- ACCOUNT
```

`ACCOUNT` is a validated separate argument. The command does not accept task
commands, paths, or shell fragments from the caller. The child starts in `/`,
uses a fixed small environment, and has a new session without the server's
controlling terminal. The password is supplied once through a pipe and the
temporary byte buffer is cleared afterwards. It is absent from argv, the child
environment, audit records, and diagnostic output. NUL, CR, and LF credentials
are rejected; this is not an interactive PAM or password-change conversation.

The provider checks the account's configured shell before invoking `su`.
An empty shell or `/bin/sh` is usable directly; another shell must appear in
`/etc/shells`. `nologin`, `false`, and unrecognized/restricted shells are denied.
This preflight matters because util-linux ignores `--shell` for restricted
target shells. The supported path runs the fixed non-login `/bin/sh` probe,
without user login startup files. PAM authentication, account restrictions,
expiration, and session checks still apply. Expired credentials are denied;
TimeWeaver does not complete a PAM password-change challenge.

Standard output is limited to 128 bytes and must reduce to the exact expected
canonical UID. Standard error is discarded. Cancellation sends `SIGTERM` to
`su`, whose documented handler terminates its child; a three-second Go wait
delay allows its two-second cleanup interval before a hard termination.

### Linux subject and host binding

The persistent subject is:

```text
linux:uid:<canonical decimal UID>:<64 lowercase hexadecimal HMAC digits>
```

The HMAC is SHA-256 with the decoded 16-byte `/etc/machine-id` as its key and the
fixed application domain
`github.com/horrible-gh/TimeWeaver/os-account/linux/v1` as its message. The raw
machine ID is not stored in the identity table or exposed to the client. Missing,
empty, uninitialized, malformed, uppercase, or all-zero machine IDs disable OS
login. A fixed test vector protects this persistent format from accidental
changes between releases.

This follows systemd's recommendation to derive an application-specific keyed
identifier rather than disclose the machine ID. A database copied to a host
with a different machine ID cannot make that host's equal numeric UID satisfy
the stored binding during a fresh login. The provider does not generate,
replace, or repair a host's machine ID.

Host uniqueness remains an operating-system provisioning requirement. A cloned
VM/container that retains the same machine ID retains the same host scope.
Reusing an existing numeric UID on the same host also reuses this OS identity;
the provider cannot distinguish that from a rename of the original account.
Administrators must disable or rebind the application user before deliberately
reassigning such an identity. Changing the host machine ID requires explicit
relinking; it is not an automatic account migration.

## Windows behavior

Windows resolves the requested account with `LookupAccountName` through
`LookupSID`, requires `SidTypeUser`, and reverse-resolves the SID to a canonical
`DOMAIN\account` display name. It does not depend on user-profile or primary
group lookup being available to the service account.

| Input form | Scope |
| --- | --- |
| `account` | Local server account database; resolution is explicitly host-qualified. |
| `.\account` | The same local account. |
| `HOST\account` | Explicit host account. |
| `DOMAIN\account` | Explicit domain account. |
| `account@example.test` | UPN form, with a null domain argument to `LogonUserW`. |

The system DLL is loaded through `windows.NewLazySystemDLL`. Authentication uses
`LogonUserW` with `LOGON32_LOGON_NETWORK` (3) and the default provider (0).
`NEW_CREDENTIALS` is not used. The returned token's user SID must exactly match
the previously resolved user; a different token identity, including an account
mapped to Guest, is rejected. The token is closed on every exit path, and the
temporary UTF-16 password buffer is cleared. No token impersonation, process
creation, task execution, or elevation follows successful login.

The stored subject is `windows:sid:<SID>`. Local SIDs carry their machine scope;
domain SIDs carry their domain scope. A rename preserves a SID. Deleting and
recreating an account normally creates a different SID and requires a new
administrator binding. Native logon rights, disabled/expired account policy,
and password-change requirements remain enforced by Windows.

Domain and UPN parsing are supported, but this slice does not claim a successful
live Active Directory/domain test. The native CI fixtures are local Windows
accounts. Smart-card, Windows Hello, integrated browser authentication, and
multi-step interactive authentication are not implemented.

## Schema v3, sessions, and recovery

Access schema v3 adds only the auxiliary `tw_user_identity` table:

| Column/constraint | Meaning |
| --- | --- |
| `subject VARCHAR(255)`, ASCII binary primary key | One exact stable OS subject per binding. |
| `user_id`, unique foreign key to `users` | At most one OS identity for each application user; retains the legacy user-ID type/collation. |
| `os_name VARCHAR(255)`, UTF-8 binary | Current canonical account name for administration and display. |
| `created_at BIGINT` | Creation time as Unix seconds. |

OS-only users carry the invalid local-password marker `!os`; OS passwords and
OS password hashes are never persisted. The marker is not a usable credential.
A successful verified rename refreshes `os_name` while preserving the subject
and application ID.

The explicit access migration upgrades v2 to v3, and v1 passes through the
existing v2 audit upgrade first. It does not reconstruct removed grants, rewrite
local password hashes, delete local sessions, or manufacture OS bindings for
legacy users. It retains the migration advisory lock and `running`/`ready`
journal. An untracked identity table, incompatible schema, or partial DDL fails
closed for inspection instead of being automatically replayed. Existing backup
and old-server shutdown prerequisites for a writable access migration remain
in force.

An external OS authentication runs without holding the application transaction
open. Before issuing a cookie, the transaction rechecks the user status,
version, authentication method, binding, verified subject, provider availability,
and request context. A concurrent disable, role change, or rebind cannot be
undone by a late password-verification result. User/binding/group/audit changes
remain atomic, and audit details contain policy changes rather than credentials
or native tokens.

The existing session policy remains: random opaque cookies, stored digests,
HttpOnly/SameSite/HTTPS attributes, CSRF, idle expiry, 12-hour absolute expiry,
and a ten-session cap. `tw_session.auth_method` records the method. OS sessions
fingerprint `os` plus their exact subject, rather than the shared `!os` marker.
Method mismatches, missing/rebound identities, application status/version
changes, and provider unavailability reject those sessions.

OS policy is checked when a password is authenticated. Existing application
sessions do not retain passwords or reauthenticate to PAM/Windows on every
request; an OS password change or OS-side account disable is not an immediate
application-session revocation mechanism. Application administrators can disable
the user to revoke access immediately. Existing cookies also require deliberate
session revocation during a database/host migration; the Linux host hash protects
fresh authentication and does not by itself rewrite stored session state.

The final usable local administrator cannot be disabled, demoted, or put into a
forced-password-change state merely because OS administrators exist
(`409 last_local_admin`). This retains a recovery path when the native provider
is disabled or unavailable. The existing last-administrator guard also remains.

## Verification and remaining gates

The following checks are implemented or wired as verification gates; this
document does not assert that a new remote CI run has passed.

- Pure provider tests cover cancellation races, bounded native admission,
  malformed credentials, local account parsing, fixed probe arguments,
  controlling-terminal isolation, bounded output through the actual `io.Copy`
  path, restricted shells, root unavailability, and the stable host-HMAC vector.
- Windows-specific unit tests cover name/domain/UPN parsing. The Windows access
  test binary cross-compiles with `CGO_ENABLED=0`.
- `scripts/test-native-os-auth-linux.sh` creates disposable normal users on the
  hosted Linux runner, checks a correct/wrong password, missing account, locked
  account, expired account, expired password, and `nologin`, then removes the
  accounts. It separately checks root-provider refusal without credentials.
- `scripts/test-native-os-auth-windows.ps1` creates disposable local users,
  checks correct/wrong credentials, local aliases and stable SID, disabled and
  expired accounts, and the required-password-change policy, then removes the
  accounts. The fixture uses the documented WinNT `PasswordExpired` property.
- Native scripts build the test binary without cgo, require an explicit hosted
  CI environment, and do not modify PAM, sudoers, network-logon policy, or global
  password rules. They have not been executed against local workspace accounts.
- The MySQL/MariaDB access tests inject a deterministic provider to test identity
  isolation, explicit grants, binding ownership/rebinding, no local fallback,
  password-management boundaries, session invalidation, concurrent policy
  changes during authentication, rate limits, audit rollback, local-admin
  recovery, and v2 migration preservation.
- `TestBrowserOSFlow` drives the real embedded Vue/HTTP surface with a test-only
  injected provider. It verifies the client/session/administration contract;
  real PAM or Windows password verification is exercised separately by the
  native tests, not claimed as browser end-to-end coverage.

Real domain authentication, native Windows browser/end-to-end deployment checks,
SQLite's complete production schema/dialect path, deployment inventory and
production cutover remain separate gates. Passing the native provider tests does
not unlock production `serve` or authorize running application tasks with OS
credentials. A Python rollback still does not enforce the Go RBAC/status model.

## Primary references

- [util-linux su manual](https://man7.org/linux/man-pages/man1/su.1.html): PAM,
  restricted-shell handling, command behavior, and signal cleanup.
- [PAM account validation](https://man7.org/linux/man-pages/man3/pam_acct_mgmt.3.html):
  account/token expiry and denial when required password changes cannot be handled.
- [systemd machine-id](https://www.freedesktop.org/software/systemd/man/latest/machine-id.html)
  and [application-specific machine IDs](https://www.freedesktop.org/software/systemd/man/262/sd_id128_get_machine.html):
  host-ID format, confidentiality, and keyed derivation.
- [LogonUserW](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-logonuserw):
  logon types, local/UPN scope, token ownership, and logon rights.
- [LookupAccountNameW](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-lookupaccountnamew):
  explicit account-domain resolution and account kinds.
- [WinNT password-change requirement](https://learn.microsoft.com/en-us/windows/win32/adsi/winnt-user-must-change-password-at-next-logon):
  the native Windows fixture's `PasswordExpired` setting.
- [SpawnPoint reference](https://github.com/horrible-gh/SpawnPoint/tree/a2927a9c37623706283483e1b8dcf261641165a1/internal/access):
  the original Linux `su` and Windows `LogonUserW` provider design; TimeWeaver
  additionally scopes Linux subjects to the host and bounds native admission.

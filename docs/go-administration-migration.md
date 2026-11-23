# User/group administration and scoped device browsing

> Historical implementation slice. The unified executable now supports `serve`;
> see [Go server setup and operation](go-server.md) for current startup and
> upgrade instructions. Status statements below describe this slice when introduced.

Branch: `feature/go-server-unified`, following `a86a45e`.
This remains a migration build. Production `serve` is not enabled. The main
branch, deployed server/database, legacy Python/SQL and separate agent repository
are not changed by this slice.

The subsequent [OS account slice](go-os-access-migration.md) extends this surface
with pre-linked OS users and schema v3, while preserving the local administration
and audited-transaction rules described here.

## Implemented surface

- Embedded Vue now has an administrator-only Users page: create local users,
  edit name/email/status/global role, assign viewer/operator/editor memberships,
  and reset another user's password. English/Korean/Japanese labels use the
  existing shell and shared modal. No OS identity is claimed to be supported.
- New/reset passwords are temporary: the existing password-change gate applies
  before group/device access. Credentials are never written to browser storage
  or audit detail. The administrator's own password uses the existing authenticated
  password-change API, not the reset endpoint.
- User list/create/update: GET/POST/PUT `/dashboard/users`.
  Password reset: POST `/dashboard/users/password`.
  These paths, like the existing group/device paths, are under the API context.
- Existing group list/create/update/delete paths are served by Go. Mutations
  require an administrator; lists are filtered by current group memberships.
  Client-supplied creator/modifier values cannot replace the session actor.
- Device lists at `/dashboard/devices/get_devices` and
  `/dashboard/schedule/get_devices` use parameterized server-side scopes. The
  legacy group_id query is accepted but cannot enlarge permissions. Responses
  retain legacy fields and add the actual group name; no device secrets are sent.
  Embedded device edits/enrollment controls remain hidden until their APIs are
  ported. Standalone Vue retains its existing Python behavior.

## Mutation safety

Every user/group mutation locks the access-schema guard row first, then the
actor and target users, and keeps session/CSRF/role verification in the same
transaction as its writes and audit. The database lock coordinates independent
service instances, rather than relying on a process-local mutex. A concurrent
revocation is ordered before or after the whole mutation, never between an
unchecked permission test and a write.

User updates use the last displayed version. A stale edit returns 409 rather
than overwriting newer permissions. Successful edits or password resets increment
the user version and revoke all that user's sessions. An administrator editing
themselves is sent back to login. The last active, unrestricted administrator
cannot be demoted/disabled; a seeded/password-change-restricted administrator
is not counted as a usable fallback. Users are disabled rather than hard-deleted.

Deleting a group retains legacy reassignment of devices, schedules and enrollment
records to group 0, but removes that group's memberships and revokes affected
users' sessions. Remaining explicit memberships determine default-group metadata.
An empty membership list does NOT gain a global group-zero grant. Unused
enrollment tokens are revoked before reassignment so they cannot enroll into a
different scope after deletion. Reserved group 0 cannot be deleted or edited.

Account changes, grants, session invalidation and audit records commit together.
Audit/storage failures fail closed and roll back transactional changes. Transport,
Origin, CSRF, human/device credential separation, request limits and password-work
admission are the same as the prior human-authentication surface.

## Access schema v2

Fresh access initialization includes audit `target` and `detail` columns.
The existing explicit `access init --allow-write` command upgrades the known v1
shape under the migration advisory lock. It preserves sessions, hashes and grants
and never repeats the initial membership import. Stop old servers/migrators and
back up first. The journal changes to running before MySQL DDL; failed/partial
upgrades are not silently replayed. `access status` stays read-only. A v1 schema
cannot start the new access service until explicitly upgraded.

## Verification coverage

Tests target disposable, explicitly allowed loopback databases only. The added
DB tests cover administrator/CSRF enforcement, initial/reset password gates,
version conflicts, revoked sessions, cross-group grants, invalid group rollback,
concurrent last-admin protection across two services, audit-failure rollback,
group-zero non-escalation, unused enrollment revocation, and v1 upgrade preserving
sessions and already-removed grants.

`TestBrowserAdministrationFlow` uses the actual embedded Vue build and Chromium:
group create/rename/delete, user create/edit/reset, mandatory password change,
limited-user group/device views, forged group_id and Authorization requests,
revoked-session rejection, and last-admin rejection in the UI. It runs beside
the existing authentication browser test on MySQL 8.4 and MariaDB 11.4. Existing
legacy migration/password/agent interop and real-agent lifecycle gates remain.
Passing browser tests in this slice does not imply that every dashboard works.

## Still pending

Device mutations/enrollment administration, schedule/task/manual-execution/history
and chart APIs, complete permission integration for those resources, OS login and
identity linking, session remember-me/rotation, full SQLite support, native Windows
end-to-end/browser checks, installable server CLI, deployment and permission-safe
rollback. No production server launch or main-branch merge is authorized here.

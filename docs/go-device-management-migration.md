# Device management and enrollment administration

> Historical implementation slice. The unified executable now supports `serve`;
> see [Go server setup and operation](go-server.md) for current startup and
> upgrade instructions. Status statements below describe this slice when introduced.

Branch: `feature/go-server-unified`, following `0ff6cb7`.
This is still a migration build; production `serve` remains disabled. No legacy
Python/SQL files, main branch, deployed database or separate agent repository
are changed by this slice.

## Implemented

- Existing device insert/update/delete routes now use the current human session,
  Origin/CSRF protection and a transaction spanning authorization, writes and
  audit. Creation requires editor rights in an existing active group. Updates
  and deletions resolve the device's actual group under a row lock before
  checking editor rights. Viewer/operator cannot edit; global editors still
  cannot issue enrollment material. Client creator/modifier fields are ignored.
- Updating a device changes only its name/status, like the Python API. It does
  not move the device between groups. A supplied mismatching group ID is rejected.
  Group-zero authority must be an explicit global membership (or administrator).
  Names support Unicode, are trimmed and bounded; reserved nonpositive device
  IDs cannot be changed. Duplicate names within a group return 409.
- New requests using credentials of an inactive device are blocked by the
  existing agent boundary. Re-enabling preserves the legacy pause semantics:
  it does not silently destroy working credentials or force re-enrollment.
  This is NOT a command to cancel work already running on an agent.
- Physical deletion is deliberately more conservative than Python's unguarded
  DELETE: schedules, credential history, events, consumed enrollment records or
  live name-bound enrollment material prevent removal (409 `device_in_use`).
  Disable a registered device instead. Only unused, unreferenced manual entries
  can be removed. No records are cascade-deleted or reassigned here.
- GET/POST `/dashboard/agent-enrollment-tokens` and DELETE `/{uuid}` preserve
  the existing envelope, field names, status codes and single trailing-slash
  aliases without redirecting. All require global administrator privileges.
  Issue TTL defaults to 24 hours, permits 1..168, uses database UTC time and
  binds an optional normalized device name to an active group.
- Enrollment material is `enr_` plus 32 random bytes in hex. Only its SHA-256
  digest is stored; the random UUID uses the existing binary(16) representation.
  The raw material is returned once, only after a successful commit, with
  `Cache-Control: no-store`. List/revoke/audit responses never contain secrets or
  their digests. Failed or uncertain commits do not return generated material.
- Revocation locks the same enrollment row that the agent consumes. A consumed
  token returns `already_used`; a repeated revocation returns its original time.
  List status precedence remains used, revoked, expired, unused. A group filter
  on this admin-only list is validated, not interpreted as a permission grant.

## Existing Vue integration

The device screen now exposes add/edit/delete according to current permission
hints and its server-provided `can_edit` field. Actual API authorization remains
mandatory. Creation offers only editable groups, including explicit default
(group 0) access. Saving/removing cannot be double-submitted; failed removal is
visible outside the modal. The manual form sends only the intended fields.

Administrators can use the existing enrollment wizard and token history. The
embedded role check uses `/auth/me`, not a speculative admin API failure.
Reissue choices and validation both require an active device in the chosen
group. Switching group clears an existing-device selection, and successful
registration highlighting matches group plus name, not name alone. The history
now displays the actual `used_by_device_id` field. Added labels/errors have
matching English, Korean and Japanese translations; the clock favicon remains.
Standalone Vue keeps its Python JWT/API behavior.

## Verification coverage

Node tests cover display permission boundaries, global versus default-group
metadata, same-name cross-group reissue choices, inactive devices and locale keys.
Go validation tests cover typed/bounded JSON, name and ID bounds, and UUID parsing.
Disposable MySQL/MariaDB tests cover viewer/operator/editor/admin boundaries,
forged group IDs and actors, missing CSRF, disabled groups, name conflicts,
linked-record preservation, server-side credential pause, reissue identity and
old credential rejection, used/expired/revoked material, consume-versus-revoke
races, and audit-failure rollback of issue/revoke/device create/update/delete.

`TestBrowserDevicesFlow` drives the actual embedded Vue build: manual create,
edit and remove; scoped viewer/operator/editor controls; wizard issue/reissue;
agent enrollment over HTTP; disable/enable; linked-device delete rejection and
material revocation. Existing authentication/administration browser tests stay.
The real Go-agent lifecycle test now obtains its material through the human
management service rather than manually inserting an enrollment row. Existing
Python contract tests, both databases and Windows/Linux build gates remain.

Tests create only explicitly permitted random loopback databases. Browser and
real process integration still run on Linux; native Windows end-to-end is pending.
No new schema migration is introduced in this slice.

## Next gates

Schedule/task/manual-execution/history/chart APIs and their complete RBAC,
OS identities/login, session remember-me/rotation, full SQLite schema/dialect,
installable CLI, deployment inventory and permission-safe rollback remain.
Do not install this branch as a replacement for the production server yet.

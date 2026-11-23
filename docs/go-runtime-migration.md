# Agent heartbeat and snapshot slice

> Historical implementation slice. The unified executable now supports `serve`;
> see [Go server setup and operation](go-server.md) for current startup and
> upgrade instructions. Status statements below describe this slice when introduced.

Branch: `feature/go-server-unified`, following identity baseline `6eaadc1`.
This is not an operational release. `serve` remains disabled, and the legacy
Python sources, original SQL, main branch and deployed instances are untouched.

## Implemented

- `Runtime` and `RuntimeHTTP` add `/heartbeat` and `/snapshot`, including explicit
  trailing-slash aliases without redirects. Human/dashboard authentication is
  not introduced or bypassed by this handler.
- Heartbeats retain the four runtime states, nullable applied revision, database
  UTC time and active-device predicate. Identical updates in the same second do
  not falsely revoke a device. Only recognized capabilities are persisted; a
  legacy agent's missing capability clears the previous value.
- Snapshot reads pin one connection and use the existing repeatable-read,
  consistent, read-only transaction across devices, schedules, details, task
  definitions and manual requests. Cancellation/error cleanup rolls back before
  returning the connection, discarding it if cleanup fails.
- Snapshot normalization retains nulls, seven cron fields, task-type-specific
  clearing, dense execution sequence ranks, UUID ordering, manual-run sorting,
  lease expiry and the soft-delete filters. Missing task definitions fail the
  whole response rather than returning a partial executable plan.
- Revision hashing excludes transport timestamps and hashes only exposed device,
  schedule and manual-run values. Python-compatible compact sorted UTF-8 JSON
  includes raw U+2028/U+2029 without changing literal backslash-u strings.
- Authentication and device state checks occur before conditional responses.
  Recursive housekeeping capability is checked BEFORE a matching ETag can
  return 304. An unsupported agent receives the existing 409 error code.
- Identity and runtime handlers share admission slots and device rate buckets.
  Headers/body are bounded. Database errors and invalid source rows do not echo
  SQL, tokens, commands, paths or credentials in API error messages.

## Verification

Tests use only explicitly authorized, random loopback `tw_identity_*` CI databases.
They cover task normalization, a Python-generated canonical JSON/hash vector,
heartbeat updates, redirect-free routes, empty-body 304, capability-before-cache,
revoked devices, orphan tasks, database read-only enforcement and a concurrent
writer while an old snapshot retains one consistent view.

`TestPythonRuntimeInterop` imports the frozen Python repository/service with an
isolated config module (no application startup or production .env), reads the
same disposable MySQL/MariaDB database, and compares every exposed business value,
revision and ETag. Only independently sampled server/generated timestamps are
excluded from equality; their envelope consistency is still checked.

CI also compiles a test-only validator inside TimeWeaver-Agent pinned to
`aa509b196c1caf35cc28b38f8015994da8906819`. It runs the actual
`protocol.DecodeSnapshot` on each successful interop fixture. This does not run
commands or modify the agent repository. It is decoder compatibility, NOT proof
of a complete scheduling/execution cycle.

## Still pending

Execution start/results, manual claims, events, health, the claim sweeper and
full agent end-to-end runs. SQLite schema/dialect completion, browser APIs,
OS login, sessions/RBAC, server lifecycle and production cutover remain separate
gates. The runtime currently requires MySQL/MariaDB; no SQLite support is claimed.

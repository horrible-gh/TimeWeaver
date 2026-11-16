# Agent execution migration slice

Branch: `feature/go-server-unified`, following runtime baseline `5071346`.
This is NOT an operational release. `serve` remains disabled. No production
server/database/agent, main branch, frozen Python source or legacy SQL is changed.

## Implemented

- Device-authenticated POST `/manual-runs/{id}/claim`,
  `/executions/{uuid}/start`, `/executions/{uuid}/results`, and `/events`.
  One explicit trailing-slash alias is accepted without redirects; handlers
  share the existing identity/admission/device-rate-limit boundary.
- Thirty-minute manual leases, expired-lease reclamation, random claim tokens,
  constant-time token comparison, and device/schedule/detail ownership checks.
- Running-state upsert and atomic result insert, running-state clear, and status
  transitions. Lock order remains schedule_group -> schedule_detail -> manual.
- Durable idempotency on execution group/detail/attempt. An identical replay
  returns the original execution_id before checking the now-cleared manual lease;
  a changed payload is rejected. Group error-stop precedes detail error-stop;
  manual completion changes only its manual request, not schedule error status.
- Soft-deleted details reject new starts/claims but accept delayed result reports,
  preserving the existing tombstone/outbox contract.
- Existing schema-1 envelopes, database UTC/second timestamps, bounded bodies and
  environment JSON. Canonical environment text preserves Python's sorted UTF-8,
  finite floating-point representation and arbitrary-sized JSON integer values.
  Non-finite/overflow floats are rejected rather than stored as non-JSON values.
- Read-committed transactions with a three-second lock wait. Session settings
  are restored on the pinned connection, or that connection is discarded.
  Only a statement-level whole-transaction deadlock is retried, after rollback;
  ambiguous COMMIT failures and lock timeouts are not automatically replayed.
- Append-only incident events. Expired-claim sweeps use 500-row batches and
  `FOR UPDATE SKIP LOCKED`. The blocking minute-interval sweeper accepts a root
  context; construction starts no listener or background goroutine.

## Verification

Unit tests cover required fields, identifiers, UTC timestamps, body/environment
limits and a Python-generated canonical environment vector. Disposable loopback
MySQL/MariaDB tests cover twenty simultaneous claims and result retries, duplicate
conflicts, expired tokens, device isolation, tombstones, all error-stop branches,
rollback after a later SQL failure, skipped locked rows and pool-setting cleanup.

`TestPythonExecutionInterop` imports the frozen Python execution service,
repository and schemas without application startup/.env loading. It cross-delivers
claims, starts and results in both directions against one isolated database,
compares persisted environment text, duplicate IDs and transition lists, and
checks ownership/errors/events. This is HTTP/service-contract interoperability,
NOT a running-agent scheduler/outbox end-to-end test.

## Deliberately unchanged / still pending

The reference clears execution_running by schedule/detail, including on duplicate
results; it does not distinguish a newer attempt. This legacy behavior is retained
and requires a separate reviewed change rather than a silent migration fix.
Claim eligibility remains the reference's wait-state rule (no new scheduling or
server-side due-time policy). SQLite execution writes, health, server composition,
full Go-agent execution, browser APIs, OS login, sessions/RBAC and cutover remain
separate gates. Python rollback still does not preserve new RBAC restrictions.

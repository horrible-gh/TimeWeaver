# Agent identity and schema-guard slice

> Historical implementation slice. The unified executable now supports `serve`;
> see [Go server setup and operation](go-server.md) for current startup and
> upgrade instructions. Status statements below describe this slice when introduced.

Work branch: `feature/go-server-unified`; follows store baseline `204e602`.
This progress note supersedes the earlier note that the schema guard and agent
identity API were entirely unimplemented. It is NOT an operational release.
`serve` remains disabled; no production server, database or agent is changed.

## Implemented

- `internal/agentapi`: existing `/enroll`, `/token` and `/token/refresh` handlers,
  including trailing-slash aliases without redirects, schema-1 envelopes and
  existing error codes. A body-size limit and non-disclosing validation are added.
- Device access JWT verification is separate from human authentication. HS256,
  audience, type, subject, issued-at, expiry and credential binding are checked.
  Signature handling uses pinned `github.com/golang-jwt/jwt/v5` v5.3.1.
- Enrollment and rotation preserve the existing MySQL/MariaDB transaction model,
  SHA-256 refresh digests, 15-minute access tokens and 90-day refresh credentials.
  Rotation/re-enrollment revokes prior credentials; disabled devices fail closed.
  Statement-level InnoDB deadlock 1213 (whole-transaction rollback) is retried
  at most twice after rollback; lock timeouts and ambiguous commit failures
  are NOT automatically retried. Each retry rechecks the original token.
- HTTP admission is bounded; rate limits are bounded in-memory token buckets.
  Forwarded peer identity is accepted only from explicitly trusted proxies.
  Server composition must supply its configured limits and trusted proxy list.
  Restart resets these rate buckets, NOT durable device credential revocations.
- `schema status` inspects the two critical enums and hidden group 0 read-only.
  `schema repair --allow-write` requires completed clean migration history, takes
  the migration lock and pins/discards its connection. Missing enum values are
  appended without deleting extra values or changing their ordinal order.
  Defaults, nullability, character set, collation and comments are preserved.
  Unknown column shapes require manual review instead of replacement.
- These DDL repairs are not one atomic transaction. A later failure can leave an
  earlier widening applied. Re-inspection makes additive retries safe, but backup
  and stopping the old server remain prerequisites. No force-repair flag exists.

## Verification scope

CI runs real MySQL 8.4 and MariaDB 11.4 fixtures. Identity tests create only random
`tw_identity_*` databases on loopback with explicit permission, never DB_DATABASE.
They cover enrollment replay, simultaneous rotation, credential revocation,
re-enrollment, inactive-device rollback, invalid JWTs, validation and rate limits.

`TestPythonIdentityInterop` starts a test-only loopback HTTP server, imports the
frozen Python identity service/repository without its application's config side
effects, and performs Python/Go refresh and signature checks against the same
isolated DB. Key modes: derived key, duplicated user/agent secret, separate agent
secret. The `/verify` helper exists only inside the test, not in the product API.

This does not prove complete Go-agent execution compatibility: heartbeat,
snapshots, claims/results, events and health are still to be ported. The admin
endpoint for enrollment-token issuance is not exposed before human auth/RBAC.
SQLite identity writes remain unavailable until its schema/dialect is complete.
Browser sessions, OS login, RBAC, dashboard APIs and production cutover remain
separate gates. Existing Python sources and original SQL are unchanged.

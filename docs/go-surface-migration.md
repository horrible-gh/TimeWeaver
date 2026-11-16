# Unified agent surface, lifecycle and favicon

Branch: `feature/go-server-unified`, following execution baseline `29b07cc`.
This is still a migration build, NOT a browser-ready production replacement.
The production CLI's `serve` remains disabled. No deployed server, database,
agent registration, Caddy configuration or main branch is changed.

## Composition

`internal/server.NewAgentSurface` joins identity, heartbeat/snapshot, execution,
events and the legacy A8 health route with the compiled Vue UI on one listener.
All agent handlers share the same identity/admission limits. Construction takes
already-open stores and a validated webui handler; it does not create databases,
run migrations, bind a port or start a background writer. The caller closes both
borrowed stores only after the blocking `Serve` has returned.

The health route uses a distinct read-only pool with two connections and two
admission slots, plus a context deadline covering pool acquisition and SQL. It
retains the legacy `{"ready":true/false}` response and required-table check.
Saturation/errors/timeouts return 503 with Retry-After and no database details.
This is **agent schema readiness**, not proof that browser authentication/RBAC
or all production-release gates are complete. There is no misleading root health.

Dispatch preserves explicit single trailing-slash aliases without redirects.
Malformed/encoded traversal paths and repeated separators are rejected. Unknown
API routes stay JSON 404 instead of serving a Vue page. Dashboard/login data APIs
remain absent; serving their HTML is not authorization or implemented login.

The settings loader now carries RATE_LIMIT_CAPACITY/REFILL, TRUSTED_PROXIES,
BLOCKING_DB_WORKER_COUNT/QUEUE_CAPACITY, DB_HEALTH_TIMEOUT and HEALTH_RETRY_AFTER.
Go bounds admitted DB work by workers + queue, rather than emulating Python
threads. Invalid/nonfinite values fail closed; unset proxy trust remains empty.

## Lifecycle

Serve owns an already-bound listener and starts the cancellable minute-interval
claim sweeper. HTTP header/read/write/idle deadlines and header sizes are bounded.
Request contexts are independent of the stop signal: accepted result transactions
get up to ten seconds to drain before forced cancellation/connection close. The
sweeper is then cancelled and joined; an unexpected worker exit stops the listener.
Drain/worker failures return errors rather than claiming a clean shutdown.
Panic handling does not log panic values, request bodies, tokens or full URLs.
No OS service is installed and no production serve command is enabled in this slice.

## Favicon

The tab icon is derived from the existing `client/public/logo-timeweaver.png`
clock brand, with a transparent square canvas and 16/32/48-pixel ICO entries.
Both Vue entry templates link the same root-relative favicon with a version query.
The ordinary Vue build and Go embedded build include the same bytes. Unhashed
icons revalidate with ETag instead of being cached forever. No external image host,
icon CDN or new runtime dependency is introduced by the favicon.

## Verification scope

Unit/DB tests cover routing boundaries, no redirects, graceful/forced shutdown,
redacted panic responses, probe deadline/admission, separate health connection
availability during business-pool exhaustion and enforced probe write protection.
Frontend tests validate the ICO entries and both templates; Go tests fetch the
actual embedded icon and pages, compare icon bytes, and check conditional GET.

`TestGoAgentEndToEnd` launches TimeWeaver-Agent pinned to
`aa509b196c1caf35cc28b38f8015994da8906819` as a real Linux subprocess.
It uses only an explicitly allowed loopback DB with a random `tw_surface_*` name
and a temporary work directory. It enrolls through HTTP, writes two temporary
files via real scheduled/manual command execution, deliberately loses a successful
result ACK after commit, observes retry/outbox drain with one persisted result,
then restarts from its saved credential without another enrollment token.
The fixture also checks heartbeat/304 snapshots, unified UI/favicon routes and
graceful subprocess/server stop. CI runs this against MySQL 8.4 and MariaDB 11.4.
Native Windows execution of this whole process scenario remains a separate gate;
Windows unit tests and cross-builds are not presented as its substitute.

Still pending: SQLite schema/dialect completion, human sessions/OS login/RBAC,
dashboard APIs and their UI changes, native OS-auth tests, full-browser operation,
production configuration/DB inventory, deployment/cutover and permission-safe
rollback. Existing Python sources, SQL and the separate agent repository remain
unchanged. Do not deploy this migration CLI over the running Python service.

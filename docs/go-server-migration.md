# Go server migration

Work branch: `feature/go-server-unified`.
Baseline: `d6bdb751e08e68ff98781b5b41e5f90b09383ce2`.

This branch implements the architecture reviewed on 2026-11-16. It is not an
operational replacement for the Python server yet. Do not deploy it over a
running TimeWeaver installation. `main`, existing launch commands, the database,
and registered agents are not changed by this work.

## Target

One Go executable serves the API and compiled Vue UI; no Node or Vue server is
needed at runtime. Keep the independent Go agent and Agent API v1. Use the seven
package boundaries from the design: config, store, access, dashboard, agentapi,
webui, server. Preserve MySQL and complete SQLite before a production release.
Do not add a scheduler or push channel to the server.

## Required corrections to the design

- Never put JWT revocation only in memory in an operational intermediate release.
  Preserve Redis until durable DB revocation/sessions are available.
- Python rollback is not permission-preserving after RBAC changes. A rollback
  requires an explicit access restriction/review; disabled accounts, group roles,
  forced password changes and new OS identities cannot be silently ignored.
- Complete and verify SQLite before the first supported production release.
  Do not infer that there are no SQLite installations from a broken fresh install.
- Enforce the initial password change before operational exposure. New installs
  must not depend on a publicly known seed password for administrator ownership.
- Prefer HTTPS via the existing Caddy deployment. Do not add automatic redirects
  to agent endpoints; explicitly review any retained insecure transport mode.
- Keep existing API CONTEXT values (including `/timeweaver_svr`) configurable.
  The final Caddy upstream should serve both UI and API from the Go listener.
- Use SpawnPoint to manage the process in the user's deployment. Service install
  scripts are optional packaging, not a prerequisite or an automatic installation.
- Separate the real group 0 from wildcard privileges when implementing RBAC;
  reassignment to the legacy Unknown group must not grant access to every group.
- Preserve browser session continuity/renewal as a requirement; importing idle
  expiry alone is not a substitute for SpawnPoint's remembered-session behavior.

## Status

Implemented slices now cover the MySQL/MariaDB store and migration preflight,
agent identity/runtime/execution APIs, embedded Vue surface, human sessions and
group permissions, user/group administration, device enrollment, schedules and
tasks, manual execution and history, scoped dashboard charts/recent schedules,
pre-linked Linux/Windows OS-account login, and the complete native SQLite schema,
query catalog and application-service path. Remembered browser sessions now
preserve continuity across browser/server restarts and rotate credentials while
retaining a fixed absolute deadline.
Each slice retains its own compatibility limits and test evidence:

- [Human access](go-human-access-migration.md) and [administration](go-administration-migration.md)
- [Devices and enrollment](go-device-management-migration.md)
- [Schedules and tasks](go-schedule-task-migration.md)
- [Manual execution and history](go-manual-execution-migration.md)
- [Dashboard charts and recent schedules](go-chart-migration.md)
- [OS accounts and native login](go-os-access-migration.md)
- [SQLite schema, migrations and services](go-sqlite-migration.md)
- [Remembered browser sessions and renewal](go-remembered-session-migration.md)
- [Agent identity](go-identity-migration.md), [runtime](go-runtime-migration.md)
  and [execution](go-execution-migration.md)

CI includes real-file SQLite and disposable MySQL/MariaDB integration, Python
interoperability, embedded-browser workflows, native Linux/Windows account
authentication and an actual pinned Go agent. Consult the exact commit's
workflow run for executed results; a static route inventory alone is
not runtime compatibility. The read-only baseline guard continues to compare
tracked legacy server/agent files and embedded SQL against the pinned baseline.
It never imports the Python app or opens a database.

Still required before production: installable server startup,
production DB inventory (including the exact
historical SQLite schema shapes), and deployment/rollback validation. OS login
has specific host/PAM/domain limitations recorded in its slice document.
`serve` remains disabled. The sections below record the foundation as it was
introduced; newer slice documents describe subsequent implementation.

Run from a full Git checkout:

```sh
python -m unittest discover -s scripts/tests -p 'test_go_baseline.py' -v
python scripts/check-go-baseline.py
```

Legacy SQL and Python sources are frozen while their behavior is being ported.
New implementation belongs outside `server/` and `agent/`. Do not weaken the
baseline guard merely to make a failing migration test pass. No Python code is
removed before replacement and rollback verification have both been completed.

## Initial Go implementation

The Go module targets Go 1.26. The initial code provides a read-only configuration
loader, the compiled-Vue HTTP handler, and a guarded migration CLI. `serve` is
explicitly disabled until the API/database implementation is ready; there is no
fake health endpoint and no accidental production cutover.

- `config.Load` reads an explicit env file and honors environment overrides. It
  does not connect to or create a database. Relative SQLite paths are anchored to
  the env file's directory; an explicit absolute path is preferred at cutover.
  Plain, single-quoted, double-quoted and `export KEY=...` dotenv values work.
  Duplicate keys, multiline values and `${...}` interpolation fail explicitly
  instead of silently resolving to a different database or secret.
- `AgentSigningKey` preserves the current Python derivation, including the case
  where the configured agent secret equals the user secret. This unit check is
  not a complete cross-server JWT interoperability test.
- `webui.New` validates both Vue entry points and their local referenced assets.
  It injects the API CONTEXT at response time, serves deep links without
  redirects, keeps API misses out of SPA fallback, rejects unsafe paths, and
  caches only content-hashed assets as immutable. Unhashed CSS remains revalidated.
- `TW_EMBEDDED_BUILD=1` selects `client/config.embedded.js` regardless of a local
  developer `client/config.js`, and writes to `internal/webui/assets/dist/`.
  Ordinary standalone Vue builds keep their existing configuration and output.
- Generated UI files are not committed. The tracked placeholder sits outside
  `dist/`, so a clean frontend build does not delete tracked files. A missing or
  partial bundle makes `assets-check` fail.
- Existing external font/icon URLs remain unchanged in this first slice; an
  embedded UI does not yet imply fully offline asset loading.

Native build commands (Node is needed only on the build machine):

```sh
bash scripts/build-go.sh
# Windows PowerShell: .\scripts\build-go.ps1
```

Read-only commands:

```sh
go run ./cmd/timeweaver version
go run ./cmd/timeweaver check --env server/.env
go run ./cmd/timeweaver assets-check --context /timeweaver_svr
```

`check` reports **configuration-only** validation, `database_connected: false`
and `server_implemented: false`. It is not the final design's database preflight
command yet. Build outputs are migration CLIs, not deployable API servers.

CI checks the untouched legacy sources, standalone and embedded Vue builds,
Go 1.26 tests on Windows/Linux, Linux race checks, actual embedded assets and
CGO-free Windows/Linux amd64/arm64 compilation. Native OS-login, real DB migration,
full-browser workflows and agent execution are still separate future gates.

## Store foundation and migration preflight

Implemented after the initial UI/config slice:

- `internal/store` embeds all 54 legacy SQL files byte-for-byte. The original
  `server/` and `agent/` remain untouched. Tests verify both copies, all 31 MySQL
  and 7 SQLite migration files, and 39 MySQL query keys.
- SQL splitting respects quotes, escaped quotes and comments; placeholder
  conversion does not alter string literals or date formats. Unsupported
  compound SQL and malformed constructs fail before execution.
- MySQL connections use `database/sql`, pinned driver versions and committed
  `go.sum`. Connection errors expose codes rather than credentials/query values.
- `migrate status` opens an existing database read-only, reports pending/unknown/
  unfinished migrations, and does not create tracking tables. A missing SQLite
  file is an error, not an invitation to create an empty replacement database.
- `migrate up` requires `--allow-write` and works only for the MySQL dialect in
  this slice. It must be used only after a backup and stopping the Python server.
  An untracked, nonempty database is refused rather than treated as a fresh install.
- Files execute on one pinned connection, preserving user variables and PREPARE.
  The legacy `migrations(filename, applied_at)` contract is unchanged. A separate
  `tw_migration_attempt` journal records a durable start before executing SQL.
  Failed/interrupted work blocks automatic replay; MySQL DDL is not falsely
  described as fully rollback-safe. There is no force/automatic repair command.
- A database advisory lock serializes Go migrators only. The Python migrator does
  not participate: this is not permission to run old and new migrators together.
- SQLite read-only inspection and connection protection are tested, but the
  missing legacy bootstrap/query dialect is still incomplete. SQLite `up` fails
  BEFORE opening/creating a database. Full SQLite support remains a release gate.

```sh
# Reads the actual DB but creates no tables or missing SQLite file.
go run ./cmd/timeweaver migrate status --env server/.env
# Writes only after explicit operator acknowledgement; use a DB copy first.
go run ./cmd/timeweaver migrate up --env copied-server.env --allow-write
```

`check` remains configuration-only; use `migrate status` for the database history.
Neither command proves complete API readiness. `serve` remains disabled. No
operational database or running agent has been changed by this work.

### MariaDB versus native MySQL

The existing Python CI uses MariaDB 11.4. On that engine the new CI compares every
legacy table definition with sqloader 0.2.18, verifies retained user hashes,
devices, schedules, tasks, execution history and binary token digests, and checks
that both migrators recognize each other's applied filenames without replay.

Native MySQL 8.4 rejects the frozen `groups_001.sql`: bare `groups` is a reserved
identifier. The Go execution path quotes that table name in table positions while
leaving source bytes/checksums and string literals untouched. The test explicitly
reproduces the untouched Python bootstrap failure on native MySQL rather than
silently modifying the reference and claiming strict parity there. Native MySQL
then verifies Go bootstrap, all query preparation, constraints and no-replay data
preservation. Python can read the completed migration history; this is NOT proof
that the old Python APIs work on native MySQL or that RBAC rollback is safe.

Both disposable database jobs include partial-DDL failure/retry blocking,
connection pinning, read-only reconnection enforcement and untracked-data refusal.
Windows/Linux Go tests also exercise real SQLite files. This slice does not yet
include the schema guard repair port, Agent API transactions, live token exchange,
or production DB inventory.

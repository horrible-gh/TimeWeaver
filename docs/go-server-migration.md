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

Implemented: read-only SHA-256 comparison of tracked legacy server/agent files
against the pinned baseline, static route inventory, seven guard regression tests,
and a branch CI job retaining a source snapshot and inventory as verification
artifacts. The guard never imports the Python app or opens a database.

Not yet complete: runtime response fixtures, production DB inventory, cross-server
token/snapshot tests, Go database/API implementation, session/RBAC implementation,
and operational cutover tests. Static inventory must not be described as runtime compatibility.

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

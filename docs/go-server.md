# Unified Go server

The Go executable serves the compiled Vue dashboard, human authentication,
dashboard APIs and Agent API v1 on one listener. The `serve` command performs
readiness checks, opens the listener, starts the execution claim sweeper, and
drains requests on shutdown. It works with SQLite, MySQL and MariaDB.

The unified server is available on `main`. The legacy Python server and SQL
remain migration references. Install the independent Go agent from
[TimeWeaver-Agent](https://github.com/horrible-gh/TimeWeaver-Agent); its README
contains the agent's configuration and service instructions. The old Python
`agent/` directory and its installer/launcher integration have been retired.

## Build

The build machine needs Git, Go 1.26 and Node.js 22 with npm. The resulting server
does not need a Python installation, Node.js, a Vue development server or Redis.
Build from a full checkout on the target operating system:

```powershell
.\scripts\build-go.ps1
.\dist\timeweaver.exe version
```

```bash
bash scripts/build-go.sh
./dist/timeweaver version
```

The scripts install the locked frontend dependencies, compile the embedded UI,
validate its entries, run Go tests, and build the server. They publish the new
executable to `dist/` only after those steps succeed. Stop an already running
Windows executable before replacing it. `version` identifies the build commit;
a checkout with modified or untracked source files gets a `-dirty` suffix. Release builders
can set `TIMEWEAVER_BUILD_VERSION` and `TIMEWEAVER_BUILD_COMMIT` explicitly.

CI also produces the `timeweaver-server` artifact containing Windows and Linux
executables for amd64 and arm64. These have the same embedded dashboard and a
commit identifier. Choose the executable matching the deployment host.

## Fresh SQLite installation

Create `server/.env` only if it does not already exist. The following examples
generate a private signing key and bind to loopback. `DB_PATH` is relative to
the env file, so this configuration creates `server/timeweaver.sqlite3`.

Windows PowerShell:

```powershell
if (Test-Path -LiteralPath server/.env) { throw 'server/.env already exists; keep the existing configuration.' }
$keyBytes = New-Object byte[] 32
$generator = [Security.Cryptography.RandomNumberGenerator]::Create()
try { $generator.GetBytes($keyBytes) } finally { $generator.Dispose() }
$serverKey = ([BitConverter]::ToString($keyBytes)).Replace('-', '').ToLowerInvariant()
@"
SECRET_KEY=$serverKey
CONTEXT=/timeweaver_svr
LISTEN_HOST=127.0.0.1
LISTEN_PORT=8000
PUBLIC_ORIGIN=http://127.0.0.1:8000
DB_TYPE=sqlite3
DB_PATH=./timeweaver.sqlite3
"@ | Set-Content -LiteralPath server/.env -Encoding UTF8
Remove-Variable serverKey, keyBytes
.\run-go.ps1 -Setup
```

Linux, with OpenSSL available for key generation:

```bash
test ! -e server/.env || { printf '%s\n' 'server/.env already exists; keep it.' >&2; exit 1; }
umask 077
server_key="$(openssl rand -hex 32)"
cat > server/.env <<EOF
SECRET_KEY=$server_key
CONTEXT=/timeweaver_svr
LISTEN_HOST=127.0.0.1
LISTEN_PORT=8000
PUBLIC_ORIGIN=http://127.0.0.1:8000
DB_TYPE=sqlite3
DB_PATH=./timeweaver.sqlite3
EOF
unset server_key
bash run-go.sh --setup
```

The runner builds the executable, applies database setup because `-Setup` or
`--setup` was explicitly supplied, and starts the server in the foreground.
The executable itself checks readiness before listening. Setup asks for a password for the initial
`timeweaver` administrator, with terminal input hidden. Use at least 12
characters. Open `http://127.0.0.1:8000` and sign in with that account and password.

For an already built executable, the equivalent commands are:

```powershell
.\dist\timeweaver.exe setup --env server/.env --allow-write
.\dist\timeweaver.exe check --env server/.env --database
.\dist\timeweaver.exe serve --env server/.env
```

On Linux replace `.\dist\timeweaver.exe` with `./dist/timeweaver`.
For automated provisioning, `setup --admin-password-stdin` reads the password
from standard input. Supply it through the provisioning system's secret input;
there is no password command-line argument. A later setup run preserves an
established password rather than resetting it.

## Existing installation

Keep the existing env file, especially `SECRET_KEY`, `AGENT_SECRET_KEY`,
`CONTEXT`, and database location. Relative SQLite paths resolve against the env
file's directory; check the absolute path printed by `check` before writing.
Existing agent credentials depend on the same signing keys and preserved DB.

1. Stop the Python server and its background workers in SpawnPoint. Take a
   restorable database backup and keep the previous executable/configuration.
2. Review `check --env server/.env` and `migrate status --env server/.env` against
   the intended database. Rehearse the setup on a restored copy when upgrading
   a historical installation.
3. Build `main` and run `setup --env server/.env --allow-write`. This runs
   the known legacy migrations, repairs supported critical schema omissions,
   initializes/upgrades the human access schema, and claims the seed
   administrator if needed. Existing non-seed passwords remain unchanged.
4. Set the canonical `PUBLIC_ORIGIN` and trusted reverse proxy as described
   below. Run `check --env server/.env --database`, then start `serve`.
5. Verify a browser login, the dashboard, and an existing agent heartbeat before
   returning the listener to normal use. Browser users sign in again because
   Go uses durable cookie sessions instead of the legacy browser JWT.

For MySQL/MariaDB, create the target database and grant the configured user
appropriate permissions before setup. Keep `DB_TYPE=mysql`, `DB_HOST`,
`DB_PORT`, `DB_USER`, `DB_PASSWORD`, `DB_DATABASE` and any existing `DB_SCHEMA`.
Setup creates application tables inside the selected database; it does not
provision database users or a database server.

Unknown schema shapes, unfinished migration journals and untracked populated
databases stop setup. Review or restore the failed migration before retrying.
MySQL DDL can leave partial changes after a failure; setup does not claim a
transactional rollback of those statements. SQLite migrations use transactions.

### Retired MySQL/MariaDB migration history

Existing Python server databases may retain the following 13 agent migration
filenames removed in commit `750d315`. The Go server recognizes these as known
historical predecessors:

| Filename family | Recognized files |
| --- | --- |
| `devices` | `devices_001.sql` through `devices_003.sql` |
| `execution_log` | `execution_log_001.sql` through `execution_log_003.sql` |
| `manual_execution` | `manual_execution_001.sql` through `manual_execution_003.sql` |
| `schedule_detail` | `schedule_detail_001.sql` and `schedule_detail_002.sql` |
| `schedule_group` | `schedule_group_001.sql` and `schedule_group_002.sql` |

These rows no longer appear in `unknown_migrations` or block a completed server
installation. They are reported only when present, never
queued or replayed, and their stored timestamps are preserved. The executable
MySQL catalog contains the 31 frozen migrations plus Go-owned incremental
migrations. A missing legacy migration still needs explicit offline setup. `task_detail_001.sql` remains an active server migration because its SQL
differs from the retired agent file with the same basename.

Update and rebuild the executable, then rerun `check --database`. Do not delete
or rewrite existing migration history to bypass the old unknown-name error.
Python history without a Go attempt journal is supported. If an attempt journal
exists for a retired file, its historical checksum, applied state and completed
statement count must match. Unknown names and unfinished or altered attempts
continue to block setup and startup.

This compatibility applies to server databases that already recorded
`timeweaver_server_001.sql`. An agent-only database without that server
conversion remains blocked with `legacy_schema_requires_review`: the frozen
conversion SQL assumes an older column layout and a limited integer-ID mapping,
so recognizing historical filenames does not authorize running it over an
unconverted installation.

## Caddy and SpawnPoint

For the existing Caddy deployment, keep the API context, for example:

```dotenv
CONTEXT=/timeweaver_svr
LISTEN_HOST=127.0.0.1
LISTEN_PORT=8000
PUBLIC_ORIGIN=https://timeweaver.example.com
TRUSTED_PROXIES=127.0.0.1,::1
```

Replace the example hostname with the actual browser origin. It contains a
scheme and host, plus a port when required, and no `/timeweaver_svr` path.
Only add proxy addresses that actually connect to the Go listener. With a proxy
on another host, configure that host's specific address and listener/network
access accordingly.

Both UI and API now use the same upstream. On a dedicated Caddy hostname:

```caddyfile
timeweaver.example.com {
    reverse_proxy 127.0.0.1:8000
}
```

Retain `/timeweaver_svr` in upstream requests. A `handle_path` rule that strips it
breaks the API. Caddy's normal reverse proxy forwards the host and HTTPS scheme;
the Go server accepts forwarded information only from `TRUSTED_PROXIES`.
The former Vue development server on port 10808 is no longer needed.

In SpawnPoint, use the built executable as a foreground process:

| Setting | Windows example | Linux example |
| --- | --- | --- |
| Executable | `C:\TimeWeaver\dist\timeweaver.exe` | `/opt/timeweaver/dist/timeweaver` |
| Arguments | `serve --env C:\TimeWeaver\server\.env` | `serve --env /opt/timeweaver/server/.env` |
| Working directory | `C:\TimeWeaver` | `/opt/timeweaver` |

Use the existing process account and its DB/file permissions. No new operating
system service is required. Preserve stdout/stderr in the supervisor log. A
normal shutdown drains active HTTP requests and joins the claim sweeper; allow
the process time to exit before forcing termination.

HTTP browser login is accepted only on loopback. For direct local use, browse
the exact origin reported by the configuration check: `localhost` and
`127.0.0.1` are distinct origins. Remote browser login needs HTTPS through the
configured proxy. `ALLOWED_ORIGIN` belongs to the legacy Python configuration;
the Go browser surface uses `PUBLIC_ORIGIN` and same-origin requests.

`ACCESS_TOKEN_EXPIRE_MINUTES` controls ordinary-session idle expiry (30 minutes
by default), and `SESSION_ABSOLUTE_HOURS` controls their absolute lifetime (12
hours by default). Remembered sessions retain a fixed 30-day deadline and rotate
credentials every 12 hours. Changing these settings affects new sessions; stored
absolute deadlines are preserved across restarts.

## Everyday commands

| Command | Behavior |
| --- | --- |
| `version` | Prints the server version and build commit. |
| `check --env FILE` | Validates configuration without connecting to a DB. |
| `check --env FILE --database` | Opens the existing DB read-only and checks migrations, critical schema, access schema, administrator ownership and embedded UI. |
| `setup --env FILE --allow-write` | Applies the supported DB setup/upgrade and securely claims a remaining seed administrator. |
| `serve --env FILE` | Applies approved pending incremental migrations for a configured DB, checks readiness and starts the UI/API server. Use `--no-auto-migrate` to require manual preparation. |
| `assets-check --context /timeweaver_svr` | Validates the compiled dashboard and its referenced local assets. |
| `migrate status --env FILE` | Reads the migration journal and pending migrations. |
| `schema status --env FILE` | Reads the critical legacy schema report. |
| `access status --env FILE` | Reads the human authentication schema version and readiness. |

The root runners accept `-Pull` / `--pull` for `git pull --ff-only` and
`-EnvFile FILE` / `--env FILE` for an explicit configuration. Without `-NoBuild`
or `--no-build`, they rebuild the UI and server on every invocation.
`-ForceBuild` / `--force-build` is accepted for launcher compatibility and has
the same effect as the default. `-Setup` / `--setup` is for initial installation
or explicit offline repair. Routine starts apply reviewed incremental database
migrations automatically. Use the executable directly for routine SpawnPoint
restarts so a process restart does not trigger a build.

### Routine upgrades

Stop the previous instance, keep a current restorable database backup, and
update/build as usual. No separate setup command is needed:

```powershell
.\run-go.ps1 -Pull
```

```bash
bash run-go.sh --pull
```

The same behavior applies to direct `serve` under SpawnPoint. Automatic
migration is limited to reviewed additive migrations explicitly registered in
`internal/store/startup_migrate.go`, currently the empty-directory option.
The full migration plan is checked again under the database migration lock;
unknown/unfinished/checksum-mismatched history, unapproved migrations,
unconfigured accounts and schema issues stop startup. A missing database or
seed administrator still needs first-time setup. No history is deleted or
password reset by automatic upgrade. On a migration failure, inspect the
diagnostic and migration status before retrying. Do not automatically replay
failed MySQL DDL. `check --database` remains read-only, so it can report pending
migrations before the first restart; launchers must not use it as a blocking
precondition for `serve`.

## Troubleshooting and rollback

`serve is disabled` or a `dev-migration (unknown)` version identifies an old
executable. Update `main` and rebuild; pulling source alone does not
replace a previously built binary. The current runners rebuild by default.

A readiness failure provides the missing setup stage before any listener is
opened. A plain `check` success confirms configuration only; use `--database`
to inspect the selected database. A missing SQLite file is not created by a
read-only check or by a failed startup.

Before rolling back to Python, stop Go and restore the verified DB/config
backup together with the old process configuration. Changes made after that
backup need explicit reconciliation. Running the old Python app against the
updated DB is not a permission-preserving rollback: it does not enforce Go
account disabling, group roles, forced password changes or OS identity rules.
Do not run both application migrators concurrently.

## Verification coverage

The workflow tests the built executable's setup, readiness, login, dashboard,
agent endpoint and restart flow on Windows and Linux with the embedded UI.
It also runs SQLite and disposable MySQL/MariaDB migration/service tests,
browser workflows, native OS-account authentication, and the pinned independent
Go agent end-to-end fixture. The pinned agent commit is
`ae4778a3b674beb6e8075de5c71ec6facf37008d`.

Use the exact commit's GitHub Actions results for executed evidence. These
fixtures verify the shipped paths; the actual deployment's database inventory,
host OS account/PAM configuration, Caddy hostname and backup restoration remain
specific to that installation. The [migration record](go-server-migration.md)
and linked slice documents describe those compatibility limits.

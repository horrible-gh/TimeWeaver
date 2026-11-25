# TimeWeaver

## Unified Go server

The `main` branch runs the API and compiled Vue dashboard
from one Go executable. `serve` starts the real server after checking database
migrations, access schema and administrator ownership. Node.js is needed only
when building the dashboard; the executable needs no Python or Node runtime.

For an existing checkout with `server/.env`, stop the old server and back up the
database before the first Go setup. Keep the existing `CONTEXT`, database path
and signing keys. When using Caddy, set `PUBLIC_ORIGIN` to the HTTPS browser
origin and `TRUSTED_PROXIES` to Caddy's connecting address as shown in the guide.
From the repository root:

```powershell
# Windows: update the current branch, build, prepare the DB and start.
.\run-go.ps1 -Pull -Setup
```

```bash
# Linux: the same foreground workflow.
bash run-go.sh --pull --setup
```

Setup prompts for a private password if the initial `timeweaver` administrator
still has its public seed password. It preserves established user passwords.
Routine updates use `.\run-go.ps1 -Pull` or `bash run-go.sh --pull`;
reviewed incremental database migrations run automatically on server startup.
Keep a restorable backup. Initial setup and exceptional schema repair remain explicit.
Later starts can use `.\run-go.ps1 -NoBuild` or `bash run-go.sh --no-build` to
reuse the validated executable, or run `dist/timeweaver serve --env server/.env`
directly under SpawnPoint. The default runner rebuilds, so source or embedded UI
changes cannot be skipped merely because an old binary exists.

See **[Go server setup and operation](docs/go-server.md)** for a fresh install,
HTTPS/Caddy configuration, DB upgrade steps, build requirements, and rollback.
The [migration record](docs/go-server-migration.md) describes compatibility and
verification for MySQL, MariaDB, SQLite, browsers and the independent Go agent.

## Go agent

Install and run the device agent from
**[TimeWeaver-Agent](https://github.com/horrible-gh/TimeWeaver-Agent)**. Its own
README covers configuration, enrollment, builds and service operation. The agent
connects to this server through Agent API v1, synchronizes device-scoped schedule
snapshots, and executes tasks on the target device without database access.

The retired Python `agent/` directory and its launchers are no longer shipped in
this repository. Their last version before removal is available in
[Git history at `8420a34`](https://github.com/horrible-gh/TimeWeaver/tree/8420a3410db18a3ce53ba4b185a334336695b3c9/agent).
The server's `internal/agentapi/` package remains the API used by the Go agent.

## Legacy Python server and standalone client

These projects remain available for compatibility review and rollback:

- `client`: Vue 3 dashboard and login UI.
- `server`: FastAPI backend used by the dashboard and agent.

The UI manages devices, schedule groups, schedule details, manual executions,
and execution history. The server exposes the authenticated API, owns database
access and migrations, and loads SQL resources for MySQL or SQLite. The retained
Python server and SQL resources also serve as migration compatibility references.

## Legacy Python Quick Start (One-Step Setup)

The `install.ps1` and `install.sh` commands below install the legacy Python
server and standalone client. For the unified Go server and device agent, use
the instructions above.

Install both legacy components from the repository root.

Windows PowerShell:

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\install.ps1
```

Linux:

```bash
chmod +x install.sh
./install.sh
```

Run with no arguments for an **interactive install**: the installer asks which
component to install and then **prompts for every config value that lands in
`server/.env` and `client/config.js`** — `ALLOWED_ORIGIN`, `CONTEXT`,
`ACCESS_TOKEN_EXPIRE_MINUTES`, the server database connection, the optional Redis
endpoint, and the client API URL. Each prompt shows the default in brackets; press Enter to
accept it or type your own value. Whatever you decide at install time is written
into the config, so **after the install there is nothing to copy or hand-edit.**
`SECRET_KEY` is generated as a fresh cryptographic random value (never a
placeholder). With every default accepted the server uses a local SQLite
database, so it starts immediately. The installer also creates Python virtual environments, installs
dependencies, builds the Vue UI (client only), and creates `run-server.cmd`
(Windows) or `run-server.sh` (Linux) in the project root.

### Installing a single component

The legacy server and standalone client can be installed separately. A server-only
installation skips Node.js and the UI build. Pick the component interactively,
or pass it directly:

```powershell
# Windows
.\install.ps1 -Component server      # server | client | all (default)
```

```bash
# Linux
./install.sh --component server      # server | client | all (default)
```

### Unattended (non-interactive) install

For CI or scripted provisioning, supply config as flags/env and pass
`--non-interactive` / `-NonInteractive` (a `CI` session enables this
automatically). Note: a plain interactive install **always** walks you through
the config and never silences the prompts on its own — only an explicit
non-interactive run uses defaults without asking.

Re-running the installer always re-enters the config step: existing values are
offered as the prompt defaults (press Enter to keep them) and the previous file
is copied to `backups/<timestamp>/` before being rewritten. (`--reconfigure` /
`-Reconfigure` is accepted for backward compatibility but is no longer required.)

```powershell
# Windows: server with defaults (sqlite3, generated SECRET_KEY)
.\install.ps1 -Component server -NonInteractive
```

```bash
# Linux: server with defaults (sqlite3, generated SECRET_KEY)
./install.sh --component server --non-interactive
```

Run `./install.sh --help` (Linux) or `Get-Help .\install.ps1` (Windows) for the
full list of config flags.

To also start the legacy Python server on boot as a service:

```bash
# Linux (systemd) — run as root so the unit files can be written
sudo ./install.sh --install-services --service-user "$USER"
sudo systemctl start timeweaver-server
```

```powershell
# Windows (scheduled task that runs at boot as SYSTEM) — from an elevated shell
.\install.ps1 -InstallServices
```

> An **interactive install also asks** "Register TimeWeaver to start on boot?"
> when the server is selected, so you no longer have to know the flag
> exists. The flags above are only needed for unattended/CI runs. If you answer
> yes (or pass the flag) without the required privileges — root on Linux, an
> elevated PowerShell on Windows — the rest of the install still completes and
> the installer prints the exact command to add the services in a second pass.
> Both `all` and `server` register only the legacy server service. The independent
> Go agent has its own installation instructions.

## Repository Layout

| Path | Purpose |
| --- | --- |
| `cmd/timeweaver/` | Unified Go server command |
| `internal/` | Go server services, database resources and embedded UI |
| `internal/agentapi/` | Server-side Agent API v1 |
| `client/` | Vue dashboard source, built into the Go executable |
| `server/` | Legacy FastAPI server and migration compatibility references |
| `scripts/` | Build, verification and legacy server/client setup scripts |
| `docs/` | Current operation guides and historical migration records |

## Legacy Components

### client

The frontend is a Vue CLI application with separate login and dashboard pages. It reads the backend API base URL from `config.js`. The installer generates `config.js` for you (prompting for the API URL); the commands below are only for manual/advanced workflows.

Useful commands:

```powershell
cd client
npm ci
npm run serve
npm run build
npm run lint
```

The development server is configured in `vue.config.js` to listen on port `10808` and to serve the dashboard under `/dashboard/`.

### server

The server is a FastAPI application. `app.py` exposes the application from `routers/main.py`, which registers login, logout, dashboard, chart, device, schedule, group, task, and manual execution routers under the configured `CONTEXT`.

Useful commands:

The installer generates `server/.env` for you (sqlite3 + generated `SECRET_KEY`
by default, or MySQL when selected). The commands below are only for
manual/advanced workflows:

```powershell
cd server
python -m venv .venv
.\.venv\Scripts\Activate.ps1
pip install -r requirements.txt
uvicorn app:app --host 0.0.0.0 --port 8000 --workers 1 --reload
```

`run.bat` starts the same Uvicorn server on port `8000`. `stop.ps1` and `stop.py` are helper scripts for stopping the local server process.

## Legacy Configuration

**Every configuration value below is decided at install time and written by the
installer** — there is nothing left to edit before the component runs. In an
interactive install each value is **prompted** (the "Default" column is what the
prompt offers; press Enter to accept or type your own). A flag/env always wins
over the prompt, and a `--non-interactive` install applies the defaults without
asking. The tables list each value, its default, and the flag that overrides it
(Windows `-Flag` / Linux `--flag`).

### server — `server/.env`

| Key | Default | Override flag |
|---|---|---|
| `ALLOWED_ORIGIN` (CORS) | `*` | `-AllowedOrigin` / `--allowed-origin` |
| `SECRET_KEY` (JWT) | cryptographically random (never `change-me`) | `-SecretKey` / `--secret-key` |
| `ACCESS_TOKEN_EXPIRE_MINUTES` | `30` | `-AccessTokenExpireMinutes` / `--access-token-expire-minutes` |
| `CONTEXT` (API base path) | `/time_weaver` | `-Context` / `--context` |
| `DB_TYPE` | `sqlite3` | `-DbType` / `--db-type` |
| `DB_HOST/PORT/USER/PASSWORD/DATABASE/SCHEMA` | sqlite: empty; mysql: prompted | `-Db*` / `--db-*` |
| `DB_PATH` (sqlite file) | `./timeweaver.sqlite3` | `-DbPath` / `--db-path` |
| `DB_LOG` (log SQL queries) | `true` | `-DbLog` / `--db-log` |
| `REDIS_HOST/PORT/DB` | `localhost` / `6379` / `0` | `-RedisHost/-RedisPort/-RedisDb` / `--redis-host/--redis-port/--redis-db` |
| uvicorn bind host (written into `run-server.cmd` / start command) | `0.0.0.0` | `-ServerHost` / `--server-host` |
| uvicorn bind port (written into `run-server.cmd` / start command) | `8000` | `-ServerPort` / `--server-port` |

> **The server owns the database.** The independent Go agent connects through
> the API and does not receive the server's database credentials.

> **Redis is optional.** It is only a shared logout-blacklist store for
> multi-worker / multi-host deployments. If no Redis is running, the server falls
> back to an in-process blacklist and logout still works — so the default install
> needs no Redis installed or started.

### client — `client/config.js`

| Value | Default | Override flag |
|---|---|---|
| `API_SERVER_URL` | `http://127.0.0.1:8000/time_weaver` | `-ApiUrl` / `--api-url` |

`server/res/sql/migration/` and `server/res/sql/sqloader/` hold the migration and
SQL resources; database migrations run automatically on first start.

Do not commit local credentials or environment-specific configuration files.

## Task Types

The [Go agent](https://github.com/horrible-gh/TimeWeaver-Agent) executes these
task types on the selected device:

- `command`: execute a shell command.
- `copy`: copy a source file to a destination file.
- `archive`: create a ZIP archive from a source directory.
- `housekeep`: delete files older than the configured retention period.

Task paths and commands can use `{date}` placeholders. Date formatting is controlled by the task's `date_format`, `target_date_format`, and `destination_date_format` values.

## Operational Notes

- The agent enrolls its configured device with a one-time token on first run; an operator can later set the device to `inactive` from the dashboard to pause it.
- The server persists execution history reported by the agent API.
- Configure synchronization and logging in the independent Go agent; see its README for supported settings.
- See [housekeeping](docs/housekeeping.md) for recursive cleanup and empty-directory options.

## Setup Scripts (Details)

The legacy `install.ps1` / `install.sh` wrappers (see [Legacy Python Quick Start](#legacy-python-quick-start-one-step-setup)) call the platform setup scripts under `scripts/`. You can also invoke them directly:

Linux:

```bash
chmod +x scripts/setup-linux.sh
./scripts/setup-linux.sh                       # legacy server and client
./scripts/setup-linux.sh --component server    # legacy server only
```

To also register the FastAPI server as a boot service
(interactive installs ask about this; the flags are for unattended runs):

```bash
# Linux (systemd), as root
sudo ./scripts/setup-linux.sh --install-services --service-user "$USER"
sudo systemctl start timeweaver-server
```

```powershell
# Windows (scheduled task at boot, runs as SYSTEM), from an elevated shell
.\scripts\setup-windows.ps1 -InstallServices
```

Windows PowerShell:

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\scripts\setup-windows.ps1                    # legacy server and client
.\scripts\setup-windows.ps1 -Component server   # legacy server only
```

Both setup scripts accept a component selector (`all`, `server`, `client`; default
`all`; prompted if omitted). A server-only install skips Node.js/npm and the UI
build. The Windows setup creates `run-server.cmd`; Linux creates the equivalent
executable `run-server.sh`. Both scripts generate `server/.env` and
`client/config.js` for the selected components. Re-running the installer offers
existing values as defaults and backs up the old configuration before rewriting
it. Sample files remain as references.

## Legacy Docker stack

The Compose stack runs the legacy Python API and standalone UI with MySQL and
Redis. It does not install the independent Go agent. Build and start it with:

```bash
docker compose up --build
```

Default endpoints:

- UI: `http://127.0.0.1:10808`
- API: `http://127.0.0.1:8000/time_weaver`
- MySQL: `127.0.0.1:3306`
- Redis: `127.0.0.1:6379`

Useful Docker environment overrides:

- Server database only: `MYSQL_DATABASE`, `MYSQL_USER`, `MYSQL_PASSWORD`, `MYSQL_ROOT_PASSWORD`
- Server/UI: `SECRET_KEY`, `ALLOWED_ORIGIN`, `CONTEXT`, `SERVER_PORT`, `UI_PORT`, `API_SERVER_URL`

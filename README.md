# TimeWeaver

TimeWeaver is split into three working projects:

- `client`: Vue 3 dashboard and login UI.
- `server`: FastAPI backend used by the dashboard and agent.
- `agent`: Python scheduler agent that enrolls with the TimeWeaver server, receives device-scoped schedule snapshots, and executes tasks.

The UI is used to manage devices, schedule groups, schedule details, manual executions, and execution history. The server exposes the authenticated API, owns database access and migrations, and loads SQL resources for MySQL or SQLite. The agent runs on a target device, synchronizes schedule snapshots through the server API, and executes command, copy, archive, and housekeeping tasks without database access.

## Quick Start (One-Step Setup)

Install everything from the repository root.

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
`server/.env` (and the agent/client config)** — `ALLOWED_ORIGIN`, `CONTEXT`,
`ACCESS_TOKEN_EXPIRE_MINUTES`, the server database connection, the optional Redis
endpoint, the agent server URL, and so on. Each prompt shows the default in brackets; press Enter to
accept it or type your own value. Whatever you decide at install time is written
into the config, so **after the install there is nothing to copy or hand-edit.**
`SECRET_KEY` is generated as a fresh cryptographic random value (never a
placeholder). With every default accepted the server uses a local SQLite
database, so it starts immediately. The installer also creates Python virtual environments, installs
dependencies, builds the Vue UI (client only), and (on Windows) creates
`run-server.cmd` / `run-agent.cmd` in the project root.

### Installing a single component

Server, agent, and client can be installed separately. This is useful when a
target device only needs the scheduler agent (no Node.js / UI build required).
Pick the component interactively, or pass it directly:

```powershell
# Windows
.\install.ps1 -Component agent      # agent | server | client | all (default)
```

```bash
# Linux
./install.sh --component agent      # agent | server | client | all (default)
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

# Windows: agent pointed at the TimeWeaver server API
.\install.ps1 -Component agent -NonInteractive `
    -ServerUrl https://timeweaver.example.com/time_weaver -DeviceName floor-1-pc
$env:TIMEWEAVER_ENROLLMENT_TOKEN = "<one-time token>"  # set only for first enrollment
```

```bash
# Linux: agent pointed at the TimeWeaver server API
./install.sh --component agent --non-interactive \
    --server-url https://timeweaver.example.com/time_weaver --device-name floor-1-pc
export TIMEWEAVER_ENROLLMENT_TOKEN="<one-time token>"  # set only for first enrollment
```

Run `./install.sh --help` (Linux) or `Get-Help .\install.ps1` (Windows) for the
full list of config flags.

To also start TimeWeaver on boot as a service:

```bash
# Linux (systemd) — run as root so the unit files can be written
sudo ./install.sh --install-services --service-user "$USER"
sudo systemctl start timeweaver-server timeweaver-agent
```

```powershell
# Windows (scheduled task that runs at boot as SYSTEM) — from an elevated shell
.\install.ps1 -InstallServices
```

> An **interactive install also asks** "Register TimeWeaver to start on boot?"
> for whichever components you selected, so you no longer have to know the flag
> exists. The flags above are only needed for unattended/CI runs. If you answer
> yes (or pass the flag) without the required privileges — root on Linux, an
> elevated PowerShell on Windows — the rest of the install still completes and
> the installer prints the exact command to add the services in a second pass.
> On Linux the services register for **every component you installed** (an `all`
> install registers both the server and the agent); an agent-only install
> registers just the agent.

> On first run, the agent uses a one-time enrollment token issued by an
> administrator to register its device as active; no database seeding is needed.
> A device an operator has explicitly set to `inactive` is left as-is.
>
> The agent connects only to the TimeWeaver server API. The installer writes the
> server base URL from prompts/flags; provide `TIMEWEAVER_ENROLLMENT_TOKEN` in the
> process or service environment for the first enrollment, never in a config file.

## Repository Layout

```text
TimeWeaver/
|-- client/              Vue CLI multi-page frontend
|-- server/              FastAPI API server
|-- agent/               Python background scheduler agent
`-- README.md           Project overview
```

## Components

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

### agent

The agent is a long-running Python process. On startup it loads or enrolls device credentials, obtains an access token, sends a heartbeat, validates a device-scoped schedule snapshot, and reconciles APScheduler jobs.

Useful commands:

The installer generates `conf/server.json` and `conf/time_weaver.json` for you
(prompting for the TimeWeaver server URL and device name). The commands below are
only for manual/advanced workflows:

```powershell
cd agent
python -m venv .venv
.\.venv\Scripts\Activate.ps1
pip install -r requirements.txt
python timeweaver.py
```

To start the agent on boot, install it as a service: on Linux with
`sudo ./install.sh --component agent --install-services --service-user "$USER"`
(systemd), or on Windows with `.\install.ps1 -Component agent -InstallServices`
from an elevated shell (a scheduled task that runs at boot as SYSTEM). An
interactive install offers this as a prompt, so the flag is optional.

## Configuration

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

> **The server owns the database.** Its SQLite/MySQL choice and credentials are
> never copied into the agent configuration. An `all` install configures the
> server database and the agent API URL independently.

> **Redis is optional.** It is only a shared logout-blacklist store for
> multi-worker / multi-host deployments. If no Redis is running, the server falls
> back to an in-process blacklist and logout still works — so the default install
> needs no Redis installed or started.

### agent — `conf/server.json` + `conf/time_weaver.json`

| Value | Default | Override flag |
|---|---|---|
| `api.base_url` | `http://127.0.0.1:8000/time_weaver` | `-ServerUrl` / `--server-url` |
| one-time enrollment token | supplied at first start through `TIMEWEAVER_ENROLLMENT_TOKEN`; not stored in config | process/service environment |
| log level (`base`/`console`/`file_timed`) | `debug` | `-LogLevel` / `--log-level` |
| `device` name | machine hostname | `-DeviceName` / `--device-name` |
| `reschedule.year/month/day/hour` (poll cron) | `*` | `-RescheduleYear/-RescheduleMonth/-RescheduleDay/-RescheduleHour` / `--reschedule-year/--reschedule-month/--reschedule-day/--reschedule-hour` |
| `reschedule.minute` (poll cron) | `*/5` | `-RescheduleMinute` / `--reschedule-minute` |
| `reschedule.second` (poll cron) | `0` | `-RescheduleSecond` / `--reschedule-second` |
| `version` | shipped `conf/version.json` | — |

The remaining `server.json` fields (logging format/rotation, sqloader and
migration paths, `auto_migration`) ship with working defaults and need no input.

### client — `client/config.js`

| Value | Default | Override flag |
|---|---|---|
| `API_SERVER_URL` | `http://127.0.0.1:8000/time_weaver` | `-ApiUrl` / `--api-url` |

`server/res/sql/migration/` and `server/res/sql/sqloader/` hold the migration and
SQL resources; database migrations run automatically on first start.

Do not commit local credentials or environment-specific configuration files.

## Task Types

The agent supports these task types through `services/time_weaver/task.py`:

- `command`: execute a shell command.
- `copy`: copy a source file to a destination file.
- `archive`: create a ZIP archive from a source directory.
- `housekeep`: delete files older than the configured retention period.

Task paths and commands can use `{date}` placeholders. Date formatting is controlled by the task's `date_format`, `target_date_format`, and `destination_date_format` values.

## Operational Notes

- The agent enrolls its configured device with a one-time token on first run; an operator can later set the device to `inactive` from the dashboard to pause it.
- The server persists execution history reported by the agent API.
- Device-scoped schedule snapshots are synchronized according to `conf/time_weaver.json`.
- Logs are written according to `conf/server.json`; the sample configuration writes to `log/server.log`.

## Setup Scripts (Details)

The root `install.ps1` / `install.sh` wrappers (see [Quick Start](#quick-start-one-step-setup)) call the platform setup scripts under `scripts/`. You can also invoke them directly:

Linux:

```bash
chmod +x scripts/setup-linux.sh
./scripts/setup-linux.sh                      # all components
./scripts/setup-linux.sh --component agent    # agent only
```

To also register the FastAPI server and scheduler agent as boot services
(interactive installs ask about this; the flags are for unattended runs):

```bash
# Linux (systemd), as root
sudo ./scripts/setup-linux.sh --install-services --service-user "$USER"
sudo systemctl start timeweaver-server timeweaver-agent
```

```powershell
# Windows (scheduled task at boot, runs as SYSTEM), from an elevated shell
.\scripts\setup-windows.ps1 -InstallServices
```

Windows PowerShell:

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\scripts\setup-windows.ps1                   # all components
.\scripts\setup-windows.ps1 -Component agent  # agent only
```

Both setup scripts accept a component selector (`all`, `server`, `agent`, `client`; default `all`; prompted if omitted). When only the agent or server is selected, Node.js/npm and the UI build are skipped. The Windows setup creates `run-server.cmd` and `run-agent.cmd` in the project root. Both setup scripts **generate ready-to-run config** (`server/.env`, `agent/conf/*.json`, `client/config.js`) from prompts/flags rather than asking you to copy and edit sample files; existing config is preserved unless `--reconfigure` / `-Reconfigure` is passed. The `*.sample.*` files remain in the tree only as references.

## Docker

Build and run the database, Redis, API server, and UI:

```bash
docker compose up --build
```

Default endpoints:

- UI: `http://127.0.0.1:10808`
- API: `http://127.0.0.1:8000/time_weaver`
- MySQL: `127.0.0.1:3306`
- Redis: `127.0.0.1:6379`

Run the agent container with a one-time enrollment token issued by the server. The agent registers its device automatically and stores only the resulting credential in its credential file:

```bash
TIMEWEAVER_ENROLLMENT_TOKEN="<one-time token>" DEVICE_NAME=test \
  docker compose --profile agent up --build agent
```

Useful Docker environment overrides:

- Server database only: `MYSQL_DATABASE`, `MYSQL_USER`, `MYSQL_PASSWORD`, `MYSQL_ROOT_PASSWORD`
- Server/UI: `SECRET_KEY`, `ALLOWED_ORIGIN`, `CONTEXT`, `SERVER_PORT`, `UI_PORT`, `API_SERVER_URL`
- Agent: `TIMEWEAVER_SERVER_URL`, `TIMEWEAVER_ENROLLMENT_TOKEN`, `DEVICE_NAME`, `RESCHEDULE_MINUTE`

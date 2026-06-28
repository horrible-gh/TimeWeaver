# TimeWeaver

TimeWeaver is split into three working projects:

- `client`: Vue 3 dashboard and login UI.
- `server`: FastAPI backend used by the dashboard and agent.
- `agent`: Python scheduler agent that loads schedules from the TimeWeaver database and executes tasks on a registered device.

The UI is used to manage devices, schedule groups, schedule details, manual executions, and execution history. The server exposes the authenticated API, initializes database access and migrations, and loads SQL resources for MySQL or SQLite. The agent runs on a target device, periodically reloads schedule definitions from the database, and executes command, copy, archive, and housekeeping tasks.

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
component to install and prompts for the few config values it needs (press Enter
to accept the shown default). It then writes **ready-to-run config files for you
— there is nothing to copy or hand-edit.** With all defaults the server uses a
local SQLite database and an auto-generated `SECRET_KEY`, so it starts
immediately. The installer also creates Python virtual environments, installs
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

For CI or scripted provisioning, supply config as flags/env and skip all prompts
(a no-TTY / `CI` session is also auto-detected). Existing config is kept unless
you pass `--reconfigure` / `-Reconfigure`.

```powershell
# Windows: server with defaults (sqlite3, generated SECRET_KEY)
.\install.ps1 -Component server -NonInteractive

# Windows: agent pointed at a MySQL database
.\install.ps1 -Component agent -NonInteractive `
    -DbHost db.example.com -DbUser tw -DbPassword secret -DbName tw -DeviceName floor-1-pc
```

```bash
# Linux: agent pointed at a MySQL database
./install.sh --component agent --non-interactive \
    --db-host db.example.com --db-user tw --db-password secret --db-name tw --device-name floor-1-pc
```

Run `./install.sh --help` (Linux) or `Get-Help .\install.ps1` (Windows) for the
full list of config flags.

To also install systemd services on Linux:

```bash
sudo ./install.sh --install-services --service-user "$USER"
sudo systemctl start timeweaver-server timeweaver-agent
```

> Prerequisite: before the agent runs, a matching device row must exist in the
> TimeWeaver database with `active` status.

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

The agent is a long-running Python process. On startup it initializes the database connection and migrations, validates its configured device, loads active schedules, and registers jobs with APScheduler.

Useful commands:

The installer generates `conf/server.json` and `conf/time_weaver.json` for you
(prompting for the database connection and device name). The commands below are
only for manual/advanced workflows:

```powershell
cd agent
python -m venv .venv
.\.venv\Scripts\Activate.ps1
pip install -r requirements.txt
python timeweaver.py
```

On Linux, install the agent as a `systemd` service with
`sudo ./install.sh --component agent --install-services --service-user "$USER"`.

## Configuration

Frontend configuration:

- `client/config.js`: API server URL used by Axios.

Agent configuration:

- `agent/conf/server.json`: logging and database connection settings.
- `agent/conf/time_weaver.json`: device name and schedule reload cron expression.
- `agent/conf/version.json`: agent version reported to the database.

Server configuration:

- `server/.env`: CORS origin, JWT secret, context path, and database settings loaded by Pydantic.
- `server/res/sql/migration/`: migration scripts for MySQL and SQLite.
- `server/res/sql/sqloader/`: SQL files loaded by sqloader for dashboard, schedule, task, and chart APIs.

Do not commit local credentials or environment-specific configuration files.

## Task Types

The agent supports these task types through `services/time_weaver/task.py`:

- `command`: execute a shell command.
- `copy`: copy a source file to a destination file.
- `archive`: create a ZIP archive from a source directory.
- `housekeep`: delete files older than the configured retention period.

Task paths and commands can use `{date}` placeholders. Date formatting is controlled by the task's `date_format`, `target_date_format`, and `destination_date_format` values.

## Operational Notes

- The configured device must exist in the database and have `active` status.
- The agent records execution results in `execution_log`.
- Schedule definitions are periodically reloaded according to `conf/time_weaver.json`.
- Logs are written according to `conf/server.json`; the sample configuration writes to `log/server.log`.

## Setup Scripts (Details)

The root `install.ps1` / `install.sh` wrappers (see [Quick Start](#quick-start-one-step-setup)) call the platform setup scripts under `scripts/`. You can also invoke them directly:

Linux:

```bash
chmod +x scripts/setup-linux.sh
./scripts/setup-linux.sh                      # all components
./scripts/setup-linux.sh --component agent    # agent only
```

To also install systemd services for the FastAPI server and scheduler agent:

```bash
sudo ./scripts/setup-linux.sh --install-services --service-user "$USER"
sudo systemctl start timeweaver-server timeweaver-agent
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

Run the agent container after a matching active device exists in the TimeWeaver database:

```bash
DEVICE_NAME=test docker compose --profile agent up --build agent
```

Useful Docker environment overrides:

- `MYSQL_DATABASE`, `MYSQL_USER`, `MYSQL_PASSWORD`, `MYSQL_ROOT_PASSWORD`
- `SECRET_KEY`, `ALLOWED_ORIGIN`, `CONTEXT`, `SERVER_PORT`, `UI_PORT`
- `API_SERVER_URL`
- `DEVICE_NAME`, `RESCHEDULE_MINUTE`

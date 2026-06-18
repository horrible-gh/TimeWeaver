# TimeWeaver

TimeWeaver is split into three working projects:

- `client`: Vue 3 dashboard and login UI.
- `server`: FastAPI backend used by the dashboard and agent.
- `agent`: Python scheduler agent that loads schedules from the TimeWeaver database and executes tasks on a registered device.

The UI is used to manage devices, schedule groups, schedule details, manual executions, and execution history. The server exposes the authenticated API, initializes database access and migrations, and loads SQL resources for MySQL or SQLite. The agent runs on a target device, periodically reloads schedule definitions from the database, and executes command, copy, archive, and housekeeping tasks.

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

The frontend is a Vue CLI application with separate login and dashboard pages. It reads the backend API base URL from `config.js`, which should be created from `config.sample.js`.

Useful commands:

```powershell
cd client
npm install
Copy-Item config.sample.js config.js
npm run serve
npm run build
npm run lint
```

The development server is configured in `vue.config.js` to listen on port `10808` and to serve the dashboard under `/dashboard/`.

### server

The server is a FastAPI application. `app.py` exposes the application from `routers/main.py`, which registers login, logout, dashboard, chart, device, schedule, group, task, and manual execution routers under the configured `CONTEXT`.

Useful commands:

```powershell
cd server
python -m venv .venv
.\.venv\Scripts\Activate.ps1
pip install -r requirements.txt
Copy-Item .env.sample .env
uvicorn app:app --host 0.0.0.0 --port 8000 --workers 1 --reload
```

`run.bat` starts the same Uvicorn server on port `8000`. `stop.ps1` and `stop.py` are helper scripts for stopping the local server process.

### agent

The agent is a long-running Python process. On startup it initializes the database connection and migrations, validates its configured device, loads active schedules, and registers jobs with APScheduler.

Useful commands:

```powershell
cd agent
python -m venv .venv
.\.venv\Scripts\Activate.ps1
pip install -r requirements.txt
Copy-Item conf\server.sample.json conf\server.json
Copy-Item conf\time_weaver.sample.json conf\time_weaver.json
python timeweaver.py
```

On Linux, `install-service.sh` can install the agent as a `systemd` service after configuration files and dependencies are in place.

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

## One-Step Setup Scripts

Linux:

```bash
chmod +x scripts/setup-linux.sh
./scripts/setup-linux.sh
```

To also install systemd services for the FastAPI server and scheduler agent:

```bash
sudo ./scripts/setup-linux.sh --install-services --service-user "$USER"
sudo systemctl start timeweaver-server timeweaver-agent
```

Windows PowerShell:

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\scripts\setup-windows.ps1
```

The Windows setup creates `run-server.cmd` and `run-agent.cmd` in the project root. Both setup scripts create local config files from samples only when those files are missing.

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

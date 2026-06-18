# TimeWeaver Server

The `server` directory contains the FastAPI backend for TimeWeaver. It serves the login/logout API and the authenticated dashboard APIs used to manage devices, groups, schedules, tasks, manual execution requests, charts, and execution history.

## Project Layout

```text
server/
|-- app.py                 FastAPI application export
|-- config.py              Environment settings and database initialization
|-- routers/               Login, logout, dashboard, chart, and management APIs
|-- schemas/               Pydantic request models
|-- res/sql/migration/     MySQL and SQLite migration scripts
|-- res/sql/sqloader/      SQL resources loaded by sqloader
|-- util/                  Utility helpers
|-- requirements.txt       Python dependencies
|-- run.bat                Local Uvicorn start command
|-- stop.ps1, stop.py      Local stop helpers
`-- .env.sample            Environment template
```

## Runtime Stack

- FastAPI and Uvicorn provide the HTTP API.
- JWT access tokens protect dashboard routes.
- Redis is used by logout handling to store token blacklist entries.
- sqloader initializes the database layer, runs migrations, and loads SQL resources.
- MySQL and SQLite configuration paths are both present under `res/sql/`.

## Configuration

Create a local `.env` file from the sample before running the server:

```powershell
Copy-Item .env.sample .env
```

Important settings:

- `ALLOWED_ORIGIN`: comma-separated CORS origins.
- `SECRET_KEY`: JWT signing key.
- `ACCESS_TOKEN_EXPIRE_MINUTES`: token lifetime in minutes.
- `CONTEXT`: API base path, for example `/test`.
- `DB_TYPE`: `mysql`, `sqlite`, `sqlite3`, or `local`.
- `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, `DB_DATABASE`, `DB_SCHEMA`: MySQL connection settings.
- `DB_PATH`: SQLite database path when using the SQLite/local modes.

Do not commit `.env` with local secrets or environment-specific database values.

## Running Locally

```powershell
python -m venv .venv
.\.venv\Scripts\Activate.ps1
pip install -r requirements.txt
Copy-Item .env.sample .env
uvicorn app:app --host 0.0.0.0 --port 8000 --workers 1 --reload
```

The same Uvicorn command is available in `run.bat`.

To stop a local server bound to port `8000`, run:

```powershell
.\stop.ps1
```

## API Areas

Routes are registered in `routers/main.py` below the configured `CONTEXT`.

- `/login`: authenticates a user and returns a bearer token.
- `/logout`: records the current token as logged out.
- `/dashboard`: dashboard data including latest schedules and execution history.
- `/dashboard/charts`: chart data for devices, schedules, and tasks.
- `/dashboard/devices`: device list, create, update, and remove APIs.
- `/dashboard/groups`: group list, create, update, and remove APIs.
- `/dashboard/schedule`: schedule list, create, update, remove, device lookup, and manual schedule insertion APIs.
- `/dashboard/tasks`: task list, create, update, remove, schedule lookup, and manual task insertion APIs.
- `/dashboard/manual_execution`: manual execution list, update, and abandon APIs.

Dashboard routes depend on bearer token verification from `routers/login/auth.py`.

## Database Resources

Database initialization happens when `config.DatabaseSetting` is created. The selected `DB_TYPE` determines which SQL loader and migration directory are used:

- MySQL: `res/sql/sqloader/mysql` and `res/sql/migration/mysql`
- SQLite/local: `res/sql/sqloader/sqlite` and `res/sql/migration/sqlite`

The API routers load SQL statements through `sqloader.load_sql(...)` and execute them through the initialized database instance.

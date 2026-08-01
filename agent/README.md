# TimeWeaver Agent

The `agent` directory contains TimeWeaver's Python scheduler process. It enrolls with the TimeWeaver server, receives device-scoped schedule snapshots through the agent API, registers jobs with APScheduler, executes task details in sequence order, and reports results through an in-memory outbox.

## Stack

- Python 3
- APScheduler
- Requests
- LogAssist
- PyCryptodome

## Project Layout

```text
agent/
|-- timeweaver.py                 Process entry point and API polling loops
|-- configure.py                  Loads config files and initializes logging
|-- requirements.txt              Python dependencies
|-- conf/
|   |-- server.sample.json        Logging configuration template
|   |-- time_weaver.sample.json   Device and agent API configuration template
|   `-- version.json              Agent version metadata
|-- services/time_weaver/
|   |-- api_client.py             Agent API transport boundary
|   |-- credential_manager.py     Enrollment and access-token lifecycle
|   |-- sync_coordinator.py       Snapshot validation and reconciliation
|   |-- scheduler_adapter.py      APScheduler reconciliation adapter
|   |-- app.py                    Scheduling, execution, and result delivery
|   |-- outbox.py                 In-memory result delivery queue
|   `-- task.py                   Task implementations
`-- util/                         Utility modules
```

## Setup

Create and activate a virtual environment:

```powershell
python -m venv .venv
.\.venv\Scripts\Activate.ps1
```

Install dependencies:

```powershell
pip install -r requirements.txt
```

Create local configuration files from the samples:

```powershell
Copy-Item conf\server.sample.json conf\server.json
Copy-Item conf\time_weaver.sample.json conf\time_weaver.json
```

Update `conf/server.json` with logging settings. Configure the device identity and server API options in `conf/time_weaver.json`. On first enrollment, provide the one-time token through the environment variable named by `api.enrollment_token_env` (default: `TIMEWEAVER_ENROLLMENT_TOKEN`).

## Run

Start the agent:

```powershell
python timeweaver.py
```

On startup, the agent:

1. Loads `conf/server.json`, `conf/time_weaver.json`, and `conf/version.json`.
2. Loads or enrolls device credentials and obtains an access token.
3. Sends a heartbeat and requests the device-scoped schedule snapshot.
4. Validates the complete snapshot before reconciling APScheduler jobs.
5. Polls heartbeat and snapshot channels independently with retry backoff.
6. Executes eligible regular and claimed manual work, then delivers results through the API-backed outbox.

## Configuration Files

`conf/server.json` contains logger configuration only.

`conf/time_weaver.json` contains:

- `device`: the device name presented during enrollment.
- `api.base_url`: the TimeWeaver server base URL.
- `api.credential_path`: the local credential file path.
- `api.enrollment_token_env`: the environment variable containing a one-time enrollment token.
- API timeouts, polling intervals, retry policy, shutdown grace, and outbox capacity/watermarks.

The server owns device activation, schedule data, manual claims, execution state transitions, and persistent execution logs.

## Task Types

Task execution is implemented in `services/time_weaver/task.py`.

- `command`: runs a shell command. Commands can use `{date}`.
- `copy`: copies a source file to a destination file.
- `archive`: creates a ZIP archive from a source directory.
- `housekeep`: deletes files older than `house_keep_days`.

Path fields can use `{date}` and are formatted with `date_format`, `target_date_format`, and `destination_date_format`. If a source path is missing, `error_on_missing_source` decides whether the task fails or is skipped.

## Execution Flow

- `timeweaver.py` maintains independent heartbeat and snapshot polling channels.
- `sync_coordinator.py` validates snapshots and applies an all-or-nothing scheduler diff.
- Schedule groups are registered as cron jobs; claimable manual runs are claimed through the server before dispatch.
- Detail tasks execute by sequence, with details in the same sequence scheduled together.
- Immutable results are queued in memory and sent through the server API in per-execution-group FIFO order.
- Server acknowledgements and applied transitions determine whether the next sequence may continue.
- Authentication or device revocation halts new work; transient communication failures retain the last valid snapshot.

## Logs

Logging is configured in `conf/server.json`. The sample configuration writes rotating daily logs to:

```text
log/server.log
```
# TimeWeaver Agent

The `agent` directory contains the Python scheduler process for TimeWeaver. It connects to the TimeWeaver database, loads schedule definitions for the configured device, registers jobs with APScheduler, executes task details in sequence order, and records execution results.

## Stack

- Python 3
- APScheduler
- PyMySQL
- sqloader
- LogAssist
- PyCryptodome

## Project Layout

```text
agent/
|-- timeweaver.py                 Process entry point
|-- configure.py                  Loads config files and initializes logging
|-- requirements.txt              Python dependencies
|-- install-service.sh            Linux systemd installer
|-- conf/
|   |-- server.sample.json        Logging and database configuration template
|   |-- time_weaver.sample.json   Device and reschedule configuration template
|   `-- version.json              Agent version metadata
|-- services/time_weaver/
|   |-- app.py                    Scheduler, DB synchronization, execution logging
|   `-- task.py                   Task implementations
|-- res/time_weaver/sql/
|   |-- migration/                Database migration SQL files
|   `-- sqloader/                 SQLoader query definitions
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

Update `conf/server.json` with the database connection and logging settings. Update `conf/time_weaver.json` with the device name and schedule reload cron expression.

## Run

Start the agent:

```powershell
python timeweaver.py
```

On startup, the agent:

1. Loads `conf/server.json`, `conf/time_weaver.json`, and `conf/version.json`.
2. Initializes the database, SQLoader, and migrations.
3. Validates that the configured device exists and is active.
4. Loads matching schedule groups and details from the database.
5. Registers jobs with APScheduler.
6. Periodically reloads schedule definitions.

## Linux Service

`install-service.sh` can install the agent as a `systemd` service:

```bash
chmod +x install-service.sh
./install-service.sh
```

The installer prompts for the install directory, optional mount dependency, and service user. After installation, use standard `systemctl` commands:

```bash
sudo systemctl enable timeweaver-agent
sudo systemctl start timeweaver-agent
sudo systemctl status timeweaver-agent
journalctl -u timeweaver-agent -f
```

## Configuration Files

`conf/server.json` contains:

- Logger configuration.
- TimeWeaver database connection.
- Migration path.
- SQLoader path.

`conf/time_weaver.json` contains:

- `device`: the device name used to select schedules.
- `reschedule`: cron fields used to reload schedules from the database.

The configured device must exist in the `devices` table and have `active` status.

## Task Types

Task execution is implemented in `services/time_weaver/task.py`.

- `command`: runs a shell command. Commands can use `{date}`.
- `copy`: copies a source file to a destination file.
- `archive`: creates a ZIP archive from a source directory.
- `housekeep`: deletes files older than `house_keep_days`.

Path fields can use `{date}` and are formatted with:

- `date_format`
- `target_date_format`
- `destination_date_format`

If a source path is missing, `error_on_missing_source` decides whether the task fails or is skipped.

## Execution Flow

- `services/time_weaver/app.py` loads schedule rows with SQLoader key `get_tasks_all`.
- Schedule groups are registered as cron or date jobs.
- Detail tasks are executed by sequence.
- If a sequence has multiple details, they are scheduled together.
- Results are written to `execution_log`.
- Manual execution status is updated to `processing`, `done`, or `failed`.
- Group or detail error-stop flags control whether subsequent tasks continue.

## Logs

Logging is configured in `conf/server.json`. The sample configuration writes rotating daily logs to:

```text
log/server.log
```

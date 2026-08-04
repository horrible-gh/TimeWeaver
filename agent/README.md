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

Update `conf/server.json` with logging settings. Configure the device identity and server API options in `conf/time_weaver.json`.

> The agent setup is easy to get wrong: a single mismatched account or a leftover
> enrollment-token environment variable is enough to leave the agent in a retry
> loop that never recovers. Read **Enrollment and Credential Lifecycle** and, if you
> run the agent as a Windows service, **Running as a Windows Service (NSSM)** before
> first launch. The **Troubleshooting** table maps every bootstrap failure reason to
> its cause and fix.

## Enrollment and Credential Lifecycle

This is the part that most commonly goes wrong. Read it once, fully.

### The enrollment token is single-use

An enrollment token is consumed by the server the first time an agent enrolls with
it (`used_at` is stamped). After that, **the same token can never be used again** —
re-enrolling with it fails with `enroll_failed:enrollment_token_invalid`. Issue a
fresh token from the dashboard whenever you need to enroll again.

### Enrollment happens once; the credential file drives everything after

1. **First run only** — provide the one-time token through the environment variable
   named by `api.enrollment_token_env` (default `TIMEWEAVER_ENROLLMENT_TOKEN`):

   ```powershell
   $env:TIMEWEAVER_ENROLLMENT_TOKEN = "enr_..."
   python timeweaver.py
   ```

   On success the log prints `[bootstrap] enrollment succeeded` and the agent writes
   a credential file to `api.credential_path` (default `conf/agent_credential.json`).
   This file holds a long-lived refresh token (about 90 days).

2. **Every subsequent run (including the service)** — do **not** set the enrollment
   token variable. The agent loads the stored credential, obtains an access token,
   and rotates the refresh token automatically. Setting the (already consumed) token
   again only causes failures after the credential is gone.

Only re-enroll (with a **new** token) if the credential file is lost, expired, or the
device was revoked on the server.

### One refresh token per device — never run two copies at once

Each cold start rotates the refresh token: the server issues a new one and revokes
the previous one, so **only the most recently issued refresh token is valid**. If the
agent runs twice against the same credential file (for example a manual run *and* the
service at the same time), the second copy presents a token the first already
rotated away, gets `invalid_token`, and the credential is discarded. **Run the agent
exactly one way at a time.**

### Credential file permissions (Windows)

The credential file is written with an owner-only ACL. On Windows the agent grants
Modify to the writing account plus the well-known SIDs `*S-1-5-18` (SYSTEM) and
`*S-1-5-32-544` (BUILTIN\Administrators). The account that later reads the file must
be one of those, or the read fails. See the service section below for how this
interacts with the service logon account.

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

## Running as a Windows Service (NSSM)

The repository ships `run-agent.cmd`, which `cd`s to the `agent` directory and runs
the venv Python. Point the service at that script so the working directory is correct
and the relative `conf/` and `log/` paths resolve.

Follow these steps in order — most "the service won't run" reports come from skipping
one of them.

1. **Enroll once, interactively, with the service stopped.** With no agent and no
   service running, run `run-agent.cmd` with a fresh `TIMEWEAVER_ENROLLMENT_TOKEN`
   set, confirm `[bootstrap] enrollment succeeded` and that
   `conf/agent_credential.json` now exists, then stop it (Ctrl+C).

2. **Do not give the service the enrollment token.** The token is single-use, so a
   service that carries it would fail on every restart after the first. The service
   must rely on the credential file only.

3. **Run the service under an account that can read the credential file.** The
   simplest reliable choice is to run the service under the **same account used for
   enrollment**, so the owner-only ACL matches. (SYSTEM and Administrators are also
   granted automatically, so `LocalSystem` can read a file written by an
   administrator, but a file written by a non-admin interactive user is only readable
   by that user, SYSTEM, and Administrators.)

4. **Ensure the service process has `USERNAME` in its environment.** Writing/rotating
   the credential resolves the current account with `getpass.getuser()`, which reads
   the `USERNAME` (or `USER`/`LOGNAME`) environment variable. A service process may
   start without `USERNAME` set; when that happens `getpass.getuser()` raises and the
   credential write fails with `credential_persist_failed`. Set it explicitly, e.g.
   with NSSM:

   ```powershell
   nssm set TimeWeaverAgent AppEnvironmentExtra USERNAME=<service-account-name>
   ```

5. **Never run the agent manually while the service is running** (and vice versa).
   Two copies rotate the same refresh token and invalidate each other — see
   "One refresh token per device" above.

Example NSSM setup:

```powershell
nssm install TimeWeaverAgent "D:\path\to\TimeWeaver\run-agent.cmd"
nssm set     TimeWeaverAgent AppDirectory "D:\path\to\TimeWeaver\agent"
nssm set     TimeWeaverAgent AppEnvironmentExtra USERNAME=<service-account-name>
nssm set     TimeWeaverAgent Start SERVICE_AUTO_START
nssm start   TimeWeaverAgent
```

The service account also needs read/write access to `conf/` (credential rotation) and
`log/` (log files).

## Troubleshooting

Bootstrap failures are logged as `[bootstrap] failed: reason=<reason>`. Common cases:

| Reason | Meaning | Fix |
| --- | --- | --- |
| `enroll_failed:enrollment_token_invalid` | The enrollment token was already used, expired, or revoked (tokens are single-use). | Issue a fresh token from the dashboard and enroll once. |
| `needs_enrollment` (no credential, no token) | No stored credential and no enrollment token are available. | Provide a fresh token for a one-time enrollment, or restore the credential file. |
| `needs_enrollment` (credential file was present) | The stored credential could not be read or its refresh token was rejected. If the file was readable and valid, another copy of the agent likely rotated the refresh token. | Ensure only one agent runs; verify the run account can read the credential file (ACL / service account). |
| `credential_persist_failed` | The rotated credential could not be written. On Windows this is usually a missing `USERNAME` in the (service) environment so `getpass.getuser()` fails, or the account lacks write/ACL permission on `conf/`. | Set `USERNAME` for the service process (step 4 above) and confirm write access to `conf/`. |

If a run enrolled successfully but the next start reports `needs_enrollment` and the
credential file has disappeared, the credential was discarded on a failed read or
refresh; re-check the account and `USERNAME`/ACL conditions above before spending a
new enrollment token.

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

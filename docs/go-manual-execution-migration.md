# Human manual execution and history

> Historical implementation slice. The unified executable now supports `serve`;
> see [Go server setup and operation](go-server.md) for current startup and
> upgrade instructions. Status statements below describe this slice when introduced.

Branch: `feature/go-server-unified`, following `a2741f8`.
This slice adds the dashboard's manual-run queue and execution history to the
Go server. Production `serve` remains disabled. It does not modify the frozen
Python server, SQL assets or separate agent repository, and does not migrate or
operate a production database.

## HTTP surface and authorization

The configured API context exposes the existing paths below. A single trailing
slash is accepted without redirecting. Lists return arrays compatible with the
existing Vue screens; mutation responses explicitly confirm success and return
created queue IDs where applicable.

| Method | Dashboard path | Purpose |
| --- | --- | --- |
| POST | `/dashboard/schedule/insert_manual_schedule` | Queue the active tasks in one schedule |
| POST | `/dashboard/tasks/insert_manual_task` | Queue one active task |
| GET | `/dashboard/manual_execution/get_manual_execution_list` | Read visible manual requests and their status |
| PUT | `/dashboard/manual_execution/update_manual_execution` | Change an unclaimed request's execution time |
| PUT | `/dashboard/manual_execution/abandon_manual_execution` | Cancel an unclaimed request |
| GET | `/dashboard/schedule/execution_history` | Read visible execution results in a date interval |

Viewer permission is required for both the schedule's actual group and the
execution device's actual group. Queueing, rescheduling and cancellation require
operator permission for both; editor and global administrator roles include
that permission. Resources and their groups are resolved from stored rows.
Client group IDs, creator/modifier names and cached browser roles cannot grant
access. The existing cookie, CSRF, same-origin and password-change guards apply.

Server-provided `can_run`, `can_edit` and `can_cancel` flags control embedded Vue
buttons. They are presentation hints; every mutation independently authenticates
and authorizes in its transaction. Viewer and operator users still cannot edit
the executable schedule/task definitions.

## Queue semantics

New requests are stored with the server-controlled status `wait`. The existing
agent snapshot only includes `wait` and `processing` manual requests; there is
no server promotion of legacy `active` rows. An immediate request uses the
database's UTC clock. A reservation must supply an explicit RFC3339 time zone,
is normalized to UTC, and must be in the future according to that same database
clock. Naive local date strings are rejected by the embedded API. Database
timestamps retain the existing second precision.

A schedule-wide request creates one manual row per active, non-deleted child
task. Inactive, error and manual-status children are excluded because the pinned
agent does not publish them as runnable tasks. An empty runnable selection is
rejected rather than reported as success. The schedule, target device and their
groups must be active. Every selected task must have its paired definition; a
missing definition or unsupported recursive-housekeeping capability rejects the
entire operation. A rejected batch leaves no partial queue behind.
One request may include at most 500 runnable tasks; a larger selection is
rejected with HTTP 422 rather than being partially queued.

The queue reuses the existing agent message schema. It does not execute commands
in the server process, change task order/retry semantics, or add an atomic remote
batch-execution guarantee. Creating a request confirms a durable queue entry,
not that an offline agent has already received or completed it. Repeating a new
creation request creates another run; this human endpoint has no request-
idempotency key.

### Reservation safety correction

The pinned agent (`aa509b196c1caf35cc28b38f8015994da8906819`) dispatches every
eligible manual row after its dispatch delay. Its `Manual.Eligible` and
`Engine.Tick` do not enforce `schedule_datetime`. The frozen Python server and
the prior Go port marked every waiting row claimable, including future
reservations. Merely writing a future time therefore did not prevent early
execution.

Go now sets the existing snapshot `claimable` field only when the request is
immediate or its reserved UTC time has arrived according to the database clock,
in addition to the existing status/lease checks. Crossing the due time changes
the canonical revision and ETag even without a database row update, so an agent
polling conditionally receives the newly eligible request. The claim transaction
rechecks the locked row's due time and rejects premature claims with HTTP 409
`not_due`. The pinned agent retries claim errors, so no agent code or protocol
schema change is needed. Immediate requests still ignore legacy future date
values; already-due requests retain the existing behavior.

This is an explicit behavior correction to the frozen reference, not an
unreported parity exclusion. Runtime interoperability tests continue comparing
all business fields and canonical hashes for due/immediate fixtures. A separate
future fixture asserts that only `claimable` and its derived revision/ETag differ
from Python, and passes the result through the pinned agent's decoder. Switching
back to the frozen Python server does not preserve this reservation protection;
outstanding future reservations require review before that rollback.

## Edits, cancellation and races

Only pending `active` or `wait` rows can be changed or cancelled. Rescheduling
requires the target to remain runnable. It atomically retires the old row as
`failed` and creates a replacement `wait` row with a new manual ID. The response
returns both `manual_id` and `previous_manual_id`, and the audit record connects
them. A cached agent snapshot can no longer claim the retired ID. The former
request remains visible as history; it is not an execution result. Cancellation
also stores `failed`, the existing compatible terminal enum value. The audit
action identifies human cancellation; it is not represented as a successful
task execution. Cancellation remains possible after the relevant
device, schedule or group is disabled, provided the user still has the required
permissions and the resource relationships can be resolved.

`processing`, `done`, `failed` and other non-pending states reject mutation with
HTTP 409. In particular, cancelling a `processing` row is not a supported remote
process termination mechanism. An expired processing lease remains protected
until the existing claim/sweeper flow makes it pending again.

Human changes follow the agent's schedule-group, schedule-detail, manual-row
lock order. A preliminary parent lookup is rechecked after locking. If an agent
claim wins, an overlapping human edit/cancel is rejected without creating a
replacement. If cancellation or rescheduling wins, the agent can no longer
claim the retired request. This preserves claim tokens, result idempotency and
delayed result acknowledgement. Completed manual records and
execution history are retained when task definitions are deleted; tombstones
do not become runnable again.

Queue writes, audit records and session activity commit in one transaction.
Audit failure rolls back the queue change. Creator/modifier values come from
the authenticated user; audit detail contains identifiers and operation data,
not command text, paths, credential material or agent claim tokens.

## Execution-history ownership

History is scoped using `execution_log.schedule_id`, the parent recorded for
that execution. It is not authorized through whichever parent the task happens
to have now. Task names and definitions join only when the detail still belongs
to that recorded schedule. Tombstone names remain usable after their executable
task definition has been removed; unavailable definition fields remain null.

The recorded schedule and its execution device must both be visible to a
non-administrator. System rows and legacy rows whose ownership cannot be
resolved are available only to a global administrator. Agent-provided
`environment_info` is never used as an authorization source. Date parameters
require explicit time zones, are normalized to UTC, and must form a valid
ordered interval. Omitting the interval preserves the existing default of the
last 30 days. Both interval boundaries are inclusive, matching the legacy SQL.
An interval may span at most 366 days. If more than 1,000 visible results match,
the server returns `history_too_large` and the UI asks for a narrower interval;
it does not silently truncate the result set. Queries apply authorization before
that limit, so another group's volume cannot hide the user's own results.

The legacy execution schema has no immutable device ID, group snapshot, task
definition snapshot or historical target mapping. Historical retargeting done
by an older server therefore cannot be reconstructed by this slice. Current Go
management prevents changing a schedule's target/group once children or history
exist and prevents moving a task that has manual/execution records. Displayed
definition fields are the surviving compatible definition, not a claim that
they are a historical command snapshot.

## Vue integration

Embedded schedule/task Run buttons are enabled only for authorized runnable
targets. Immediate and reserved runs use fresh forms and explicit payloads;
display metadata and browser-supplied actor names are stripped. Local date/time
input is converted to an explicit UTC timestamp before sending. Pending manual
requests expose edit and cancel controls; processing requests do not expose a
misleading remote-stop action. Busy states prevent double submission, and errors
remain visible in the relevant list/modal. The standalone Python client retains
its existing API behavior.

The execution-history screen requests its selected date interval from the
server rather than only filtering an already downloaded default month. Results
and manual status remain readable for viewers within their authorized scope.
Existing table/modal styling and English, Korean and Japanese locales are used.

## Validation and remaining gates

This change is exercised by focused payload/date validation tests, disposable
MySQL/MariaDB integration tests, the embedded Chromium workflow, and the real
pinned Linux agent lifecycle test. Coverage includes operator/viewer boundaries,
forged IDs and cross-group resources, atomic batch/audit failure, UTC reservation
handling, reservation due-time/revision/claim boundaries, cached claims after
rescheduling, pending versus claimed mutation races, tombstones, and recorded-
parent history isolation. The real agent test includes a future reservation and
checks that it starts no earlier than its stored due time. Test fixtures use
explicitly permitted, randomly named
databases on loopback; test commands operate only inside temporary directories.
Consult the commit's CI run for the actual executed result rather than treating
this coverage description as a production-readiness claim.

Charts are covered in [the following slice](go-chart-migration.md).
OS identity/login, full SQLite support, installable server startup and
deployment/rollback validation remain separate gates. Native Windows browser and
agent lifecycle validation is still pending. Existing production service and
main-branch cutover remain outside this slice.

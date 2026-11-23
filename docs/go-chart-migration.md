# Dashboard charts and recent schedules

> Historical implementation slice. The unified executable now supports `serve`;
> see [Go server setup and operation](go-server.md) for current startup and
> upgrade instructions. Status statements below describe this slice when introduced.

Branch: `feature/go-server-unified`, following `39d566b`.
This slice connects the existing dashboard summary, charts and recent-schedule
table to the Go server. It does not enable production `serve`, change the frozen
Python server/SQL or separate agent repository, or operate a production database.

## HTTP and authorization

All paths are relative to the configured API context. Their original names and
JSON field names are retained, including the existing `lastest` spelling. A
single trailing slash is accepted without a redirect.

| Method | Path | Result |
| --- | --- | --- |
| GET | `/dashboard/charts/devices` | `active_count`, `error_count`, `inactive_count` |
| GET | `/dashboard/charts/schedules` | `active_count`, `error_count`, `inactive_count` |
| GET | `/dashboard/charts/tasks` | `pending_count`, `in_progress_count`, `completed_count`, `error_count` |
| GET | `/dashboard/lastest-schedules` | Up to three visible schedules and their latest execution-instance summary |

Count endpoints return one object with integer values, including zeros for an
empty authorized scope. The recent-schedule endpoint returns an array, including
`[]` when no rows qualify. Unavailable storage or an expired session is an error,
not a fabricated zero-count result.

The existing human cookie, same-origin and password-change guards apply. Every
query authenticates through the access service. Viewer, operator and editor
users see only their assigned scope; only explicit global membership or the
administrator role grants wider visibility. Client `group_id` or other query
parameters do not alter permission scope.

Device counts require viewer access to the device's actual group. Schedule,
task and recent-instance data require viewer access to both the schedule group
and its target device's actual group. This protects legacy mismatched targets as
well as normally configured rows. Disabled groups and resources remain readable
when authorized, consistently with existing management lists. Internal device
and schedule sentinels with IDs at or below zero are excluded even for an
administrator. Recent rows are scoped before the limit, so invisible schedules
cannot consume the user's three result slots.

## Preserved counting rules

These charts summarize different operational states. Their historical windows
and status rules are deliberately retained; their totals are not interchangeable
inventory counts. In particular, a task chart can contain fewer tasks than the
task-management list.

All age windows use the database's UTC clock and rolling durations, independent
of the application host or browser time zone. Date boundaries below are inclusive
as specified. The legacy execution queries have only a lower time bound; future-
dated execution records are not newly excluded by this migration.

### Devices

| Condition, in priority order | Category |
| --- | --- |
| Stored status is `inactive` | Inactive |
| Heartbeat exists and is at least five minutes old | Error |
| No heartbeat, and last login is at least one day old | Error |
| Otherwise | Stored status |

A stale heartbeat is authoritative even if a newer last-login time exists. If
both timestamps are null, the stored status survives the legacy SQL's null
comparison; an active device therefore remains in the active category. This
endpoint does not invent a new unknown state or silently change that behavior.

### Schedules

Each visible schedule is counted once. A last-seen device timestamp at least one
day old, or the schedule's stored `inactive` status, makes the schedule inactive.
Otherwise any nonzero result for that recorded schedule whose start time is at
or after UTC now minus one day makes it an error. No recent history, or only
successful results, makes it active.

This chart intentionally retains its one-day device window; it does not adopt
the device chart's five-minute heartbeat window. It also does not directly use
the device's stored inactive status or treat a schedule's stored `error` status
alone as an execution error. Null last-seen timestamps do not satisfy the stale
condition. These are existing aggregation semantics, not new server policy.

### Tasks

Eligible rows must have a non-deleted task detail with status `active` or
`error`, a parent schedule outside `inactive`/`manual`, and a target device
outside `inactive`/`manual`. Its heartbeat, or last login when no heartbeat
exists, must be at or after UTC now minus one day. Null last-seen timestamps fail
that condition and exclude the task from this chart. The query does not newly
require the paired executable task-definition row.

For each eligible task, a running marker at or after UTC now minus one hour
takes precedence and produces `in_progress`. Otherwise the latest execution
starting at or after UTC now minus one day produces `completed` for result code
zero or `error` for a nonzero result. With no such execution the task is
`pending`. Old running markers and old executions do not affect those states.

The latest result is selected within the task's recorded schedule and detail
pair, with `execution_id` breaking equal start-time ties. A task moved by a
legacy server cannot acquire another schedule's historical result merely because
its detail UUID stayed the same. This is an authorization and correctness
correction to the former detail-only join.

## Recent execution instances

The recent table is not limited to the last day. Executions are grouped by the
recorded `schedule_id` and `execution_grp_id`. The latest instance of each
schedule is chosen by its latest task start time, with an execution-ID tie
breaker, then the three newest visible schedules are returned deterministically.
Group start/end times are the minimum start and maximum non-null end time in
that instance. Returned timestamps are explicit UTC values for display.

The expected task count is the current count of non-deleted details whose status
is not `inactive`, preserving the legacy query. The executed count is the number
of distinct details recorded in the chosen instance. These are current-definition
versus historical-instance counts; adding or removing tasks can therefore affect
the summary of an older instance. Schedules with no current qualifying details,
including schedules whose details are all tombstones, are omitted. Their
execution history remains available through the separate history endpoint.

The existing custom-status categories remain:

| Condition, in priority order | Custom status |
| --- | --- |
| Schedule status is `error`, or fewer details executed than are currently expected | `error` |
| Schedule is `active`, all expected details executed, and the instance contains a failed execution | `warning` |
| Otherwise | `completed` |

Failure detection now recognizes every nonzero result code. The selected message
comes from the first failure in chronological order, with execution ID resolving
equal start times. The previous query recognized only `-1` and used the
lexicographically smallest message despite calling it the first error. Newlines
are flattened and the existing 25-character preview rule remains: messages
longer than 25 characters show the first 22 followed by `...`. Null failure
messages stay null and do not hide the failure. If the first failed execution
has no message, the server does not substitute a later message or invent text.

The separate `group_status` field retains the device chart's five-minute
heartbeat/one-day login fallback for `disconnected`, then its schedule/error
rules. The dashboard's recent error and warning cards count the returned recent
rows; they are not a site-wide count of all historical failures.

## Data and UI preservation

The server derives scoped reads from the embedded legacy catalog using checked,
code-owned replacements. Frozen source files and embedded SQL bytes remain
unchanged. Unexpected query structure fails closed rather than dropping a scope
predicate. No migration, mutation API, polling daemon or writable chart state is
introduced.

The existing Vue dashboard, chart types, theme and table layout are retained.
Summary cards and charts show the same fetched count values. Loading, failed
requests and successful empty data have distinct presentation; refresh updates
the displayed fetch time after success. The standalone Python mode retains its
existing API behavior. New labels and errors follow the existing English,
Korean and Japanese locale structure.

## Validation and remaining gates

Focused tests exercise scoped query construction and HTTP contracts, count
objects for empty scopes, device liveness fallbacks, rolling execution/running
windows, deterministic ties, moved-task isolation, sentinel exclusion and
permission filtering before the recent-row limit. Disposable MySQL and MariaDB
tests verify the actual aggregation results. Embedded Chromium tests cover
visible cards/charts/recent rows in a viewer-scoped dashboard, refresh and
request-error presentation. Administrator and global membership scopes are
covered through the real HTTP integration tests. Consult the commit's CI run for the actual executed
results; this coverage description is not a production-readiness claim.

OS identity/login is implemented in the subsequent
[OS account slice](go-os-access-migration.md). Native SQLite query and service
support is implemented in the [SQLite slice](go-sqlite-migration.md).
Remembered-session continuity, installable server startup and deployment/rollback
validation remain separate gates. Production `serve` stays blocked, and this
slice does not change the main-branch deployment.

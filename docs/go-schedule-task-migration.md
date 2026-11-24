# Schedule and task management

> Historical implementation slice. The unified executable now supports `serve`;
> see [Go server setup and operation](go-server.md) for current startup and
> upgrade instructions. Status statements below describe this slice when introduced.

Branch: `feature/go-server-unified`, following `aef493d`.
This slice does not enable production `serve`, merge main, change frozen Python
sources/SQL, access production data or modify the separate agent repository.

## Implemented surface

Go now serves the existing schedule/task list, insert, update and remove paths
under the configured API context. Single trailing-slash aliases do not redirect.
The original SQL catalog is reused for mutations. Schedule and task rows, audit
records, session activity and authorization commit in the same transaction.
Creator/modifier claims from clients cannot replace the authenticated actor.

Lists require viewer rights for both the schedule group and its execution
device's actual group. The legacy group_id/schedule_id list parameters cannot
enlarge access; embedded clients request the authorized scope rather than group
zero. Changes require editor rights. Updates resolve and lock existing rows, and
moves check both source and destination schedule/device groups. Task moves lock
parents in ID order and recheck the detail's parent after locking it. User role
or membership revocation is ordered against mutations by the existing user lock.
Viewer and operator roles cannot edit executable definitions.

New or changed schedule assignments must use a target in the selected group.
Existing legacy assignments, including group zero (Unknown) schedules targeting
named-group devices, can be edited without changing the group/device pair.
Both groups still require editor rights and must be active for task writes and
non-pausing schedule changes. Disabling a schedule with an unchanged target
remains possible after the device or group was disabled. Existing assignments
are not silently reassigned or repaired. Tasks inherit the existing schedule's
target and are not rejected merely because that target belongs to another group.
Recursive housekeeping still requires the target agent's declared capability.

Seven cron fields follow the pinned agent's APScheduler contract, including
Monday=0 and AND day semantics. Numeric fields, lists, ranges, aliases and steps
are checked before saving, as are legacy column lengths (four characters except
weekday's thirty). Unsupported Quartz expressions are not reinterpreted. No
schema widening or time-zone conversion is introduced. New cron omissions mean
`*`. Existing stored expressions are not rewritten by the migration.

Tasks preserve command/copy/archive/housekeep fields, order and retry count.
Input is bounded and typed; inapplicable task fields are cleared just as in the
Python normalization. Retry count is limited to 0..100 for new edits. Recursive
housekeeping requires an explicit target capability `housekeep_recursive_v1`.
The server never executes submitted commands; the existing agent contract does.
Audit detail includes IDs/type, not command text or paths.

## Preservation rules

A schedule with details (including tombstones), execution history or running
records cannot be physically removed. Disable it instead. Retargeting a schedule
with existing children/history is similarly refused rather than transferring
executable commands or historical ownership implicitly.

Task moves are refused when execution/running/manual records are attached.
Task deletion is refused while running or while manual requests are queued or
processing. Otherwise the paired task definition is removed and the detail is
soft-deleted atomically. Its tombstone, execution history and completed manual
records remain available for delayed result reports. Neither disabling nor
soft-deleting is a remote cancellation command for already-dispatched work.
These checks deliberately tighten the older unguarded deletion/move behavior.

## Vue integration

Existing list/modal layout, theme and clock favicon are retained. Embedded
schedule creation selects an authorized group and a device from that group.
Only editable parent schedules appear in the task form. Server-provided edit
hints control buttons, but server authorization is still mandatory. Payloads
contain only write fields, not display metadata or cached user identities.

Embedded weekday labels now match the actual agent (Sunday=6, Monday=0).
Existing range/alias values remain visible when editing. Error-stop and missing-
source flags preserve false values rather than interpreting the string "0" as
true. Fresh forms cannot inherit the previously edited item's ID or status.
Saving blocks duplicate submissions and shows localized English/Korean/Japanese
errors. Order and retry count can be edited. Existing standalone Vue retains its
Python API and weekday behavior. Embedded manual-run buttons remain hidden until
the human manual-execution APIs have been ported; they are not fake successes.

## Verification

Node tests cover typed payloads, metadata/actor stripping, boolean false/null
normalization and matching locale keys. Go validation tests cover cron grammar,
bounds, oversized input and task-specific normalization. Disposable database
integration tests cover role and cross-group restrictions, source/target checks,
paired-row rollback on audit failure, history-safe moves/deletes, recursive
capability checks, inactive-device pause, and redirect-free HTTP boundaries.

`TestBrowserSchedulesFlow` drives Chromium against the real embedded Vue build:
create/edit/remove schedules and tasks, invalid cron rejection, weekday labels,
retry/order persistence, viewer/operator restrictions, filtered lists, disabling
an occupied schedule, and fresh-form state. The real Go-agent lifecycle test now
creates its schedule/task definitions through the new management service rather
than direct SQL. The manual queue is still a test fixture until that API is ported.
All test databases are explicitly permitted, randomly named and on loopback.

CI includes MySQL 8.4 and MariaDB 11.4, the existing Python/Go agent contract
checks, actual Linux Go-agent execution, Vue standalone/embedded builds,
Windows/Linux unit tests and vet, Linux race tests and four cross-build targets.
A successful build is not a production-readiness claim. Native Windows browser
and agent lifecycle integration are still pending.

## Remaining gates

Human manual-execution/history/chart APIs, OS identity/login, session persistence
and rotation extensions, full SQLite schema/dialect support, installable server
CLI and deployment/permission-safe rollback remain. Production `serve` stays
blocked; do not replace the deployed server with this branch yet.

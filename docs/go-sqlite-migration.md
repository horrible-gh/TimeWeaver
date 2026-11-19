# SQLite store, migrations and API support

Branch: `feature/go-server-unified`, following OS-account baseline `3284eb4`.
This slice adds SQLite to the implemented Go application services. Production
`serve` remains gated; deployment inventory, remembered-session continuity,
installable startup and deployment/rollback verification remain separate work.

## Scope

The same composed listener can use MySQL/MariaDB or a real SQLite file for:

- Human login, forced password changes, durable sessions, group permissions,
  administration, and pre-linked Linux/Windows OS identities.
- Devices and enrollment, schedules and tasks, manual requests, execution history,
  dashboard counts and recent schedules.
- Agent enrollment/credential rotation, heartbeat and consistent snapshots,
  execution start/results, manual claims, events, lease expiry and health probes.

The public HTTP contracts and embedded Vue screens are retained. SQLite is
selected from `DB_TYPE=sqlite3` (the existing `sqlite` and `local` aliases also
work) and `DB_PATH`. A relative path remains relative to the selected env file.
The independent agent still talks only to Agent API v1; it has no database access.

## Native schema and query assets

The frozen `server/`, `agent/` and `internal/store/sql/` resources are unchanged.
New assets live in `internal/store/sqlite/`:

- `migrations/go_sqlite_001.sql` provides the complete twelve-table business and
  agent schema, keys, indexes, binary UUID/token constraints, and UTC date defaults.
- The native query catalog supplies the same 39 query keys as the MySQL catalog.
  It uses SQLite date functions, JSON extraction and string expressions directly.
  Application code chooses the catalog explicitly; no database driver translates
  arbitrary MySQL SQL behind the caller's back.
- The seven frozen historical SQLite files remain available for inventory and
  history verification. They are incomplete as a fresh bootstrap, so the Go
  migrator does not replay them or invent completion records for them. A fresh
  installation records only the new migration that actually ran.

Git attributes preserve the exact versioned bytes of both legacy SQL resource
copies and the new SQLite assets on every checkout. Native Windows builds and
Linux builds therefore compute the same migration checksums; line-ending
conversion must not change the identity of an already recorded migration.

A fresh database seeds reserved group 0 (`Unknown`), the legacy device -1
sentinel, and the existing restricted administrator seed. Access initialization
and login retain the mandatory initial password-change gate. Upgrading an
existing database must preserve its users and password hashes; it must not
create an additional seed administrator.

## Migration and inspection boundaries

Read-only commands open an existing file with SQLite `mode=ro` and `query_only`.
A missing file is an error and is never created by status inspection. Write
commands still require `--allow-write` before opening a writable store.

```sh
# Inspect a database copy without changing it.
go run ./cmd/timeweaver migrate status --env copied-server.env
go run ./cmd/timeweaver schema status --env copied-server.env
go run ./cmd/timeweaver access status --env copied-server.env

# Explicitly migrate the copy, then install/upgrade the auxiliary access schema.
go run ./cmd/timeweaver migrate up --env copied-server.env --allow-write
go run ./cmd/timeweaver access init --env copied-server.env --allow-write
```

SQLite migration DDL, copied data and completion/checksum records use one
`BEGIN IMMEDIATE` transaction. Pre-commit failure rolls back the entire unit.
A commit error can leave the caller uncertain about the outcome, while SQLite
still commits all changes or none; the application does not blindly replay an
uncertain write. Existing
historical completion records are retained; unknown, unfinished or changed
history blocks automatic migration. A nonempty database with no migration
history is refused. Existing deployments require a stopped writer and a verified
backup before using these write commands; the CI fixtures are disposable copies.

Tracked legacy upgrades accept recognized table/column types and known additive
gaps. They preserve existing IDs, password hashes, blob identities, nullable
flags, task tombstones, results, ordinary indexes and automatic-ID high-water
marks. Supported historical schemas are rebuilt to the native definitions;
missing tables are created only when the recorded history does not claim they
already exist. Numeric task identities are not guessed into UUIDs. Unknown
columns, generated columns, triggers, views, custom key/constraint/default
policies, duplicate execution identities, orphaned group references and invalid
timestamps require manual review. Refusal leaves schema, rows and history
unchanged. A native completion record without its matching checksum journal is
also incomplete, even when the migration filename is present.

During a supported table rebuild, foreign-key enforcement is disabled only on
the pinned migration connection, outside the transaction as SQLite requires.
Foreign-key integrity is checked before commit. Cleanup attempts to restore
foreign-key enforcement and discards the connection on every exit, so a pooled
application connection cannot inherit that
temporary migration setting.

Critical schema inspection compares columns, types, defaults, keys,
AUTOINCREMENT behavior, check constraints and group foreign keys against the
native reference. `schema repair --allow-write` is limited to restoring reserved
group 0 after a complete clean migration. It does not rebuild an unexpected
application schema or remove invalid data, and the final foreign-key check must
still pass before repair commits.

The access schema retains its own version (currently v3). Its SQLite installer
is transactional and preserves existing session/grant rows on a supported
upgrade. Schema status also checks key order, binary key collations, foreign
keys and the unique OS-identity binding. Unexpected partial structures are
rejected rather than silently adopted.

## Transactions and concurrency

SQLite writable pools use WAL, foreign-key enforcement and immediate
transactions. Authorization and mutations acquire the SQLite writer reservation
before reading session, role, resource or claim state. Independent stores on the
same file therefore cannot both act on stale last-administrator, enrollment,
credential or manual-claim state. SQLite has one writer; the implementation does
not pretend that `FOR UPDATE` or `SKIP LOCKED` exist there.

Agent snapshots pin one connection and explicitly start a deferred read
transaction. All rows come from the same snapshot while another connection can
commit a newer WAL state. The snapshot connection is temporarily read-only;
success, cancellation and scan-error paths finish or roll back the transaction
and restore its settings, or discard the connection if cleanup cannot be
completed. Health uses a separate read-only store.

SQLite's native busy handler can delay context interruption. The native busy
timeout is bounded to 250 ms. Application writer admission retries busy/locked
errors only while acquiring `BEGIN IMMEDIATE`, before a transaction or any
authorization read/write exists. The busy retry window has a five-second budget
and honors the caller's cancellation. Waiting for a pooled connection and a
successful transaction both retain the caller's original context. Migration lock
acquisition has its own context-aware five-second
budget. Application statements, authentication callbacks and commits are not
replayed by this admission retry, including after an uncertain commit.

## Dates, binary values and response types

Dates are bound as canonical UTC SQLite text, with a space between date and time
and fractional seconds retained when present. Database `CURRENT_TIMESTAMP` is
UTC. Supported legacy date values are normalized during migration, including
timezone offsets and fractional seconds; an unparseable value aborts the
transaction. Explicit scanners accept native driver dates and supported
historical text formats; nullable dates remain nullable. Date expressions and
aggregates are normalized at the JSON boundary, so SQLite's string-returning
aggregates do not change the browser/agent timestamp contract.

Binary UUIDs remain sixteen-byte blobs and token digests remain thirty-two-byte
blobs. Token material is not added to audit records or error diagnostics. SQLite
unique/foreign-key errors use the existing conflict responses; other database
failures remain unavailable responses rather than false empty results.

Old SQLite `BOOLEAN` declarations can return Go booleans while the fresh schema
uses integer flags. Agent scanning accepts both representations and preserves
the existing nullable/default behavior. Immediate manual requests may retain a
legacy null due time; a reserved request without a due time is not claimable.

## Verification

The test suite uses real temporary SQLite files and, where concurrency matters,
independent `Store` instances. Existing MySQL tests keep their assertions and
opt-in gates. Test-fixture SQL adapts only setup/inspection syntax; runtime
queries execute the native SQLite implementation.

- CLI tests cover missing-file read protection, write consent, migration replay,
  critical schema status and access initialization.
- Store tests cover the native schema/query catalog, historical migration
  handling, data preservation, transaction rollback, concurrency and schema
  refusal/repair boundaries.
- Access scenarios cover the existing local/OS/session/administration contracts,
  independent-store authorization/revocation and last-administrator protection,
  rate limits, schema initialization/upgrade and rejection of key/FK drift.
- Nine agent integration scenarios cover enrollment/rotation races, lost result
  acknowledgements, claim serialization, transaction rollback, stable/read-only
  snapshots, lease sweeping, delayed-writer admission, cancellation and
  independent health inspection.
- Twenty-one shared dashboard/HTTP scenarios cover schedules/tasks, devices,
  enrollment, manual requests, scoped history, chart aggregation and failure
  behavior.

The `store-sqlite` CI job runs the real-file suites with the race detector under
`TZ=Asia/Tokyo`, then the same seven embedded Chromium workflows used by the
MySQL/MariaDB gates. It builds TimeWeaver-Agent at
`aa509b196c1caf35cc28b38f8015994da8906819` and runs that actual executable against
the SQLite unified listener. The end-to-end assertions retain reservation
timing, result replay/deduplication, credential restart and graceful shutdown.

Consult the exact commit's workflow run for executed results. The full workflow
also retains legacy-source freezing, both Vue builds, Linux/Windows Go tests,
native OS-account authentication, cross-compilation, MySQL 8.4 and MariaDB 11.4
integration and Python interoperability. A SQLite pass does not replace those
regression gates or establish production rollback safety.

## References

- [SQLite isolation](https://www.sqlite.org/isolation.html): single-writer
  serialization and WAL snapshots.
- [SQLite transactions](https://www.sqlite.org/lang_transaction.html): deferred
  and immediate transaction behavior and busy errors.
- [SQLite date and time functions](https://www.sqlite.org/lang_datefunc.html):
  supported date formats and UTC evaluation.

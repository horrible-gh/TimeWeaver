# Housekeeping: include subfolders

In **Tasks**, select **Housekeep**, set the destination directory and retention
in days, then optionally check **Include subfolders**. The checkbox is off for
new tasks and for existing tasks migrated from older versions.

The server stores `house_keep_recursive` in `task_detail`, includes it in the
agent snapshot and its revision/ETag, and never deletes local files itself.
The Go TimeWeaver-Agent executes the cleanup on the selected device. Scheduled
and manual execution use the same task options.

## Behavior

- Off: clean direct regular files only. Expired subdirectories are kept and
  reported as skipped, not failed. No recursive cleanup is implied.
- On: apply the existing modification-time cutoff independently to regular files
  at every depth. A file is expired only when its mtime is strictly before
  `now - house_keep_days * 86400` seconds. Files exactly on the cutoff remain.
- Do not delete directories (including empty folders and the selected root),
  symbolic links, Windows junctions/reparse points, or special files. An old
  directory does not cause its newer files to be removed as a group.
- Nonpositive retention does not delete files. Permission-denied child paths
  are skipped while other files continue. Skipped paths are summarized in the
  agent log and returned with result code 0 and a [housekeep-warning] message.
  The Go dashboard distinguishes these warnings from errors. They do not stop
  schedules or fail manual execution. A child already removed by another
  process is skipped silently. An inaccessible or invalid root, other I/O
  errors, and cancellation remain failures.
- Shell command tasks (including custom rm/find commands) are not built-in
  housekeeping; their nonzero exit status remains an error. Old error rows
  are not rewritten. Use a Housekeep task for the safe skip policy.

This is not in-use-file protection or an atomic filesystem snapshot. Use an
explicit cleanup directory controlled by the agent's account. Do not use this
as a security sandbox for a directory other users can concurrently replace or
retarget, or point it at arbitrary locations under `/tmp`.

## Deployment and compatibility

Update and rebuild **TimeWeaver-Agent first**, then update the server and rebuild
the client. Restart the agent with its existing configuration and credentials;
there is no re-enrollment step. The server applies migration
`timeweaver_server_015.sql` on startup. MySQL/MariaDB and SQLite migration files
are included; the rest of the existing database-backend support is unchanged.

The updated Go agent advertises `housekeep_recursive_v1` in
`X-TW-Agent-Capabilities`. Heartbeats record this in `devices.agent_capabilities`;
a heartbeat without it clears the stored capability. The server refuses to save
a recursive task until the target agent has reported support. The UI displays a
short update-and-start instruction instead of a generic save failure.

The server also refuses a snapshot containing recursive work to an agent without
this capability, including conditional/304 requests, with HTTP 409 and
`agent_upgrade_required`. This protects fresh or downgraded agents. Start-report
endpoints in legacy binaries are advisory, not an execution security boundary;
stop legacy agents before upgrading and never share one device's credentials
between two agents. Already-running work is not retroactively cancelled.

The legacy Python agent does not implement the new feature. Keep the checkbox
off for that agent, or migrate it to the updated Go agent. Existing non-recursive
work remains compatible. No schema-version bump or database credentials on the
agent are required.

## Delete empty directories (unified Go server)

The separate **Delete empty directories** checkbox defaults to off, including
for existing tasks. With subfolders disabled, remove only empty direct child
directories. With subfolders enabled, remove empty descendants bottom-up after
expired files have been cleaned. Never remove the configured root, nonempty
directories, symbolic links or Windows junctions/reparse points. Directory age
is not considered; retention applies to files. Zero days with the option
selected performs directory-only cleanup. Permission-denied child directories
are skipped with warnings. Other root/I/O errors remain failures.

The Go server adds `house_keep_delete_empty_dirs` with default false
using `timeweaver_server_016.sql` (MySQL/MariaDB) or `go_sqlite_002.sql`
(SQLite). These reviewed additive migrations are applied automatically on
routine server startup. Update both the unified server and agent. Saving or executing an
opted-in task requires the agent capability `housekeep_delete_empty_dirs_v1`.
The flag is included in the snapshot revision/ETag when enabled. Old agents
and unchanged tasks retain the original behavior. No re-enrollment is needed.

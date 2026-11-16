# Housekeeping: include subfolders

In **Tasks**, select **Housekeep**, set the destination directory and retention
in days, then optionally check **Include subfolders**. The checkbox is off for
new tasks and for existing tasks migrated from older versions.

The server stores `house_keep_recursive` in `task_detail`, includes it in the
agent snapshot and its revision/ETag, and never deletes local files itself.
The Go TimeWeaver-Agent executes the cleanup on the selected device. Scheduled
and manual execution use the same task options.

## Behavior

- Off: existing direct-child behavior is unchanged, including the error for an
  expired real directory. No recursive cleanup is implied.
- On: apply the existing modification-time cutoff independently to regular files
  at every depth. A file is expired only when its mtime is strictly before
  `now - house_keep_days * 86400` seconds. Files exactly on the cutoff remain.
- Do not delete directories (including empty folders and the selected root),
  symbolic links, Windows junctions/reparse points, or special files. An old
  directory does not cause its newer files to be removed as a group.
- Nonpositive retention does not delete files. Permission/I/O errors fail the
  task; a child already removed by another process is skipped. Cancellation is
  checked during traversal and before removal.

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

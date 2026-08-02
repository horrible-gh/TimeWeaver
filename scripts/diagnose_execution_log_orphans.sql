-- Read-only diagnosis for timeweaver_server_011 invalid_detail_id findings.
--
-- Usage:
--   mysql -u <user> -p <database> < scripts/diagnose_execution_log_orphans.sql
--
-- The 011 migration records every execution_log row it found without a
-- schedule_detail match in execution_log_quarantine and commits that evidence
-- before it does anything else. This script only displays it. It never updates,
-- remaps, or removes execution history.
--
-- Read auto_restored_tombstone first. It says what already happened:
--   yes  011 restored the schedule_detail row as a tombstone under the original
--        UUID and startup continued. Nothing to do. The row is listed as a
--        record of that repair, not as an outstanding problem.
--   no   011 could not rebuild an identity for the row, so it still fails closed
--        on every startup. That only happens when detail_id is not a 16-byte
--        UUID at all. Check source_detail_data_type and quarantine_reason, then
--        convert execution_log.detail_id to BINARY(16), or explicitly approve
--        exactly one action per source_execution_id:
--          1. Remap execution_log.detail_id to a verified replacement UUID.
--          2. Add a schedule_detail tombstone carrying the original UUID.
--          3. Back up and then remove the source execution_log row under the
--             site's audit-retention policy.
-- Never infer a replacement UUID from schedule_id or display text alone.

START TRANSACTION READ ONLY;

SELECT
    q.source_execution_id,
    HEX(q.execution_grp_id) AS execution_group_hex,
    q.schedule_id AS logged_schedule_id,
    q.detail_id_hex,
    q.source_detail_data_type,
    q.quarantine_reason,
    CASE
        WHEN rl.detail_id IS NULL THEN 'no'
        ELSE 'yes'
    END AS auto_restored_tombstone,
    rl.restored_at,
    q.attempt,
    q.manual_id,
    q.start_time,
    q.end_time,
    q.result_code,
    q.result_message,
    CASE
        WHEN sd.detail_id IS NULL THEN 'unmapped'
        ELSE 'mapped'
    END AS current_mapping_state,
    sd.schedule_id AS mapped_schedule_id,
    sd.schedule_name AS mapped_schedule_name,
    CASE
        WHEN td.detail_id IS NULL THEN 'missing'
        ELSE 'present'
    END AS current_task_detail_state,
    q.quarantined_at
FROM execution_log_quarantine q
LEFT JOIN schedule_detail sd
    ON sd.detail_id = q.detail_id
LEFT JOIN task_detail td
    ON td.detail_id = q.detail_id
LEFT JOIN schedule_detail_restore_log rl
    ON rl.detail_id = q.detail_id
ORDER BY q.source_execution_id;

SELECT
    q.quarantine_reason,
    CASE
        WHEN rl.detail_id IS NULL THEN 'no'
        ELSE 'yes'
    END AS auto_restored_tombstone,
    COUNT(*) AS affected_rows,
    MIN(q.quarantined_at) AS first_quarantined_at,
    MAX(q.quarantined_at) AS last_quarantined_at
FROM execution_log_quarantine q
LEFT JOIN schedule_detail_restore_log rl
    ON rl.detail_id = q.detail_id
GROUP BY q.quarantine_reason, auto_restored_tombstone
ORDER BY affected_rows DESC, q.quarantine_reason;

COMMIT;

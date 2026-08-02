-- Read-only diagnosis for timeweaver_server_011 invalid_detail_id failures.
--
-- Usage:
--   mysql -u <user> -p <database> < scripts/diagnose_execution_log_orphans.sql
--
-- The 011 migration creates and commits execution_log_quarantine before it
-- fails closed. This script only displays that evidence. It never updates,
-- remaps, or deletes execution history.
--
-- After reviewing the rows, an operator must explicitly approve exactly one
-- action for each source_execution_id:
--   1. Remap execution_log.detail_id to a verified replacement task UUID.
--   2. Restore a verified schedule_detail tombstone with the original UUID.
--   3. Back up and then delete the source execution_log row under the site's
--      audit-retention policy.
-- Never infer a replacement UUID from schedule_id or display text alone.

START TRANSACTION READ ONLY;

SELECT
    q.source_execution_id,
    HEX(q.execution_grp_id) AS execution_group_hex,
    q.schedule_id AS logged_schedule_id,
    q.detail_id_hex,
    q.source_detail_data_type,
    q.quarantine_reason,
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
ORDER BY q.source_execution_id;

SELECT
    quarantine_reason,
    COUNT(*) AS affected_rows,
    MIN(quarantined_at) AS first_quarantined_at,
    MAX(quarantined_at) AS last_quarantined_at
FROM execution_log_quarantine
GROUP BY quarantine_reason
ORDER BY affected_rows DESC, quarantine_reason;

COMMIT;
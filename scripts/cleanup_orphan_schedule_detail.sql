-- One-off repair for orphaned schedule_detail rows (see NR0003 / B0001).
--
-- Before insert_task became transactional, the schedule_detail INSERT committed on
-- its own connection while the task_detail INSERT failed on archive_type='null'.
-- Every failed "add task" therefore left a schedule_detail row with no task_detail.
-- get_tasks LEFT JOINs task_detail, so those rows appear in the UI as empty tasks.
--
-- This is NOT an auto-migration on purpose: it deletes user-entered rows, and the
-- migrator would run it unattended at server startup. Run it by hand, review the
-- SELECT output first, and keep schedule_detail_orphan_bak until you are satisfied.
--
-- Usage:
--   mysql -u <user> -p <database> < scripts/cleanup_orphan_schedule_detail.sql
--
-- NOTE: keep every statement on its own line and never put a semicolon inside a
-- comment in res/sql/migration - sqloader splits files on ';' and would treat the
-- comment tail as SQL.

-- 1. Inspect what would be removed.
SELECT sd.schedule_id, sd.schedule_name, sd.created_at, sd.creator
FROM schedule_detail sd
LEFT JOIN task_detail td ON sd.detail_id = td.detail_id
WHERE td.detail_id IS NULL
ORDER BY sd.created_at;

-- 2. Keep a copy.
CREATE TABLE IF NOT EXISTS schedule_detail_orphan_bak LIKE schedule_detail;

INSERT INTO schedule_detail_orphan_bak
SELECT sd.* FROM schedule_detail sd
LEFT JOIN task_detail td ON sd.detail_id = td.detail_id
WHERE td.detail_id IS NULL;

-- 3. Remove the orphans only after step 2 reports the expected row count.
DELETE sd FROM schedule_detail sd
LEFT JOIN task_detail td ON sd.detail_id = td.detail_id
WHERE td.detail_id IS NULL;

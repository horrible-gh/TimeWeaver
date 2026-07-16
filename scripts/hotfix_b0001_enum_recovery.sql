-- B0001 hotfix: restore the enum members that task saves depend on.
-- Use this ONLY when the server process cannot be restarted right away.
-- On restart the server applies timeweaver_server_004.sql (and the startup
-- schema guard) which performs the same widening, so running this first is
-- safe and the definitions converge.
--
-- Run as an operator, against the SERVER database from server/.env
-- (DB_DATABASE), for example:
--   mysql -u <user> -p <database> < scripts/hotfix_b0001_enum_recovery.sql
--
-- NOTE for maintainers: keep this file OUT of the migration directories.
-- If it is ever moved there, remember the migrator splits files on every
-- semicolon, including semicolons inside comments.

-- 1) Inspect the current state. archive_type showing enum('zip') confirms
--    the B0001 symptom (error 1265 on task save).
SELECT TABLE_NAME, COLUMN_NAME, COLUMN_TYPE
  FROM information_schema.COLUMNS
 WHERE TABLE_SCHEMA = DATABASE()
   AND ((TABLE_NAME = 'task_detail'     AND COLUMN_NAME = 'archive_type')
     OR (TABLE_NAME = 'schedule_detail' AND COLUMN_NAME = 'status'));

-- 2) Non-destructive widening (identical to timeweaver_server_004.sql).
ALTER TABLE task_detail MODIFY COLUMN archive_type enum('null','zip') DEFAULT NULL;

ALTER TABLE schedule_detail MODIFY COLUMN status enum('active','inactive','error','manual') DEFAULT 'active';

-- 3) Verify: archive_type must now be enum('null','zip') and status must
--    include 'manual'.
SELECT TABLE_NAME, COLUMN_NAME, COLUMN_TYPE
  FROM information_schema.COLUMNS
 WHERE TABLE_SCHEMA = DATABASE()
   AND ((TABLE_NAME = 'task_detail'     AND COLUMN_NAME = 'archive_type')
     OR (TABLE_NAME = 'schedule_detail' AND COLUMN_NAME = 'status'));

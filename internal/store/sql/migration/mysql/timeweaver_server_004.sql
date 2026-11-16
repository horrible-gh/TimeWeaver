-- Re-apply the column widenings that timeweaver_server_001.sql reverted.
-- Migrations run in plain filename order (sorted glob), so task_detail_001.sql
-- and schedule_detail_003.sql ran BEFORE timeweaver_server_001.sql re-created
-- these tables from the older definitions, silently dropping both enum members.
ALTER TABLE task_detail MODIFY COLUMN archive_type enum('null','zip') DEFAULT NULL;

ALTER TABLE schedule_detail MODIFY COLUMN status enum('active','inactive','error','manual') DEFAULT 'active';

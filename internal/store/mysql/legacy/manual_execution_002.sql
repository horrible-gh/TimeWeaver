ALTER TABLE manual_execution MODIFY status ENUM('inactive', 'active', 'wait', 'processing', 'done', 'failed') DEFAULT 'active';
ALTER TABLE manual_execution DROP COLUMN schedule_id;


-- Preserve schedule detail identity for immutable execution history.
-- The column-existence guard makes replay safe after a partial deployment.
SET @tw_schedule_detail_deleted_at_exists = (
    SELECT COUNT(*) FROM information_schema.columns
     WHERE table_schema = DATABASE() AND table_name = 'schedule_detail'
       AND column_name = 'deleted_at'
);
SET @tw_schedule_detail_deleted_at_sql = IF(
    @tw_schedule_detail_deleted_at_exists = 0,
    'ALTER TABLE schedule_detail ADD COLUMN deleted_at DATETIME NULL DEFAULT NULL COMMENT ''Soft-delete tombstone timestamp retained for execution history'' AFTER modified_at',
    'SELECT 1'
);
PREPARE tw_schedule_detail_deleted_at_stmt FROM @tw_schedule_detail_deleted_at_sql;
EXECUTE tw_schedule_detail_deleted_at_stmt;
DEALLOCATE PREPARE tw_schedule_detail_deleted_at_stmt;
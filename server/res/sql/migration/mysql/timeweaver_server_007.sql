-- Add the device-scoped snapshot lookup index without duplicating it on replay.
SET @tw_snapshot_index_exists = (
    SELECT COUNT(*)
      FROM information_schema.statistics
     WHERE table_schema = DATABASE()
       AND table_name = 'schedule_group'
       AND index_name = 'idx_schedule_group_001'
);
SET @tw_snapshot_index_sql = IF(
    @tw_snapshot_index_exists = 0,
    'ALTER TABLE schedule_group ADD INDEX idx_schedule_group_001 (target_device)',
    'SELECT 1'
);
PREPARE tw_snapshot_index_stmt FROM @tw_snapshot_index_sql;
EXECUTE tw_snapshot_index_stmt;
DEALLOCATE PREPARE tw_snapshot_index_stmt;
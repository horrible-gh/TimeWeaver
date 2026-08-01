-- Record agent liveness and the snapshot revision currently applied by a device.
-- Dynamic DDL keeps this migration harmless when an installation already owns
-- either column.
SET @tw_last_heartbeat_exists = (
    SELECT COUNT(*)
      FROM information_schema.columns
     WHERE table_schema = DATABASE()
       AND table_name = 'devices'
       AND column_name = 'last_heartbeat_at'
);
SET @tw_last_heartbeat_sql = IF(
    @tw_last_heartbeat_exists = 0,
    'ALTER TABLE devices ADD COLUMN last_heartbeat_at DATETIME NULL DEFAULT NULL',
    'SELECT 1'
);
PREPARE tw_last_heartbeat_stmt FROM @tw_last_heartbeat_sql;
EXECUTE tw_last_heartbeat_stmt;
DEALLOCATE PREPARE tw_last_heartbeat_stmt;

SET @tw_applied_revision_exists = (
    SELECT COUNT(*)
      FROM information_schema.columns
     WHERE table_schema = DATABASE()
       AND table_name = 'devices'
       AND column_name = 'applied_revision'
);
SET @tw_applied_revision_sql = IF(
    @tw_applied_revision_exists = 0,
    'ALTER TABLE devices ADD COLUMN applied_revision VARCHAR(80) NULL DEFAULT NULL',
    'SELECT 1'
);
PREPARE tw_applied_revision_stmt FROM @tw_applied_revision_sql;
EXECUTE tw_applied_revision_stmt;
DEALLOCATE PREPARE tw_applied_revision_stmt;
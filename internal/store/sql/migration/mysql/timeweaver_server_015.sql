-- Opt-in only. Existing tasks stay non-recursive. The guard also makes a
-- retry after DDL succeeded but migration-history persistence failed safe.
SET @tw015_housekeep_sql = (
    SELECT IF(COUNT(*) = 0,
        'ALTER TABLE task_detail ADD COLUMN house_keep_recursive BOOLEAN NOT NULL DEFAULT FALSE',
        'SELECT 1')
    FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'task_detail'
      AND COLUMN_NAME = 'house_keep_recursive'
);
PREPARE tw015_housekeep_stmt FROM @tw015_housekeep_sql;
EXECUTE tw015_housekeep_stmt;
DEALLOCATE PREPARE tw015_housekeep_stmt;

SET @tw015_capabilities_sql = (
    SELECT IF(COUNT(*) = 0,
        'ALTER TABLE devices ADD COLUMN agent_capabilities VARCHAR(255) NOT NULL DEFAULT ''''',
        'SELECT 1')
    FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'devices'
      AND COLUMN_NAME = 'agent_capabilities'
);
PREPARE tw015_capabilities_stmt FROM @tw015_capabilities_sql;
EXECUTE tw015_capabilities_stmt;
DEALLOCATE PREPARE tw015_capabilities_stmt;

-- Extend execution history with retry and manual-run identity columns.
SET @tw_attempt_exists = (
    SELECT COUNT(*) FROM information_schema.columns
     WHERE table_schema = DATABASE() AND table_name = 'execution_log'
       AND column_name = 'attempt'
);
SET @tw_attempt_sql = IF(
    @tw_attempt_exists = 0,
    'ALTER TABLE execution_log ADD COLUMN attempt INT NOT NULL DEFAULT 1 AFTER detail_id',
    'SELECT 1'
);
PREPARE tw_attempt_stmt FROM @tw_attempt_sql;
EXECUTE tw_attempt_stmt;
DEALLOCATE PREPARE tw_attempt_stmt;

SET @tw_manual_id_exists = (
    SELECT COUNT(*) FROM information_schema.columns
     WHERE table_schema = DATABASE() AND table_name = 'execution_log'
       AND column_name = 'manual_id'
);
SET @tw_manual_id_sql = IF(
    @tw_manual_id_exists = 0,
    'ALTER TABLE execution_log ADD COLUMN manual_id INT NULL DEFAULT NULL AFTER attempt',
    'SELECT 1'
);
PREPARE tw_manual_id_stmt FROM @tw_manual_id_sql;
EXECUTE tw_manual_id_stmt;
DEALLOCATE PREPARE tw_manual_id_stmt;
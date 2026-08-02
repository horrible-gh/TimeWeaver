-- Fail-closed preflight for the execution result idempotency key.
-- Existing successful installations skip every persistent operation.
SET @tw_result_uq_exists = (
    SELECT COUNT(*) FROM information_schema.statistics
     WHERE table_schema = DATABASE() AND table_name = 'execution_log'
       AND index_name = 'uq_execution_log_idempotency'
);

SET @tw_repair_log_sql = IF(
    @tw_result_uq_exists = 0,
    'CREATE TABLE IF NOT EXISTS execution_log_attempt_repair_log (
        repair_id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
        migration_name VARCHAR(128) NOT NULL,
        source_execution_id BIGINT NOT NULL,
        execution_grp_id VARBINARY(64) NOT NULL,
        detail_id VARBINARY(64) NOT NULL,
        old_attempt INT NOT NULL,
        new_attempt INT NOT NULL,
        repaired_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
        PRIMARY KEY (repair_id),
        KEY idx_execution_log_attempt_repair_source (source_execution_id)
    ) ENGINE=InnoDB COMMENT=''Insert-only audit of deterministic attempt repairs by timeweaver_server_011.sql''',
    'SELECT 1'
);
PREPARE tw_repair_log_stmt FROM @tw_repair_log_sql;
EXECUTE tw_repair_log_stmt;
DEALLOCATE PREPARE tw_repair_log_stmt;

SET @tw_quarantine_table_sql = IF(
    @tw_result_uq_exists = 0,
    'CREATE TABLE IF NOT EXISTS execution_log_quarantine (
        source_execution_id BIGINT NOT NULL,
        execution_grp_id VARBINARY(64) NOT NULL,
        schedule_id BIGINT NOT NULL,
        detail_id VARBINARY(64) NULL,
        detail_id_hex VARCHAR(128) NULL,
        attempt INT NOT NULL,
        manual_id BIGINT NULL,
        start_time DATETIME NOT NULL,
        end_time DATETIME NULL,
        result_code INT NOT NULL,
        result_message LONGTEXT NULL,
        environment_info LONGTEXT NULL,
        source_detail_data_type VARCHAR(64) NOT NULL,
        quarantine_reason VARCHAR(255) NOT NULL,
        quarantined_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
        PRIMARY KEY (source_execution_id)
    ) ENGINE=InnoDB COMMENT=''Original execution rows retained here for manual UUID mapping repair; source rows are never deleted''',
    'SELECT 1'
);
PREPARE tw_quarantine_table_stmt FROM @tw_quarantine_table_sql;
EXECUTE tw_quarantine_table_stmt;
DEALLOCATE PREPARE tw_quarantine_table_stmt;

SET @tw_detail_data_type = (
    SELECT LOWER(data_type) FROM information_schema.columns
     WHERE table_schema = DATABASE() AND table_name = 'execution_log'
       AND column_name = 'detail_id'
     LIMIT 1
);
SET @tw_invalid_detail_count = IF(
    @tw_result_uq_exists = 0,
    (
        SELECT COUNT(*)
          FROM execution_log e
          LEFT JOIN schedule_detail d ON d.detail_id = e.detail_id
         WHERE COALESCE(@tw_detail_data_type, '') NOT IN ('binary', 'varbinary')
            OR e.detail_id IS NULL
            OR OCTET_LENGTH(e.detail_id) <> 16
            OR e.detail_id = UNHEX(REPEAT('00', 16))
            OR d.detail_id IS NULL
    ),
    0
);

SET @tw_quarantine_insert_sql = IF(
    @tw_result_uq_exists = 0,
    'INSERT IGNORE INTO execution_log_quarantine (
        source_execution_id, execution_grp_id, schedule_id,
        detail_id, detail_id_hex, attempt, manual_id,
        start_time, end_time, result_code, result_message, environment_info,
        source_detail_data_type, quarantine_reason
    )
    SELECT e.execution_id, e.execution_grp_id, e.schedule_id,
           e.detail_id, HEX(e.detail_id), e.attempt, e.manual_id,
           e.start_time, e.end_time, e.result_code,
           e.result_message, e.environment_info,
           COALESCE(@tw_detail_data_type, ''missing''),
           CASE
               WHEN COALESCE(@tw_detail_data_type, '''') NOT IN (''binary'', ''varbinary'')
                   THEN CONCAT(''legacy non-UUID detail_id column type: '', COALESCE(@tw_detail_data_type, ''missing''))
               WHEN e.detail_id IS NULL THEN ''NULL detail_id cannot map to a UUID''
               WHEN OCTET_LENGTH(e.detail_id) <> 16 THEN ''detail_id is not 16 UUID bytes''
               WHEN e.detail_id = UNHEX(REPEAT(''00'', 16)) THEN ''all-zero detail_id is not a mapped UUID''
               ELSE ''detail_id has no schedule_detail UUID mapping''
           END
      FROM execution_log e
      LEFT JOIN schedule_detail d ON d.detail_id = e.detail_id
     WHERE COALESCE(@tw_detail_data_type, '''') NOT IN (''binary'', ''varbinary'')
        OR e.detail_id IS NULL
        OR OCTET_LENGTH(e.detail_id) <> 16
        OR e.detail_id = UNHEX(REPEAT(''00'', 16))
        OR d.detail_id IS NULL',
    'SELECT 1'
);
PREPARE tw_quarantine_insert_stmt FROM @tw_quarantine_insert_sql;
EXECUTE tw_quarantine_insert_stmt;
DEALLOCATE PREPARE tw_quarantine_insert_stmt;

SET @tw_quarantine_commit_sql = IF(
    @tw_result_uq_exists = 0 AND @tw_invalid_detail_count > 0,
    'COMMIT',
    'SELECT 1'
);
PREPARE tw_quarantine_commit_stmt FROM @tw_quarantine_commit_sql;
EXECUTE tw_quarantine_commit_stmt;
DEALLOCATE PREPARE tw_quarantine_commit_stmt;

SET @tw_invalid_detail_abort_sql = IF(
    @tw_invalid_detail_count > 0,
    'SELECT tw011_invalid_detail_run_scripts_diagnose_execution_log_orphans FROM execution_log LIMIT 1',
    'SELECT 1'
);
PREPARE tw_invalid_detail_abort_stmt FROM @tw_invalid_detail_abort_sql;
EXECUTE tw_invalid_detail_abort_stmt;
DEALLOCATE PREPARE tw_invalid_detail_abort_stmt;

SET @tw_repair_temp_drop_sql = IF(
    @tw_result_uq_exists = 0,
    'DROP TEMPORARY TABLE IF EXISTS tw_execution_log_attempt_repair',
    'SELECT 1'
);
PREPARE tw_repair_temp_drop_stmt FROM @tw_repair_temp_drop_sql;
EXECUTE tw_repair_temp_drop_stmt;
DEALLOCATE PREPARE tw_repair_temp_drop_stmt;

SET @tw_repair_temp_sql = IF(
    @tw_result_uq_exists = 0,
    'CREATE TEMPORARY TABLE tw_execution_log_attempt_repair ENGINE=InnoDB AS
     SELECT e.execution_id, e.execution_grp_id, e.detail_id,
            e.attempt AS old_attempt,
            ROW_NUMBER() OVER (
                PARTITION BY e.execution_grp_id, e.detail_id
                ORDER BY e.execution_id
            ) AS new_attempt
       FROM execution_log e
       JOIN (
           SELECT execution_grp_id, detail_id
             FROM execution_log
            GROUP BY execution_grp_id, detail_id
           HAVING COUNT(*) > COUNT(DISTINCT attempt)
       ) duplicated
         ON duplicated.execution_grp_id = e.execution_grp_id
        AND duplicated.detail_id <=> e.detail_id',
    'SELECT 1'
);
PREPARE tw_repair_temp_stmt FROM @tw_repair_temp_sql;
EXECUTE tw_repair_temp_stmt;
DEALLOCATE PREPARE tw_repair_temp_stmt;

SET @tw_repair_audit_sql = IF(
    @tw_result_uq_exists = 0,
    'INSERT INTO execution_log_attempt_repair_log (
        migration_name, source_execution_id, execution_grp_id,
        detail_id, old_attempt, new_attempt
    )
    SELECT ''timeweaver_server_011.sql'', execution_id, execution_grp_id,
           detail_id, old_attempt, new_attempt
      FROM tw_execution_log_attempt_repair
     ORDER BY execution_id',
    'SELECT 1'
);
PREPARE tw_repair_audit_stmt FROM @tw_repair_audit_sql;
EXECUTE tw_repair_audit_stmt;
DEALLOCATE PREPARE tw_repair_audit_stmt;

SET @tw_repair_update_sql = IF(
    @tw_result_uq_exists = 0,
    'UPDATE execution_log e
     JOIN tw_execution_log_attempt_repair r ON r.execution_id = e.execution_id
        SET e.attempt = r.new_attempt
      WHERE e.attempt <> r.new_attempt',
    'SELECT 1'
);
PREPARE tw_repair_update_stmt FROM @tw_repair_update_sql;
EXECUTE tw_repair_update_stmt;
DEALLOCATE PREPARE tw_repair_update_stmt;

SET @tw_duplicate_count = IF(
    @tw_result_uq_exists = 0,
    (
        SELECT COUNT(*) FROM (
            SELECT 1
              FROM execution_log
             GROUP BY execution_grp_id, detail_id, attempt
            HAVING COUNT(*) > 1
        ) remaining_duplicates
    ),
    0
);
SET @tw_duplicate_abort_sql = IF(
    @tw_duplicate_count > 0,
    'SELECT tw_migration_011_abort_duplicate_attempt_repair_failed FROM execution_log LIMIT 1',
    'SELECT 1'
);
PREPARE tw_duplicate_abort_stmt FROM @tw_duplicate_abort_sql;
EXECUTE tw_duplicate_abort_stmt;
DEALLOCATE PREPARE tw_duplicate_abort_stmt;

SET @tw_result_uq_sql = IF(
    @tw_result_uq_exists = 0,
    'ALTER TABLE execution_log ADD UNIQUE KEY uq_execution_log_idempotency (execution_grp_id, detail_id, attempt)',
    'SELECT 1'
);
PREPARE tw_result_uq_stmt FROM @tw_result_uq_sql;
EXECUTE tw_result_uq_stmt;
DEALLOCATE PREPARE tw_result_uq_stmt;

SET @tw_repair_temp_cleanup_sql = IF(
    @tw_result_uq_exists = 0,
    'DROP TEMPORARY TABLE IF EXISTS tw_execution_log_attempt_repair',
    'SELECT 1'
);
PREPARE tw_repair_temp_cleanup_stmt FROM @tw_repair_temp_cleanup_sql;
EXECUTE tw_repair_temp_cleanup_stmt;
DEALLOCATE PREPARE tw_repair_temp_cleanup_stmt;

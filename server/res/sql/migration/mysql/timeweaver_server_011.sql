-- The deployment preflight must confirm there are no duplicate
-- (execution_grp_id, detail_id, attempt) tuples before this UNIQUE is added.
SET @tw_result_uq_exists = (
    SELECT COUNT(*) FROM information_schema.statistics
     WHERE table_schema = DATABASE() AND table_name = 'execution_log'
       AND index_name = 'uq_execution_log_idempotency'
);
SET @tw_result_uq_sql = IF(
    @tw_result_uq_exists = 0,
    'ALTER TABLE execution_log ADD UNIQUE KEY uq_execution_log_idempotency (execution_grp_id, detail_id, attempt)',
    'SELECT 1'
);
PREPARE tw_result_uq_stmt FROM @tw_result_uq_sql;
EXECUTE tw_result_uq_stmt;
DEALLOCATE PREPARE tw_result_uq_stmt;
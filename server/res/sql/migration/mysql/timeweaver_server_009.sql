-- Add short-lived manual-run lease columns without duplicating them on replay.
SET @tw_claim_token_exists = (
    SELECT COUNT(*) FROM information_schema.columns
     WHERE table_schema = DATABASE() AND table_name = 'manual_execution'
       AND column_name = 'claim_token'
);
SET @tw_claim_token_sql = IF(
    @tw_claim_token_exists = 0,
    'ALTER TABLE manual_execution ADD COLUMN claim_token CHAR(64) NULL DEFAULT NULL AFTER status',
    'SELECT 1'
);
PREPARE tw_claim_token_stmt FROM @tw_claim_token_sql;
EXECUTE tw_claim_token_stmt;
DEALLOCATE PREPARE tw_claim_token_stmt;

SET @tw_claim_expires_exists = (
    SELECT COUNT(*) FROM information_schema.columns
     WHERE table_schema = DATABASE() AND table_name = 'manual_execution'
       AND column_name = 'claim_expires_at'
);
SET @tw_claim_expires_sql = IF(
    @tw_claim_expires_exists = 0,
    'ALTER TABLE manual_execution ADD COLUMN claim_expires_at DATETIME NULL DEFAULT NULL AFTER claim_token',
    'SELECT 1'
);
PREPARE tw_claim_expires_stmt FROM @tw_claim_expires_sql;
EXECUTE tw_claim_expires_stmt;
DEALLOCATE PREPARE tw_claim_expires_stmt;
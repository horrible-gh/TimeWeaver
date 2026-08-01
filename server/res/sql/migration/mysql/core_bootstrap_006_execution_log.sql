-- Reproduce the cumulative agent execution_log schema before server conversion.
CREATE TABLE IF NOT EXISTS execution_log (
    execution_id INT AUTO_INCREMENT,
    execution_grp_id BINARY(16) NOT NULL,
    schedule_id INT NOT NULL,
    detail_id INT NOT NULL,
    start_time DATETIME NOT NULL,
    end_time DATETIME,
    result_code INT NOT NULL,
    result_message TEXT,
    environment_info TEXT,
    PRIMARY KEY (execution_grp_id, execution_id),
    UNIQUE KEY execution_id (execution_id),
    KEY idx_execution_log_001 (schedule_id, detail_id),
    KEY idx_execution_log_002 (schedule_id, execution_grp_id, start_time),
    KEY idx_execution_log_003 (result_code, start_time)
);
CREATE INDEX idx_execution_log_002 ON execution_log(schedule_id, execution_grp_id, start_time);
CREATE INDEX idx_execution_log_003 ON execution_log(result_code, start_time);

-- Execution Log
CREATE TABLE execution_log (
    execution_id INT AUTO_INCREMENT PRIMARY KEY, -- Unique identifier for execution log
    schedule_id INT NOT NULL,                   -- Foreign key to the group table
    detail_id INT NOT NULL,                     -- Foreign key to the detail table
    start_time DATETIME NOT NULL,               -- Start time of the execution
    end_time DATETIME,                          -- End time of the execution
    result_code INT NOT NULL,                   -- Result code: 0 = success, 1 = failure
    result_message TEXT,                        -- Result message or log details
    environment_info TEXT                      -- Hostname, IP, or other environment info
    -- , FOREIGN KEY (schedule_id) REFERENCES schedule_group(schedule_id) ON DELETE CASCADE
    -- , FOREIGN KEY (detail_id) REFERENCES schedule_detail(detail_id) ON DELETE CASCADE
);

CREATE INDEX idx_execution_log_001 ON execution_log(schedule_id, detail_id);

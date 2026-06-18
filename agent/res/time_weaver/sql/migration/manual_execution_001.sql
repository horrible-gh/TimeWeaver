CREATE TABLE manual_execution (
    manual_id INT AUTO_INCREMENT PRIMARY KEY
    , schedule_id INT
    , detail_id binary(16)
    , is_immediate BOOLEAN DEFAULT FALSE        -- Indicates if the task is immediate
    , schedule_datetime DATETIME DEFAULT CURRENT_TIMESTAMP
    , status ENUM('inactive', 'active', 'wait', 'processing', 'success', 'failed') DEFAULT 'active'
    , creator VARCHAR(255) DEFAULT NULL
    , created_at DATETIME DEFAULT CURRENT_TIMESTAMP NOT NULL
    , modifier VARCHAR(255) DEFAULT NULL
    , modified_at DATETIME DEFAULT NULL
);

CREATE INDEX idx_manual_execution_001 ON manual_execution(schedule_id, detail_id);
CREATE INDEX idx_manual_execution_002 ON manual_execution(detail_id);

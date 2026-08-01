-- Reproduce the cumulative agent manual_execution schema.
CREATE TABLE IF NOT EXISTS manual_execution (
    manual_id INT AUTO_INCREMENT PRIMARY KEY,
    detail_id BINARY(16),
    is_immediate BOOLEAN DEFAULT FALSE,
    schedule_datetime DATETIME DEFAULT CURRENT_TIMESTAMP,
    status ENUM('inactive','active','wait','processing','done','failed') DEFAULT 'active',
    creator VARCHAR(255) DEFAULT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    modifier VARCHAR(255) DEFAULT NULL,
    modified_at DATETIME DEFAULT NULL,
    KEY idx_manual_execution_002 (detail_id)
);
-- Reproduce the input shape expected by timeweaver_server_001.sql.
-- The agent later dropped is_immediate, but the server migration selects it from
-- schedule_detail_bak and therefore requires the earlier agent column here.
CREATE TABLE IF NOT EXISTS schedule_detail (
    detail_id INT AUTO_INCREMENT PRIMARY KEY,
    schedule_name VARCHAR(255),
    schedule_id INT NOT NULL,
    year VARCHAR(4) DEFAULT '*',
    month VARCHAR(4) DEFAULT '*',
    day_of_week VARCHAR(30) DEFAULT '*',
    day VARCHAR(4) DEFAULT '*',
    hour VARCHAR(4) DEFAULT '*',
    minute VARCHAR(4) DEFAULT '*',
    second VARCHAR(4) DEFAULT '*',
    is_immediate BOOLEAN DEFAULT FALSE,
    is_error_stop BOOLEAN DEFAULT TRUE,
    sequence INT NOT NULL DEFAULT 0,
    retry_count INT DEFAULT 0,
    status ENUM('active','inactive','error') DEFAULT 'active',
    creator VARCHAR(255) DEFAULT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    modifier VARCHAR(255) DEFAULT NULL,
    modified_at DATETIME DEFAULT NULL,
    KEY idx_schedule_detail_001 (schedule_id)
);
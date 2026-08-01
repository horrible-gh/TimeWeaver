-- Reproduce the cumulative agent schedule_group schema.
CREATE TABLE IF NOT EXISTS schedule_group (
    group_id INT NOT NULL DEFAULT 0,
    schedule_id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    year VARCHAR(4) DEFAULT '*',
    month VARCHAR(4) DEFAULT '*',
    day_of_week VARCHAR(30) DEFAULT '*',
    day VARCHAR(4) DEFAULT '*',
    hour VARCHAR(4) DEFAULT '*',
    minute VARCHAR(4) DEFAULT '*',
    second VARCHAR(4) DEFAULT '*',
    is_error_stop BOOLEAN DEFAULT TRUE,
    status ENUM('active','inactive','error') DEFAULT 'active',
    target_device INT NOT NULL DEFAULT -1,
    creator VARCHAR(255) DEFAULT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    modifier VARCHAR(255) DEFAULT NULL,
    modified_at DATETIME DEFAULT NULL
);
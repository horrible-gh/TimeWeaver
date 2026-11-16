-- Reproduce the agent-owned input schema before server migrations transform it.
CREATE TABLE IF NOT EXISTS devices (
    group_id INT NOT NULL DEFAULT 0,
    device_id INT AUTO_INCREMENT PRIMARY KEY,
    device_name VARCHAR(255) NOT NULL UNIQUE,
    status ENUM('active','inactive') DEFAULT 'active',
    version VARCHAR(50) DEFAULT NULL,
    creator VARCHAR(255) DEFAULT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    modifier VARCHAR(255) DEFAULT NULL,
    modified_at DATETIME DEFAULT NULL,
    last_login_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

INSERT IGNORE INTO devices(device_id, device_name) VALUES (-1, '%');
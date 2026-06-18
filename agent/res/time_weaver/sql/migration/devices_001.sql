CREATE TABLE devices (
    group_id INT DEFAULT '0' NOT NULL
    , device_id INT AUTO_INCREMENT PRIMARY KEY
    , device_name VARCHAR(255) NOT NULL UNIQUE
    , status ENUM('active', 'inactive') DEFAULT 'active' -- Status: active or inactive
    , creator VARCHAR(255) DEFAULT NULL
    , created_at DATETIME DEFAULT CURRENT_TIMESTAMP NOT NULL
    , modifier VARCHAR(255) DEFAULT NULL
    , modified_at DATETIME DEFAULT NULL
);

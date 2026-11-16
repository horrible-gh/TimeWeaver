CREATE TABLE groups (
    group_id INT AUTO_INCREMENT PRIMARY KEY
    , group_name VARCHAR(255) NOT NULL
    , status ENUM('active', 'inactive') DEFAULT 'active' -- Status: active or inactive
    , creator VARCHAR(255) DEFAULT NULL
    , created_at DATETIME DEFAULT CURRENT_TIMESTAMP NOT NULL
    , modifier VARCHAR(255) DEFAULT NULL
    , modified_at DATETIME DEFAULT NULL

)

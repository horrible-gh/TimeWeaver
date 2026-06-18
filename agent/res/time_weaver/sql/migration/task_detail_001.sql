CREATE TABLE task_detail (
    detail_id INT NOT NULL PRIMARY KEY
    , command TEXT
    , task_type ENUM('command', 'archive', 'copy', 'housekeep') DEFAULT 'command'
    , archive_type ENUM('zip') DEFAULT NULL
    , source_path TEXT
    , error_on_missing_source BOOLEAN DEFAULT TRUE
    , destination_path TEXT
    , date_format VARCHAR(50) DEFAULT '%Y%m%d'
    , target_date_format VARCHAR(50)
    , destination_date_format VARCHAR(50)
    , house_keep_days INT
    , creator VARCHAR(255) DEFAULT NULL
    , created_at DATETIME DEFAULT CURRENT_TIMESTAMP NOT NULL
    , modifier VARCHAR(255) DEFAULT NULL
    , modified_at DATETIME DEFAULT NULL
)

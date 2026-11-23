-- Schedule Management: Group Table
CREATE TABLE schedule_group (
    group_id INT DEFAULT '0' NOT NULL,
    schedule_id INT AUTO_INCREMENT PRIMARY KEY,  -- Unique identifier for the group
    name VARCHAR(255) NOT NULL,                 -- Name of the schedule group
    year VARCHAR(4) DEFAULT '*',               -- Cron field for year
    month VARCHAR(4) DEFAULT '*',              -- Cron field for month
    day_of_week VARCHAR(30) DEFAULT '*',       -- Cron field for month
    day VARCHAR(4) DEFAULT '*',                -- Cron field for day
    hour VARCHAR(4) DEFAULT '*',               -- Cron field for hour
    minute VARCHAR(4) DEFAULT '*',             -- Cron field for minute
    second VARCHAR(4) DEFAULT '*',             -- Cron field for second
    is_manual BOOLEAN DEFAULT FALSE,        -- Indicates if the task is immediate
    is_error_stop BOOLEAN DEFAULT TRUE,
    status ENUM('active', 'inactive', 'error') DEFAULT 'active' -- Status: active or inactive
    , target_device INT DEFAULT -1 NOT NULL
    , creator VARCHAR(255) DEFAULT NULL
    , created_at DATETIME DEFAULT CURRENT_TIMESTAMP NOT NULL
    , modifier VARCHAR(255) DEFAULT NULL
    , modified_at DATETIME DEFAULT NULL
);

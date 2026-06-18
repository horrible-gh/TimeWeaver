
-- Schedule Management: Detail Table
CREATE TABLE schedule_detail (
    detail_id INT AUTO_INCREMENT PRIMARY KEY,   -- Unique identifier for the detail
    schedule_name varchar(255),
    schedule_id INT NOT NULL,                  -- Foreign key to the group table
    year VARCHAR(4) DEFAULT '*',               -- Cron field for year
    month VARCHAR(4) DEFAULT '*',              -- Cron field for month
    day_of_week VARCHAR(30) DEFAULT '*',       -- Cron field for month
    day VARCHAR(4) DEFAULT '*',                -- Cron field for day
    hour VARCHAR(4) DEFAULT '*',               -- Cron field for hour
    minute VARCHAR(4) DEFAULT '*',             -- Cron field for minute
    second VARCHAR(4) DEFAULT '*',             -- Cron field for second
    is_immediate BOOLEAN DEFAULT FALSE,        -- Indicates if the task is immediate
    is_error_stop BOOLEAN DEFAULT TRUE,
    sequence INT DEFAULT 0 NOT NULL,           -- Execution sequence within the group
    retry_count INT DEFAULT 0,                 -- Maximum retry count on failure
    status ENUM('active', 'inactive', 'error') DEFAULT 'active' -- Status: active or inactive
    -- , FOREIGN KEY (schedule_id) REFERENCES schedule_group(schedule_id) ON DELETE CASCADE
    , creator VARCHAR(255) DEFAULT NULL
    , created_at DATETIME DEFAULT CURRENT_TIMESTAMP NOT NULL
    , modifier VARCHAR(255) DEFAULT NULL
    , modified_at DATETIME DEFAULT NULL
);

CREATE INDEX idx_schedule_detail_001 ON schedule_detail(schedule_id);

-- Track only the latest active run for each schedule detail.
CREATE TABLE IF NOT EXISTS execution_running (
    schedule_id INT(11) NOT NULL,
    detail_id BINARY(16) NOT NULL,
    execution_grp_id BINARY(16) NOT NULL,
    attempt INT(11) NOT NULL,
    start_time DATETIME NOT NULL,
    PRIMARY KEY (schedule_id, detail_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
-- SQLite counterpart for deployments using the local database backend.
CREATE TABLE IF NOT EXISTS execution_running (
    schedule_id INTEGER NOT NULL,
    detail_id BLOB NOT NULL,
    execution_grp_id BLOB NOT NULL,
    attempt INTEGER NOT NULL,
    start_time TEXT NOT NULL,
    PRIMARY KEY (schedule_id, detail_id)
);
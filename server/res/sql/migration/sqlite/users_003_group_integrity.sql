-- SQLite cannot add or drop table constraints in place. Rebuild the four
-- group-owned tables so group references and per-group device names match
-- the MariaDB schema. CREATE IF NOT EXISTS also fills the legacy SQLite
-- bootstrap gap on a new local database.
PRAGMA foreign_keys = OFF;
BEGIN TRANSACTION;

CREATE TABLE IF NOT EXISTS devices (
    group_id INTEGER NOT NULL DEFAULT 0,
    device_id INTEGER PRIMARY KEY AUTOINCREMENT,
    device_name TEXT NOT NULL UNIQUE,
    status TEXT CHECK (status IN ('active', 'inactive')) DEFAULT 'active',
    version TEXT DEFAULT NULL,
    creator TEXT DEFAULT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    modifier TEXT DEFAULT NULL,
    modified_at DATETIME DEFAULT NULL,
    last_login_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_heartbeat_at DATETIME DEFAULT NULL,
    applied_revision TEXT DEFAULT NULL
);
INSERT OR IGNORE INTO devices(device_id, device_name) VALUES (-1, '%');

CREATE TABLE devices_group_integrity (
    group_id INTEGER NOT NULL DEFAULT 0,
    device_id INTEGER PRIMARY KEY AUTOINCREMENT,
    device_name TEXT NOT NULL,
    status TEXT CHECK (status IN ('active', 'inactive')) DEFAULT 'active',
    version TEXT DEFAULT NULL,
    creator TEXT DEFAULT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    modifier TEXT DEFAULT NULL,
    modified_at DATETIME DEFAULT NULL,
    last_login_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_heartbeat_at DATETIME DEFAULT NULL,
    applied_revision TEXT DEFAULT NULL,
    CONSTRAINT uq_devices_group_device_name UNIQUE (group_id, device_name),
    CONSTRAINT fk_devices_group FOREIGN KEY (group_id) REFERENCES groups(group_id)
);
INSERT INTO devices_group_integrity
SELECT group_id, device_id, device_name, status, version, creator, created_at,
       modifier, modified_at, last_login_at, last_heartbeat_at, applied_revision
  FROM devices;
DROP TABLE devices;
ALTER TABLE devices_group_integrity RENAME TO devices;

CREATE TABLE IF NOT EXISTS agent_enrollment_token (
    enrollment_id BLOB NOT NULL PRIMARY KEY,
    token_hash BLOB NOT NULL UNIQUE,
    device_name TEXT DEFAULT NULL,
    group_id INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME NOT NULL,
    used_at DATETIME DEFAULT NULL,
    used_by_device_id INTEGER DEFAULT NULL,
    revoked_at DATETIME DEFAULT NULL
);
CREATE TABLE agent_enrollment_token_group_integrity (
    enrollment_id BLOB NOT NULL PRIMARY KEY,
    token_hash BLOB NOT NULL UNIQUE,
    device_name TEXT DEFAULT NULL,
    group_id INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME NOT NULL,
    used_at DATETIME DEFAULT NULL,
    used_by_device_id INTEGER DEFAULT NULL,
    revoked_at DATETIME DEFAULT NULL,
    CONSTRAINT fk_agent_enrollment_token_group
        FOREIGN KEY (group_id) REFERENCES groups(group_id)
);
INSERT INTO agent_enrollment_token_group_integrity
SELECT enrollment_id, token_hash, device_name, group_id, created_at, expires_at,
       used_at, used_by_device_id, revoked_at
  FROM agent_enrollment_token;
DROP TABLE agent_enrollment_token;
ALTER TABLE agent_enrollment_token_group_integrity RENAME TO agent_enrollment_token;
CREATE INDEX idx_agent_enrollment_token_001
    ON agent_enrollment_token(group_id, created_at);

CREATE TABLE IF NOT EXISTS schedule_group (
    group_id INTEGER NOT NULL DEFAULT 0,
    schedule_id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    year TEXT DEFAULT '*',
    month TEXT DEFAULT '*',
    day_of_week TEXT DEFAULT '*',
    day TEXT DEFAULT '*',
    hour TEXT DEFAULT '*',
    minute TEXT DEFAULT '*',
    second TEXT DEFAULT '*',
    is_error_stop INTEGER DEFAULT 1,
    status TEXT DEFAULT 'active',
    target_device INTEGER DEFAULT 0,
    creator TEXT DEFAULT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    modifier TEXT DEFAULT NULL,
    modified_at DATETIME DEFAULT NULL
);
CREATE TABLE schedule_group_group_integrity (
    group_id INTEGER NOT NULL DEFAULT 0,
    schedule_id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    year TEXT DEFAULT '*',
    month TEXT DEFAULT '*',
    day_of_week TEXT DEFAULT '*',
    day TEXT DEFAULT '*',
    hour TEXT DEFAULT '*',
    minute TEXT DEFAULT '*',
    second TEXT DEFAULT '*',
    is_error_stop INTEGER DEFAULT 1,
    status TEXT DEFAULT 'active',
    target_device INTEGER DEFAULT 0,
    creator TEXT DEFAULT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    modifier TEXT DEFAULT NULL,
    modified_at DATETIME DEFAULT NULL,
    CONSTRAINT fk_schedule_group_group
        FOREIGN KEY (group_id) REFERENCES groups(group_id)
);
INSERT INTO schedule_group_group_integrity
SELECT group_id, schedule_id, name, year, month, day_of_week, day, hour, minute,
       second, is_error_stop, status, target_device, creator, created_at,
       modifier, modified_at
  FROM schedule_group;
DROP TABLE schedule_group;
ALTER TABLE schedule_group_group_integrity RENAME TO schedule_group;

CREATE TABLE users_group_integrity (
    group_id INTEGER NOT NULL DEFAULT 0,
    user_id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    password TEXT NOT NULL,
    email TEXT,
    role TEXT CHECK (role IN ('admin', 'user')) DEFAULT 'user',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_users_group FOREIGN KEY (group_id) REFERENCES groups(group_id)
);
INSERT INTO users_group_integrity
SELECT group_id, user_id, name, password, email, role, created_at FROM users;
DROP TABLE users;
ALTER TABLE users_group_integrity RENAME TO users;

COMMIT;
PRAGMA foreign_keys = ON;
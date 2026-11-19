-- Native Go SQLite foundation. Historical Python files remain immutable and
-- are recognized as predecessors; they are not replayed or marked executed.
CREATE TABLE groups (
    group_id INTEGER PRIMARY KEY AUTOINCREMENT,
    group_name TEXT NOT NULL,
    status TEXT DEFAULT 'active' CHECK (status IN ('active','inactive')),
    creator TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    modifier TEXT,
    modified_at DATETIME
);
CREATE TABLE users (
    group_id INTEGER NOT NULL DEFAULT 0 REFERENCES groups(group_id),
    user_id TEXT NOT NULL PRIMARY KEY,
    name TEXT NOT NULL,
    password TEXT NOT NULL,
    email TEXT,
    role TEXT DEFAULT 'user' CHECK (role IN ('admin','user')),
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE devices (
    group_id INTEGER NOT NULL DEFAULT 0 REFERENCES groups(group_id),
    device_id INTEGER PRIMARY KEY AUTOINCREMENT,
    device_name TEXT NOT NULL,
    status TEXT DEFAULT 'active' CHECK (status IN ('active','inactive')),
    version TEXT,
    creator TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    modifier TEXT,
    modified_at DATETIME,
    last_login_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_heartbeat_at DATETIME,
    applied_revision TEXT,
    agent_capabilities TEXT NOT NULL DEFAULT '',
    UNIQUE (group_id,device_name)
);
CREATE TABLE schedule_group (
    group_id INTEGER NOT NULL DEFAULT 0 REFERENCES groups(group_id),
    schedule_id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    year TEXT DEFAULT '*', month TEXT DEFAULT '*', day_of_week TEXT DEFAULT '*',
    day TEXT DEFAULT '*', hour TEXT DEFAULT '*', minute TEXT DEFAULT '*', second TEXT DEFAULT '*',
    is_error_stop INTEGER DEFAULT 1,
    status TEXT DEFAULT 'active' CHECK (status IN ('active','inactive','error','manual')),
    target_device INTEGER NOT NULL DEFAULT -1,
    creator TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    modifier TEXT,
    modified_at DATETIME
);
CREATE TABLE schedule_detail (
    detail_id BLOB NOT NULL PRIMARY KEY CHECK (typeof(detail_id)='blob' AND length(detail_id)=16),
    schedule_name TEXT,
    schedule_id INTEGER NOT NULL,
    year TEXT DEFAULT '*', month TEXT DEFAULT '*', day_of_week TEXT DEFAULT '*',
    day TEXT DEFAULT '*', hour TEXT DEFAULT '*', minute TEXT DEFAULT '*', second TEXT DEFAULT '*',
    is_immediate INTEGER DEFAULT 0,
    is_error_stop INTEGER DEFAULT 1,
    sequence INTEGER NOT NULL DEFAULT 0,
    retry_count INTEGER DEFAULT 0,
    status TEXT DEFAULT 'active' CHECK (status IN ('active','inactive','error','manual')),
    creator TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    modifier TEXT,
    modified_at DATETIME,
    deleted_at DATETIME
);
CREATE TABLE task_detail (
    detail_id BLOB NOT NULL PRIMARY KEY CHECK (typeof(detail_id)='blob' AND length(detail_id)=16),
    command TEXT,
    task_type TEXT DEFAULT 'command' CHECK (task_type IN ('command','archive','copy','housekeep')),
    archive_type TEXT CHECK (archive_type IN ('null','zip')),
    source_path TEXT,
    error_on_missing_source INTEGER DEFAULT 1,
    destination_path TEXT,
    date_format TEXT,
    target_date_format TEXT,
    destination_date_format TEXT,
    house_keep_days INTEGER,
    house_keep_recursive INTEGER NOT NULL DEFAULT 0,
    creator TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    modifier TEXT,
    modified_at DATETIME
);
CREATE TABLE manual_execution (
    manual_id INTEGER PRIMARY KEY AUTOINCREMENT,
    detail_id BLOB CHECK (detail_id IS NULL OR (typeof(detail_id)='blob' AND length(detail_id)=16)),
    is_immediate INTEGER DEFAULT 0,
    schedule_datetime DATETIME DEFAULT CURRENT_TIMESTAMP,
    status TEXT DEFAULT 'active' CHECK (status IN ('inactive','active','wait','processing','done','failed')),
    claim_token TEXT,
    claim_expires_at DATETIME,
    creator TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    modifier TEXT,
    modified_at DATETIME
);
CREATE TABLE execution_log (
    execution_id INTEGER PRIMARY KEY AUTOINCREMENT,
    execution_grp_id BLOB NOT NULL CHECK (typeof(execution_grp_id)='blob' AND length(execution_grp_id)=16),
    schedule_id INTEGER NOT NULL,
    detail_id BLOB NOT NULL CHECK (typeof(detail_id)='blob' AND length(detail_id)=16),
    attempt INTEGER NOT NULL DEFAULT 1,
    manual_id INTEGER,
    start_time DATETIME NOT NULL,
    end_time DATETIME,
    result_code INTEGER NOT NULL,
    result_message TEXT,
    environment_info TEXT,
    UNIQUE (execution_grp_id,detail_id,attempt)
);
CREATE TABLE execution_running (
    schedule_id INTEGER NOT NULL,
    detail_id BLOB NOT NULL CHECK (typeof(detail_id)='blob' AND length(detail_id)=16),
    execution_grp_id BLOB NOT NULL CHECK (typeof(execution_grp_id)='blob' AND length(execution_grp_id)=16),
    attempt INTEGER NOT NULL,
    start_time DATETIME NOT NULL,
    PRIMARY KEY (schedule_id,detail_id)
);
CREATE TABLE agent_enrollment_token (
    enrollment_id BLOB NOT NULL PRIMARY KEY CHECK (typeof(enrollment_id)='blob' AND length(enrollment_id)=16),
    token_hash BLOB NOT NULL UNIQUE CHECK (typeof(token_hash)='blob' AND length(token_hash)=32),
    device_name TEXT,
    group_id INTEGER NOT NULL DEFAULT 0 REFERENCES groups(group_id),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME NOT NULL,
    used_at DATETIME,
    used_by_device_id INTEGER,
    revoked_at DATETIME
);
CREATE TABLE agent_device_credential (
    credential_id INTEGER PRIMARY KEY AUTOINCREMENT,
    device_id INTEGER NOT NULL,
    token_hash BLOB NOT NULL UNIQUE CHECK (typeof(token_hash)='blob' AND length(token_hash)=32),
    issued_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME NOT NULL,
    revoked_at DATETIME
);
CREATE TABLE agent_event (
    event_id INTEGER PRIMARY KEY AUTOINCREMENT,
    device_id INTEGER NOT NULL,
    event_type TEXT NOT NULL CHECK (event_type IN ('startup_error','sync_error','degraded','recovered','outbox_backlog')),
    severity TEXT NOT NULL CHECK (severity IN ('info','warning','error')),
    occurred_at DATETIME NOT NULL,
    message TEXT,
    environment_info TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_schedule_group_001 ON schedule_group(target_device);
CREATE INDEX IF NOT EXISTS idx_schedule_detail_001 ON schedule_detail(schedule_id);
CREATE INDEX IF NOT EXISTS idx_manual_execution_002 ON manual_execution(detail_id);
CREATE INDEX IF NOT EXISTS idx_manual_execution_lease ON manual_execution(status,claim_expires_at,manual_id);
CREATE INDEX IF NOT EXISTS idx_execution_log_001 ON execution_log(schedule_id,detail_id);
CREATE INDEX IF NOT EXISTS idx_execution_log_002 ON execution_log(schedule_id,execution_grp_id,start_time);
CREATE INDEX IF NOT EXISTS idx_execution_log_003 ON execution_log(result_code,start_time);
CREATE INDEX IF NOT EXISTS idx_execution_log_004 ON execution_log(start_time DESC,execution_id DESC);
CREATE INDEX IF NOT EXISTS idx_agent_enrollment_token_001 ON agent_enrollment_token(group_id,created_at);
CREATE INDEX IF NOT EXISTS idx_agent_device_credential_001 ON agent_device_credential(device_id,revoked_at);
CREATE INDEX IF NOT EXISTS idx_agent_event_001 ON agent_event(device_id,occurred_at);
CREATE INDEX IF NOT EXISTS idx_agent_event_002 ON agent_event(event_type,occurred_at);

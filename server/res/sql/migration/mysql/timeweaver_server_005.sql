-- Device enrollment secrets are stored only as SHA-256 digests.
CREATE TABLE IF NOT EXISTS agent_enrollment_token (
    enrollment_id BINARY(16) NOT NULL PRIMARY KEY,
    token_hash BINARY(32) NOT NULL,
    device_name VARCHAR(255) DEFAULT NULL,
    group_id INT NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME NOT NULL,
    used_at DATETIME DEFAULT NULL,
    used_by_device_id INT DEFAULT NULL,
    revoked_at DATETIME DEFAULT NULL,
    UNIQUE KEY uq_agent_enrollment_token_hash (token_hash),
    KEY idx_agent_enrollment_token_001 (group_id, created_at)
);

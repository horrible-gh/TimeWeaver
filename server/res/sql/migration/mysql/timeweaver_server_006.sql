-- Refresh-token history supports rotation and explicit credential revocation.
CREATE TABLE IF NOT EXISTS agent_device_credential (
    credential_id INT AUTO_INCREMENT PRIMARY KEY,
    device_id INT NOT NULL,
    token_hash BINARY(32) NOT NULL,
    issued_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME NOT NULL,
    revoked_at DATETIME DEFAULT NULL,
    UNIQUE KEY uq_agent_device_credential_hash (token_hash),
    KEY idx_agent_device_credential_001 (device_id, revoked_at)
);

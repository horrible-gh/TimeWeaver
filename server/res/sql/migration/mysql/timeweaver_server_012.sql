-- Agent incidents are append-only and owned by the authenticated device.
CREATE TABLE IF NOT EXISTS agent_event (
    event_id INT AUTO_INCREMENT PRIMARY KEY,
    device_id INT NOT NULL,
    event_type ENUM('startup_error','sync_error','degraded','recovered','outbox_backlog') NOT NULL,
    severity ENUM('info','warning','error') NOT NULL,
    occurred_at DATETIME NOT NULL,
    message TEXT DEFAULT NULL,
    environment_info TEXT DEFAULT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    KEY idx_agent_event_001 (device_id, occurred_at),
    KEY idx_agent_event_002 (event_type, occurred_at)
);
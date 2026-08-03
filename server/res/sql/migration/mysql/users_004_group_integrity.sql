-- Hidden group 0 now exists before this migration, so every group reference
-- can be enforced and device names can be unique within (rather than across)
-- a group.
--
-- The device_name index is only guaranteed to exist under its automatic name
-- when core_bootstrap_001_devices.sql actually created the devices table. On
-- installations where devices already existed (agent-owned legacy schema),
-- that bootstrap migration is a no-op and this index may be named
-- differently or be absent, so drop it dynamically like the other
-- installation-dependent devices DDL in this migration path
-- (see timeweaver_server_008.sql).
SET @tw_device_name_index_exists = (
    SELECT COUNT(*)
      FROM information_schema.statistics
     WHERE table_schema = DATABASE()
       AND table_name = 'devices'
       AND index_name = 'device_name'
);
SET @tw_device_name_index_sql = IF(
    @tw_device_name_index_exists > 0,
    'ALTER TABLE devices DROP INDEX device_name',
    'SELECT 1'
);
PREPARE tw_device_name_index_stmt FROM @tw_device_name_index_sql;
EXECUTE tw_device_name_index_stmt;
DEALLOCATE PREPARE tw_device_name_index_stmt;

ALTER TABLE devices
    ADD CONSTRAINT uq_devices_group_device_name UNIQUE (group_id, device_name),
    ADD CONSTRAINT fk_devices_group
        FOREIGN KEY (group_id) REFERENCES groups(group_id);

ALTER TABLE users
    MODIFY COLUMN group_id INT NOT NULL DEFAULT 0,
    ADD CONSTRAINT fk_users_group
        FOREIGN KEY (group_id) REFERENCES groups(group_id);

ALTER TABLE agent_enrollment_token
    ADD CONSTRAINT fk_agent_enrollment_token_group
        FOREIGN KEY (group_id) REFERENCES groups(group_id);

ALTER TABLE schedule_group
    ADD CONSTRAINT fk_schedule_group_group
        FOREIGN KEY (group_id) REFERENCES groups(group_id);
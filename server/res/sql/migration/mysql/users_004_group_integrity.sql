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

-- Orphaned group_id values -- rows whose group_id has no matching row in
-- groups -- would make every ADD CONSTRAINT ... FOREIGN KEY below fail with
-- MySQL 1452. That can happen even without a bad migration: see NR0007
-- (timeweaver.server.0007.0007-NR) -- insert_device/insert_schedule accepted
-- a group_id without checking it existed, and remove_group deleted a group
-- without checking whether devices/users/agent_enrollment_token/
-- schedule_group still pointed at it -- including group 0 itself, on any
-- database where an old, unguarded remove_group already deleted it before
-- this migration and T0008's remove_group fix existed. Reassign whatever
-- is left over to the reserved group 0 ("Unknown"). groups_002.sql
-- normally creates group 0, but it is recorded in the migrations table
-- once applied and will not run again, so if group 0 was later deleted it
-- stays missing and every ADD CONSTRAINT below would keep failing with
-- 1452 against group 0 itself, the same failure this migration exists to
-- prevent. Recreate it idempotently so this migration does not depend on
-- groups_002.sql's effects still being present.
SET @tw_previous_sql_mode = @@SESSION.sql_mode;
SET SESSION sql_mode = CONCAT_WS(',', NULLIF(@tw_previous_sql_mode, ''), 'NO_AUTO_VALUE_ON_ZERO');
INSERT IGNORE INTO groups(group_id, group_name) VALUES (0, 'Unknown');
SET SESSION sql_mode = @tw_previous_sql_mode;

UPDATE devices d
    LEFT JOIN groups g ON g.group_id = d.group_id
    SET d.group_id = 0
    WHERE g.group_id IS NULL;

ALTER TABLE devices
    ADD CONSTRAINT uq_devices_group_device_name UNIQUE (group_id, device_name),
    ADD CONSTRAINT fk_devices_group
        FOREIGN KEY (group_id) REFERENCES groups(group_id);

UPDATE users u
    LEFT JOIN groups g ON g.group_id = u.group_id
    SET u.group_id = 0
    WHERE g.group_id IS NULL;

ALTER TABLE users
    MODIFY COLUMN group_id INT NOT NULL DEFAULT 0,
    ADD CONSTRAINT fk_users_group
        FOREIGN KEY (group_id) REFERENCES groups(group_id);

UPDATE agent_enrollment_token t
    LEFT JOIN groups g ON g.group_id = t.group_id
    SET t.group_id = 0
    WHERE g.group_id IS NULL;

ALTER TABLE agent_enrollment_token
    ADD CONSTRAINT fk_agent_enrollment_token_group
        FOREIGN KEY (group_id) REFERENCES groups(group_id);

UPDATE schedule_group s
    LEFT JOIN groups g ON g.group_id = s.group_id
    SET s.group_id = 0
    WHERE g.group_id IS NULL;

ALTER TABLE schedule_group
    ADD CONSTRAINT fk_schedule_group_group
        FOREIGN KEY (group_id) REFERENCES groups(group_id);
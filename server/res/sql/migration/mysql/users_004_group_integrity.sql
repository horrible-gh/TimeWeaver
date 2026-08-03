-- Hidden group 0 now exists before this migration, so every group reference
-- can be enforced and device names can be unique within (rather than across)
-- a group.
ALTER TABLE devices DROP INDEX device_name;
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
-- Opt-in only: existing tasks retain their non-recursive behavior.
ALTER TABLE task_detail
    ADD COLUMN house_keep_recursive BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE devices
    ADD COLUMN agent_capabilities TEXT NOT NULL DEFAULT '';

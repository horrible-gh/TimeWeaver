-- Existing tasks never delete directories unless the user enables the option.
ALTER TABLE task_detail ADD COLUMN house_keep_delete_empty_dirs INTEGER NOT NULL DEFAULT 0 CHECK (house_keep_delete_empty_dirs IN (0,1));

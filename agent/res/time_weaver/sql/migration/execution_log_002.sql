ALTER TABLE execution_log ADD COLUMN execution_grp_id BINARY(16) NOT NULL AFTER execution_id;
ALTER TABLE execution_log DROP PRIMARY KEY, ADD PRIMARY KEY (execution_grp_id, execution_id), ADD UNIQUE (execution_id);

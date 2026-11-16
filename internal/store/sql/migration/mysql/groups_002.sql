SET @tw_previous_sql_mode = @@SESSION.sql_mode;
SET SESSION sql_mode = CONCAT_WS(',', NULLIF(@tw_previous_sql_mode, ''), 'NO_AUTO_VALUE_ON_ZERO');
INSERT INTO groups(group_id, group_name) VALUES (0, 'Unknown');
SET SESSION sql_mode = @tw_previous_sql_mode;
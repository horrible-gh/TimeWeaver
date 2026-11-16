RENAME TABLE
  schedule_detail TO schedule_detail_bak,
  execution_log TO execution_log_bak,
  task_detail TO task_detail_bak
;

CREATE TABLE `schedule_detail` (
  `detail_id` binary(16) NOT NULL,
  `schedule_name` varchar(255) DEFAULT NULL,
  `schedule_id` int(11) NOT NULL,
  `year` varchar(4) DEFAULT '*',
  `month` varchar(4) DEFAULT '*',
  `day_of_week` varchar(30) DEFAULT '*',
  `day` varchar(4) DEFAULT '*',
  `hour` varchar(4) DEFAULT '*',
  `minute` varchar(4) DEFAULT '*',
  `second` varchar(4) DEFAULT '*',
  `is_immediate` tinyint(1) DEFAULT 0,
  `is_error_stop` tinyint(1) DEFAULT 1,
  `sequence` int(11) NOT NULL DEFAULT 0,
  `retry_count` int(11) DEFAULT 0,
  `status` enum('active','inactive','error') DEFAULT 'active',
  `creator` varchar(255) DEFAULT NULL,
  `created_at` datetime NOT NULL DEFAULT current_timestamp(),
  `modifier` varchar(255) DEFAULT NULL,
  `modified_at` datetime DEFAULT NULL,
  PRIMARY KEY (`detail_id`),
  KEY `idx_schedule_detail_001` (`schedule_id`)
) ENGINE=InnoDB AUTO_INCREMENT=23 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci
;

CREATE TABLE `task_detail` (
  `detail_id` binary(16) NOT NULL,
  `command` text DEFAULT NULL,
  `task_type` enum('command','archive','copy','housekeep') DEFAULT 'command',
  `archive_type` enum('zip') DEFAULT NULL,
  `source_path` text DEFAULT NULL,
  `error_on_missing_source` tinyint(1) DEFAULT 1,
  `destination_path` text DEFAULT NULL,
  `date_format` varchar(50) DEFAULT NULL,
  `target_date_format` varchar(50) DEFAULT NULL,
  `destination_date_format` varchar(50) DEFAULT NULL,
  `house_keep_days` int(11) DEFAULT NULL,
  `creator` varchar(255) DEFAULT NULL,
  `created_at` datetime NOT NULL DEFAULT current_timestamp(),
  `modifier` varchar(255) DEFAULT NULL,
  `modified_at` datetime DEFAULT NULL,
  PRIMARY KEY (`detail_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci
;

CREATE TABLE execution_log (
  execution_id INT(11) NOT NULL AUTO_INCREMENT,
  execution_grp_id BINARY(16) NOT NULL,
  schedule_id INT(11) NOT NULL,
  detail_id BINARY(16) NOT NULL,
  start_time DATETIME NOT NULL,
  end_time DATETIME DEFAULT NULL,
  result_code INT(11) NOT NULL,
  result_message TEXT DEFAULT NULL,
  environment_info TEXT DEFAULT NULL,
  PRIMARY KEY (execution_grp_id, execution_id),
  UNIQUE KEY execution_id (execution_id),
  KEY idx_execution_log_001 (schedule_id, detail_id),
  KEY idx_execution_log_002 (schedule_id, execution_grp_id, start_time),
  KEY idx_execution_log_003 (result_code, start_time)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci
;

CREATE TABLE detail_id_mapping (
  old_detail_id INT NOT NULL PRIMARY KEY,
  new_detail_uuid BINARY(16) NOT NULL UNIQUE,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP
)
;

INSERT INTO detail_id_mapping (old_detail_id, new_detail_uuid) VALUES
(1,  UNHEX(REPLACE('8f0d65c5-b6a4-4bb0-a2c5-f23672fc9b76', '-',''))),
(2,  UNHEX(REPLACE('1a749b4b-bad4-4ff3-a3c4-49eb3dcde0b4', '-',''))),
(3,  UNHEX(REPLACE('8be11f59-b472-4d48-bcae-49e98b00e86b', '-',''))),
(4,  UNHEX(REPLACE('a5c5c8b2-62db-4f0d-bd17-d104aa56d85d', '-',''))),
(5,  UNHEX(REPLACE('fa2c6ef9-9cf5-4e62-8504-1bd26fe00f26', '-',''))),
(6,  UNHEX(REPLACE('97fa25c3-e837-47db-8416-8594c6f1e3f3', '-',''))),
(7,  UNHEX(REPLACE('3a66aa90-90e4-4c3a-b621-f82375f0e8df', '-',''))),
(8,  UNHEX(REPLACE('1dff0f58-53f5-41df-a13f-e2dfcf8aa951', '-',''))),
(9,  UNHEX(REPLACE('cfc979e2-8614-491b-b087-dcc22fae148b', '-',''))),
(10, UNHEX(REPLACE('a95e2cf2-5e18-442b-9294-bf3dca5a5cfa', '-',''))),
(11, UNHEX(REPLACE('1efc4e4b-b3c7-4c46-8c3b-2c2b49a3fe69', '-',''))),
(12, UNHEX(REPLACE('da3b1906-73b4-42d4-84b1-83d7eb25f321', '-',''))),
(13, UNHEX(REPLACE('5d514348-8ab6-499f-800b-5887aa4ccf1d', '-',''))),
(14, UNHEX(REPLACE('c74cdcc4-d9f0-487b-9216-bff4f0a35f90', '-',''))),
(15, UNHEX(REPLACE('11075cd2-71bb-44db-aef5-020ec6f32c8d', '-',''))),
(16, UNHEX(REPLACE('3d427c38-f409-4c5f-888a-fb5d5a9f49dc', '-',''))),
(17, UNHEX(REPLACE('8c1c17ce-38d5-4718-8e30-97aa10a12f38', '-',''))),
(18, UNHEX(REPLACE('3d6f4146-fc68-44ea-bd0a-5f963d7e6832', '-',''))),
(19, UNHEX(REPLACE('40cf7e3d-97ca-4fa2-8c03-0fcbbdc30a5e', '-',''))),
(20, UNHEX(REPLACE('3cc202df-69cc-497c-87c1-65c4b1d9f71d', '-',''))),
(21, UNHEX(REPLACE('d3fe88b6-1d17-4f4d-b4fa-bbdd6d6ef4d4', '-',''))),
(22, UNHEX(REPLACE('25203899-22ae-4a76-9202-c65cde6cfa96', '-',''))),
(23, UNHEX(REPLACE('1a83e028-7a14-4eb4-a396-6e2631a3b44e', '-',''))),
(24, UNHEX(REPLACE('ee9b0be7-2646-4a8e-a7ed-40d2fcb65880', '-',''))),
(25, UNHEX(REPLACE('2d3c1df9-79f1-4c5e-9d64-62f64a0e9a32', '-',''))),
(26, UNHEX(REPLACE('d97abf6e-598f-4cf7-9829-19fc06df7c36', '-',''))),
(27, UNHEX(REPLACE('b88e4e25-41ae-4c61-931f-47a75b5291db', '-',''))),
(28, UNHEX(REPLACE('ed54d270-10f3-4f65-bc94-390e87b4a4e3', '-',''))),
(29, UNHEX(REPLACE('fa7ae6e0-415c-4c68-b8fc-cd205b7b6461', '-',''))),
(30, UNHEX(REPLACE('31d56c5f-9145-419b-b964-1f7d86ce1729', '-',''))),
(31, UNHEX(REPLACE('1d2195e5-f190-4a2f-89e0-f87196a8a2f3', '-',''))),
(32, UNHEX(REPLACE('0a8c02ae-645b-4aa1-a75d-89bb55e3d9e8', '-',''))),
(33, UNHEX(REPLACE('97dd88a7-5954-4cc2-b4ae-194edc84e7cf', '-',''))),
(34, UNHEX(REPLACE('9b037f0e-1fd8-4f0e-b29e-51fcd4c46cde', '-',''))),
(35, UNHEX(REPLACE('e8ec3f42-06df-4e4f-84b1-cb2e4e8c648a', '-',''))),
(36, UNHEX(REPLACE('6366109f-1e7f-4c14-9364-748437003b49', '-',''))),
(37, UNHEX(REPLACE('a2c8c41d-7eb3-422f-a7c0-4175f21e8f57', '-',''))),
(38, UNHEX(REPLACE('51d0d9e7-48f0-4fe1-b94d-3501dc8cf456', '-',''))),
(39, UNHEX(REPLACE('dbbca314-1c35-46cd-b2e3-eabbb0d13556', '-',''))),
(40, UNHEX(REPLACE('18d91b43-ff55-48f2-a2f5-cbf0b9fbd7a5', '-',''))),
(41, UNHEX(REPLACE('75b3bc34-54a0-4656-a83a-010ccf1e771f', '-',''))),
(42, UNHEX(REPLACE('06f914f4-70db-4d4f-9006-0f7b17d06cf5', '-',''))),
(43, UNHEX(REPLACE('e5d48e67-3f61-48f0-8f82-6904fc5b76cc', '-',''))),
(44, UNHEX(REPLACE('f3090d47-5a7c-4f63-91ed-fbb962ca1b3b', '-',''))),
(45, UNHEX(REPLACE('d6f935bd-c39c-41cf-968a-1ad557ca0b9c', '-',''))),
(46, UNHEX(REPLACE('d2db6a34-6237-4865-a0fd-b08aa41adccf', '-',''))),
(47, UNHEX(REPLACE('508b0f5a-6f83-48ab-a799-66bb6137585e', '-',''))),
(48, UNHEX(REPLACE('47978082-771f-4e4f-a2f6-0d69d6cb36a2', '-',''))),
(49, UNHEX(REPLACE('baaa03b7-92de-4f30-96cd-1b12f8de8e46', '-',''))),
(50, UNHEX(REPLACE('b24dc2e5-8a2f-4063-8646-9849bfcddf17', '-','')));


INSERT INTO schedule_detail (
  detail_id, schedule_name, schedule_id,
  year, month, day_of_week, day, hour, minute, second,
  is_immediate, is_error_stop, sequence, retry_count,
  status, creator, created_at, modifier, modified_at
)
SELECT
  m.new_detail_uuid, s.schedule_name, s.schedule_id,
  s.year, s.month, s.day_of_week, s.day, s.hour, s.minute, s.second,
  s.is_immediate, s.is_error_stop, s.sequence, s.retry_count,
  s.status, s.creator, s.created_at, s.modifier, s.modified_at
FROM schedule_detail_bak s
JOIN detail_id_mapping m ON s.detail_id = m.old_detail_id
;

INSERT INTO task_detail (
  detail_id, command, task_type, archive_type, source_path,
  error_on_missing_source, destination_path,
  date_format, target_date_format, destination_date_format,
  house_keep_days, creator, created_at, modifier, modified_at
)
SELECT
  m.new_detail_uuid, t.command, t.task_type, t.archive_type, t.source_path,
  t.error_on_missing_source, t.destination_path,
  t.date_format, t.target_date_format, t.destination_date_format,
  t.house_keep_days, t.creator, t.created_at, t.modifier, t.modified_at
FROM task_detail_bak t
JOIN detail_id_mapping m ON t.detail_id = m.old_detail_id
;

INSERT INTO execution_log (
  execution_id, execution_grp_id, schedule_id, detail_id,
  start_time, end_time, result_code, result_message, environment_info
)
SELECT
  e.execution_id, e.execution_grp_id, e.schedule_id, m.new_detail_uuid,
  e.start_time, e.end_time, e.result_code, e.result_message, e.environment_info
FROM execution_log_bak e
JOIN detail_id_mapping m ON e.detail_id = m.old_detail_id
;


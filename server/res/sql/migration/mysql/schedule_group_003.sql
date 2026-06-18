ALTER TABLE schedule_group MODIFY COLUMN status enum('active','inactive','error', 'manual') DEFAULT 'active'

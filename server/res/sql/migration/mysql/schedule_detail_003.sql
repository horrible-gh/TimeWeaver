ALTER TABLE schedule_detail MODIFY COLUMN status enum('active','inactive','error', 'manual') DEFAULT 'active'

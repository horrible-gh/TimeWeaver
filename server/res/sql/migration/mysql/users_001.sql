-- User Management
CREATE TABLE users (
    user_id VARCHAR(50) PRIMARY KEY,     -- Unique identifier for the user
    name VARCHAR(255) NOT NULL,                 -- Name of the user
    password VARCHAR(60) NOT NULL,
    email VARCHAR(100),
    role ENUM('admin', 'user') DEFAULT 'user',  -- Role: admin or user
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP -- Account creation timestamp
);

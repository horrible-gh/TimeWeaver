-- Widen the password column for passlib PBKDF2 hashes and provision the initial
-- administrator on a blank database. Existing installations retain their user
-- metadata while receiving the same one-time password migration as the legacy file.
ALTER TABLE users MODIFY password VARCHAR(255);

INSERT INTO users (group_id, user_id, name, password, email, role)
VALUES (
    0,
    'timeweaver',
    'TimeWeaver Administrator',
    '$pbkdf2-sha256$29000$UGptrTWGsPbeO2csJaTUWg$L0UlEAeIEwTtylnWYs2Jx/DgDdB174/k7ba7VeDj3xg',
    NULL,
    'admin'
)
ON DUPLICATE KEY UPDATE password = VALUES(password);
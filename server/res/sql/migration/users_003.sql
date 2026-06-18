ALTER TABLE users MODIFY password VARCHAR(255);
UPDATE users SET password = '$pbkdf2-sha256$29000$UGptrTWGsPbeO2csJaTUWg$L0UlEAeIEwTtylnWYs2Jx/DgDdB174/k7ba7VeDj3xg' WHERE user_id = 'timeweaver';

WITH device_status AS (
    SELECT 'active' AS status, 1 AS seq
    UNION ALL
    SELECT 'error' AS status, 2 AS seq
    UNION ALL
    SELECT 'inactive' AS status, 3 AS seq
),
device_status_conv AS (
    SELECT
        CASE
            WHEN status = 'inactive' THEN status
            WHEN last_heartbeat_at IS NOT NULL THEN
                CASE
                    WHEN COALESCE(last_heartbeat_at, last_login_at) <= DATE_SUB(UTC_TIMESTAMP(), INTERVAL 5 MINUTE) THEN 'error'
                    ELSE status
                END
            WHEN COALESCE(last_heartbeat_at, last_login_at) <= DATE_SUB(NOW(), INTERVAL 1 DAY) THEN 'error'
            ELSE status
        END AS status,
        device_id
    FROM devices
    WHERE device_id > 0
),
device_status_count AS (
    SELECT status, COUNT(*) cnt
    FROM device_status_conv
    GROUP BY status
)
SELECT
    COALESCE(MAX(CASE WHEN ds.status = 'active' THEN dsc.cnt END), 0) AS active_count,
    COALESCE(MAX(CASE WHEN ds.status = 'error' THEN dsc.cnt END), 0) AS error_count,
    COALESCE(MAX(CASE WHEN ds.status = 'inactive' THEN dsc.cnt END), 0) AS inactive_count
FROM device_status AS ds
LEFT JOIN device_status_count AS dsc ON ds.status = dsc.status
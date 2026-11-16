WITH latest_execution_log AS (
    SELECT *
    FROM (
        SELECT
            el.*,
            ROW_NUMBER() OVER (PARTITION BY el.detail_id ORDER BY el.start_time DESC) AS rn
        FROM execution_log el
        WHERE el.start_time >= DATE_SUB(UTC_TIMESTAMP(), INTERVAL 1 DAY)
    ) ranked
    WHERE rn = 1
),
task_status AS (
    SELECT
        sd.schedule_id,
        sd.detail_id,
        sd.status,
        el.result_code,
        el.start_time,
        el.end_time,
        CASE
            WHEN er.detail_id IS NOT NULL THEN 'in_progress'  -- active run wins over history
            WHEN el.result_code = 0 THEN 'completed'  -- completed
            WHEN el.result_code IS NULL THEN 'pending'  -- pending
            ELSE 'error'  -- error
        END AS task_state
    FROM schedule_detail sd
    JOIN schedule_group sg ON sg.schedule_id = sd.schedule_id
    JOIN devices d ON sg.target_device = d.device_id
    LEFT JOIN latest_execution_log el
        ON el.detail_id = sd.detail_id
    LEFT JOIN execution_running er
        ON er.schedule_id = sd.schedule_id
        AND er.detail_id = sd.detail_id
        -- Ignore stale markers left behind if an agent dies mid-task.
        AND er.start_time >= DATE_SUB(UTC_TIMESTAMP(), INTERVAL 1 HOUR)
    WHERE sd.deleted_at IS NULL
      AND sd.status IN ('active', 'error')
      AND sg.status NOT IN ('inactive', 'manual')
      AND COALESCE(d.last_heartbeat_at, d.last_login_at) >= DATE_SUB(UTC_TIMESTAMP(), INTERVAL 1 DAY)
      AND d.status NOT IN ('inactive', 'manual')
),
all_states AS (
    -- Explicitly include every status to prevent zero counts
    SELECT 'pending' AS task_state
    UNION ALL
    SELECT 'in_progress'
    UNION ALL
    SELECT 'completed'
    UNION ALL
    SELECT 'error'
),
task_counts AS (
    SELECT
        a.task_state,
        COALESCE(COUNT(t.task_state), 0) AS task_count
    FROM all_states a
    LEFT JOIN task_status t ON a.task_state = t.task_state
    GROUP BY a.task_state
)
SELECT
    MAX(CASE WHEN task_state = 'pending' THEN task_count ELSE 0 END) AS pending_count,
    MAX(CASE WHEN task_state = 'in_progress' THEN task_count ELSE 0 END) AS in_progress_count,
    MAX(CASE WHEN task_state = 'completed' THEN task_count ELSE 0 END) AS completed_count,
    MAX(CASE WHEN task_state = 'error' THEN task_count ELSE 0 END) AS error_count
FROM task_counts

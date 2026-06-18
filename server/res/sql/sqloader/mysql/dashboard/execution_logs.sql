WITH schedule_histories AS (
    SELECT
        td.detail_id
        , sg.name sg_name
        , sd.schedule_name
        , td.task_type
        , td.command
        , td.source_path
        , td.destination_path
    FROM schedule_group sg
    JOIN schedule_detail sd
        ON sg.schedule_id = sd.schedule_id
    JOIN task_detail td
        ON td.detail_id = sd.detail_id
)
SELECT
    el.execution_id
    , CASE WHEN sh.sg_name IS NOT NULL THEN sh.sg_name ELSE sys.name END AS sg_name
    , CASE WHEN sh.schedule_name IS NOT NULL THEN sh.schedule_name ELSE JSON_VALUE(environment_info, '$.device_name') END AS schedule_name
    , el.start_time
    , el.end_time
    , el.result_code
    , el.result_message
    , sh.task_type
    , sh.command
    , sh.source_path
    , sh.destination_path
FROM execution_log el
LEFT JOIN schedule_histories sh
    ON sh.detail_id = el.detail_id
LEFT JOIN (
    SELECT
        -1 id,
        'System Error' name
    ) sys
    ON el.schedule_id = sys.id
WHERE start_time >= %s AND start_time <= %s
ORDER BY el.execution_id DESC

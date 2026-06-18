WITH execution_log AS (
    SELECT el.detail_id,
           max(el.start_time) lastest_start_time
      FROM execution_log el
    GROUP BY el.detail_id
)
SELECT
    sg.name schedule_group_name
    , LOWER(
        CONCAT_WS( 
            '-'
            , HEX(SUBSTR(sd.detail_id, 1, 4))
            , HEX(SUBSTR(sd.detail_id, 5, 2))
            , HEX(SUBSTR(sd.detail_id, 7, 2))
            , HEX(SUBSTR(sd.detail_id, 9, 2))
            , HEX(SUBSTR(sd.detail_id, 11))
        )
    ) AS detail_id
    , sd.schedule_name task_name
    , el.lastest_start_time
    , sd.schedule_id
    , sd.year
    , sd.month
    , sd.day
    , sd.day_of_week
    , sd.hour
    , sd.minute
    , sd.second
    , sd.is_error_stop
    , sd.sequence
    , sd.retry_count
    , sd.status
    , sd.creator
    , sd.created_at
    , sd.modifier
    , sd.modified_at
    , td.task_type
    , td.command
    , td.archive_type
    , td.source_path
    , td.error_on_missing_source
    , td.destination_path
    , td.date_format
    , td.target_date_format
    , td.destination_date_format
    , td.house_keep_days
FROM
    schedule_detail sd
JOIN schedule_group sg
    ON sd.schedule_id = sg.schedule_id
LEFT JOIN task_detail td 
    ON sd.detail_id = td.detail_id
LEFT JOIN execution_log el
    ON el.detail_id = td.detail_id
ORDER BY
    sd.schedule_id
    , sd.sequence

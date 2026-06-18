WITH base_datas AS (
    SELECT
    	sg.schedule_id pre_schedule_id
    	, sg.name
    	, sg.year AS sg_year
    	, sg.month AS sg_month
        , sg.day_of_week AS sg_day_of_week
    	, sg.day AS sg_day
    	, sg.hour AS sg_hour
    	, sg.minute AS sg_minute
    	, sg.second AS sg_second
    	, sg.is_error_stop AS sg_is_error_stop
		, sg.status AS sg_status
    	, sd.detail_id
    	, sd.year AS sd_year
    	, sd.month AS sd_month
        , sd.day_of_week AS sd_day_of_week
    	, sd.day AS sd_day
    	, sd.hour AS sd_hour
    	, sd.minute AS sd_minute
    	, sd.second AS sd_second
    	, sd.is_error_stop AS sd_is_error_stop
    	, sd.sequence
    	, sd.retry_count
		, sd.status AS sd_status
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
    FROM schedule_detail AS sd
    JOIN schedule_group AS sg
    	ON sd.schedule_id = sg.schedule_id
    JOIN task_detail AS td
    	ON td.detail_id = sd.detail_id
    JOIN devices
		ON devices.device_name = %s
    	AND devices.device_id = sg.target_device
    	AND devices.status = 'active'
)
, task_list AS (
    -- 일반 스케줄 리스트
    SELECT pre_schedule_id schedule_id
        , bd.*
        , '0' is_manual
        , NULL manual_id
        , NULL me_status
        , NULL is_immediate
        , NULL schedule_datetime
    FROM base_datas bd
    WHERE bd.sg_status = 'active'
      AND bd.sd_status = 'active'

    UNION ALL

    -- detail_id로 매칭
    SELECT CONCAT('m_', pre_schedule_id, '_d_', UNIX_TIMESTAMP(me.schedule_datetime)) schedule_id
        , bd.*
        , '1' is_manual
        , me.manual_id
        , me.status me_status
        , me.is_immediate
        , me.schedule_datetime
    FROM base_datas bd
    JOIN manual_execution me
      ON me.detail_id = bd.detail_id
     AND me.detail_id IS NOT NULL
     AND me.status IN ('wait', 'processing')
    -- WHERE 1 = 0
)
SELECT
    *,
    DENSE_RANK() OVER(PARTITION BY schedule_id ORDER BY sequence) AS new_sequence
FROM task_list

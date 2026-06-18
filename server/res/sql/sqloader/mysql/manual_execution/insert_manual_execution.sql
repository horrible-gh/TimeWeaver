INSERT INTO manual_execution(detail_id, is_immediate, schedule_datetime, status, creator, created_at)
SELECT sd.detail_id,
       %s,
       CASE WHEN %s = '0' THEN %s ELSE NOW() END,
       %s,
       %s,
       NOW()
  FROM schedule_group sg
  JOIN schedule_detail sd
    ON sg.schedule_id = sd.schedule_id
WHERE (
    (%s IS NOT NULL AND sg.schedule_id = %s)
    OR ( UNHEX(REPLACE(%s, '-', ''))  IS NOT NULL AND sd.detail_id = UNHEX(REPLACE(%s, '-', '')) )
    )
  AND sg.status not in ('inactive')
  AND sd.status not in ('inactive')

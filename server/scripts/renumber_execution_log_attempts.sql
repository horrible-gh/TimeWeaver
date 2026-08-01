-- Manual pre-deployment repair. This file is intentionally outside the
-- automatic migration directory and never deletes execution history.
UPDATE execution_log e
JOIN (
    SELECT execution_id,
           ROW_NUMBER() OVER (
               PARTITION BY execution_grp_id, detail_id
               ORDER BY execution_id
           ) AS rn
      FROM execution_log
) ranked ON ranked.execution_id = e.execution_id
SET e.attempt = ranked.rn;
from dataclasses import dataclass
from datetime import datetime


class RuntimeRepositoryError(Exception):
    def __init__(self, code: str):
        super().__init__(code)
        self.code = code


@dataclass(frozen=True)
class HeartbeatRecord:
    db_now: datetime


@dataclass(frozen=True)
class SnapshotRows:
    db_now: datetime
    device: dict
    groups: list[dict]
    details: list[dict]
    manuals: list[dict]


class AgentRuntimeRepository:
    """SQL boundary for agent heartbeat and device-scoped snapshot reads."""

    def __init__(self, db_instance):
        self.db = db_instance

    def record_heartbeat(
        self,
        device_id: int,
        agent_version: str,
        applied_revision: str | None,
    ) -> HeartbeatRecord:
        with self.db.begin_transaction() as txn:
            db_now = txn.fetch_one("SELECT UTC_TIMESTAMP() AS db_now")["db_now"]
            affected = txn.execute(
                """
                UPDATE devices
                   SET last_heartbeat_at = %s,
                       version = %s,
                       applied_revision = %s
                 WHERE device_id = %s
                   AND status = 'active'
                """,
                (db_now, agent_version, applied_revision, device_id),
            )
            if affected != 1:
                # MySQL can report zero changed rows when two identical
                # heartbeats land in the same DATETIME second. Distinguish that
                # harmless case from a revoked/deleted device while retaining
                # the status predicate on the write itself.
                active = txn.fetch_one(
                    """
                    SELECT device_id
                      FROM devices
                     WHERE device_id = %s
                       AND status = 'active'
                    """,
                    (device_id,),
                )
                if not active:
                    raise RuntimeRepositoryError("device_revoked")
        return HeartbeatRecord(db_now=db_now)

    def load_snapshot(self, device_id: int) -> SnapshotRows:
        with self.db.begin_transaction() as txn:
            # These are the first statements on this dedicated connection.
            # They establish one non-locking, repeatable source view for every
            # table used by the snapshot.
            txn.execute("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ")
            txn.execute("START TRANSACTION WITH CONSISTENT SNAPSHOT, READ ONLY")

            db_now = txn.fetch_one("SELECT UTC_TIMESTAMP() AS db_now")["db_now"]
            device = txn.fetch_one(
                """
                SELECT device_id, device_name, status, version
                  FROM devices
                 WHERE device_id = %s
                   AND status = 'active'
                """,
                (device_id,),
            )
            if not device:
                raise RuntimeRepositoryError("device_revoked")

            groups = txn.fetch_all(
                """
                SELECT schedule_id, name,
                       year, month, day_of_week, day, hour, minute, second,
                       is_error_stop
                  FROM schedule_group
                 WHERE target_device = %s
                   AND status = 'active'
                """,
                (device_id,),
            )
            details = txn.fetch_all(
                """
                SELECT sd.detail_id, sd.schedule_id, sd.schedule_name,
                       sd.year, sd.month, sd.day_of_week, sd.day,
                       sd.hour, sd.minute, sd.second,
                       sd.is_error_stop, sd.sequence, sd.retry_count,
                       td.detail_id AS task_detail_id,
                       td.task_type, td.command, td.archive_type,
                       td.source_path, td.error_on_missing_source,
                       td.destination_path, td.date_format,
                       td.target_date_format, td.destination_date_format,
                       td.house_keep_days
                  FROM schedule_group sg
                  JOIN schedule_detail sd
                    ON sd.schedule_id = sg.schedule_id
                   AND sd.status = 'active'
                  LEFT JOIN task_detail td
                    ON td.detail_id = sd.detail_id
                 WHERE sg.target_device = %s
                   AND sg.status = 'active'
                """,
                (device_id,),
            )
            manuals = txn.fetch_all(
                """
                SELECT me.manual_id, sd.schedule_id, me.detail_id,
                       me.status, me.is_immediate, me.schedule_datetime,
                       me.claim_expires_at
                  FROM schedule_group sg
                  JOIN schedule_detail sd
                    ON sd.schedule_id = sg.schedule_id
                   AND sd.status = 'active'
                  JOIN manual_execution me
                    ON me.detail_id = sd.detail_id
                 WHERE sg.target_device = %s
                   AND sg.status = 'active'
                   AND me.status IN ('wait', 'processing')
                """,
                (device_id,),
            )

        return SnapshotRows(
            db_now=db_now,
            device=device,
            groups=list(groups),
            details=list(details),
            manuals=list(manuals),
        )
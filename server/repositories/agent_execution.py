import hmac
import time
import uuid
from dataclasses import dataclass
from datetime import datetime, timedelta

import pymysql


MANUAL_CLAIM_TTL_MINUTES = 30
EXPIRED_CLAIM_SWEEP_BATCH = 500
TRANSACTION_RETRY_COUNT = 3
TRANSACTION_RETRY_DELAYS = (0.02, 0.05)


class ExecutionRepositoryError(Exception):
    def __init__(self, code: str, detail: str | None = None):
        super().__init__(code)
        self.code = code
        self.detail = detail


@dataclass(frozen=True)
class ClaimRecord:
    manual_id: int
    claim_token: str
    claim_expires_at: datetime
    db_now: datetime


@dataclass(frozen=True)
class ResultRecord:
    execution_id: int
    duplicate: bool
    applied_transitions: list[dict]
    db_now: datetime


@dataclass(frozen=True)
class EventRecord:
    db_now: datetime


def _mysql_code(exc: BaseException) -> int | None:
    if isinstance(exc, pymysql.MySQLError) and exc.args:
        try:
            return int(exc.args[0])
        except (TypeError, ValueError):
            return None
    return None


def _uuid_bytes(value) -> bytes:
    if isinstance(value, uuid.UUID):
        return value.bytes
    if isinstance(value, memoryview):
        return value.tobytes()
    if isinstance(value, (bytes, bytearray)):
        return bytes(value)
    return uuid.UUID(str(value)).bytes


def _uuid_text(value) -> str:
    return str(uuid.UUID(bytes=_uuid_bytes(value)))


def _same_result(row: dict, payload: dict) -> bool:
    return (
        int(row["schedule_id"]) == payload["schedule_id"]
        and row.get("manual_id") == payload["manual_id"]
        and row["start_time"] == payload["started_at"]
        and row["end_time"] == payload["finished_at"]
        and int(row["result_code"]) == payload["result_code"]
        and row.get("result_message") == payload["result_message"]
        and row.get("environment_info") == payload["environment_info"]
    )


class AgentExecutionRepository:
    """Transactional SQL boundary for A5 claims, A6 results, and A7 events."""

    def __init__(self, db_instance):
        self.db = db_instance

    def _run_retryable(self, operation):
        for attempt in range(TRANSACTION_RETRY_COUNT):
            try:
                return operation()
            except ExecutionRepositoryError:
                raise
            except pymysql.MySQLError as exc:
                if _mysql_code(exc) == 1213 and attempt + 1 < TRANSACTION_RETRY_COUNT:
                    time.sleep(TRANSACTION_RETRY_DELAYS[attempt])
                    continue
                raise ExecutionRepositoryError("unavailable") from exc
            except Exception as exc:
                raise ExecutionRepositoryError("unavailable") from exc
        raise ExecutionRepositoryError("unavailable")

    @staticmethod
    def _prepare_transaction(txn):
        txn.execute("SET TRANSACTION ISOLATION LEVEL READ COMMITTED")
        txn.execute("SET innodb_lock_wait_timeout = 3")

    def claim_manual_run(
        self,
        device_id: int,
        manual_id: int,
        claim_token: str,
    ) -> ClaimRecord:
        def operation():
            with self.db.begin_transaction() as txn:
                self._prepare_transaction(txn)
                candidate = txn.fetch_one(
                    """
                    SELECT me.detail_id, sd.schedule_id
                      FROM manual_execution me
                      JOIN schedule_detail sd ON sd.detail_id = me.detail_id
                     WHERE me.manual_id = %s
                    """,
                    (manual_id,),
                )
                if not candidate:
                    raise ExecutionRepositoryError("not_found")

                group = txn.fetch_one(
                    """
                    SELECT schedule_id, target_device
                      FROM schedule_group
                     WHERE schedule_id = %s
                     FOR UPDATE
                    """,
                    (candidate["schedule_id"],),
                )
                detail = txn.fetch_one(
                    """
                    SELECT detail_id, schedule_id
                      FROM schedule_detail
                     WHERE detail_id = %s
                     FOR UPDATE
                    """,
                    (candidate["detail_id"],),
                )
                manual = txn.fetch_one(
                    """
                    SELECT manual_id, detail_id, status,
                           claim_token, claim_expires_at
                      FROM manual_execution
                     WHERE manual_id = %s
                     FOR UPDATE
                    """,
                    (manual_id,),
                )
                if (
                    not group
                    or not detail
                    or not manual
                    or int(group["target_device"]) != device_id
                    or int(detail["schedule_id"]) != int(group["schedule_id"])
                    or _uuid_bytes(detail["detail_id"]) != _uuid_bytes(manual["detail_id"])
                    or _uuid_bytes(detail["detail_id"]) != _uuid_bytes(candidate["detail_id"])
                ):
                    raise ExecutionRepositoryError("not_found")

                db_now = txn.fetch_one("SELECT UTC_TIMESTAMP() AS db_now")["db_now"]
                if (
                    manual["status"] == "processing"
                    and manual["claim_expires_at"] is not None
                    and manual["claim_expires_at"] <= db_now
                ):
                    txn.execute(
                        """
                        UPDATE manual_execution
                           SET status = 'wait', claim_token = NULL,
                               claim_expires_at = NULL, modified_at = %s
                         WHERE manual_id = %s
                        """,
                        (db_now, manual_id),
                    )
                    manual["status"] = "wait"

                if manual["status"] != "wait":
                    raise ExecutionRepositoryError("already_claimed")

                claim_expires_at = db_now + timedelta(minutes=MANUAL_CLAIM_TTL_MINUTES)
                txn.execute(
                    """
                    UPDATE manual_execution
                       SET status = 'processing', claim_token = %s,
                           claim_expires_at = %s, modified_at = %s
                     WHERE manual_id = %s
                    """,
                    (claim_token, claim_expires_at, db_now, manual_id),
                )
            return ClaimRecord(manual_id, claim_token, claim_expires_at, db_now)

        return self._run_retryable(operation)

    def accept_result(self, device_id: int, payload: dict) -> ResultRecord:
        def operation():
            with self.db.begin_transaction() as txn:
                self._prepare_transaction(txn)
                candidate = txn.fetch_one(
                    """
                    SELECT schedule_id
                      FROM schedule_detail
                     WHERE detail_id = %s
                    """,
                    (payload["detail_id"],),
                )
                if not candidate:
                    raise ExecutionRepositoryError("not_found")

                group = txn.fetch_one(
                    """
                    SELECT schedule_id, target_device, is_error_stop
                      FROM schedule_group
                     WHERE schedule_id = %s
                     FOR UPDATE
                    """,
                    (candidate["schedule_id"],),
                )
                detail = txn.fetch_one(
                    """
                    SELECT detail_id, schedule_id, is_error_stop
                      FROM schedule_detail
                     WHERE detail_id = %s
                     FOR UPDATE
                    """,
                    (payload["detail_id"],),
                )
                if (
                    not group
                    or not detail
                    or int(group["target_device"]) != device_id
                    or int(group["schedule_id"]) != payload["schedule_id"]
                    or int(detail["schedule_id"]) != payload["schedule_id"]
                ):
                    raise ExecutionRepositoryError("not_found")

                db_now = txn.fetch_one("SELECT UTC_TIMESTAMP() AS db_now")["db_now"]
                existing = self._find_result(txn, payload)
                if existing:
                    return self._duplicate(existing, payload, db_now)

                manual = None
                if payload["manual_id"] is not None:
                    manual = txn.fetch_one(
                        """
                        SELECT manual_id, detail_id, status,
                               claim_token, claim_expires_at
                          FROM manual_execution
                         WHERE manual_id = %s
                         FOR UPDATE
                        """,
                        (payload["manual_id"],),
                    )
                    if (
                        not manual
                        or _uuid_bytes(manual["detail_id"]) != payload["detail_id"]
                    ):
                        raise ExecutionRepositoryError("not_found")
                    stored_claim = manual.get("claim_token") or ""
                    if (
                        manual["status"] != "processing"
                        or not hmac.compare_digest(stored_claim, payload["claim_token"])
                        or manual["claim_expires_at"] is None
                        or manual["claim_expires_at"] <= db_now
                    ):
                        raise ExecutionRepositoryError(
                            "claim_expired",
                            str(manual.get("claim_expires_at") or ""),
                        )

                try:
                    txn.execute(
                        """
                        INSERT INTO execution_log
                            (execution_grp_id, schedule_id, detail_id, attempt,
                             manual_id, start_time, end_time, result_code,
                             result_message, environment_info)
                        VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s)
                        """,
                        (
                            payload["execution_grp_id"],
                            payload["schedule_id"],
                            payload["detail_id"],
                            payload["attempt"],
                            payload["manual_id"],
                            payload["started_at"],
                            payload["finished_at"],
                            payload["result_code"],
                            payload["result_message"],
                            payload["environment_info"],
                        ),
                    )
                except pymysql.IntegrityError as exc:
                    if _mysql_code(exc) != 1062:
                        raise
                    existing = self._find_result(txn, payload)
                    if not existing:
                        raise
                    return self._duplicate(existing, payload, db_now)

                execution_id = int(
                    txn.fetch_one("SELECT LAST_INSERT_ID() AS execution_id")["execution_id"]
                )
                transitions = []
                if manual is not None:
                    next_status = "done" if payload["result_code"] == 0 else "failed"
                    txn.execute(
                        """
                        UPDATE manual_execution
                           SET status = %s, claim_token = NULL,
                               claim_expires_at = NULL, modified_at = %s
                         WHERE manual_id = %s
                        """,
                        (next_status, db_now, payload["manual_id"]),
                    )
                    transitions.append(
                        {
                            "target": "manual_execution",
                            "id": payload["manual_id"],
                            "status": next_status,
                        }
                    )
                elif payload["result_code"] != 0 and bool(group["is_error_stop"]):
                    txn.execute(
                        "UPDATE schedule_group SET status = 'error', modified_at = %s WHERE schedule_id = %s",
                        (db_now, payload["schedule_id"]),
                    )
                    transitions.append(
                        {
                            "target": "schedule_group",
                            "id": payload["schedule_id"],
                            "status": "error",
                        }
                    )
                elif payload["result_code"] != 0 and bool(detail["is_error_stop"]):
                    txn.execute(
                        "UPDATE schedule_detail SET status = 'error', modified_at = %s WHERE detail_id = %s",
                        (db_now, payload["detail_id"]),
                    )
                    transitions.append(
                        {
                            "target": "schedule_detail",
                            "id": _uuid_text(payload["detail_id"]),
                            "status": "error",
                        }
                    )

            return ResultRecord(execution_id, False, transitions, db_now)

        return self._run_retryable(operation)

    @staticmethod
    def _find_result(txn, payload: dict):
        return txn.fetch_one(
            """
            SELECT execution_id, schedule_id, manual_id, start_time, end_time,
                   result_code, result_message, environment_info
              FROM execution_log
             WHERE execution_grp_id = %s AND detail_id = %s AND attempt = %s
            """,
            (
                payload["execution_grp_id"],
                payload["detail_id"],
                payload["attempt"],
            ),
        )

    @staticmethod
    def _duplicate(existing: dict, payload: dict, db_now: datetime):
        if not _same_result(existing, payload):
            raise ExecutionRepositoryError("invalid_request")
        return ResultRecord(int(existing["execution_id"]), True, [], db_now)

    def accept_event(
        self,
        device_id: int,
        event_type: str,
        severity: str,
        occurred_at: datetime,
        message: str | None,
        environment_info: str | None,
    ) -> EventRecord:
        try:
            with self.db.begin_transaction() as txn:
                db_now = txn.fetch_one("SELECT UTC_TIMESTAMP() AS db_now")["db_now"]
                txn.execute(
                    """
                    INSERT INTO agent_event
                        (device_id, event_type, severity, occurred_at,
                         message, environment_info)
                    VALUES (%s, %s, %s, %s, %s, %s)
                    """,
                    (
                        device_id,
                        event_type,
                        severity,
                        occurred_at,
                        message,
                        environment_info,
                    ),
                )
            return EventRecord(db_now)
        except Exception as exc:
            raise ExecutionRepositoryError("unavailable") from exc

    def sweep_expired_claims(self, batch_size: int = EXPIRED_CLAIM_SWEEP_BATCH) -> int:
        total = 0
        while True:
            try:
                with self.db.begin_transaction() as txn:
                    self._prepare_transaction(txn)
                    db_now = txn.fetch_one("SELECT UTC_TIMESTAMP() AS db_now")["db_now"]
                    rows = txn.fetch_all(
                        """
                        SELECT manual_id
                          FROM manual_execution
                         WHERE status = 'processing'
                           AND claim_expires_at <= %s
                         ORDER BY claim_expires_at, manual_id
                         LIMIT %s
                         FOR UPDATE SKIP LOCKED
                        """,
                        (db_now, batch_size),
                    )
                    ids = [int(row["manual_id"]) for row in rows]
                    if ids:
                        placeholders = ",".join(["%s"] * len(ids))
                        txn.execute(
                            f"""
                            UPDATE manual_execution
                               SET status = 'wait', claim_token = NULL,
                                   claim_expires_at = NULL, modified_at = %s
                             WHERE manual_id IN ({placeholders})
                               AND status = 'processing'
                               AND claim_expires_at <= %s
                            """,
                            (db_now, *ids, db_now),
                        )
                total += len(ids)
                if len(ids) < batch_size:
                    return total
            except Exception as exc:
                raise ExecutionRepositoryError("unavailable") from exc
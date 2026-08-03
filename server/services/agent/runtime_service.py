import hashlib
import json
import uuid
from dataclasses import dataclass
from datetime import datetime, timezone

from config import db
from repositories.agent_runtime import (
    AgentRuntimeRepository,
    RuntimeRepositoryError,
    SnapshotRows,
)
from schemas.agent_runtime import HeartbeatRequest
from services.agent.identity_service import DeviceIdentity
from services.task_contract import normalize_task_row


SCHEMA_VERSION = "1"
MIN_SCHEMA_VERSION = 1
ETAG_DIGEST_PREFIX = 16


class AgentRuntimeError(Exception):
    def __init__(self, status_code: int, code: str, message: str):
        super().__init__(message)
        self.status_code = status_code
        self.code = code
        self.message = message


@dataclass(frozen=True)
class SnapshotResult:
    data: dict
    etag: str
    generated_at: datetime


def _utc(value: datetime) -> datetime:
    if value.tzinfo is None:
        value = value.replace(tzinfo=timezone.utc)
    else:
        value = value.astimezone(timezone.utc)
    return value.replace(microsecond=0)


def _utc_text(value: datetime) -> str:
    return _utc(value).strftime("%Y-%m-%dT%H:%M:%SZ")


def _uuid_text(value) -> str:
    if isinstance(value, uuid.UUID):
        return str(value)
    if isinstance(value, memoryview):
        value = value.tobytes()
    if isinstance(value, (bytes, bytearray)):
        return str(uuid.UUID(bytes=bytes(value)))
    return str(uuid.UUID(str(value)))


def _cron(row: dict) -> dict:
    keys = ("year", "month", "day_of_week", "day", "hour", "minute", "second")
    return {key: str(row[key]) if row.get(key) is not None else "*" for key in keys}


def _canonical_default(value):
    if isinstance(value, datetime):
        return _utc_text(value)
    raise TypeError(f"Unsupported canonical snapshot value: {type(value).__name__}")


def canonical_snapshot_bytes(snapshot: dict) -> bytes:
    """Encode only exposed, stable business values for revision hashing."""

    canonical = {
        "device": snapshot["device"],
        "schedules": snapshot["schedules"],
        "manual_runs": snapshot["manual_runs"],
    }
    return json.dumps(
        canonical,
        default=_canonical_default,
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":"),
    ).encode("utf-8")


def calculate_snapshot_revision(snapshot: dict) -> tuple[str, str]:
    digest = hashlib.sha256(canonical_snapshot_bytes(snapshot)).hexdigest()
    return f"sha256:{digest}", f'W/"{digest[:ETAG_DIGEST_PREFIX]}"'


class AgentRuntimeService:
    def __init__(self, repository: AgentRuntimeRepository):
        self.repository = repository

    def heartbeat(
        self,
        principal: DeviceIdentity,
        request: HeartbeatRequest,
    ) -> dict:
        try:
            result = self.repository.record_heartbeat(
                principal.device_id,
                request.agent_version,
                request.applied_revision,
            )
        except RuntimeRepositoryError as exc:
            self._raise_repository_error(exc)
        server_time = _utc(result.db_now)
        return {
            "device_id": principal.device_id,
            "device_status": "active",
            "server_time": server_time,
        }

    def snapshot(self, principal: DeviceIdentity) -> SnapshotResult:
        try:
            rows = self.repository.load_snapshot(principal.device_id)
        except RuntimeRepositoryError as exc:
            self._raise_repository_error(exc)

        snapshot = self._normalize_snapshot(rows)
        revision, etag = calculate_snapshot_revision(snapshot)
        generated_at = _utc(rows.db_now)
        data = {
            "revision": revision,
            "generated_at": generated_at,
            **snapshot,
        }
        return SnapshotResult(data=data, etag=etag, generated_at=generated_at)

    def _normalize_snapshot(self, rows: SnapshotRows) -> dict:
        groups_by_id = {}
        for row in sorted(rows.groups, key=lambda item: int(item["schedule_id"])):
            schedule_id = int(row["schedule_id"])
            groups_by_id[schedule_id] = {
                "schedule_id": schedule_id,
                "name": row["name"],
                "cron": _cron(row),
                "is_error_stop": bool(row["is_error_stop"]),
                "details": [],
            }

        detail_rows = sorted(
            rows.details,
            key=lambda item: (
                int(item["schedule_id"]),
                int(item["sequence"]),
                _uuid_text(item["detail_id"]),
            ),
        )
        previous_sequence = {}
        dense_rank = {}
        for row in detail_rows:
            schedule_id = int(row["schedule_id"])
            if schedule_id not in groups_by_id:
                continue
            if row.get("task_detail_id") is None:
                raise AgentRuntimeError(
                    500,
                    "server_error",
                    "Snapshot source data is inconsistent",
                )

            sequence = int(row["sequence"])
            if previous_sequence.get(schedule_id) != sequence:
                dense_rank[schedule_id] = dense_rank.get(schedule_id, 0) + 1
                previous_sequence[schedule_id] = sequence

            groups_by_id[schedule_id]["details"].append(
                {
                    "detail_id": _uuid_text(row["detail_id"]),
                    "schedule_name": row.get("schedule_name"),
                    "cron": _cron(row),
                    "is_error_stop": bool(row["is_error_stop"]),
                    "sequence": sequence,
                    "exec_sequence": dense_rank[schedule_id],
                    "retry_count": int(row.get("retry_count") or 0),
                    "task": normalize_task_row(
                        {
                            "task_type": row["task_type"],
                            "command": row.get("command"),
                            "archive_type": row.get("archive_type"),
                            "source_path": row.get("source_path"),
                            "error_on_missing_source": bool(
                                row.get("error_on_missing_source")
                            ),
                            "destination_path": row.get("destination_path"),
                            "date_format": row.get("date_format"),
                            "target_date_format": row.get("target_date_format"),
                            "destination_date_format": row.get(
                                "destination_date_format"
                            ),
                            "house_keep_days": row.get("house_keep_days"),
                        }
                    ),
                }
            )

        manuals = []
        for row in rows.manuals:
            schedule_datetime = _utc(row["schedule_datetime"])
            status = row["status"]
            manuals.append(
                {
                    "manual_id": int(row["manual_id"]),
                    "schedule_id": int(row["schedule_id"]),
                    "detail_id": _uuid_text(row["detail_id"]),
                    "status": status,
                    "is_immediate": bool(row["is_immediate"]),
                    "schedule_datetime": schedule_datetime,
                    "claimable": (
                        status == "wait"
                        or (
                            status == "processing"
                            and row.get("claim_expires_at") is not None
                            and row["claim_expires_at"] <= rows.db_now
                        )
                    ),
                }
            )
        manuals.sort(
            key=lambda item: (
                _utc_text(item["schedule_datetime"]),
                item["manual_id"],
            )
        )

        device = rows.device
        return {
            "device": {
                "device_id": int(device["device_id"]),
                "device_name": device["device_name"],
                "status": "active",
                "known_agent_version": device.get("version"),
            },
            "schedules": list(groups_by_id.values()),
            "manual_runs": manuals,
        }

    @staticmethod
    def _raise_repository_error(exc: RuntimeRepositoryError):
        if exc.code == "device_revoked":
            raise AgentRuntimeError(
                403,
                "device_revoked",
                "Device credential has been revoked",
            ) from exc
        raise AgentRuntimeError(
            503,
            "unavailable",
            "Agent runtime storage unavailable",
        ) from exc


def get_runtime_service() -> AgentRuntimeService:
    return AgentRuntimeService(AgentRuntimeRepository(db.db_instance))
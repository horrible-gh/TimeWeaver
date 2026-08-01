"""Typed immutable models for the TimeWeaver agent API contract."""

from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime
from typing import Any, Mapping
from uuid import UUID


class ModelValidationError(ValueError):
    """Raised when an API payload cannot be represented safely."""


def require_mapping(value: Any, field_name: str) -> Mapping[str, Any]:
    if not isinstance(value, Mapping):
        raise ModelValidationError(f"{field_name} must be an object")
    return value


def required_text(data: Mapping[str, Any], name: str) -> str:
    value = data.get(name)
    if not isinstance(value, str) or not value:
        raise ModelValidationError(f"{name} must be a non-empty string")
    return value


def optional_text(data: Mapping[str, Any], name: str) -> str | None:
    value = data.get(name)
    if value is None:
        return None
    if not isinstance(value, str):
        raise ModelValidationError(f"{name} must be a string or null")
    return value


def integer(data: Mapping[str, Any], name: str, *, minimum: int | None = None) -> int:
    value = data.get(name)
    if isinstance(value, bool) or not isinstance(value, int):
        raise ModelValidationError(f"{name} must be an integer")
    if minimum is not None and value < minimum:
        raise ModelValidationError(f"{name} must be >= {minimum}")
    return value


def parse_bool(value: Any, field_name: str) -> bool:
    if isinstance(value, bool):
        return value
    if isinstance(value, int) and not isinstance(value, bool) and value in (0, 1):
        return value == 1
    if isinstance(value, str):
        normalized = value.strip().lower()
        if normalized in {"true", "1"}:
            return True
        if normalized in {"false", "0"}:
            return False
    raise ModelValidationError(f"{field_name} must be a boolean")


def parse_uuid(value: Any, field_name: str, *, require_lowercase: bool = False) -> UUID:
    if isinstance(value, UUID):
        parsed = value
        source = str(value)
    elif isinstance(value, str):
        source = value
        try:
            parsed = UUID(value)
        except (ValueError, AttributeError) as exc:
            raise ModelValidationError(f"{field_name} must be a valid UUID") from exc
    else:
        raise ModelValidationError(f"{field_name} must be a UUID string")
    if require_lowercase and source != str(parsed):
        raise ModelValidationError(f"{field_name} must be a lowercase canonical UUID")
    return parsed


def parse_datetime(value: Any, field_name: str) -> datetime:
    if not isinstance(value, str):
        raise ModelValidationError(f"{field_name} must be an RFC3339 string")
    candidate = value[:-1] + "+00:00" if value.endswith("Z") else value
    try:
        parsed = datetime.fromisoformat(candidate)
    except ValueError as exc:
        raise ModelValidationError(f"{field_name} must be a valid RFC3339 datetime") from exc
    if parsed.tzinfo is None or parsed.utcoffset() is None:
        raise ModelValidationError(f"{field_name} must include a timezone")
    return parsed


def optional_datetime(data: Mapping[str, Any], name: str) -> datetime | None:
    value = data.get(name)
    return None if value is None else parse_datetime(value, name)


def cron_value(data: Mapping[str, Any], name: str) -> str:
    value = data.get(name, "*")
    if isinstance(value, bool) or not isinstance(value, (str, int)):
        raise ModelValidationError(f"cron.{name} must be a string or integer")
    text = str(value)
    if not text or text != text.strip() or any(ord(character) < 32 for character in text):
        raise ModelValidationError(f"cron.{name} contains an invalid expression")
    return text


@dataclass(frozen=True, slots=True)
class CronFields:
    year: str = "*"
    month: str = "*"
    day_of_week: str = "*"
    day: str = "*"
    hour: str = "*"
    minute: str = "*"
    second: str = "*"

    @classmethod
    def from_dict(cls, value: Any) -> "CronFields":
        data = require_mapping(value, "cron")
        return cls(**{name: cron_value(data, name) for name in cls.__dataclass_fields__})


@dataclass(frozen=True, slots=True)
class Device:
    device_id: int
    device_name: str
    status: str
    known_agent_version: str | None = None

    @property
    def name(self) -> str:
        return self.device_name

    @property
    def agent_version(self) -> str | None:
        return self.known_agent_version

    @classmethod
    def from_dict(cls, value: Any) -> "Device":
        data = require_mapping(value, "device")
        return cls(
            device_id=integer(data, "device_id", minimum=1),
            device_name=required_text(data, "device_name"),
            status=required_text(data, "status"),
            known_agent_version=optional_text(data, "known_agent_version"),
        )


@dataclass(frozen=True, slots=True)
class AccessCredential:
    access_token: str
    access_token_expires_at: datetime
    refresh_token: str
    refresh_token_expires_at: datetime
    device_id: int | None = None
    device_name: str | None = None

    @classmethod
    def from_dict(cls, value: Any) -> "AccessCredential":
        data = require_mapping(value, "credential")
        device_id = data.get("device_id")
        if device_id is not None and (isinstance(device_id, bool) or not isinstance(device_id, int) or device_id < 1):
            raise ModelValidationError("device_id must be a positive integer or null")
        device_name = data.get("device_name")
        if device_name is not None and (not isinstance(device_name, str) or not device_name):
            raise ModelValidationError("device_name must be a non-empty string or null")
        return cls(
            access_token=required_text(data, "access_token"),
            access_token_expires_at=parse_datetime(data.get("access_token_expires_at"), "access_token_expires_at"),
            refresh_token=required_text(data, "refresh_token"),
            refresh_token_expires_at=parse_datetime(data.get("refresh_token_expires_at"), "refresh_token_expires_at"),
            device_id=device_id,
            device_name=device_name,
        )


@dataclass(frozen=True, slots=True)
class ScheduleDetail:
    detail_id: UUID
    schedule_name: str
    cron: CronFields
    is_error_stop: bool
    sequence: int
    exec_sequence: int
    retry_count: int
    task_type: str
    command: str | None = None
    archive_type: str | None = None
    source_path: str | None = None
    error_on_missing_source: bool = False
    destination_path: str | None = None
    date_format: str | None = None
    target_date_format: str | None = None
    destination_date_format: str | None = None
    house_keep_days: int | None = None

    @property
    def new_sequence(self) -> int:
        return self.exec_sequence

    @classmethod
    def from_dict(cls, value: Any) -> "ScheduleDetail":
        data = require_mapping(value, "schedule detail")
        task = require_mapping(data.get("task", {}), "task")
        combined = dict(data)
        combined.update(task)
        sequence = integer(data, "sequence", minimum=0)
        exec_value = data.get("exec_sequence", data.get("new_sequence"))
        if isinstance(exec_value, bool) or not isinstance(exec_value, int) or exec_value < 1:
            raise ModelValidationError("exec_sequence must be an integer >= 1")
        retry_count = integer(data, "retry_count", minimum=0)
        house_keep_days = combined.get("house_keep_days")
        if house_keep_days is not None and (
            isinstance(house_keep_days, bool) or not isinstance(house_keep_days, int) or house_keep_days < 0
        ):
            raise ModelValidationError("house_keep_days must be a non-negative integer or null")
        return cls(
            detail_id=parse_uuid(data.get("detail_id"), "detail_id", require_lowercase=True),
            schedule_name=required_text(data, "schedule_name"),
            cron=CronFields.from_dict(data.get("cron", {})),
            is_error_stop=parse_bool(data.get("is_error_stop"), "is_error_stop"),
            sequence=sequence,
            exec_sequence=exec_value,
            retry_count=retry_count,
            task_type=required_text(combined, "task_type"),
            command=optional_text(combined, "command"),
            archive_type=optional_text(combined, "archive_type"),
            source_path=optional_text(combined, "source_path"),
            error_on_missing_source=parse_bool(combined.get("error_on_missing_source", False), "error_on_missing_source"),
            destination_path=optional_text(combined, "destination_path"),
            date_format=optional_text(combined, "date_format"),
            target_date_format=optional_text(combined, "target_date_format"),
            destination_date_format=optional_text(combined, "destination_date_format"),
            house_keep_days=house_keep_days,
        )


@dataclass(frozen=True, slots=True)
class ScheduleGroup:
    schedule_id: int
    name: str
    cron: CronFields
    is_error_stop: bool
    details: tuple[ScheduleDetail, ...] = field(default_factory=tuple)

    @classmethod
    def from_dict(cls, value: Any) -> "ScheduleGroup":
        data = require_mapping(value, "schedule")
        details = data.get("details")
        if not isinstance(details, list):
            raise ModelValidationError("details must be an array")
        return cls(
            schedule_id=integer(data, "schedule_id", minimum=1),
            name=required_text(data, "name"),
            cron=CronFields.from_dict(data.get("cron", {})),
            is_error_stop=parse_bool(data.get("is_error_stop"), "is_error_stop"),
            details=tuple(ScheduleDetail.from_dict(item) for item in details),
        )


@dataclass(frozen=True, slots=True)
class ManualExecution:
    manual_id: int
    schedule_id: int
    detail_id: UUID
    status: str
    is_immediate: bool
    schedule_datetime: datetime
    claimable: bool

    @classmethod
    def from_dict(cls, value: Any) -> "ManualExecution":
        data = require_mapping(value, "manual run")
        return cls(
            manual_id=integer(data, "manual_id", minimum=1),
            schedule_id=integer(data, "schedule_id", minimum=1),
            detail_id=parse_uuid(data.get("detail_id"), "detail_id", require_lowercase=True),
            status=required_text(data, "status"),
            is_immediate=parse_bool(data.get("is_immediate"), "is_immediate"),
            schedule_datetime=parse_datetime(data.get("schedule_datetime"), "schedule_datetime"),
            claimable=parse_bool(data.get("claimable"), "claimable"),
        )


@dataclass(frozen=True, slots=True)
class ScheduleSnapshot:
    schema_version: str
    revision: str
    etag: str
    server_time: datetime
    generated_at: datetime
    device: Device
    schedules: tuple[ScheduleGroup, ...]
    manual_runs: tuple[ManualExecution, ...] = field(default_factory=tuple)

    @property
    def groups(self) -> tuple[ScheduleGroup, ...]:
        return self.schedules

    @property
    def manual_executions(self) -> tuple[ManualExecution, ...]:
        return self.manual_runs

    @classmethod
    def from_envelope(cls, value: Any, *, etag: str) -> "ScheduleSnapshot":
        envelope = require_mapping(value, "snapshot response")
        schema_version = required_text(envelope, "schema_version")
        data = require_mapping(envelope.get("data"), "data")
        schedules = data.get("schedules")
        manuals = data.get("manual_runs", [])
        if not isinstance(schedules, list):
            raise ModelValidationError("schedules must be an array")
        if not isinstance(manuals, list):
            raise ModelValidationError("manual_runs must be an array")
        return cls(
            schema_version=schema_version,
            revision=required_text(data, "revision"),
            etag=etag,
            server_time=parse_datetime(envelope.get("server_time"), "server_time"),
            generated_at=parse_datetime(data.get("generated_at"), "generated_at"),
            device=Device.from_dict(data.get("device")),
            schedules=tuple(ScheduleGroup.from_dict(item) for item in schedules),
            manual_runs=tuple(ManualExecution.from_dict(item) for item in manuals),
        )
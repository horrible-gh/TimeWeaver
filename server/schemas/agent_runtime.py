from datetime import datetime
from typing import Literal

from pydantic import BaseModel, Field


REVISION_PATTERN = r"^sha256:[0-9a-f]{64}$"


class HeartbeatRequest(BaseModel):
    agent_version: str = Field(min_length=1, max_length=50)
    applied_revision: str | None = Field(
        default=None,
        pattern=REVISION_PATTERN,
        max_length=71,
    )
    state: Literal["BOOTSTRAP", "HEALTHY", "DEGRADED", "HALTED"]


class HeartbeatData(BaseModel):
    device_id: int
    device_status: Literal["active"]
    server_time: datetime


class HeartbeatEnvelope(BaseModel):
    schema_version: Literal["1"] = "1"
    server_time: datetime
    data: HeartbeatData


class CronData(BaseModel):
    year: str
    month: str
    day_of_week: str
    day: str
    hour: str
    minute: str
    second: str


class SnapshotTaskData(BaseModel):
    task_type: Literal["command", "archive", "copy", "housekeep"]
    command: str | None
    archive_type: str | None
    source_path: str | None
    error_on_missing_source: bool
    destination_path: str | None
    date_format: str | None
    target_date_format: str | None
    destination_date_format: str | None
    house_keep_days: int | None


class SnapshotDetailData(BaseModel):
    detail_id: str
    schedule_name: str | None
    cron: CronData
    is_error_stop: bool
    sequence: int
    exec_sequence: int
    retry_count: int
    task: SnapshotTaskData


class SnapshotScheduleData(BaseModel):
    schedule_id: int
    name: str
    cron: CronData
    is_error_stop: bool
    details: list[SnapshotDetailData]


class SnapshotDeviceData(BaseModel):
    device_id: int
    device_name: str
    status: Literal["active"]
    known_agent_version: str | None


class SnapshotManualRunData(BaseModel):
    manual_id: int
    schedule_id: int
    detail_id: str
    status: Literal["wait", "processing"]
    is_immediate: bool
    schedule_datetime: datetime
    claimable: bool


class SnapshotData(BaseModel):
    revision: str = Field(pattern=REVISION_PATTERN)
    generated_at: datetime
    device: SnapshotDeviceData
    schedules: list[SnapshotScheduleData]
    manual_runs: list[SnapshotManualRunData]


class SnapshotEnvelope(BaseModel):
    schema_version: Literal["1"] = "1"
    server_time: datetime
    data: SnapshotData
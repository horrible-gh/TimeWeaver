import json
from datetime import datetime, timedelta
from typing import Any, Literal
from uuid import UUID

from pydantic import BaseModel, Field, field_validator, model_validator


ENVIRONMENT_INFO_MAX = 32768
EVENT_MESSAGE_MAX = 16384
CLAIM_TOKEN_PATTERN = r"^[0-9a-f]{64}$"


def canonical_environment(value: dict[str, Any] | None) -> str | None:
    if value is None:
        return None
    return json.dumps(
        value,
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":"),
    )


def _require_utc(value: datetime) -> datetime:
    if value.tzinfo is None or value.utcoffset() != timedelta(0):
        raise ValueError("timestamp must be UTC RFC3339")
    return value


class ExecutionStartRequest(BaseModel):
    execution_grp_id: UUID
    schedule_id: int
    detail_id: UUID
    attempt: int = Field(ge=1)
    started_at: datetime

    @field_validator("started_at")
    @classmethod
    def started_at_is_utc(cls, value: datetime):
        return _require_utc(value)


class ExecutionStartData(BaseModel):
    accepted: Literal[True] = True


class ExecutionStartEnvelope(BaseModel):
    schema_version: Literal["1"] = "1"
    server_time: datetime
    data: ExecutionStartData


class ExecutionResultRequest(BaseModel):
    execution_grp_id: UUID
    schedule_id: int
    detail_id: UUID
    attempt: int = Field(ge=1)
    manual_id: int | None = None
    claim_token: str | None = Field(default=None, pattern=CLAIM_TOKEN_PATTERN)
    started_at: datetime
    finished_at: datetime
    result_code: int
    result_message: str | None = None
    environment_info: dict[str, Any] | None = None

    @field_validator("started_at", "finished_at")
    @classmethod
    def timestamps_are_utc(cls, value: datetime):
        return _require_utc(value)

    @field_validator("environment_info")
    @classmethod
    def environment_is_bounded(cls, value):
        encoded = canonical_environment(value)
        if encoded is not None and len(encoded.encode("utf-8")) > ENVIRONMENT_INFO_MAX:
            raise ValueError("environment_info exceeds 32768 UTF-8 bytes")
        return value

    @model_validator(mode="after")
    def result_contract_is_consistent(self):
        if self.started_at > self.finished_at:
            raise ValueError("started_at must not be after finished_at")
        if (self.manual_id is None) != (self.claim_token is None):
            raise ValueError("manual_id and claim_token must both be null or both be set")
        return self


class AppliedTransition(BaseModel):
    target: Literal["schedule_group", "schedule_detail", "manual_execution"]
    id: int | str
    status: Literal["error", "done", "failed"]


class ExecutionResultData(BaseModel):
    execution_id: int
    duplicate: bool
    applied_transitions: list[AppliedTransition]


class ExecutionResultEnvelope(BaseModel):
    schema_version: Literal["1"] = "1"
    server_time: datetime
    data: ExecutionResultData


class ManualClaimData(BaseModel):
    manual_id: int
    claim_token: str = Field(pattern=CLAIM_TOKEN_PATTERN)
    claim_expires_at: datetime
    execution_grp_id: None = None


class ManualClaimEnvelope(BaseModel):
    schema_version: Literal["1"] = "1"
    server_time: datetime
    data: ManualClaimData


class AgentEventRequest(BaseModel):
    event_type: Literal[
        "startup_error",
        "sync_error",
        "degraded",
        "recovered",
        "outbox_backlog",
    ]
    severity: Literal["info", "warning", "error"]
    occurred_at: datetime
    message: str | None = None
    environment_info: dict[str, Any] | None = None

    @field_validator("occurred_at")
    @classmethod
    def occurred_at_is_utc(cls, value: datetime):
        return _require_utc(value)

    @field_validator("message")
    @classmethod
    def message_is_bounded(cls, value: str | None):
        if value is not None and len(value.encode("utf-8")) > EVENT_MESSAGE_MAX:
            raise ValueError("message exceeds 16384 UTF-8 bytes")
        return value

    @field_validator("environment_info")
    @classmethod
    def event_environment_is_bounded(cls, value):
        encoded = canonical_environment(value)
        if encoded is not None and len(encoded.encode("utf-8")) > ENVIRONMENT_INFO_MAX:
            raise ValueError("environment_info exceeds 32768 UTF-8 bytes")
        return value


class AgentEventData(BaseModel):
    accepted: Literal[True] = True


class AgentEventEnvelope(BaseModel):
    schema_version: Literal["1"] = "1"
    server_time: datetime
    data: AgentEventData
from datetime import datetime
from typing import Literal

from pydantic import BaseModel, Field, field_validator


class EnrollRequest(BaseModel):
    enrollment_token: str = Field(min_length=5, max_length=256)
    device_name: str = Field(min_length=1, max_length=255)
    agent_version: str | None = Field(default=None, max_length=50)

    @field_validator("device_name")
    @classmethod
    def normalize_device_name(cls, value: str) -> str:
        value = value.strip()
        if not value:
            raise ValueError("device_name must not be blank")
        return value


class RefreshRequest(BaseModel):
    refresh_token: str = Field(min_length=5, max_length=256)


class EnrollmentTokenCreateRequest(BaseModel):
    device_name: str | None = Field(default=None, max_length=255)
    group_id: int = Field(ge=0)
    ttl_hours: int = Field(default=24, ge=1, le=168)

    @field_validator("device_name")
    @classmethod
    def normalize_optional_device_name(cls, value: str | None) -> str | None:
        if value is None:
            return None
        value = value.strip()
        return value or None


class EnrollmentTokenData(BaseModel):
    enrollment_id: str
    token: str
    device_name: str | None
    group_id: int
    expires_at: datetime


class EnrollmentTokenListItem(BaseModel):
    enrollment_id: str
    device_name: str | None
    group_id: int
    created_at: datetime
    expires_at: datetime
    used_at: datetime | None
    used_by_device_id: int | None
    revoked_at: datetime | None
    status: Literal["unused", "used", "expired", "revoked"]


class EnrollmentTokenListData(BaseModel):
    items: list[EnrollmentTokenListItem]


class EnrollmentTokenRevokedData(BaseModel):
    enrollment_id: str
    revoked_at: datetime


class AgentTokenData(BaseModel):
    access_token: str
    access_token_expires_at: datetime
    refresh_token: str
    refresh_token_expires_at: datetime
    token_type: str = "bearer"


class EnrollData(AgentTokenData):
    device_id: int
    device_name: str

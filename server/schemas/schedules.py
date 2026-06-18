# schemas/group_schemas.py
from pydantic import BaseModel

class ScheduleGetRequest(BaseModel):
    group_id: int

class ScheduleInsertRequest(BaseModel):
    group_id: int
    name: str | None = None
    year: str | None = None
    month: str | None = None
    day_of_week: str | None = None
    day: str | None = None
    hour: str | None = None
    minute: str | None = None
    second: str | None = None
    is_manual: bool | str = False
    is_error_stop: bool | str = False
    target_device: str | int | None = None
    status: str | None = None
    creator: str | None = None

    is_immediate: bool | str = False
    schedule_datetime: str | None = None
    schedule_id: int | None = None
    detail_id: str | None = None

class ScheduleUpdateRequest(BaseModel):
    group_id: int
    schedule_id: int
    name: str | None = None
    year: str | None = None
    month: str | None = None
    day_of_week: str | None = None
    day: str | None = None
    hour: str | None = None
    minute: str | None = None
    second: str | None = None
    is_manual: bool | str = False
    is_error_stop: bool | str = False
    target_device: str | int | None = None
    status: str | None = None
    modifier: str | None = None

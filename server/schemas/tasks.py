# schemas/group_schemas.py
from pydantic import BaseModel

class TaskGetRequest(BaseModel):
    schedule_id: int | str | None = None

class TaskInsertRequest(BaseModel):
    schedule_name: str | None = None
    schedule_id: int
    task_name: str | None = None
    year: str | None = None
    month: str | None = None
    day_of_week: str | None = None
    day: str | None = None
    hour: str | None = None
    minute: str | None = None
    second: str | None = None
    is_error_stop: bool | str = False
    sequence: int | None = None
    retry_count: int | None = None
    status: str | None = None
    command: str | None = None
    task_type: str | None = None
    archive_type: str | None = None
    source_path: str | None = None
    error_on_missing_source: bool | int | str = False
    destination_path: str | None = None
    date_format: str | None = None
    house_keep_days: int | None = None
    creator: str | None = None

    is_immediate: bool | str = False
    schedule_datetime: str | None = None
    detail_id: str | None = None


class TaskUpdateRequest(BaseModel):
    detail_id: str
    schedule_name: str | None = None
    schedule_id: int
    task_name: str | None = None
    year: str | None = None
    month: str | None = None
    day_of_week: str | None = None
    day: str | None = None
    hour: str | None = None
    minute: str | None = None
    second: str | None = None
    is_error_stop: bool | str = False
    sequence: int | None = None
    retry_count: int | None = None
    status: str | None = None
    command: str | None = None
    task_type: str | None = None
    archive_type: str | None = None
    source_path: str | None = None
    error_on_missing_source: bool | int | str = False
    destination_path: str | None = None
    date_format: str | None = None
    house_keep_days: int | None = None
    modifier: str | None = None

class ScheduleGetRequest(BaseModel):
    group_id: int | str | None = None

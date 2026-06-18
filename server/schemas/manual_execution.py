# schemas/group_schemas.py
from pydantic import BaseModel

class ManualExecutionGetRequest(BaseModel):
    manual_id: int


class ManualExecutionUpdateRequest(BaseModel):
    manual_id: int
    is_immediate: bool | str = False
    schedule_datetime: str = False
    status: str = False
    modifier: str = False

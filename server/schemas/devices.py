# schemas/group_schemas.py
from pydantic import BaseModel

class DeviceGetRequest(BaseModel):
    group_id: int

class DeviceInsertRequest(BaseModel):
    group_id: int
    device_name: str | None = None
    status: str | None = None
    creator: str | None = None

class DeviceUpdateRequest(BaseModel):
    device_id: int
    device_name: str | None = None
    status: str | None = None
    modifier: str | None = None

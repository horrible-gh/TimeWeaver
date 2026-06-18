# schemas/group_schemas.py
from pydantic import BaseModel

class GroupInsertRequest(BaseModel):
    group_name: str
    status: str | None = None
    creator: str | None = None

class GroupUpdateRequest(BaseModel):
    group_id: int
    group_name: str | None = None
    status: str | None = None
    modifier: str | None = None

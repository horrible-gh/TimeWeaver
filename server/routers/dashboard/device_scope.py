"""Resolve dashboard device visibility from the authenticated user, never request input."""

from fastapi import HTTPException


def requester_group_id(db_instance, sqloader, user_id: str) -> int:
    row = db_instance.fetch_one(
        sqloader.load_sql("time_weaver.json", "get_user"),
        (user_id,),
    )
    if not row or row.get("group_id") is None:
        raise HTTPException(
            status_code=401,
            detail={"code": "invalid_user", "message": "Authenticated user not found"},
        )
    return int(row["group_id"])
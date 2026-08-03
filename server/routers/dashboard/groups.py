from fastapi import APIRouter, Depends, HTTPException
from config import settings, db
from schemas.groups import GroupInsertRequest, GroupUpdateRequest  # ✅ Import
from routers.login.auth import verify_token

db_instance = db.db_instance
sqloader = db.sqloader

router = APIRouter()


@router.get("/get_groups", dependencies=[Depends(verify_token)])
async def get_groups():
    return db_instance.fetch_all(sqloader.load_sql("time_weaver.json", "groups.get_groups"))

@router.post("/insert_group", dependencies=[Depends(verify_token)])
async def insert_group(group: GroupInsertRequest):
    query = sqloader.load_sql("time_weaver.json", "groups.insert_group")
    group_data = group.model_dump()
    data = (group_data['group_name'], group_data['creator'])
    return db_instance.execute_query(query, data)

@router.put("/update_group", dependencies=[Depends(verify_token)])
async def update_groups(group: GroupUpdateRequest):
    query = sqloader.load_sql("time_weaver.json", "groups.update_group")
    group_data = group.model_dump()
    print(group_data)
    data = (
        group_data['group_name'],
        group_data['status'],
        group_data['modifier'],
        group_data['group_id'],
    )
    return db_instance.execute_query(query, data)

@router.delete("/remove_group/{group_id}", dependencies=[Depends(verify_token)])
async def remove_group(group_id: int):
    # group 0 ("Unknown") is the reserved fallback every reassignment below
    # lands on; removing it would leave those rows with nowhere safe to go
    # (see NR0007, timeweaver.server.0007.0007-NR).
    if group_id == 0:
        raise HTTPException(
            status_code=400,
            detail={
                "code": "group_reserved",
                "message": "group 0 is reserved and cannot be removed",
            },
        )
    # Reassign every row that still references this group to group 0 before
    # deleting it, in the same transaction as the delete. Without this,
    # devices/users/agent_enrollment_token/schedule_group rows are left
    # pointing at a group_id that no longer exists, which is exactly the
    # orphan data users_004_group_integrity.sql's FK constraints reject on
    # the next startup (NR0007).
    with db_instance.begin_transaction() as txn:
        for reassign_key in (
            "groups.reassign_devices_group",
            "groups.reassign_users_group",
            "groups.reassign_agent_enrollment_token_group",
            "groups.reassign_schedule_group_group",
        ):
            txn.execute(
                sqloader.load_sql("time_weaver.json", reassign_key), (group_id,)
            )
        result = txn.execute(
            sqloader.load_sql("time_weaver.json", "groups.remove_group"),
            (group_id,),
        )
    return result

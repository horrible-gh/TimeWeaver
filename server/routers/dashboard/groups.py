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
    data = {"group_name": group_data['group_name'], "creator": group_data['creator']}
    return db_instance.execute_query(query, data)

@router.put("/update_group", dependencies=[Depends(verify_token)])
async def update_groups(group: GroupUpdateRequest):
    query = sqloader.load_sql("time_weaver.json", "groups.update_group")
    group_data = group.model_dump()
    print(group_data)
    data = {
        "group_name": group_data['group_name'],
        "status": group_data['status'],
        "modifier": group_data['modifier'],
        "group_id": group_data['group_id'],
    }
    return db_instance.execute_query(query, data)

@router.delete("/remove_group/{group_id}", dependencies=[Depends(verify_token)])
async def remove_group(group_id: int):
    query = sqloader.load_sql("time_weaver.json", "groups.remove_group")
    return db_instance.execute_query(query, {"group_id": group_id})

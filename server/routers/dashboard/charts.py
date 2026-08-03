from fastapi import APIRouter, Depends

from config import db
from routers.dashboard.device_scope import requester_group_id
from routers.login.auth import verify_token


db_instance = db.db_instance
sqloader = db.sqloader
router = APIRouter()


@router.get("/devices")
async def devices(user_id: str = Depends(verify_token)):
    group_id = requester_group_id(db_instance, sqloader, user_id)
    if group_id == 0:
        return db_instance.fetch_one(
            sqloader.load_sql("time_weaver.json", "charts.get_chart_devices_all")
        )
    return db_instance.fetch_one(
        sqloader.load_sql("time_weaver.json", "charts.get_chart_devices"),
        (group_id,),
    )


@router.get("/schedules", dependencies=[Depends(verify_token)])
async def schedules():
    return db_instance.fetch_one(sqloader.load_sql("time_weaver.json", "charts.get_chart_schedules"))


@router.get("/tasks", dependencies=[Depends(verify_token)])
async def tasks():
    return db_instance.fetch_one(sqloader.load_sql("time_weaver.json", "charts.get_chart_tasks"))
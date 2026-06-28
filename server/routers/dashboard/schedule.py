from fastapi import APIRouter, Depends, HTTPException
from config import settings, db
from schemas.schedules import ScheduleInsertRequest, ScheduleUpdateRequest, ScheduleGetRequest  # ✅ Import
from routers.login.auth import verify_token
import LogAssist.log as logger
from datetime import datetime, timedelta
from typing import Optional

db_instance = db.db_instance
sqloader = db.sqloader

router = APIRouter()

@router.get("/execution_history", dependencies=[Depends(verify_token)])
async def execution_history(
    start_date: Optional[str] = None,
    end_date: Optional[str] = None
):
    # Default: last month
    if not end_date:
        end_date_dt = datetime.now()
    else:
        end_date_dt = datetime.fromisoformat(end_date.replace('Z', '+00:00'))

    if not start_date:
        start_date_dt = end_date_dt - timedelta(days=30)
    else:
        start_date_dt = datetime.fromisoformat(start_date.replace('Z', '+00:00'))

    # Separate SQL loading and parameter binding
    sql = sqloader.load_sql("time_weaver.json", "get_execution_logs")
    return db_instance.fetch_all(sql, [start_date_dt, end_date_dt])

@router.get("/get_schedule_groups", dependencies=[Depends(verify_token)])
async def get_schedule_groups(schedule: ScheduleGetRequest = Depends()):
    schedule_data = schedule.model_dump()
    data = {"group_id": schedule_data['group_id']}
    return db_instance.fetch_all(sqloader.load_sql("time_weaver.json", "schedules.get_schedule_groups"), data)

@router.post("/insert_schedule", dependencies=[Depends(verify_token)])
async def insert_schedule(schedule: ScheduleInsertRequest):
    query = sqloader.load_sql("time_weaver.json", "schedules.insert_schedule")
    schedule_data = schedule.model_dump()
    data = (
        schedule_data["group_id"],
        schedule_data["name"],
        schedule_data.get("year"),
        schedule_data.get("month"),
        schedule_data.get("day_of_week"),
        schedule_data.get("day"),
        schedule_data.get("hour"),
        schedule_data.get("minute"),
        schedule_data.get("second"),
        schedule_data.get("is_error_stop", False),
        schedule_data["status"],
        schedule_data.get("target_device"),
        schedule_data["creator"]
    )

    return db_instance.execute_query(query, data)

@router.put("/update_schedule", dependencies=[Depends(verify_token)])
async def update_schedules(schedule: ScheduleUpdateRequest):
    query = sqloader.load_sql("time_weaver.json", "schedules.update_schedule")
    schedule_data = schedule.model_dump()
    data = (
        schedule_data["group_id"],
        schedule_data["name"],
        schedule_data.get("year"),
        schedule_data.get("month"),
        schedule_data.get("day_of_week"),
        schedule_data.get("day"),
        schedule_data.get("hour"),
        schedule_data.get("minute"),
        schedule_data.get("second"),
        schedule_data.get("is_error_stop", False),
        schedule_data["status"],
        schedule_data.get("target_device"),
        schedule_data["modifier"],
        schedule_data["schedule_id"],
    )
    logger.debug(query)
    logger.debug(data)
    return db_instance.execute_query(query, data)

@router.delete("/remove_schedule/{schedule_id}", dependencies=[Depends(verify_token)])
async def remove_schedule(schedule_id: int):
    query = sqloader.load_sql("time_weaver.json", "schedules.remove_schedule")
    return db_instance.execute_query(query, {"schedule_id": schedule_id})

@router.get("/get_devices", dependencies=[Depends(verify_token)])
async def get_devices(schedule: ScheduleGetRequest = Depends()):
    schedule_data = schedule.model_dump()
    data = {"group_id": schedule_data['group_id']}
    return db_instance.fetch_all(sqloader.load_sql("time_weaver.json", "schedules.get_devices"), data)


@router.post("/insert_manual_schedule", dependencies=[Depends(verify_token)])
async def insert_manual_schedule(schedule: ScheduleInsertRequest):
    query = sqloader.load_sql("time_weaver.json", "manual_execution.insert_manual_execution")
    schedule_data = schedule.model_dump()
    data = (
        schedule_data["is_immediate"],      # 1. for INSERT
        schedule_data["is_immediate"],      # 2. for CASE condition
        schedule_data["schedule_datetime"], # 3. CASE THEN
        schedule_data.get("status"),        # 4. status
        schedule_data["creator"],           # 5. creator
        schedule_data["schedule_id"],       # 6. first WHERE condition
        schedule_data["schedule_id"],       # 7. WHERE comparison
        None,                               # 8. second WHERE condition
        None                                # 9. WHERE comparison
    )

    return db_instance.execute_query(query, data)

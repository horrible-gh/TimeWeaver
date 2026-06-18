from fastapi import APIRouter, Depends, HTTPException
from config import settings, db
from schemas.tasks import TaskInsertRequest, TaskUpdateRequest, TaskGetRequest, ScheduleGetRequest  # ✅ Import
from routers.login.auth import verify_token
import LogAssist.log as logger
import uuid

db_instance = db.db_instance
sqloader = db.sqloader

router = APIRouter()

@router.get("/get_tasks", dependencies=[Depends(verify_token)])
async def get_Tasks(task: TaskGetRequest = Depends()):
    task_data = task.model_dump()
    #data = {"group_id": task_data['group_id']}
    data = {}
    return db_instance.fetch_all(sqloader.load_sql("time_weaver.json", "tasks.get_tasks"), data)


@router.get("/get_schedule_groups", dependencies=[Depends(verify_token)])
async def get_schedule_groups(schedule: ScheduleGetRequest = Depends()):
    schedule_data = schedule.model_dump()
    data = {"group_id": schedule_data['group_id']}
    return db_instance.fetch_all(sqloader.load_sql("time_weaver.json", "schedules.get_schedule_groups"), data)


@router.post("/insert_task", dependencies=[Depends(verify_token)])
async def insert_task(task: TaskInsertRequest):
    row = task.model_dump()
    detail_id = uuid.uuid4()

    query = sqloader.load_sql("time_weaver.json", "tasks.insert_schedule_detail")
    schedule_detail_data = (
        str(detail_id),
        row.get("task_name"),
        row["schedule_id"],
        row.get("year"),
        row.get("month"),
        row.get("day_of_week"),
        row.get("day"),
        row.get("hour"),
        row.get("minute"),
        row.get("second"),
        bool(row.get("is_error_stop", 1)),
        row.get("sequence", 0),
        row.get("retry_count", 0),
        row.get("status"),
        row.get("creator"),
    )
    db_instance.execute_query(query, schedule_detail_data)

    task_data = (
        str(detail_id),  # detail_id - UUID를 문자열로 변환
        row.get("command"),  # command
        row.get("task_type"),  # task_type
        row.get("archive_type"),  # archive_type
        row.get("source_path"),  # source_path
        bool(row.get("error_on_missing_source", 1)),  # error_on_missing_source
        row.get("destination_path"),  # destination_path
        row.get("date_format"),  # date_format
        row.get("target_date_format"),  # target_date_format
        row.get("destination_date_format"),  # destination_date_format
        row.get("house_keep_days"),  # house_keep_days
        row.get("creator"),  # creator
    )
    query = sqloader.load_sql("time_weaver.json", "tasks.insert_task")
    return db_instance.execute_query(query, task_data)

@router.put("/update_task", dependencies=[Depends(verify_token)])
async def update_tasks(task: TaskUpdateRequest):
    row = task.model_dump()
    query = sqloader.load_sql("time_weaver.json", "tasks.update_schedule_detail")
    schedule_detail_data = {
        "schedule_name": row.get("task_name"),
        "schedule_id": row["schedule_id"],
        "year": row.get("year"),
        "month": row.get("month"),
        "day_of_week": row.get("day_of_week"),
        "day": row.get("day"),
        "hour": row.get("hour"),
        "minute": row.get("minute"),
        "second": row.get("second"),
        "is_error_stop": bool(row.get("is_error_stop", 1)),
        "sequence": row.get("sequence", 0),
        "retry_count": row.get("retry_count", 0),
        "status": row.get("status"),
        "modifier": row.get("modifier"),
        "detail_id": row.get("detail_id"),
    }
    # 딕셔너리 값을 튜플로 변환 (SQL의 %s 순서대로)
    schedule_detail_tuple = (
        schedule_detail_data["schedule_name"],
        schedule_detail_data["schedule_id"],
        schedule_detail_data["year"],
        schedule_detail_data["month"],
        schedule_detail_data["day_of_week"],
        schedule_detail_data["day"],
        schedule_detail_data["hour"],
        schedule_detail_data["minute"],
        schedule_detail_data["second"],
        schedule_detail_data["is_error_stop"],
        schedule_detail_data["sequence"],
        schedule_detail_data["retry_count"],
        schedule_detail_data["status"],
        schedule_detail_data["modifier"],
        schedule_detail_data["detail_id"],
    )
    db_instance.execute_query(query, schedule_detail_tuple)

    task_data = {
        "command": row.get("command"),
        "task_type": row.get("task_type"),
        "archive_type": row.get("archive_type"),
        "source_path": row.get("source_path"),
        "error_on_missing_source": bool(row.get("error_on_missing_source", 1)),
        "destination_path": row.get("destination_path"),
        "date_format": row.get("date_format"),
        "target_date_format": row.get("target_date_format"),
        "destination_date_format": row.get("destination_date_format"),
        "house_keep_days": row.get("house_keep_days"),
        "modifier": row.get("modifier"),
        "detail_id": row.get("detail_id"),
    }
    # 딕셔너리 값을 튜플로 변환 (SQL의 %s 순서대로)
    task_tuple = (
        task_data["command"],
        task_data["task_type"],
        task_data["archive_type"],
        task_data["source_path"],
        task_data["error_on_missing_source"],
        task_data["destination_path"],
        task_data["date_format"],
        task_data["target_date_format"],
        task_data["destination_date_format"],
        task_data["house_keep_days"],
        task_data["modifier"],
        task_data["detail_id"],
    )
    query = sqloader.load_sql("time_weaver.json", "tasks.update_task")
    return db_instance.execute_query(query, task_tuple)

@router.delete("/remove_task/{task_id}", dependencies=[Depends(verify_token)])
async def remove_Task(task_id: str):
    query = sqloader.load_sql("time_weaver.json", "tasks.remove_task")
    result_task = db_instance.execute_query(query, {"task_id": task_id})
    query = sqloader.load_sql("time_weaver.json", "tasks.remove_schedule_detail")
    result_detail = db_instance.execute_query(query, {"task_id": task_id})
    return result_task & result_detail


@router.post("/insert_manual_task", dependencies=[Depends(verify_token)])
async def insert_manual_task(schedule: TaskInsertRequest):
    query = sqloader.load_sql("time_weaver.json", "manual_execution.insert_manual_execution")
    schedule_data = schedule.model_dump()
    data = (
        schedule_data["is_immediate"],      # 1. INSERT용
        schedule_data["is_immediate"],      # 2. CASE 조건용
        schedule_data["schedule_datetime"], # 3. CASE THEN
        schedule_data.get("status"),        # 4. status
        schedule_data["creator"],           # 5. creator
        None,                               # 6. WHERE 두번째 조건
        None,                               # 7. WHERE 비교
        schedule_data["detail_id"],         # 8. WHERE 첫번째 조건
        schedule_data["detail_id"],         # 9. WHERE 비교
    )

    return db_instance.execute_query(query, data)
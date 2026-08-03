from fastapi import APIRouter, Depends, HTTPException
from config import settings, db
from schemas.tasks import TaskInsertRequest, TaskUpdateRequest, TaskGetRequest, ScheduleGetRequest  # ✅ Import
from routers.login.auth import verify_token
from services.blocking import BlockingQueueFull, RETRY_AFTER, run_blocking
from services.task_contract import normalize_task_row
import LogAssist.log as logger
import uuid

db_instance = db.db_instance
sqloader = db.sqloader

router = APIRouter()


async def _db_call(function, *args):
    try:
        return await run_blocking(function, *args)
    except BlockingQueueFull as exc:
        raise HTTPException(
            status_code=503,
            detail={"code": "unavailable", "message": "Service is busy"},
            headers={"Retry-After": str(RETRY_AFTER)},
        ) from exc


def to_bool(value, default=True):
    """The form posts "0"/"1" as strings, and bool("0") is True, so a plain
    bool() turns every "No" into a "Yes"."""
    if value is None:
        return default
    if isinstance(value, bool):
        return value
    if isinstance(value, int):
        return value != 0
    return str(value).strip().lower() not in ("0", "false", "no", "")


def _normalize_task_row(row: dict) -> dict:
    normalized = normalize_task_row(row)
    if (
        normalized.get("task_type") == "archive"
        and normalized.get("archive_type") != "zip"
    ):
        raise HTTPException(
            status_code=422,
            detail={
                "code": "invalid_archive_type",
                "message": "archive_type must be zip for archive tasks",
            },
        )
    return normalized

@router.get("/get_tasks", dependencies=[Depends(verify_token)])
async def get_Tasks(task: TaskGetRequest = Depends()):
    return await _db_call(
        db_instance.fetch_all,
        sqloader.load_sql("time_weaver.json", "tasks.get_tasks"),
    )


@router.get("/get_schedule_groups", dependencies=[Depends(verify_token)])
async def get_schedule_groups(schedule: ScheduleGetRequest = Depends()):
    schedule_data = schedule.model_dump()
    data = (schedule_data['group_id'],)
    return await _db_call(db_instance.fetch_all, sqloader.load_sql("time_weaver.json", "schedules.get_schedule_groups"), data)


@router.post("/insert_task", dependencies=[Depends(verify_token)])
async def insert_task(task: TaskInsertRequest):
    row = _normalize_task_row(task.model_dump())
    detail_id = uuid.uuid4()

    detail_query = sqloader.load_sql("time_weaver.json", "tasks.insert_schedule_detail")
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
        to_bool(row.get("is_error_stop")),
        row.get("sequence", 0),
        row.get("retry_count", 0),
        row.get("status"),
        row.get("creator"),
    )

    task_data = (
        str(detail_id),  # detail_id - Convert UUID to string
        row.get("command"),  # command
        row.get("task_type"),  # task_type
        row.get("archive_type"),  # archive_type
        row.get("source_path"),  # source_path
        to_bool(row.get("error_on_missing_source")),  # error_on_missing_source
        row.get("destination_path"),  # destination_path
        row.get("date_format"),  # date_format
        row.get("target_date_format"),  # target_date_format
        row.get("destination_date_format"),  # destination_date_format
        row.get("house_keep_days"),  # house_keep_days
        row.get("creator"),  # creator
    )
    task_query = sqloader.load_sql("time_weaver.json", "tasks.insert_task")

    # Both rows must land together. Committing them separately leaves an orphan
    # schedule_detail row behind every time the task_detail insert fails, and
    # get_tasks LEFT JOINs task_detail so the orphan shows up as an empty task.
    def write_task():
        with db_instance.begin_transaction() as txn:
            txn.execute(detail_query, schedule_detail_data)
            return txn.execute(task_query, task_data)

    return await _db_call(write_task)

@router.put("/update_task", dependencies=[Depends(verify_token)])
async def update_tasks(task: TaskUpdateRequest):
    row = _normalize_task_row(task.model_dump())
    detail_query = sqloader.load_sql("time_weaver.json", "tasks.update_schedule_detail")
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
        "is_error_stop": to_bool(row.get("is_error_stop")),
        "sequence": row.get("sequence", 0),
        "retry_count": row.get("retry_count", 0),
        "status": row.get("status"),
        "modifier": row.get("modifier"),
        "detail_id": row.get("detail_id"),
    }
    # Convert dictionary values to tuples in SQL %s order
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

    task_data = {
        "command": row.get("command"),
        "task_type": row.get("task_type"),
        "archive_type": row.get("archive_type"),
        "source_path": row.get("source_path"),
        "error_on_missing_source": to_bool(row.get("error_on_missing_source")),
        "destination_path": row.get("destination_path"),
        "date_format": row.get("date_format"),
        "target_date_format": row.get("target_date_format"),
        "destination_date_format": row.get("destination_date_format"),
        "house_keep_days": row.get("house_keep_days"),
        "modifier": row.get("modifier"),
        "detail_id": row.get("detail_id"),
    }
    # Convert dictionary values to tuples in SQL %s order
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
    task_query = sqloader.load_sql("time_weaver.json", "tasks.update_task")

    def write_task():
        with db_instance.begin_transaction() as txn:
            txn.execute(detail_query, schedule_detail_tuple)
            return txn.execute(task_query, task_tuple)

    return await _db_call(write_task)

@router.delete("/remove_task/{task_id}", dependencies=[Depends(verify_token)])
async def remove_Task(task_id: str):
    query = sqloader.load_sql("time_weaver.json", "tasks.remove_task")
    def remove_rows():
        detail_query = sqloader.load_sql("time_weaver.json", "tasks.remove_schedule_detail")
        with db_instance.begin_transaction() as txn:
            result_task = txn.execute(query, (task_id,))
            result_detail = txn.execute(detail_query, (task_id,))
            return result_task and result_detail

    return await _db_call(remove_rows)


@router.post("/insert_manual_task", dependencies=[Depends(verify_token)])
async def insert_manual_task(schedule: TaskInsertRequest):
    query = sqloader.load_sql("time_weaver.json", "manual_execution.insert_manual_execution")
    schedule_data = schedule.model_dump()
    data = (
        schedule_data["is_immediate"],      # 1. for INSERT
        schedule_data["is_immediate"],      # 2. for CASE condition
        schedule_data["schedule_datetime"], # 3. CASE THEN
        schedule_data.get("status"),        # 4. status
        schedule_data["creator"],           # 5. creator
        None,                               # 6. second WHERE condition
        None,                               # 7. WHERE comparison
        schedule_data["detail_id"],         # 8. first WHERE condition
        schedule_data["detail_id"],         # 9. WHERE comparison
    )

    return await _db_call(db_instance.execute_query, query, data)
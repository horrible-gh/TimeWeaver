"""Unauthenticated A8 readiness probe isolated from the business DB pool."""
import asyncio
from concurrent.futures import ThreadPoolExecutor

from fastapi import APIRouter
from fastapi.responses import JSONResponse

from config import db, settings


router = APIRouter()
_health_executor = ThreadPoolExecutor(max_workers=2, thread_name_prefix="timeweaver-health")
_REQUIRED_TABLES = (
    "devices",
    "groups",
    "schedule_group",
    "schedule_detail",
    "task_detail",
    "manual_execution",
    "execution_log",
    "agent_device_credential",
    "agent_enrollment_token",
    "agent_event",
)


def database_ready(db_instance=None) -> bool:
    database = db_instance or db.db_instance
    quoted = ", ".join(f"'{name}'" for name in _REQUIRED_TABLES)
    row = database.fetch_one(
        "SELECT COUNT(*) AS table_count FROM information_schema.tables "
        "WHERE table_schema = DATABASE() AND table_name IN (" + quoted + ")"
    )
    return bool(row) and int(row["table_count"]) == len(_REQUIRED_TABLES)


@router.get("/health")
async def health():
    loop = asyncio.get_running_loop()
    try:
        ready = await asyncio.wait_for(
            loop.run_in_executor(_health_executor, database_ready),
            timeout=float(getattr(settings, "DB_HEALTH_TIMEOUT", 1.0)),
        )
    except Exception:
        ready = False
    if ready:
        return {"ready": True}
    retry_after = int(getattr(settings, "HEALTH_RETRY_AFTER", 5))
    return JSONResponse(
        status_code=503,
        content={"ready": False},
        headers={"Retry-After": str(retry_after)},
    )
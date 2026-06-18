from fastapi import APIRouter, Depends, HTTPException
from config import settings, db
from routers.login.auth import verify_token

db_instance = db.db_instance
sqloader = db.sqloader

router = APIRouter()

@router.get("/lastest-schedules", dependencies=[Depends(verify_token)])
async def lastest_schedules():
    return db_instance.fetch_all(sqloader.load_sql("time_weaver.json", "get_lastest_schedules"))


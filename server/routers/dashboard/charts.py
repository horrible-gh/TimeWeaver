from fastapi import APIRouter, Depends, HTTPException
from config import settings, db
from routers.login.auth import verify_token

db_instance = db.db_instance
sqloader = db.sqloader

router = APIRouter()

@router.get("/devices", dependencies=[Depends(verify_token)])
async def devices():
    return db_instance.fetch_one(sqloader.load_sql("time_weaver.json", "charts.get_chart_devices"))

@router.get("/schedules", dependencies=[Depends(verify_token)])
async def schedules():
    return db_instance.fetch_one(sqloader.load_sql("time_weaver.json", "charts.get_chart_schedules"))

@router.get("/tasks", dependencies=[Depends(verify_token)])
async def tasks():
    return db_instance.fetch_one(sqloader.load_sql("time_weaver.json", "charts.get_chart_tasks"))

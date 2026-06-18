from fastapi import APIRouter, Depends, HTTPException
from config import settings, db
from schemas.manual_execution import ManualExecutionUpdateRequest, ManualExecutionGetRequest  # ✅ Import
from routers.login.auth import verify_token
import LogAssist.log as logger

db_instance = db.db_instance
sqloader = db.sqloader

router = APIRouter()

@router.get("/get_manual_execution_list", dependencies=[Depends(verify_token)])
async def get_manual_execution_list():
    return db_instance.fetch_all(sqloader.load_sql("time_weaver.json", "manual_execution.get_manual_execution"))



@router.put("/update_manual_execution", dependencies=[Depends(verify_token)])
async def update_manual_execution(param: ManualExecutionUpdateRequest):
    query = sqloader.load_sql("time_weaver.json", "manual_execution.update_manual_execution")
    param_data = param.model_dump()
    data = {
        "is_immediate": param_data['is_immediate'],
        "schedule_datetime": param_data['schedule_datetime'],
        "status": param_data['status'],
        "modifier": param_data['modifier'],
        "manual_id": param_data['manual_id'],
    }
    return db_instance.execute_query(query, data)

@router.put("/abandon_manual_execution", dependencies=[Depends(verify_token)])
async def abandon_manual_execution(param: ManualExecutionUpdateRequest):
    query = sqloader.load_sql("time_weaver.json", "manual_execution.update_abandon_manual_execution")
    param_data = param.model_dump()
    data = {
        "status": 'failed',
        "modifier": param_data['modifier'],
        "manual_id": param_data['manual_id'],
    }
    return db_instance.execute_query(query, data)


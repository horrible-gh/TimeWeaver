from fastapi import APIRouter, Depends, HTTPException, Query
from config import settings, db
from schemas.devices import DeviceInsertRequest, DeviceUpdateRequest, DeviceGetRequest  # ✅ Import
from routers.login.auth import verify_token

db_instance = db.db_instance
sqloader = db.sqloader

router = APIRouter()

@router.get("/get_devices", dependencies=[Depends(verify_token)])
async def get_devices(device: DeviceGetRequest = Depends()):
    device_data = device.model_dump()
    data = {"group_id": device_data['group_id']}
    return db_instance.fetch_all(sqloader.load_sql("time_weaver.json", "devices.get_devices"), data)

@router.post("/insert_device", dependencies=[Depends(verify_token)])
async def insert_device(device: DeviceInsertRequest):
    query = sqloader.load_sql("time_weaver.json", "devices.insert_device")
    device_data = device.model_dump()
    data = {
        "group_id": device_data['group_id']
        , "device_name": device_data['device_name']
        , "status": device_data['status']
        , "creator": device_data['creator']
    }
    return db_instance.execute_query(query, data)

@router.put("/update_device", dependencies=[Depends(verify_token)])
async def update_devices(device: DeviceUpdateRequest):
    query = sqloader.load_sql("time_weaver.json", "devices.update_device")
    device_data = device.model_dump()
    print(device_data)
    data = {
        "device_name": device_data['device_name'],
        "status": device_data['status'],
        "modifier": device_data['modifier'],
        "device_id": device_data['device_id'],
    }
    return db_instance.execute_query(query, data)

@router.delete("/remove_device/{device_id}", dependencies=[Depends(verify_token)])
async def remove_device(device_id: int):
    query = sqloader.load_sql("time_weaver.json", "devices.remove_device")
    return db_instance.execute_query(query, {"device_id": device_id})

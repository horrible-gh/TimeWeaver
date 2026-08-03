from fastapi import APIRouter, Depends, HTTPException
from pymysql.err import IntegrityError

from config import db
from routers.dashboard.device_scope import requester_group_id
from routers.login.auth import verify_token
from schemas.devices import DeviceGetRequest, DeviceInsertRequest, DeviceUpdateRequest


db_instance = db.db_instance
sqloader = db.sqloader
router = APIRouter()


@router.get("/get_devices")
async def get_devices(
    device: DeviceGetRequest = Depends(),
    user_id: str = Depends(verify_token),
):
    # device.group_id remains accepted for API compatibility, but it never
    # controls visibility. Scope comes from the authenticated user.
    group_id = requester_group_id(db_instance, sqloader, user_id)
    if group_id == 0:
        return db_instance.fetch_all(
            sqloader.load_sql("time_weaver.json", "devices.get_all_devices")
        )
    return db_instance.fetch_all(
        sqloader.load_sql("time_weaver.json", "devices.get_devices"),
        (group_id,),
    )


@router.post("/insert_device", dependencies=[Depends(verify_token)])
async def insert_device(device: DeviceInsertRequest):
    device_data = device.model_dump()
    group = db_instance.fetch_one(
        sqloader.load_sql("time_weaver.json", "groups.get_group"),
        (device_data["group_id"],),
    )
    if not group:
        raise HTTPException(
            status_code=422,
            detail={
                "code": "group_not_found",
                "message": "group_id does not reference an existing group",
            },
        )
    query = sqloader.load_sql("time_weaver.json", "devices.insert_device")
    data = (
        device_data["group_id"],
        device_data["device_name"],
        device_data["status"],
        device_data["creator"],
    )
    return db_instance.execute_query(query, data)


@router.put("/update_device", dependencies=[Depends(verify_token)])
async def update_devices(device: DeviceUpdateRequest):
    query = sqloader.load_sql("time_weaver.json", "devices.update_device")
    device_data = device.model_dump()
    data = (
        device_data["device_name"],
        device_data["status"],
        device_data["modifier"],
        device_data["device_id"],
    )
    try:
        return db_instance.execute_query(query, data)
    except IntegrityError as exc:
        if exc.args and exc.args[0] == 1062:
            raise HTTPException(
                status_code=409,
                detail={
                    "code": "device_name_conflict",
                    "message": "Device name is already used in this group",
                },
            ) from exc
        raise


@router.delete("/remove_device/{device_id}", dependencies=[Depends(verify_token)])
async def remove_device(device_id: int):
    query = sqloader.load_sql("time_weaver.json", "devices.remove_device")
    return db_instance.execute_query(query, (device_id,))
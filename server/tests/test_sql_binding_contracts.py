"""Binding contracts for dashboard SQL with unnamed ``%s`` placeholders."""
import asyncio

import pymysql
import pytest
from fastapi import HTTPException
from sqloader import SQLoader

from conftest import FakeDbInstance, SQLOADER_JSON
from schemas.devices import DeviceGetRequest, DeviceInsertRequest, DeviceUpdateRequest
from schemas.groups import GroupInsertRequest, GroupUpdateRequest
from schemas.manual_execution import ManualExecutionUpdateRequest
from schemas.schedules import ScheduleGetRequest
from schemas.tasks import ScheduleGetRequest as TaskScheduleGetRequest
from schemas.tasks import TaskGetRequest


def test_validating_fake_rejects_dict_for_unnamed_placeholder():
    db = FakeDbInstance()

    with pytest.raises(AssertionError, match="tuple/list"):
        db.execute_query("SELECT * FROM devices WHERE group_id = %s", {"group_id": 5})


def test_devices_bind_all_four_calls_in_sql_order(make_router_module):
    module, db = make_router_module("devices")

    rows = asyncio.run(module.get_devices(DeviceGetRequest(group_id=999), user_id="admin"))
    asyncio.run(module.insert_device(DeviceInsertRequest(
        group_id=5, device_name="edge-5", status="active", creator="creator-5"
    )))
    asyncio.run(module.update_devices(DeviceUpdateRequest(
        device_id=17, device_name="edge-17", status="inactive", modifier="modifier-17"
    )))
    asyncio.run(module.remove_device(17))

    assert rows == []
    assert [call[1] for call in db.fetch_all_calls] == [(5,)]
    assert [params for _, params in db.execute_query_calls] == [
        (5, "edge-5", "active", "creator-5"),
        ("edge-17", "inactive", "modifier-17", 17),
        (17,),
    ]


def test_groups_bind_all_three_calls_in_sql_order(make_router_module):
    module, db = make_router_module("groups")

    asyncio.run(module.insert_group(GroupInsertRequest(
        group_name="operators", creator="creator-5"
    )))
    asyncio.run(module.update_groups(GroupUpdateRequest(
        group_id=5, group_name="operators-2", status="active", modifier="modifier-5"
    )))
    asyncio.run(module.remove_group(5))

    assert [params for _, params in db.execute_query_calls] == [
        ("operators", "creator-5"),
        ("operators-2", "active", "modifier-5", 5),
    ]
    # remove_group now reassigns every referencing table to group 0 and
    # deletes the group in one transaction (NR0007) instead of a bare
    # autocommitting DELETE.
    assert len(db.transactions) == 1
    assert [(q.split(None, 1)[0], params) for q, params in db.transactions[0].pending] == [
        ("UPDATE", (5,)),
        ("UPDATE", (5,)),
        ("UPDATE", (5,)),
        ("UPDATE", (5,)),
        ("DELETE", (5,)),
    ]


def test_manual_execution_binds_both_updates_in_sql_order(make_router_module):
    module, db = make_router_module("manual_execution")
    request = ManualExecutionUpdateRequest(
        manual_id=23,
        is_immediate=True,
        schedule_datetime="2026-08-02 12:00:00",
        status="active",
        modifier="modifier-23",
    )

    asyncio.run(module.update_manual_execution(request))
    asyncio.run(module.abandon_manual_execution(request))

    assert [params for _, params in db.execute_query_calls] == [
        (True, "2026-08-02 12:00:00", "active", "modifier-23", 23),
        ("failed", "modifier-23", 23),
    ]


def test_schedule_binds_all_three_corrected_calls(make_router_module):
    module, db = make_router_module("schedule")
    request = ScheduleGetRequest(group_id=5)

    asyncio.run(module.get_schedule_groups(request))
    asyncio.run(module.remove_schedule(41))
    asyncio.run(module.get_devices(request, user_id="admin"))

    assert [call[1] for call in db.fetch_all_calls] == [(5,), (5,)]
    assert [params for _, params in db.execute_query_calls] == [(41,)]


def test_tasks_use_no_params_for_get_and_tuple_params_for_three_calls(make_router_module):
    module, db = make_router_module("tasks")

    asyncio.run(module.get_Tasks(TaskGetRequest()))
    asyncio.run(module.get_schedule_groups(TaskScheduleGetRequest(group_id=5)))
    asyncio.run(module.remove_Task("3f2b1c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d"))

    assert len(db.fetch_all_calls[0]) == 1
    assert db.fetch_all_calls[1][1] == (5,)
    assert [params for _, params in db.committed] == [
        ("3f2b1c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d",),
        ("3f2b1c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d",),
    ]


def test_sqloader_extension_forms_resolve_the_same_sql():
    loader = SQLoader(str(SQLOADER_JSON.parent))

    assert loader.load_sql("time_weaver", "devices.get_devices") == loader.load_sql(
        "time_weaver.json", "devices.get_devices"
    )


def test_pymysql_mogrify_accepts_corrected_device_bindings():
    """Exercise the pinned PyMySQL formatter without requiring a live server."""
    loader = SQLoader(str(SQLOADER_JSON.parent))
    connection = pymysql.Connection(defer_connect=True)
    connection.server_status = 0
    cursor = connection.cursor()

    get_sql = cursor.mogrify(
        loader.load_sql("time_weaver", "devices.get_devices"), (5,)
    )
    insert_sql = cursor.mogrify(
        loader.load_sql("time_weaver", "devices.insert_device"),
        (5, "edge-5", "active", "creator-5"),
    )

    assert "group_id = 5" in get_sql
    assert "{'group_id': '5'}" not in get_sql
    assert "VALUES(5, 'edge-5', 'active', 'creator-5')" in insert_sql


def test_hidden_group_bypasses_device_and_chart_group_filters(make_router_module):
    devices_module, devices_db = make_router_module("devices")
    devices_db.user_group_id = 0
    asyncio.run(
        devices_module.get_devices(DeviceGetRequest(group_id=777), user_id="admin")
    )
    assert len(devices_db.fetch_all_calls[-1]) == 1
    assert "group_id = %s" not in devices_db.fetch_all_calls[-1][0]

    charts_module, charts_db = make_router_module("charts")
    charts_db.user_group_id = 0
    asyncio.run(charts_module.devices(user_id="admin"))
    assert len(charts_db.fetch_one_calls[-1]) == 1
    assert "group_id = %s" not in charts_db.fetch_one_calls[-1][0]


def test_duplicate_device_name_is_mapped_to_conflict(make_router_module):
    module, db = make_router_module("devices")

    def duplicate(_query, _params):
        raise pymysql.err.IntegrityError(1062, "duplicate")

    db.execute_query = duplicate
    request = DeviceUpdateRequest(
        device_id=17,
        device_name="already-used",
        status="active",
        modifier="admin",
    )
    with pytest.raises(HTTPException) as raised:
        asyncio.run(module.update_devices(request))
    assert raised.value.status_code == 409
    assert raised.value.detail["code"] == "device_name_conflict"
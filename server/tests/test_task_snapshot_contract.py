"""End-to-end regression for dashboard task writes and agent snapshot validation."""

import asyncio
from datetime import datetime

from fastapi.encoders import jsonable_encoder

from agent.services.time_weaver.sync_coordinator import validate_snapshot
from repositories.agent_runtime import SnapshotRows
from schemas.tasks import TaskInsertRequest
from services.agent.identity_service import DeviceIdentity
from services.agent.runtime_service import AgentRuntimeService


def _params(calls, fragment):
    return next(params for query, params in calls if fragment in query)


def _payload():
    return {
        "schedule_id": 12,
        "task_name": "command-default-contract",
        "year": "*",
        "month": "*",
        "day_of_week": "*",
        "day": "*",
        "hour": "*",
        "minute": "*/1",
        "second": "0",
        "is_error_stop": True,
        "sequence": 0,
        "retry_count": 0,
        "status": "active",
        "command": "echo ok",
        "task_type": "command",
        "archive_type": "null",
        "source_path": "",
        "destination_path": "",
        "house_keep_days": None,
        "creator": "tester",
    }


def _snapshot_rows(schedule_params, task_params, *, archive_type):
    detail_id = task_params[0]
    detail = {
        "detail_id": detail_id,
        "task_detail_id": detail_id,
        "schedule_id": schedule_params[2],
        "schedule_name": schedule_params[1],
        "year": schedule_params[3],
        "month": schedule_params[4],
        "day_of_week": schedule_params[5],
        "day": schedule_params[6],
        "hour": schedule_params[7],
        "minute": schedule_params[8],
        "second": schedule_params[9],
        "is_error_stop": schedule_params[10],
        "sequence": schedule_params[11],
        "retry_count": schedule_params[12],
        "command": task_params[1],
        "task_type": task_params[2],
        "archive_type": archive_type,
        "source_path": task_params[4],
        "error_on_missing_source": task_params[5],
        "destination_path": task_params[6],
        "date_format": task_params[7],
        "target_date_format": task_params[8],
        "destination_date_format": task_params[9],
        "house_keep_days": task_params[10],
    }
    return SnapshotRows(
        db_now=datetime(2026, 8, 3, 11, 0, 0),
        device={
            "device_id": 7,
            "device_name": "dev252",
            "status": "active",
            "version": "test",
        },
        groups=[
            {
                "schedule_id": 12,
                "name": "every-minute",
                "target_device": 7,
                "status": "active",
                "year": "*",
                "month": "*",
                "day_of_week": "*",
                "day": "*",
                "hour": "*",
                "minute": "*/1",
                "second": "0",
                "is_error_stop": True,
            }
        ],
        details=[detail],
        manuals=[],
    )


class SnapshotRepository:
    def __init__(self, rows):
        self.rows = rows

    def load_snapshot(self, device_id):
        assert device_id == 7
        return self.rows


def _validate_service_snapshot(rows):
    result = AgentRuntimeService(SnapshotRepository(rows)).snapshot(
        DeviceIdentity(7, "dev252", 7)
    )
    envelope = jsonable_encoder(
        {
            "schema_version": "1",
            "server_time": result.generated_at,
            "data": result.data,
        }
    )
    validated = validate_snapshot(
        {"envelope": envelope, "etag": result.etag},
        (7, "dev252"),
    )
    return result, validated


def test_command_default_write_snapshot_and_agent_validation(make_tasks_module):
    tasks_module, db = make_tasks_module()

    asyncio.run(tasks_module.insert_task(TaskInsertRequest(**_payload())))

    schedule_params = _params(db.committed, "INSERT INTO schedule_detail")
    task_params = _params(db.committed, "INSERT INTO task_detail")
    assert task_params[3] is None
    rows = _snapshot_rows(schedule_params, task_params, archive_type=task_params[3])

    result, validated = _validate_service_snapshot(rows)

    assert result.data["schedules"][0]["details"][0]["task"]["archive_type"] is None
    assert validated.schedules[0].details[0].archive_type is None


def test_snapshot_defensively_normalizes_legacy_command_sentinel(make_tasks_module):
    tasks_module, db = make_tasks_module()
    asyncio.run(tasks_module.insert_task(TaskInsertRequest(**_payload())))
    schedule_params = _params(db.committed, "INSERT INTO schedule_detail")
    task_params = _params(db.committed, "INSERT INTO task_detail")
    rows = _snapshot_rows(schedule_params, task_params, archive_type="null")

    result, validated = _validate_service_snapshot(rows)

    assert result.data["schedules"][0]["details"][0]["task"]["archive_type"] is None
    assert validated.schedules[0].details[0].archive_type is None
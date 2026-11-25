"""Dashboard task writes validated by the independent Go agent's real decoder.

Set TW_AGENT_CONTRACT_VALIDATOR to the binary built from
scripts/testdata/agent-runtime-validator.go in the pinned TimeWeaver-Agent
checkout. CI also sets TW_REQUIRE_AGENT_CONTRACT_VALIDATOR=1 so a missing binary
fails instead of skipping these cross-project contract tests.
"""

import asyncio
from datetime import datetime
import json
import os
from pathlib import Path
import subprocess

from fastapi.encoders import jsonable_encoder
import pytest

from repositories.agent_runtime import SnapshotRows
from schemas.tasks import TaskInsertRequest
from services.agent.identity_service import DeviceIdentity
from services.agent.runtime_service import AgentRuntimeService


@pytest.fixture(scope="module")
def agent_validator():
    binary = os.environ.get("TW_AGENT_CONTRACT_VALIDATOR")
    if not binary:
        message = "Set TW_AGENT_CONTRACT_VALIDATOR to the pinned Go-agent decoder binary"
        if os.environ.get("TW_REQUIRE_AGENT_CONTRACT_VALIDATOR") == "1":
            pytest.fail(message)
        pytest.skip(message)
    path = Path(binary).resolve()
    assert path.is_file(), "Pinned Go-agent contract validator is missing: " + str(path)
    return str(path)


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


def _validate_service_snapshot(rows, agent_validator):
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
    decoded = subprocess.run(
        [agent_validator],
        input=json.dumps({
            "envelope": envelope,
            "etag": result.etag,
            "identity": {"device_id": 7, "device_name": "dev252"},
        }),
        text=True,
        capture_output=True,
        timeout=10,
        check=False,
    )
    assert decoded.returncode == 0, "Go-agent decoder rejected snapshot: " + decoded.stderr
    return result


def test_command_default_write_snapshot_and_agent_validation(make_tasks_module, agent_validator):
    tasks_module, db = make_tasks_module()

    asyncio.run(tasks_module.insert_task(TaskInsertRequest(**_payload())))

    schedule_params = _params(db.committed, "INSERT INTO schedule_detail")
    task_params = _params(db.committed, "INSERT INTO task_detail")
    assert task_params[3] is None
    rows = _snapshot_rows(schedule_params, task_params, archive_type=task_params[3])

    result = _validate_service_snapshot(rows, agent_validator)

    assert result.data["schedules"][0]["details"][0]["task"]["archive_type"] is None


def test_snapshot_defensively_normalizes_legacy_command_sentinel(make_tasks_module, agent_validator):
    tasks_module, db = make_tasks_module()
    asyncio.run(tasks_module.insert_task(TaskInsertRequest(**_payload())))
    schedule_params = _params(db.committed, "INSERT INTO schedule_detail")
    task_params = _params(db.committed, "INSERT INTO task_detail")
    rows = _snapshot_rows(schedule_params, task_params, archive_type="null")

    result = _validate_service_snapshot(rows, agent_validator)

    assert result.data["schedules"][0]["details"][0]["task"]["archive_type"] is None

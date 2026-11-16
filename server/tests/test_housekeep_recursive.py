"""Opt-in housekeeping from dashboard writes through snapshot and admission."""
import asyncio
import json
import os
import sqlite3
from pathlib import Path

import pytest
from fastapi.encoders import jsonable_encoder
from pydantic import ValidationError

from schemas.tasks import TaskInsertRequest, TaskUpdateRequest
from schemas.agent_runtime import SnapshotTaskData
from services.agent.capabilities import supports_recursive_housekeep
from services.agent.identity_service import DeviceIdentity
from services.agent.runtime_service import AgentRuntimeService
from services.task_contract import normalize_task_row
from test_tasks_router import valid_insert_payload, valid_update_payload
from conftest import RecordingTransaction
from test_task_snapshot_contract import _params, _snapshot_rows, SnapshotRepository
from test_agent_runtime import runtime_app, seed_snapshot, auth


FEATURE = "housekeep_recursive_v1"
HEADER = "X-TW-Agent-Capabilities"


@pytest.mark.parametrize("value,expected", [(True,True),(False,False),(1,True),(0,False),("1",True),("0",False),("false",False),("true",True)])
def test_recursive_flag_schema_and_normalization(value, expected):
    payload=valid_insert_payload(task_type="housekeep", house_keep_recursive=value)
    parsed=TaskInsertRequest(**payload).model_dump()
    assert parsed["house_keep_recursive"] is expected
    assert normalize_task_row(parsed)["house_keep_recursive"] is expected
    assert TaskUpdateRequest(**valid_update_payload(house_keep_recursive=value)).house_keep_recursive is expected


def test_legacy_defaults_and_unrelated_task_reset():
    assert TaskInsertRequest(**valid_insert_payload()).house_keep_recursive is False
    for task_type in ("copy","archive","command"):
        assert normalize_task_row({"task_type":task_type,"house_keep_recursive":True})["house_keep_recursive"] is False
    with pytest.raises(ValidationError):
        TaskInsertRequest(**valid_insert_payload(house_keep_recursive="maybe"))


@pytest.mark.parametrize("enabled", [False,True])
def test_create_update_binding_and_snapshot_roundtrip(make_tasks_module, monkeypatch, enabled):
    monkeypatch.setattr(RecordingTransaction, "fetch_one", lambda *args: {"agent_capabilities": FEATURE}, raising=False)
    module,db=make_tasks_module()
    payload=valid_insert_payload(schedule_id=12, sequence=0, retry_count=0, task_type="housekeep", destination_path="/cleanup", house_keep_days=1, house_keep_recursive=enabled)
    asyncio.run(module.insert_task(TaskInsertRequest(**payload)))
    schedule=_params(db.committed,"INSERT INTO schedule_detail")
    task=_params(db.committed,"INSERT INTO task_detail")
    assert task[11] is enabled
    rows=_snapshot_rows(schedule,task,archive_type=task[3])
    rows.details[0]["house_keep_recursive"]=task[11]
    result=AgentRuntimeService(SnapshotRepository(rows)).snapshot(DeviceIdentity(7,"dev252",7))
    wire=result.data["schedules"][0]["details"][0]["task"]
    assert wire["house_keep_recursive"] is enabled
    assert SnapshotTaskData(**wire).house_keep_recursive is enabled
    before=result.etag
    rows.details[0]["house_keep_recursive"]=not enabled
    after=AgentRuntimeService(SnapshotRepository(rows)).snapshot(DeviceIdentity(7,"dev252",7))
    assert after.etag!=before
    asyncio.run(module.update_tasks(TaskUpdateRequest(**valid_update_payload(task_type="housekeep",house_keep_recursive=enabled))))
    saved=_params(db.committed,"UPDATE task_detail")
    assert saved[10] is enabled
    # A real server document consumed by the Go executor in the paired CI job.
    if enabled and os.getenv("TIMEWEAVER_CONTRACT_SNAPSHOT"):
        envelope={"schema_version":"1","server_time":result.generated_at,"data":result.data}
        Path(os.environ["TIMEWEAVER_CONTRACT_SNAPSHOT"]).write_text(json.dumps(jsonable_encoder({"envelope":envelope,"etag":result.etag})),encoding="utf8")


def test_snapshot_blocks_old_agent_including_conditional_get(runtime_app):
    client,_,_,repository,first,_=runtime_app
    seed_snapshot(repository)
    detail=next(d for d in repository.details if d["schedule_id"]==12)
    detail.update(task_type="housekeep",command=None,destination_path="/cleanup",house_keep_days=1,house_keep_recursive=True)
    headers=auth(first["access_token"])
    upgraded=client.get("/api/agent/v1/snapshot",headers={**headers,HEADER:FEATURE})
    assert upgraded.status_code==200
    assert any(d["task"]["house_keep_recursive"] for g in upgraded.json()["data"]["schedules"] for d in g["details"])
    for extra in ({},{"If-None-Match":upgraded.headers["etag"]},{HEADER:FEATURE+"_typo"}):
        blocked=client.get("/api/agent/v1/snapshot",headers={**headers,**extra})
        assert blocked.status_code==409
        assert blocked.json()["detail"]["code"]=="agent_upgrade_required"
    cached=client.get("/api/agent/v1/snapshot",headers={**headers,HEADER:FEATURE,"If-None-Match":upgraded.headers["etag"]})
    assert cached.status_code==304
    detail["house_keep_recursive"]=False
    assert client.get("/api/agent/v1/snapshot",headers=headers).status_code==200


@pytest.mark.parametrize("value,expected", [(None,False),("",False),(FEATURE,True),("other, "+FEATURE,True),(FEATURE+"_v2",False)])
def test_capability_is_exact_and_optional(value,expected):
    assert supports_recursive_housekeep(value) is expected


@pytest.mark.parametrize("capabilities", [None, "", FEATURE + "_typo"])
@pytest.mark.parametrize("operation", ["insert", "update"])
def test_unsupported_agent_cannot_save_recursive_task(make_tasks_module, monkeypatch, capabilities, operation):
    module, db = make_tasks_module()
    monkeypatch.setattr(RecordingTransaction, "fetch_one", lambda *args: {"agent_capabilities": capabilities}, raising=False)
    payload = dict(task_type="housekeep", house_keep_recursive=True)
    with pytest.raises(module.HTTPException) as caught:
        if operation == "insert":
            asyncio.run(module.insert_task(TaskInsertRequest(**valid_insert_payload(**payload))))
        else:
            asyncio.run(module.update_tasks(TaskUpdateRequest(**valid_update_payload(**payload))))
    assert caught.value.status_code == 409
    assert caught.value.detail["code"] == "agent_upgrade_required"
    assert not db.committed


def test_heartbeat_records_support_and_clears_it_for_legacy_agent(runtime_app):
    client, _, _, repository, first, _ = runtime_app
    body = {"agent_version": "test", "applied_revision": None, "state": "HEALTHY"}
    headers = auth(first["access_token"])
    result = client.post("/api/agent/v1/heartbeat", headers={**headers, HEADER: FEATURE}, json=body)
    assert result.status_code == 200
    assert repository.devices[1]["agent_capabilities"] == FEATURE
    result = client.post("/api/agent/v1/heartbeat", headers=headers, json=body)
    assert result.status_code == 200
    assert repository.devices[1]["agent_capabilities"] == ""


def test_sqlite_migration_preserves_rows_and_defaults_off():
    path=Path(__file__).resolve().parents[1]/"res/sql/migration/sqlite/timeweaver_server_015.sql"
    with sqlite3.connect(":memory:") as db:
        db.execute("CREATE TABLE task_detail(detail_id INTEGER PRIMARY KEY, house_keep_days INTEGER)")
        db.execute("INSERT INTO task_detail VALUES(1,30)")
        db.execute("CREATE TABLE devices(device_id INTEGER PRIMARY KEY)")
        db.execute("INSERT INTO devices VALUES(1)")
        db.executescript(path.read_text())
        assert db.execute("SELECT agent_capabilities FROM devices").fetchone() == ("",)
        assert db.execute("SELECT house_keep_days,house_keep_recursive FROM task_detail").fetchone()==(30,0)
        db.execute("UPDATE task_detail SET house_keep_recursive=1 WHERE detail_id=1")
        assert db.execute("SELECT house_keep_recursive FROM task_detail").fetchone()==(1,)

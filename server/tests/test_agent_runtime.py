import copy
import uuid
from datetime import datetime, timedelta, timezone

import jwt
import pytest
from fastapi import FastAPI
from fastapi.testclient import TestClient

from repositories.agent_runtime import RuntimeRepositoryError, SnapshotRows
from routers.agent import runtime as runtime_router
from schemas.agent_runtime import HeartbeatRequest
from services.agent.identity_service import (
    AgentIdentityService,
    DeviceIdentity,
    _agent_secret,
    get_identity_service,
)
from services.agent.runtime_service import (
    AgentRuntimeError,
    AgentRuntimeService,
    get_runtime_service,
)
from test_agent_identity import MemoryIdentityRepository


REVISION_A = "sha256:" + "a" * 64


def utcnow():
    return datetime.now(timezone.utc).replace(tzinfo=None, microsecond=0)


class MemoryAgentRepository(MemoryIdentityRepository):
    def __init__(self):
        super().__init__()
        self.groups = []
        self.details = []
        self.manuals = []

    def record_heartbeat(self, device_id, agent_version, applied_revision):
        device = self.devices.get(device_id)
        if not device or device["status"] != "active":
            raise RuntimeRepositoryError("device_revoked")
        device.update(
            version=agent_version,
            applied_revision=applied_revision,
            last_heartbeat_at=utcnow(),
        )
        return type("Heartbeat", (), {"db_now": device["last_heartbeat_at"]})()

    def load_snapshot(self, device_id):
        device = self.devices.get(device_id)
        if not device or device["status"] != "active":
            raise RuntimeRepositoryError("device_revoked")
        groups = [
            copy.deepcopy(row)
            for row in self.groups
            if row["target_device"] == device_id and row.get("status") == "active"
        ]
        schedule_ids = {row["schedule_id"] for row in groups}
        details = [
            copy.deepcopy(row)
            for row in self.details
            if row["schedule_id"] in schedule_ids
        ]
        detail_ids = {row["detail_id"] for row in details}
        manuals = [
            copy.deepcopy(row)
            for row in self.manuals
            if row["detail_id"] in detail_ids
            and row["status"] in {"wait", "processing"}
        ]
        return SnapshotRows(
            db_now=datetime(2026, 8, 1, 5, 10, 0),
            device={
                "device_id": device_id,
                "device_name": device["device_name"],
                "status": device["status"],
                "version": device.get("version"),
            },
            groups=groups,
            details=details,
            manuals=manuals,
        )


def issue_device(service, name):
    enrollment = service.issue_enrollment("admin", name, 0, 24)
    return service.enroll(enrollment["token"], name, "agent-test")


@pytest.fixture
def runtime_app():
    repository = MemoryAgentRepository()
    identity_service = AgentIdentityService(repository)
    runtime_service = AgentRuntimeService(repository)
    first = issue_device(identity_service, "batch-01")
    second = issue_device(identity_service, "batch-02")

    app = FastAPI()
    app.include_router(runtime_router.router, prefix="/api/agent/v1")
    app.dependency_overrides[get_identity_service] = lambda: identity_service
    app.dependency_overrides[get_runtime_service] = lambda: runtime_service
    return (
        TestClient(app),
        identity_service,
        runtime_service,
        repository,
        first,
        second,
    )


def auth(token):
    return {"Authorization": f"Bearer {token}"}


def seed_snapshot(repository):
    first_detail = uuid.UUID("8f0d65c5-b6a4-4bb0-a2c5-f23672fc9b76").bytes
    tied_detail = uuid.UUID("1a749b4b-bad4-4ff3-a3c4-49eb3dcde0b4").bytes
    later_detail = uuid.UUID("8be11f59-b472-4d48-bcae-49e98b00e86b").bytes
    other_detail = uuid.UUID("a5c5c8b2-62db-4f0d-bd17-d104aa56d85d").bytes

    repository.groups = [
        {
            "schedule_id": 99,
            "name": "other-device",
            "target_device": 2,
            "status": "active",
            "year": "*",
            "month": "*",
            "day_of_week": "*",
            "day": "*",
            "hour": "9",
            "minute": "0",
            "second": "0",
            "is_error_stop": 1,
        },
        {
            "schedule_id": 12,
            "name": "nightly-batch",
            "target_device": 1,
            "status": "active",
            "year": "*",
            "month": "*",
            "day_of_week": "*",
            "day": "*",
            "hour": "2",
            "minute": "0",
            "second": "0",
            "is_error_stop": 1,
        },
    ]

    def detail(detail_id, schedule_id, sequence, command):
        return {
            "detail_id": detail_id,
            "task_detail_id": detail_id,
            "schedule_id": schedule_id,
            "schedule_name": command,
            "year": "*",
            "month": "*",
            "day_of_week": "*",
            "day": "*",
            "hour": "*",
            "minute": "*",
            "second": "*",
            "is_error_stop": 1,
            "sequence": sequence,
            "retry_count": 0,
            "task_type": "command",
            "command": command,
            "archive_type": None,
            "source_path": None,
            "error_on_missing_source": 1,
            "destination_path": None,
            "date_format": "%Y%m%d",
            "target_date_format": None,
            "destination_date_format": None,
            "house_keep_days": None,
        }

    repository.details = [
        detail(other_detail, 99, 1, "other"),
        detail(later_detail, 12, 20, "third"),
        detail(tied_detail, 12, 10, "second"),
        detail(first_detail, 12, 10, "first"),
    ]
    repository.manuals = [
        {
            "manual_id": 42,
            "schedule_id": 12,
            "detail_id": later_detail,
            "status": "processing",
            "is_immediate": 1,
            "schedule_datetime": datetime(2026, 8, 1, 7, 0, 0),
        },
        {
            "manual_id": 41,
            "schedule_id": 12,
            "detail_id": first_detail,
            "status": "wait",
            "is_immediate": 0,
            "schedule_datetime": datetime(2026, 8, 1, 6, 0, 0),
        },
        {
            "manual_id": 90,
            "schedule_id": 99,
            "detail_id": other_detail,
            "status": "wait",
            "is_immediate": 0,
            "schedule_datetime": datetime(2026, 8, 1, 6, 0, 0),
        },
    ]


def test_heartbeat_updates_device_and_validates_revision(runtime_app):
    client, _, _, repository, first, _ = runtime_app
    response = client.post(
        "/api/agent/v1/heartbeat",
        headers={
            **auth(first["access_token"]),
            "X-TW-Agent-Version": "agent-2",
            "X-TW-Schema-Version": "1",
        },
        json={
            "agent_version": "agent-2",
            "applied_revision": REVISION_A,
            "state": "HEALTHY",
        },
    )
    assert response.status_code == 200
    assert response.json()["data"]["device_id"] == 1
    assert repository.devices[1]["version"] == "agent-2"
    assert repository.devices[1]["applied_revision"] == REVISION_A
    assert repository.devices[1]["last_heartbeat_at"] is not None

    invalid = client.post(
        "/api/agent/v1/heartbeat",
        headers=auth(first["access_token"]),
        json={
            "agent_version": "agent-2",
            "applied_revision": "sha256:ABC",
            "state": "HEALTHY",
        },
    )
    assert invalid.status_code == 422


def test_heartbeat_rejects_inactive_and_revoked_credentials(runtime_app):
    client, _, _, repository, first, _ = runtime_app
    body = {
        "agent_version": "agent-2",
        "applied_revision": None,
        "state": "HALTED",
    }

    repository.devices[1]["status"] = "inactive"
    inactive = client.post(
        "/api/agent/v1/heartbeat",
        headers=auth(first["access_token"]),
        json=body,
    )
    assert inactive.status_code == 403
    assert inactive.json()["detail"]["code"] == "device_revoked"

    repository.devices[1]["status"] = "active"
    repository.credentials[1]["revoked_at"] = utcnow()
    revoked = client.post(
        "/api/agent/v1/heartbeat",
        headers=auth(first["access_token"]),
        json=body,
    )
    assert revoked.status_code == 403
    assert revoked.json()["detail"]["code"] == "device_revoked"


def test_snapshot_is_device_scoped_and_dense_ranked(runtime_app):
    client, _, _, repository, first, _ = runtime_app
    seed_snapshot(repository)

    response = client.get(
        "/api/agent/v1/snapshot",
        headers={**auth(first["access_token"]), "X-TW-Schema-Version": "1"},
    )
    assert response.status_code == 200
    body = response.json()["data"]
    assert [item["schedule_id"] for item in body["schedules"]] == [12]
    assert [item["manual_id"] for item in body["manual_runs"]] == [41, 42]
    assert [item["exec_sequence"] for item in body["schedules"][0]["details"]] == [
        1,
        1,
        2,
    ]
    assert body["manual_runs"][0]["claimable"] is True
    assert body["manual_runs"][1]["claimable"] is False
    assert response.headers["etag"].startswith('W/"')


def test_snapshot_revision_is_order_independent_and_value_sensitive(runtime_app):
    _, _, runtime_service, repository, _, _ = runtime_app
    seed_snapshot(repository)
    principal = DeviceIdentity(1, "batch-01", 1)

    first = runtime_service.snapshot(principal)
    repository.groups.reverse()
    repository.details.reverse()
    repository.manuals.reverse()
    reordered = runtime_service.snapshot(principal)
    assert reordered.data["revision"] == first.data["revision"]
    assert reordered.etag == first.etag

    repository.details[1]["command"] = "changed-command"
    changed = runtime_service.snapshot(principal)
    assert changed.data["revision"] != first.data["revision"]
    assert changed.etag != first.etag


def test_snapshot_etag_schema_and_authentication_boundaries(runtime_app):
    client, _, _, repository, first, _ = runtime_app
    seed_snapshot(repository)

    current = client.get(
        "/api/agent/v1/snapshot",
        headers=auth(first["access_token"]),
    )
    assert current.status_code == 200
    etag = current.headers["etag"]

    unchanged = client.get(
        "/api/agent/v1/snapshot",
        headers={**auth(first["access_token"]), "If-None-Match": etag},
    )
    assert unchanged.status_code == 304
    assert unchanged.content == b""
    assert unchanged.headers["etag"] == etag

    repository.details[1]["command"] = "changed-command"
    changed = client.get(
        "/api/agent/v1/snapshot",
        headers={**auth(first["access_token"]), "If-None-Match": etag},
    )
    assert changed.status_code == 200
    assert changed.headers["etag"] != etag

    mismatch = client.get(
        "/api/agent/v1/snapshot",
        headers={**auth(first["access_token"]), "X-TW-Schema-Version": "0"},
    )
    assert mismatch.status_code == 422
    assert mismatch.json()["detail"]["code"] == "schema_mismatch"

    human_jwt = jwt.encode(
        {"sub": "admin", "exp": datetime.now(timezone.utc) + timedelta(minutes=5)},
        "test-secret-key",
        algorithm="HS256",
    )
    for method, path, body in (
        ("get", "/api/agent/v1/snapshot", None),
        (
            "post",
            "/api/agent/v1/heartbeat",
            {
                "agent_version": "agent-2",
                "applied_revision": None,
                "state": "HEALTHY",
            },
        ),
    ):
        denied = client.request(method, path, headers=auth(human_jwt), json=body)
        assert denied.status_code == 401

    claims = jwt.decode(
        first["access_token"],
        _agent_secret(),
        algorithms=["HS256"],
        audience="timeweaver-agent",
    )
    claims["exp"] = datetime.now(timezone.utc) - timedelta(seconds=1)
    expired = jwt.encode(claims, _agent_secret(), algorithm="HS256")
    expired_response = client.get(
        "/api/agent/v1/snapshot",
        headers=auth(expired),
    )
    assert expired_response.status_code == 401
    assert expired_response.json()["detail"]["code"] == "token_expired"


def test_snapshot_claimable_tracks_database_lease_expiry(runtime_app):
    _, _, runtime_service, repository, _, _ = runtime_app
    seed_snapshot(repository)
    principal = DeviceIdentity(1, "batch-01", 1)
    processing = next(row for row in repository.manuals if row["manual_id"] == 42)

    processing["claim_expires_at"] = datetime(2026, 8, 1, 5, 9, 59)
    expired = runtime_service.snapshot(principal)
    expired_manual = next(
        item for item in expired.data["manual_runs"] if item["manual_id"] == 42
    )
    assert expired_manual["claimable"] is True

    processing["claim_expires_at"] = datetime(2026, 8, 1, 5, 10, 1)
    live = runtime_service.snapshot(principal)
    live_manual = next(
        item for item in live.data["manual_runs"] if item["manual_id"] == 42
    )
    assert live_manual["claimable"] is False
    assert live.data["revision"] != expired.data["revision"]
    assert live.etag != expired.etag


def test_snapshot_fails_whole_response_when_task_is_missing(runtime_app):
    _, _, runtime_service, repository, _, _ = runtime_app
    seed_snapshot(repository)
    repository.details[1]["task_detail_id"] = None

    with pytest.raises(AgentRuntimeError) as exc:
        runtime_service.snapshot(DeviceIdentity(1, "batch-01", 1))
    assert exc.value.status_code == 500
    assert exc.value.code == "server_error"


def test_heartbeat_request_accepts_all_runtime_states():
    for state in ("BOOTSTRAP", "HEALTHY", "DEGRADED", "HALTED"):
        request = HeartbeatRequest(
            agent_version="agent",
            applied_revision=None,
            state=state,
        )
        assert request.state == state
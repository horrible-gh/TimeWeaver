import copy
import threading
import uuid
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timedelta, timezone

import jwt
import pytest
from fastapi import FastAPI
from fastapi.testclient import TestClient

from repositories.agent_execution import (
    ClaimRecord,
    EventRecord,
    ExecutionRepositoryError,
    ResultRecord,
    StartRecord,
)
from routers.agent import execution as execution_router
from services.agent.execution_service import (
    AgentExecutionError,
    AgentExecutionService,
    get_execution_service,
)
from services.agent.identity_service import (
    AgentIdentityService,
    DeviceIdentity,
    get_identity_service,
)
from test_agent_identity import MemoryIdentityRepository


DETAIL_ID = uuid.UUID("8f0d65c5-b6a4-4bb0-a2c5-f23672fc9b76")
OTHER_DETAIL_ID = uuid.UUID("1a749b4b-bad4-4ff3-a3c4-49eb3dcde0b4")
EXECUTION_GROUP_ID = uuid.UUID("3f0d1c9e-7a44-4e2c-9d8b-1a2b3c4d5e6f")


def db_now():
    return datetime.now(timezone.utc).replace(tzinfo=None, microsecond=0)


class MemoryExecutionRepository(MemoryIdentityRepository):
    def __init__(self):
        super().__init__()
        self.lock = threading.Lock()
        self.groups = {
            12: {
                "schedule_id": 12,
                "target_device": 1,
                "is_error_stop": False,
                "status": "active",
            },
            13: {
                "schedule_id": 13,
                "target_device": 2,
                "is_error_stop": False,
                "status": "active",
            },
        }
        self.details = {
            DETAIL_ID.bytes: {
                "detail_id": DETAIL_ID.bytes,
                "schedule_id": 12,
                "is_error_stop": False,
                "status": "active",
            },
            OTHER_DETAIL_ID.bytes: {
                "detail_id": OTHER_DETAIL_ID.bytes,
                "schedule_id": 13,
                "is_error_stop": False,
                "status": "active",
            },
        }
        self.manuals = {
            41: {
                "manual_id": 41,
                "detail_id": DETAIL_ID.bytes,
                "status": "wait",
                "claim_token": None,
                "claim_expires_at": None,
            },
            90: {
                "manual_id": 90,
                "detail_id": OTHER_DETAIL_ID.bytes,
                "status": "wait",
                "claim_token": None,
                "claim_expires_at": None,
            },
        }
        self.logs = {}
        self.running = {}
        self.events = []
        self.next_execution_id = 1

    def _owned(self, device_id, detail_id, schedule_id=None):
        detail = self.details.get(detail_id)
        if not detail:
            return None, None
        group = self.groups.get(detail["schedule_id"])
        if (
            not group
            or group["target_device"] != device_id
            or (schedule_id is not None and schedule_id != detail["schedule_id"])
        ):
            return None, None
        return group, detail

    def claim_manual_run(self, device_id, manual_id, claim_token):
        with self.lock:
            now = db_now()
            manual = self.manuals.get(manual_id)
            if not manual:
                raise ExecutionRepositoryError("not_found")
            group, _ = self._owned(device_id, manual["detail_id"])
            if not group:
                raise ExecutionRepositoryError("not_found")
            if (
                manual["status"] == "processing"
                and manual["claim_expires_at"] is not None
                and manual["claim_expires_at"] <= now
            ):
                manual.update(status="wait", claim_token=None, claim_expires_at=None)
            if manual["status"] != "wait":
                raise ExecutionRepositoryError("already_claimed")
            expires = now + timedelta(minutes=30)
            manual.update(
                status="processing",
                claim_token=claim_token,
                claim_expires_at=expires,
            )
            return ClaimRecord(manual_id, claim_token, expires, now)

    def start_execution(self, device_id, payload):
        with self.lock:
            now = db_now()
            group, _ = self._owned(
                device_id,
                payload["detail_id"],
                payload["schedule_id"],
            )
            if not group:
                raise ExecutionRepositoryError("not_found")
            key = (payload["schedule_id"], payload["detail_id"])
            self.running[key] = copy.deepcopy(payload)
            return StartRecord(now)

    def accept_result(self, device_id, payload):
        with self.lock:
            now = db_now()
            group, detail = self._owned(
                device_id,
                payload["detail_id"],
                payload["schedule_id"],
            )
            if not group:
                raise ExecutionRepositoryError("not_found")
            key = (
                payload["execution_grp_id"],
                payload["detail_id"],
                payload["attempt"],
            )
            existing = self.logs.get(key)
            semantic = {
                name: payload[name]
                for name in (
                    "schedule_id",
                    "manual_id",
                    "started_at",
                    "finished_at",
                    "result_code",
                    "result_message",
                    "environment_info",
                )
            }
            if existing:
                if existing["semantic"] != semantic:
                    raise ExecutionRepositoryError("invalid_request")
                self.running.pop((payload["schedule_id"], payload["detail_id"]), None)
                return ResultRecord(existing["execution_id"], True, [], now)

            manual = None
            if payload["manual_id"] is not None:
                manual = self.manuals.get(payload["manual_id"])
                if not manual or manual["detail_id"] != payload["detail_id"]:
                    raise ExecutionRepositoryError("not_found")
                if (
                    manual["status"] != "processing"
                    or manual["claim_token"] != payload["claim_token"]
                    or manual["claim_expires_at"] <= now
                ):
                    raise ExecutionRepositoryError("claim_expired")

            execution_id = self.next_execution_id
            self.next_execution_id += 1
            self.logs[key] = {"execution_id": execution_id, "semantic": semantic}
            self.running.pop((payload["schedule_id"], payload["detail_id"]), None)
            transitions = []
            if manual:
                status = "done" if payload["result_code"] == 0 else "failed"
                manual.update(status=status, claim_token=None, claim_expires_at=None)
                transitions.append(
                    {"target": "manual_execution", "id": manual["manual_id"], "status": status}
                )
            elif payload["result_code"] != 0 and group["is_error_stop"]:
                group["status"] = "error"
                transitions.append(
                    {"target": "schedule_group", "id": group["schedule_id"], "status": "error"}
                )
            elif payload["result_code"] != 0 and detail["is_error_stop"]:
                detail["status"] = "error"
                transitions.append(
                    {"target": "schedule_detail", "id": str(DETAIL_ID), "status": "error"}
                )
            return ResultRecord(execution_id, False, transitions, now)

    def accept_event(
        self,
        device_id,
        event_type,
        severity,
        occurred_at,
        message,
        environment_info,
    ):
        now = db_now()
        self.events.append(
            {
                "device_id": device_id,
                "event_type": event_type,
                "severity": severity,
                "occurred_at": occurred_at,
                "message": message,
                "environment_info": environment_info,
            }
        )
        return EventRecord(now)

    def sweep_expired_claims(self, batch_size=500):
        with self.lock:
            now = db_now()
            selected = [
                row
                for row in self.manuals.values()
                if row["status"] == "processing"
                and row["claim_expires_at"] is not None
                and row["claim_expires_at"] <= now
            ][:batch_size]
            for row in selected:
                row.update(status="wait", claim_token=None, claim_expires_at=None)
            return len(selected)


def issue_device(service, name):
    enrollment = service.issue_enrollment("admin", name, 0, 24)
    return service.enroll(enrollment["token"], name, "agent-test")


@pytest.fixture
def execution_app():
    repository = MemoryExecutionRepository()
    identity_service = AgentIdentityService(repository)
    first = issue_device(identity_service, "batch-01")
    second = issue_device(identity_service, "batch-02")
    service = AgentExecutionService(repository)

    app = FastAPI()
    app.include_router(execution_router.router, prefix="/api/agent/v1")
    app.dependency_overrides[get_identity_service] = lambda: identity_service
    app.dependency_overrides[get_execution_service] = lambda: service
    return TestClient(app), service, repository, first, second


def auth(token):
    return {"Authorization": f"Bearer {token}"}


def result_body(**overrides):
    body = {
        "execution_grp_id": str(EXECUTION_GROUP_ID),
        "schedule_id": 12,
        "detail_id": str(DETAIL_ID),
        "attempt": 1,
        "manual_id": None,
        "claim_token": None,
        "started_at": "2026-08-01T02:00:01Z",
        "finished_at": "2026-08-01T02:00:09Z",
        "result_code": 0,
        "result_message": None,
        "environment_info": {"host": "batch-01", "os": "posix"},
    }
    body.update(overrides)
    return body


def start_body(**overrides):
    body = {
        "execution_grp_id": str(EXECUTION_GROUP_ID),
        "schedule_id": 12,
        "detail_id": str(DETAIL_ID),
        "attempt": 1,
        "started_at": "2026-08-01T02:00:01Z",
    }
    body.update(overrides)
    return body


def post_start(client, token, body):
    return client.post(
        f'/api/agent/v1/executions/{body["execution_grp_id"]}/start',
        headers=auth(token),
        json=body,
    )


def result_headers(token, body):
    return {
        **auth(token),
        "Idempotency-Key": (
            f'{body["execution_grp_id"]}:{body["detail_id"]}:{body["attempt"]}'
        ),
    }


def post_result(client, token, body):
    return client.post(
        f'/api/agent/v1/executions/{body["execution_grp_id"]}/results',
        headers=result_headers(token, body),
        json=body,
    )


def test_claim_success_conflict_expired_reclaim_and_scope(execution_app):
    client, _, repository, first, second = execution_app
    claimed = client.post(
        "/api/agent/v1/manual-runs/41/claim",
        headers=auth(first["access_token"]),
    )
    assert claimed.status_code == 200
    data = claimed.json()["data"]
    assert len(data["claim_token"]) == 64
    assert repository.manuals[41]["claim_token"] == data["claim_token"]
    assert repository.manuals[41]["status"] == "processing"

    conflict = client.post(
        "/api/agent/v1/manual-runs/41/claim",
        headers=auth(second["access_token"]),
    )
    assert conflict.status_code == 404

    same_owner_conflict = client.post(
        "/api/agent/v1/manual-runs/41/claim",
        headers=auth(first["access_token"]),
    )
    assert same_owner_conflict.status_code == 409
    assert same_owner_conflict.json()["detail"]["code"] == "already_claimed"

    repository.manuals[41]["claim_expires_at"] = db_now() - timedelta(seconds=1)
    reclaimed = client.post(
        "/api/agent/v1/manual-runs/41/claim",
        headers=auth(first["access_token"]),
    )
    assert reclaimed.status_code == 200
    assert reclaimed.json()["data"]["claim_token"] != data["claim_token"]

    missing = client.post(
        "/api/agent/v1/manual-runs/9999/claim",
        headers=auth(first["access_token"]),
    )
    other = client.post(
        "/api/agent/v1/manual-runs/90/claim",
        headers=auth(first["access_token"]),
    )
    assert missing.status_code == other.status_code == 404


def test_claim_and_sweep_are_concurrency_safe(execution_app):
    _, service, repository, _, _ = execution_app
    principal = DeviceIdentity(1, "batch-01", 1)

    def claim_once(_):
        try:
            service.claim_manual_run(principal, 41)
            return "ok"
        except AgentExecutionError as exc:
            return exc.code

    with ThreadPoolExecutor(max_workers=20) as pool:
        outcomes = list(pool.map(claim_once, range(20)))
    assert outcomes.count("ok") == 1
    assert outcomes.count("already_claimed") == 19

    repository.manuals[41]["claim_expires_at"] = db_now() - timedelta(seconds=1)
    with ThreadPoolExecutor(max_workers=2) as pool:
        swept = list(pool.map(lambda _: repository.sweep_expired_claims(), range(2)))
    assert sorted(swept) == [0, 1]
    assert repository.manuals[41]["status"] == "wait"
    assert repository.manuals[41]["claim_token"] is None


def test_start_then_finish_tracks_and_clears_running_state(execution_app):
    client, _, repository, first, second = execution_app
    body = start_body()

    started = post_start(client, first["access_token"], body)
    assert started.status_code == 200
    assert started.json()["data"] == {"accepted": True}
    key = (12, DETAIL_ID.bytes)
    assert repository.running[key]["execution_grp_id"] == EXECUTION_GROUP_ID.bytes

    replacement_group = uuid.uuid4()
    replaced = post_start(
        client,
        first["access_token"],
        start_body(execution_grp_id=str(replacement_group), attempt=2),
    )
    assert replaced.status_code == 200
    assert repository.running[key]["execution_grp_id"] == replacement_group.bytes

    denied = post_start(
        client,
        second["access_token"],
        start_body(execution_grp_id=str(uuid.uuid4())),
    )
    assert denied.status_code == 404

    finished = post_result(client, first["access_token"], result_body())
    assert finished.status_code == 200
    assert key not in repository.running

    # A delayed/retried start followed by a duplicate result is also self-healing.
    assert post_start(client, first["access_token"], body).status_code == 200
    duplicate = post_result(client, first["access_token"], result_body())
    assert duplicate.status_code == 200
    assert duplicate.json()["data"]["duplicate"] is True
    assert key not in repository.running

    # Existing agents that report a result without a start signal remain compatible.
    direct = post_result(
        client,
        first["access_token"],
        result_body(execution_grp_id=str(uuid.uuid4()), attempt=2),
    )
    assert direct.status_code == 200


def test_regular_results_transitions_and_idempotency(execution_app):
    client, _, repository, first, _ = execution_app
    success_body = result_body()
    success = post_result(client, first["access_token"], success_body)
    assert success.status_code == 200
    assert success.json()["data"]["duplicate"] is False
    assert success.json()["data"]["applied_transitions"] == []
    assert repository.groups[12]["status"] == "active"
    assert repository.details[DETAIL_ID.bytes]["status"] == "active"

    duplicate = post_result(client, first["access_token"], success_body)
    assert duplicate.status_code == 200
    assert duplicate.json()["data"]["duplicate"] is True
    assert duplicate.json()["data"]["execution_id"] == success.json()["data"]["execution_id"]

    changed = post_result(
        client,
        first["access_token"],
        result_body(result_message="different"),
    )
    assert changed.status_code == 422
    assert len(repository.logs) == 1

    detail_body = result_body(
        execution_grp_id=str(uuid.uuid4()),
        result_code=2,
    )
    repository.details[DETAIL_ID.bytes]["is_error_stop"] = True
    detail_error = post_result(client, first["access_token"], detail_body)
    assert detail_error.status_code == 200
    assert detail_error.json()["data"]["applied_transitions"][0]["target"] == "schedule_detail"

    group_body = result_body(
        execution_grp_id=str(uuid.uuid4()),
        attempt=2,
        result_code=3,
    )
    repository.groups[12]["is_error_stop"] = True
    group_error = post_result(client, first["access_token"], group_body)
    assert group_error.status_code == 200
    transitions = group_error.json()["data"]["applied_transitions"]
    assert transitions == [{"target": "schedule_group", "id": 12, "status": "error"}]


def test_manual_result_expiry_completion_and_duplicate_precedence(execution_app):
    client, _, repository, first, _ = execution_app
    claim = client.post(
        "/api/agent/v1/manual-runs/41/claim",
        headers=auth(first["access_token"]),
    ).json()["data"]
    body = result_body(
        execution_grp_id=str(uuid.uuid4()),
        manual_id=41,
        claim_token=claim["claim_token"],
        result_code=1,
    )
    accepted = post_result(client, first["access_token"], body)
    assert accepted.status_code == 200
    assert repository.manuals[41]["status"] == "failed"
    assert repository.manuals[41]["claim_token"] is None
    assert repository.groups[12]["status"] == "active"
    assert accepted.json()["data"]["applied_transitions"] == [
        {"target": "manual_execution", "id": 41, "status": "failed"}
    ]

    duplicate = post_result(client, first["access_token"], body)
    assert duplicate.status_code == 200
    assert duplicate.json()["data"]["duplicate"] is True

    repository.manuals[41].update(
        status="processing",
        claim_token="a" * 64,
        claim_expires_at=db_now() - timedelta(seconds=1),
    )
    expired_body = result_body(
        execution_grp_id=str(uuid.uuid4()),
        attempt=2,
        manual_id=41,
        claim_token="a" * 64,
    )
    expired = post_result(client, first["access_token"], expired_body)
    assert expired.status_code == 410
    assert expired.json()["detail"]["code"] == "claim_expired"


def test_twenty_concurrent_result_retries_create_one_record(execution_app):
    _, service, repository, _, _ = execution_app
    principal = DeviceIdentity(1, "batch-01", 1)
    from schemas.agent_execution import ExecutionResultRequest

    request = ExecutionResultRequest(**result_body())
    key = f"{request.execution_grp_id}:{request.detail_id}:{request.attempt}"

    def submit(_):
        return service.accept_result(principal, request.execution_grp_id, key, request)

    with ThreadPoolExecutor(max_workers=20) as pool:
        results = list(pool.map(submit, range(20)))
    assert sum(not item["duplicate"] for item in results) == 1
    assert sum(item["duplicate"] for item in results) == 19
    assert len(repository.logs) == 1
    assert len({item["execution_id"] for item in results}) == 1


def test_result_scope_contract_event_validation_and_human_auth(execution_app):
    client, _, repository, first, _, = execution_app
    other_body = result_body(
        execution_grp_id=str(uuid.uuid4()),
        schedule_id=13,
        detail_id=str(OTHER_DETAIL_ID),
    )
    denied_scope = post_result(client, first["access_token"], other_body)
    assert denied_scope.status_code == 404

    mismatch = result_body()
    mismatch_response = client.post(
        f"/api/agent/v1/executions/{uuid.uuid4()}/results",
        headers=result_headers(first["access_token"], mismatch),
        json=mismatch,
    )
    assert mismatch_response.status_code == 422
    assert len(repository.logs) == 0

    event = client.post(
        "/api/agent/v1/events",
        headers=auth(first["access_token"]),
        json={
            "event_type": "sync_error",
            "severity": "error",
            "occurred_at": "2026-08-01T05:10:00Z",
            "message": "snapshot rejected",
            "environment_info": {"host": "batch-01"},
        },
    )
    assert event.status_code == 200
    assert event.json()["data"] == {"accepted": True}
    assert repository.events[0]["device_id"] == 1

    invalid_event = client.post(
        "/api/agent/v1/events",
        headers=auth(first["access_token"]),
        json={
            "event_type": "unknown",
            "severity": "error",
            "occurred_at": "2026-08-01T05:10:00Z",
        },
    )
    oversized = client.post(
        "/api/agent/v1/events",
        headers=auth(first["access_token"]),
        json={
            "event_type": "sync_error",
            "severity": "error",
            "occurred_at": "2026-08-01T05:10:00Z",
            "message": "가" * 6000,
        },
    )
    assert invalid_event.status_code == oversized.status_code == 422

    human_jwt = jwt.encode(
        {"sub": "admin", "exp": datetime.now(timezone.utc) + timedelta(minutes=5)},
        "test-secret-key",
        algorithm="HS256",
    )
    for method, path, json_body, headers in (
        ("post", "/api/agent/v1/manual-runs/41/claim", None, auth(human_jwt)),
        (
            "post",
            f"/api/agent/v1/executions/{EXECUTION_GROUP_ID}/start",
            start_body(),
            auth(human_jwt),
        ),
        (
            "post",
            f"/api/agent/v1/executions/{EXECUTION_GROUP_ID}/results",
            result_body(),
            result_headers(human_jwt, result_body()),
        ),
        (
            "post",
            "/api/agent/v1/events",
            {
                "event_type": "sync_error",
                "severity": "error",
                "occurred_at": "2026-08-01T05:10:00Z",
            },
            auth(human_jwt),
        ),
    ):
        response = client.request(method, path, headers=headers, json=json_body)
        assert response.status_code == 401
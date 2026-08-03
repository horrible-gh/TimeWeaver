import hashlib
import uuid
from datetime import datetime, timedelta, timezone

import jwt
import pytest
from fastapi import Depends, FastAPI
from fastapi.testclient import TestClient

from repositories.agent_identity import (
    CredentialIssue,
    EnrollmentIssue,
    IdentityRepositoryError,
)
from routers.agent import enroll as enroll_router
from routers.agent.auth import verify_agent_token
from routers.dashboard import agent_enrollment_tokens as admin_router
from routers.login.auth import verify_token
from services.agent.identity_service import (
    AgentIdentityService,
    DeviceIdentity,
    get_identity_service,
)


def utcnow():
    return datetime.now(timezone.utc).replace(tzinfo=None)


class MemoryIdentityRepository:
    """In-memory contract double that deliberately stores only token digests."""

    def __init__(self):
        self.users = {"admin": {"user_id": "admin", "group_id": 0, "role": "admin"}}
        self.enrollments = {}
        self.devices = {}
        self.credentials = {}
        self.next_device_id = 1
        self.next_credential_id = 1

    def get_user_principal(self, user_id):
        return self.users.get(user_id)

    def issue_enrollment(self, enrollment_id, digest, device_name, group_id, ttl_hours):
        now = utcnow()
        self.enrollments[enrollment_id] = {
            "enrollment_id": enrollment_id.bytes,
            "token_hash": digest,
            "device_name": device_name,
            "group_id": group_id,
            "created_at": now,
            "expires_at": now + timedelta(hours=ttl_hours),
            "used_at": None,
            "used_by_device_id": None,
            "revoked_at": None,
        }
        return EnrollmentIssue(enrollment_id, now + timedelta(hours=ttl_hours))

    def list_enrollments(self, group_id=None):
        now = utcnow()
        rows = []
        for row in self.enrollments.values():
            if group_id is None or row["group_id"] == group_id:
                rows.append({**row, "db_now": now})
        return sorted(rows, key=lambda row: row["created_at"], reverse=True)

    def revoke_enrollment(self, enrollment_id):
        row = self.enrollments.get(enrollment_id)
        if not row:
            raise IdentityRepositoryError("not_found")
        if row["used_at"] is not None:
            raise IdentityRepositoryError("already_used")
        row["revoked_at"] = row["revoked_at"] or utcnow()
        return row["revoked_at"]

    def enroll(
        self,
        enrollment_digest,
        requested_device_name,
        agent_version,
        refresh_digest,
        access_ttl_minutes,
        refresh_ttl_days,
    ):
        now = utcnow()
        row = next(
            (
                item
                for item in self.enrollments.values()
                if item["token_hash"] == enrollment_digest
            ),
            None,
        )
        if (
            not row
            or row["used_at"] is not None
            or row["revoked_at"] is not None
            or row["expires_at"] <= now
        ):
            raise IdentityRepositoryError("enrollment_token_invalid")
        if row["device_name"] is not None and row["device_name"] != requested_device_name:
            raise IdentityRepositoryError(
                "enrollment_token_invalid",
                {
                    "reason": "device_name_mismatch",
                    "expected_device_name": row["device_name"],
                    "actual_device_name": requested_device_name,
                },
            )

        device = next(
            (
                item
                for item in self.devices.values()
                if item["device_name"] == requested_device_name
                and item["group_id"] == row["group_id"]
            ),
            None,
        )
        if device is None:
            device_id = self.next_device_id
            self.next_device_id += 1
            device = {
                "device_id": device_id,
                "device_name": requested_device_name,
                "group_id": row["group_id"],
                "status": "active",
                "version": agent_version,
            }
            self.devices[device_id] = device
        elif device["status"] != "active":
            raise IdentityRepositoryError("device_inactive")

        for credential in self.credentials.values():
            if credential["device_id"] == device["device_id"] and credential["revoked_at"] is None:
                credential["revoked_at"] = now
        issue = self._new_credential(
            device,
            refresh_digest,
            now,
            access_ttl_minutes,
            refresh_ttl_days,
        )
        row["used_at"] = now
        row["used_by_device_id"] = device["device_id"]
        return issue

    def rotate(
        self,
        refresh_digest,
        new_refresh_digest,
        access_ttl_minutes,
        refresh_ttl_days,
    ):
        now = utcnow()
        current = next(
            (
                item
                for item in self.credentials.values()
                if item["token_hash"] == refresh_digest
            ),
            None,
        )
        if (
            not current
            or current["revoked_at"] is not None
            or current["expires_at"] <= now
        ):
            raise IdentityRepositoryError("invalid_token")
        device = self.devices[current["device_id"]]
        if device["status"] != "active":
            raise IdentityRepositoryError("device_inactive")
        for credential in self.credentials.values():
            if credential["device_id"] == device["device_id"] and credential["revoked_at"] is None:
                credential["revoked_at"] = now
        return self._new_credential(
            device,
            new_refresh_digest,
            now,
            access_ttl_minutes,
            refresh_ttl_days,
        )

    def _new_credential(
        self,
        device,
        digest,
        now,
        access_ttl_minutes,
        refresh_ttl_days,
    ):
        credential_id = self.next_credential_id
        self.next_credential_id += 1
        refresh_expiry = now + timedelta(days=refresh_ttl_days)
        self.credentials[credential_id] = {
            "credential_id": credential_id,
            "device_id": device["device_id"],
            "token_hash": digest,
            "issued_at": now,
            "expires_at": refresh_expiry,
            "revoked_at": None,
        }
        return CredentialIssue(
            credential_id,
            device["device_id"],
            device["device_name"],
            now + timedelta(minutes=access_ttl_minutes),
            refresh_expiry,
        )

    def get_access_identity(self, credential_id, device_id):
        credential = self.credentials.get(credential_id)
        device = self.devices.get(device_id)
        if not credential or not device or credential["device_id"] != device_id:
            return None
        return {
            **credential,
            "device_name": device["device_name"],
            "status": device["status"],
            "db_now": utcnow(),
        }


@pytest.fixture
def identity_app():
    repository = MemoryIdentityRepository()
    service = AgentIdentityService(repository)
    app = FastAPI()
    app.include_router(enroll_router.router, prefix="/agent/v1")
    app.include_router(
        admin_router.router,
        prefix="/dashboard/agent-enrollment-tokens",
    )

    @app.get("/agent/v1/protected")
    def protected(identity: DeviceIdentity = Depends(verify_agent_token)):
        return {"device_id": identity.device_id, "device_name": identity.device_name}

    app.dependency_overrides[get_identity_service] = lambda: service
    app.dependency_overrides[verify_token] = lambda: "admin"
    return TestClient(app), service, repository


def test_enroll_refresh_and_agent_authentication_are_isolated(identity_app):
    client, service, repository = identity_app
    issued = service.issue_enrollment("admin", "batch-01", 0, 24)

    enroll_response = client.post(
        "/agent/v1/enroll",
        json={
            "enrollment_token": issued["token"],
            "device_name": "batch-01",
            "agent_version": "test-agent",
        },
    )
    assert enroll_response.status_code == 200
    enrolled = enroll_response.json()["data"]
    assert enrolled["refresh_token"].startswith("rft_")
    assert client.get(
        "/agent/v1/protected",
        headers={"Authorization": f"Bearer {enrolled['access_token']}"},
    ).status_code == 200

    # A dashboard-style JWT is signed with the user secret and has no agent
    # audience/type/credential binding, so the agent dependency rejects it.
    human_jwt = jwt.encode(
        {"sub": "admin", "exp": datetime.now(timezone.utc) + timedelta(minutes=5)},
        "test-secret-key",
        algorithm="HS256",
    )
    assert client.get(
        "/agent/v1/protected",
        headers={"Authorization": f"Bearer {human_jwt}"},
    ).status_code == 401

    refresh_response = client.post(
        "/agent/v1/token/refresh",
        json={"refresh_token": enrolled["refresh_token"]},
    )
    assert refresh_response.status_code == 200
    rotated = refresh_response.json()["data"]

    # Rotation revokes the prior credential, so both its access JWT and refresh
    # secret stop working while the new pair succeeds.
    assert client.get(
        "/agent/v1/protected",
        headers={"Authorization": f"Bearer {enrolled['access_token']}"},
    ).status_code == 403
    assert client.post(
        "/agent/v1/token/refresh",
        json={"refresh_token": enrolled["refresh_token"]},
    ).status_code == 401
    assert client.get(
        "/agent/v1/protected",
        headers={"Authorization": f"Bearer {rotated['access_token']}"},
    ).status_code == 200

    repository.devices[1]["status"] = "inactive"
    assert client.get(
        "/agent/v1/protected",
        headers={"Authorization": f"Bearer {rotated['access_token']}"},
    ).status_code == 403
    repository.devices[1]["status"] = "active"
    repository.credentials[2]["revoked_at"] = utcnow()
    assert client.get(
        "/agent/v1/protected",
        headers={"Authorization": f"Bearer {rotated['access_token']}"},
    ).status_code == 403

    # Neither enrollment nor refresh plaintext is retained by the repository.
    stored_digests = [
        row["token_hash"] for row in repository.enrollments.values()
    ] + [row["token_hash"] for row in repository.credentials.values()]
    assert all(isinstance(value, bytes) and len(value) == 32 for value in stored_digests)
    assert hashlib.sha256(issued["token"].encode()).digest() in stored_digests
    assert issued["token"].encode() not in stored_digests
    assert enrolled["refresh_token"].encode() not in stored_digests


def test_enrollment_is_one_time_and_bound_to_device_name(identity_app):
    client, service, _ = identity_app
    issued = service.issue_enrollment("admin", "batch-02", 0, 24)

    wrong_name = client.post(
        "/agent/v1/enroll",
        json={
            "enrollment_token": issued["token"],
            "device_name": "batch-other",
        },
    )
    assert wrong_name.status_code == 403
    mismatch = wrong_name.json()["detail"]
    assert mismatch["reason"] == "device_name_mismatch"
    assert mismatch["expected_device_name"] == "batch-02"
    assert mismatch["actual_device_name"] == "batch-other"

    first = client.post(
        "/agent/v1/enroll",
        json={
            "enrollment_token": issued["token"],
            "device_name": "batch-02",
        },
    )
    assert first.status_code == 200

    reused = client.post(
        "/agent/v1/enroll",
        json={
            "enrollment_token": issued["token"],
            "device_name": "batch-02",
        },
    )
    assert reused.status_code == 403


def test_admin_list_never_exposes_token_and_revoke_is_idempotent(identity_app):
    client, _, _ = identity_app
    issue_response = client.post(
        "/dashboard/agent-enrollment-tokens",
        json={"device_name": None, "group_id": 0, "ttl_hours": 1},
    )
    assert issue_response.status_code == 201
    issued = issue_response.json()["data"]
    assert issued["token"].startswith("enr_")

    list_response = client.get(
        "/dashboard/agent-enrollment-tokens",
        params={"group_id": 0},
    )
    assert list_response.status_code == 200
    items = list_response.json()["data"]["items"]
    assert len(items) == 1
    assert "token" not in items[0]
    assert items[0]["status"] == "unused"

    first = client.delete(
        f"/dashboard/agent-enrollment-tokens/{issued['enrollment_id']}"
    )
    second = client.delete(
        f"/dashboard/agent-enrollment-tokens/{issued['enrollment_id']}"
    )
    assert first.status_code == second.status_code == 200
    assert second.json()["data"]["revoked_at"] == first.json()["data"]["revoked_at"]

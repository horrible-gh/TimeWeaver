import hashlib
import secrets
import uuid
from dataclasses import dataclass
from datetime import datetime, timedelta, timezone

import jwt

from config import db, settings
from repositories.agent_identity import (
    AgentIdentityRepository,
    IdentityRepositoryError,
)


AGENT_AUDIENCE = "timeweaver-agent"
AGENT_TOKEN_TYPE = "agent"
ALGORITHM = "HS256"
ACCESS_TTL_MINUTES = 15
REFRESH_TTL_DAYS = 90


class IdentityError(Exception):
    def __init__(self, status_code: int, code: str, message: str):
        super().__init__(message)
        self.status_code = status_code
        self.code = code
        self.message = message


@dataclass(frozen=True)
class DeviceIdentity:
    device_id: int
    device_name: str
    credential_id: int


def _utc(value: datetime) -> datetime:
    if value.tzinfo is None:
        return value.replace(tzinfo=timezone.utc)
    return value.astimezone(timezone.utc)


def _digest(raw: str) -> bytes:
    return hashlib.sha256(raw.encode("utf-8")).digest()


def _agent_secret() -> bytes:
    configured = getattr(settings, "AGENT_SECRET_KEY", "")
    if configured and configured != settings.SECRET_KEY:
        return configured.encode("utf-8")
    # Even a mistakenly duplicated configuration value cannot collapse the
    # user/device signing boundary.
    return hashlib.sha256(
        ("timeweaver-agent:" + settings.SECRET_KEY).encode("utf-8")
    ).digest()


def _raw_secret(prefix: str) -> str:
    return prefix + secrets.token_hex(32)


class AgentIdentityService:
    def __init__(self, repository: AgentIdentityRepository):
        self.repository = repository

    def require_admin(self, user_id: str):
        principal = self.repository.get_user_principal(user_id)
        if not principal or principal.get("role") != "admin":
            raise IdentityError(403, "admin_required", "Administrator role required")
        return principal

    def issue_enrollment(
        self,
        user_id: str,
        device_name: str | None,
        group_id: int,
        ttl_hours: int,
    ):
        self.require_admin(user_id)
        raw = _raw_secret("enr_")
        enrollment_id = uuid.uuid4()
        try:
            issue = self.repository.issue_enrollment(
                enrollment_id,
                _digest(raw),
                device_name,
                group_id,
                ttl_hours,
            )
        except IdentityRepositoryError as exc:
            self._raise_repository_error(exc)
        return {
            "enrollment_id": str(issue.enrollment_id),
            "token": raw,
            "device_name": device_name,
            "group_id": group_id,
            "expires_at": _utc(issue.expires_at),
        }

    def list_enrollments(self, user_id: str, group_id: int | None = None):
        self.require_admin(user_id)
        items = []
        for row in self.repository.list_enrollments(group_id):
            now = row["db_now"]
            if row["used_at"] is not None:
                status = "used"
            elif row["revoked_at"] is not None:
                status = "revoked"
            elif row["expires_at"] <= now:
                status = "expired"
            else:
                status = "unused"
            items.append(
                {
                    "enrollment_id": str(uuid.UUID(bytes=row["enrollment_id"])),
                    "device_name": row["device_name"],
                    "group_id": row["group_id"],
                    "created_at": _utc(row["created_at"]),
                    "expires_at": _utc(row["expires_at"]),
                    "used_at": _utc(row["used_at"]) if row["used_at"] else None,
                    "used_by_device_id": row["used_by_device_id"],
                    "revoked_at": _utc(row["revoked_at"]) if row["revoked_at"] else None,
                    "status": status,
                }
            )
        return {"items": items}

    def revoke_enrollment(self, user_id: str, enrollment_id: uuid.UUID):
        self.require_admin(user_id)
        try:
            revoked_at = self.repository.revoke_enrollment(enrollment_id)
        except IdentityRepositoryError as exc:
            self._raise_repository_error(exc)
        return {
            "enrollment_id": str(enrollment_id),
            "revoked_at": _utc(revoked_at),
        }

    def enroll(self, enrollment_token: str, device_name: str, agent_version: str | None):
        refresh_raw = _raw_secret("rft_")
        try:
            issue = self.repository.enroll(
                _digest(enrollment_token),
                device_name,
                agent_version,
                _digest(refresh_raw),
                ACCESS_TTL_MINUTES,
                REFRESH_TTL_DAYS,
            )
        except IdentityRepositoryError as exc:
            self._raise_repository_error(exc)
        return self._token_result(issue, refresh_raw, include_device=True)

    def rotate(self, refresh_token: str):
        new_refresh_raw = _raw_secret("rft_")
        try:
            issue = self.repository.rotate(
                _digest(refresh_token),
                _digest(new_refresh_raw),
                ACCESS_TTL_MINUTES,
                REFRESH_TTL_DAYS,
            )
        except IdentityRepositoryError as exc:
            self._raise_repository_error(exc)
        return self._token_result(issue, new_refresh_raw, include_device=False)

    def verify_access(self, raw_token: str) -> DeviceIdentity:
        try:
            claims = jwt.decode(
                raw_token,
                _agent_secret(),
                algorithms=[ALGORITHM],
                audience=AGENT_AUDIENCE,
                options={"require": ["exp", "iat", "sub", "aud", "typ", "credential_id"]},
            )
        except jwt.ExpiredSignatureError as exc:
            raise IdentityError(401, "token_expired", "Access token expired") from exc
        except jwt.InvalidTokenError as exc:
            raise IdentityError(401, "invalid_token", "Invalid agent access token") from exc

        subject = claims.get("sub", "")
        if claims.get("typ") != AGENT_TOKEN_TYPE or not subject.startswith("device:"):
            raise IdentityError(401, "invalid_token", "Invalid agent access token")
        try:
            device_id = int(subject.split(":", 1)[1])
            credential_id = int(claims["credential_id"])
        except (TypeError, ValueError):
            raise IdentityError(401, "invalid_token", "Invalid agent access token")

        try:
            row = self.repository.get_access_identity(credential_id, device_id)
        except Exception as exc:
            raise IdentityError(503, "unavailable", "Identity storage unavailable") from exc
        if (
            not row
            or row["revoked_at"] is not None
            or row["expires_at"] <= row["db_now"]
        ):
            raise IdentityError(403, "device_revoked", "Device credential revoked")
        if row["status"] != "active":
            # The agent protocol intentionally does not distinguish a disabled
            # device from a revoked credential on protected runtime calls.
            raise IdentityError(403, "device_revoked", "Device credential revoked")
        return DeviceIdentity(device_id, row["device_name"], credential_id)

    def _token_result(self, issue, refresh_raw: str, include_device: bool):
        issued_at = _utc(issue.access_expires_at) - timedelta(minutes=ACCESS_TTL_MINUTES)
        claims = {
            "sub": f"device:{issue.device_id}",
            "aud": AGENT_AUDIENCE,
            "typ": AGENT_TOKEN_TYPE,
            "credential_id": issue.credential_id,
            "jti": str(uuid.uuid4()),
            "iat": issued_at,
            "exp": _utc(issue.access_expires_at),
        }
        result = {
            "access_token": jwt.encode(claims, _agent_secret(), algorithm=ALGORITHM),
            "access_token_expires_at": _utc(issue.access_expires_at),
            "refresh_token": refresh_raw,
            "refresh_token_expires_at": _utc(issue.refresh_expires_at),
            "token_type": "bearer",
        }
        if include_device:
            result.update(device_id=issue.device_id, device_name=issue.device_name)
        return result

    @staticmethod
    def _raise_repository_error(exc: IdentityRepositoryError):
        mapping = {
            "enrollment_token_invalid": (403, "Enrollment token invalid, expired, or revoked"),
            "invalid_token": (401, "Refresh token is invalid or expired"),
            "device_inactive": (403, "Device is not active"),
            "group_inactive": (422, "Group does not exist or is inactive"),
            "already_used": (409, "Enrollment token already used and cannot be revoked"),
            "not_found": (404, "Enrollment token not found"),
        }
        status, message = mapping.get(exc.code, (503, "Identity storage unavailable"))
        raise IdentityError(status, exc.code, message) from exc


def get_identity_service() -> AgentIdentityService:
    return AgentIdentityService(AgentIdentityRepository(db.db_instance))

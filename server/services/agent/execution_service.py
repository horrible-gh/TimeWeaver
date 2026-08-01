import json
import secrets
from datetime import datetime, timezone
from uuid import UUID

from config import db
from repositories.agent_execution import (
    AgentExecutionRepository,
    ExecutionRepositoryError,
)
from schemas.agent_execution import (
    AgentEventRequest,
    ExecutionResultRequest,
    canonical_environment,
)
from services.agent.identity_service import DeviceIdentity


REQUEST_BODY_MAX = 1048576


class AgentExecutionError(Exception):
    def __init__(self, status_code: int, code: str, message: str):
        super().__init__(message)
        self.status_code = status_code
        self.code = code
        self.message = message


def _utc(value: datetime) -> datetime:
    if value.tzinfo is None:
        value = value.replace(tzinfo=timezone.utc)
    else:
        value = value.astimezone(timezone.utc)
    return value.replace(microsecond=0)


def _db_datetime(value: datetime) -> datetime:
    return _utc(value).replace(tzinfo=None)


class AgentExecutionService:
    def __init__(self, repository: AgentExecutionRepository):
        self.repository = repository

    def claim_manual_run(self, principal: DeviceIdentity, manual_id: int) -> dict:
        claim_token = secrets.token_hex(32)
        try:
            record = self.repository.claim_manual_run(
                principal.device_id,
                manual_id,
                claim_token,
            )
        except ExecutionRepositoryError as exc:
            self._raise_repository_error(exc, manual_id=manual_id)
        return {
            "server_time": _utc(record.db_now),
            "manual_id": record.manual_id,
            "claim_token": record.claim_token,
            "claim_expires_at": _utc(record.claim_expires_at),
            "execution_grp_id": None,
        }

    def accept_result(
        self,
        principal: DeviceIdentity,
        path_execution_grp_id: UUID,
        idempotency_key: str | None,
        request: ExecutionResultRequest,
    ) -> dict:
        expected_key = f"{request.execution_grp_id}:{request.detail_id}:{request.attempt}"
        serialized = json.dumps(
            request.model_dump(mode="json"),
            ensure_ascii=False,
            separators=(",", ":"),
        ).encode("utf-8")
        if (
            path_execution_grp_id != request.execution_grp_id
            or idempotency_key != expected_key
            or len(serialized) > REQUEST_BODY_MAX
        ):
            raise AgentExecutionError(
                422,
                "invalid_request",
                "Execution result contract is inconsistent",
            )

        payload = {
            "execution_grp_id": request.execution_grp_id.bytes,
            "schedule_id": request.schedule_id,
            "detail_id": request.detail_id.bytes,
            "attempt": request.attempt,
            "manual_id": request.manual_id,
            "claim_token": request.claim_token,
            "started_at": _db_datetime(request.started_at),
            "finished_at": _db_datetime(request.finished_at),
            "result_code": request.result_code,
            "result_message": request.result_message,
            "environment_info": canonical_environment(request.environment_info),
        }
        try:
            record = self.repository.accept_result(principal.device_id, payload)
        except ExecutionRepositoryError as exc:
            self._raise_repository_error(exc)
        return {
            "server_time": _utc(record.db_now),
            "execution_id": record.execution_id,
            "duplicate": record.duplicate,
            "applied_transitions": record.applied_transitions,
        }

    def accept_event(
        self,
        principal: DeviceIdentity,
        request: AgentEventRequest,
    ) -> dict:
        try:
            record = self.repository.accept_event(
                principal.device_id,
                request.event_type,
                request.severity,
                _db_datetime(request.occurred_at),
                request.message,
                canonical_environment(request.environment_info),
            )
        except ExecutionRepositoryError as exc:
            self._raise_repository_error(exc)
        return {"server_time": _utc(record.db_now), "accepted": True}

    @staticmethod
    def _raise_repository_error(
        exc: ExecutionRepositoryError,
        manual_id: int | None = None,
    ):
        if exc.code == "not_found":
            message = (
                f"Manual run {manual_id} not found for this device"
                if manual_id is not None
                else "Execution target not found for this device"
            )
            raise AgentExecutionError(404, "not_found", message) from exc
        if exc.code == "already_claimed":
            raise AgentExecutionError(
                409,
                "already_claimed",
                f"Manual run {manual_id} is already claimed",
            ) from exc
        if exc.code == "claim_expired":
            message = "Manual-run claim is missing, invalid, or expired"
            if exc.detail:
                message = f"Claim token expired at {exc.detail}"
            raise AgentExecutionError(410, "claim_expired", message) from exc
        if exc.code == "invalid_request":
            raise AgentExecutionError(
                422,
                "invalid_request",
                "Idempotency key was previously used with a different result",
            ) from exc
        raise AgentExecutionError(
            503,
            "unavailable",
            "Temporarily unable to process the request",
        ) from exc


def get_execution_service() -> AgentExecutionService:
    return AgentExecutionService(AgentExecutionRepository(db.db_instance))
from uuid import UUID

from fastapi import APIRouter, Depends, Header, HTTPException

from routers.agent.auth import verify_agent_token
from schemas.agent_execution import (
    AgentEventEnvelope,
    AgentEventRequest,
    ExecutionResultEnvelope,
    ExecutionResultRequest,
    ManualClaimEnvelope,
)
from services.agent.execution_service import (
    AgentExecutionError,
    AgentExecutionService,
    get_execution_service,
)
from services.agent.identity_service import DeviceIdentity
from services.blocking import BlockingQueueFull, RETRY_AFTER, run_blocking


router = APIRouter()


def _raise(exc):
    raise HTTPException(status_code=exc.status_code, detail={"code": exc.code, "message": exc.message}) from exc


def _queue_full(exc):
    raise HTTPException(
        status_code=503,
        detail={"code": "unavailable", "message": "Service is busy"},
        headers={"Retry-After": str(RETRY_AFTER)},
    ) from exc


@router.post("/manual-runs/{manual_id}/claim", response_model=ManualClaimEnvelope)
async def claim_manual_run(
    manual_id: int,
    principal: DeviceIdentity = Depends(verify_agent_token),
    service: AgentExecutionService = Depends(get_execution_service),
):
    try:
        result = await run_blocking(service.claim_manual_run, principal, manual_id)
    except AgentExecutionError as exc:
        _raise(exc)
    except BlockingQueueFull as exc:
        _queue_full(exc)
    return {
        "schema_version": "1",
        "server_time": result["server_time"],
        "data": {
            "manual_id": result["manual_id"],
            "claim_token": result["claim_token"],
            "claim_expires_at": result["claim_expires_at"],
            "execution_grp_id": None,
        },
    }


@router.post("/executions/{execution_grp_id}/results", response_model=ExecutionResultEnvelope)
async def accept_execution_result(
    execution_grp_id: UUID,
    request: ExecutionResultRequest,
    idempotency_key: str | None = Header(default=None, alias="Idempotency-Key"),
    principal: DeviceIdentity = Depends(verify_agent_token),
    service: AgentExecutionService = Depends(get_execution_service),
):
    try:
        result = await run_blocking(
            service.accept_result,
            principal,
            execution_grp_id,
            idempotency_key,
            request,
        )
    except AgentExecutionError as exc:
        _raise(exc)
    except BlockingQueueFull as exc:
        _queue_full(exc)
    return {
        "schema_version": "1",
        "server_time": result["server_time"],
        "data": {
            "execution_id": result["execution_id"],
            "duplicate": result["duplicate"],
            "applied_transitions": result["applied_transitions"],
        },
    }


@router.post("/events", response_model=AgentEventEnvelope)
async def accept_agent_event(
    request: AgentEventRequest,
    principal: DeviceIdentity = Depends(verify_agent_token),
    service: AgentExecutionService = Depends(get_execution_service),
):
    try:
        result = await run_blocking(service.accept_event, principal, request)
    except AgentExecutionError as exc:
        _raise(exc)
    except BlockingQueueFull as exc:
        _queue_full(exc)
    return {
        "schema_version": "1",
        "server_time": result["server_time"],
        "data": {"accepted": True},
    }
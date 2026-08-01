from fastapi import APIRouter, Depends, Header, HTTPException, Response, status

from routers.agent.auth import verify_agent_token
from schemas.agent_runtime import HeartbeatEnvelope, HeartbeatRequest, SnapshotEnvelope
from services.agent.identity_service import DeviceIdentity
from services.agent.runtime_service import (
    MIN_SCHEMA_VERSION,
    AgentRuntimeError,
    AgentRuntimeService,
    get_runtime_service,
)
from services.blocking import BlockingQueueFull, RETRY_AFTER, run_blocking


router = APIRouter()


def _raise(exc):
    raise HTTPException(
        status_code=exc.status_code,
        detail={"code": exc.code, "message": exc.message},
    ) from exc


def _queue_full(exc):
    raise HTTPException(
        status_code=503,
        detail={"code": "unavailable", "message": "Service is busy"},
        headers={"Retry-After": str(RETRY_AFTER)},
    ) from exc


def _validate_schema_version(value):
    if value is None:
        return
    try:
        version = int(value)
    except (TypeError, ValueError):
        version = -1
    if version < MIN_SCHEMA_VERSION:
        raise HTTPException(
            status_code=422,
            detail={
                "code": "schema_mismatch",
                "message": f"Schema version {value} is no longer supported, minimum is {MIN_SCHEMA_VERSION}",
            },
        )


@router.post("/heartbeat", response_model=HeartbeatEnvelope)
async def heartbeat(
    request: HeartbeatRequest,
    x_tw_agent_version: str | None = Header(default=None, alias="X-TW-Agent-Version", max_length=50),
    x_tw_schema_version: str | None = Header(default=None, alias="X-TW-Schema-Version"),
    principal: DeviceIdentity = Depends(verify_agent_token),
    service: AgentRuntimeService = Depends(get_runtime_service),
):
    _ = (x_tw_agent_version, x_tw_schema_version)
    try:
        data = await run_blocking(service.heartbeat, principal, request)
    except AgentRuntimeError as exc:
        _raise(exc)
    except BlockingQueueFull as exc:
        _queue_full(exc)
    return {"schema_version": "1", "server_time": data["server_time"], "data": data}


@router.get("/snapshot", response_model=SnapshotEnvelope, responses={304: {"description": "Snapshot has not changed"}})
async def snapshot(
    response: Response,
    if_none_match: str | None = Header(default=None, alias="If-None-Match"),
    x_tw_schema_version: str | None = Header(default=None, alias="X-TW-Schema-Version"),
    principal: DeviceIdentity = Depends(verify_agent_token),
    service: AgentRuntimeService = Depends(get_runtime_service),
):
    _validate_schema_version(x_tw_schema_version)
    try:
        result = await run_blocking(service.snapshot, principal)
    except AgentRuntimeError as exc:
        _raise(exc)
    except BlockingQueueFull as exc:
        _queue_full(exc)
    if if_none_match == result.etag:
        return Response(status_code=status.HTTP_304_NOT_MODIFIED, headers={"ETag": result.etag})
    response.headers["ETag"] = result.etag
    return {"schema_version": "1", "server_time": result.generated_at, "data": result.data}
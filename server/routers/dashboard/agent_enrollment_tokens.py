import uuid
from datetime import datetime, timezone

from fastapi import APIRouter, Depends, HTTPException, Query, status

from routers.login.auth import verify_token
from schemas.agent_identity import EnrollmentTokenCreateRequest
from services.agent.identity_service import (
    AgentIdentityService,
    IdentityError,
    get_identity_service,
)


router = APIRouter()


def _envelope(data):
    return {
        "schema_version": "1",
        "server_time": datetime.now(timezone.utc),
        "data": data,
    }


def _raise(exc: IdentityError):
    raise HTTPException(
        status_code=exc.status_code,
        detail={"code": exc.code, "message": exc.message},
    ) from exc


@router.post("", status_code=status.HTTP_201_CREATED)
@router.post("/", status_code=status.HTTP_201_CREATED, include_in_schema=False)
def issue_enrollment_token(
    request: EnrollmentTokenCreateRequest,
    user_id: str = Depends(verify_token),
    service: AgentIdentityService = Depends(get_identity_service),
):
    try:
        return _envelope(
            service.issue_enrollment(
                user_id,
                request.device_name,
                request.group_id,
                request.ttl_hours,
            )
        )
    except IdentityError as exc:
        _raise(exc)


@router.get("")
@router.get("/", include_in_schema=False)
def list_enrollment_tokens(
    group_id: int | None = Query(default=None, ge=0),
    user_id: str = Depends(verify_token),
    service: AgentIdentityService = Depends(get_identity_service),
):
    try:
        return _envelope(service.list_enrollments(user_id, group_id))
    except IdentityError as exc:
        _raise(exc)


@router.delete("/{enrollment_id}")
def revoke_enrollment_token(
    enrollment_id: uuid.UUID,
    user_id: str = Depends(verify_token),
    service: AgentIdentityService = Depends(get_identity_service),
):
    try:
        return _envelope(service.revoke_enrollment(user_id, enrollment_id))
    except IdentityError as exc:
        _raise(exc)

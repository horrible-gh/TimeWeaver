from datetime import datetime, timezone

from fastapi import APIRouter, Depends, HTTPException, Request, status

from schemas.agent_identity import EnrollRequest, RefreshRequest
from services.agent.identity_service import (
    AgentIdentityService,
    IdentityError,
    get_identity_service,
)
from services.blocking import BlockingQueueFull, RETRY_AFTER, run_blocking
from services.security.rate_limit import (
    RateLimitExceeded,
    enforce_untrusted_rate_limit,
)


router = APIRouter()


def _envelope(data):
    return {"schema_version": "1", "server_time": datetime.now(timezone.utc), "data": data}


def _raise_identity(exc):
    raise HTTPException(
        status_code=exc.status_code,
        detail={"code": exc.code, "message": exc.message},
    ) from exc


def _raise_common(exc):
    if isinstance(exc, RateLimitExceeded):
        raise HTTPException(
            status_code=429,
            detail={
                "code": "rate_limited",
                "message": "Request rate exceeded",
                "retry_after": exc.retry_after,
            },
            headers={"Retry-After": str(exc.retry_after)},
        ) from exc
    raise HTTPException(
        status_code=503,
        detail={"code": "unavailable", "message": "Service is busy"},
        headers={"Retry-After": str(RETRY_AFTER)},
    ) from exc


def _enroll(service, http_request, request):
    enforce_untrusted_rate_limit(http_request, "enroll")
    return service.enroll(
        request.enrollment_token,
        request.device_name,
        request.agent_version,
    )


@router.post("/enroll", status_code=status.HTTP_200_OK)
async def enroll(
    request: EnrollRequest,
    http_request: Request,
    service: AgentIdentityService = Depends(get_identity_service),
):
    try:
        return _envelope(await run_blocking(_enroll, service, http_request, request))
    except IdentityError as exc:
        _raise_identity(exc)
    except (RateLimitExceeded, BlockingQueueFull) as exc:
        _raise_common(exc)


def _rotate(service, http_request, request):
    enforce_untrusted_rate_limit(http_request, "refresh")
    return service.rotate(request.refresh_token)


@router.post("/token/refresh")
@router.post("/token", include_in_schema=False)
async def refresh(
    request: RefreshRequest,
    http_request: Request,
    service: AgentIdentityService = Depends(get_identity_service),
):
    try:
        return _envelope(await run_blocking(_rotate, service, http_request, request))
    except IdentityError as exc:
        _raise_identity(exc)
    except (RateLimitExceeded, BlockingQueueFull) as exc:
        _raise_common(exc)
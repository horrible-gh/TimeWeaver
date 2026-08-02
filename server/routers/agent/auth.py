from fastapi import Depends, HTTPException, Request
from fastapi.security import HTTPAuthorizationCredentials, HTTPBearer

from services.agent.identity_service import (
    AgentIdentityService,
    DeviceIdentity,
    IdentityError,
    get_identity_service,
)
from services.blocking import BlockingQueueFull, RETRY_AFTER, run_blocking
from services.security.rate_limit import (
    RateLimitExceeded,
    enforce_device_rate_limit,
)


agent_bearer = HTTPBearer(auto_error=False)


def _verify_and_limit(service, token):
    principal = service.verify_access(token)
    enforce_device_rate_limit(principal.device_id)
    return principal


async def verify_agent_token(
    request: Request,
    credentials: HTTPAuthorizationCredentials | None = Depends(agent_bearer),
    service: AgentIdentityService = Depends(get_identity_service),
) -> DeviceIdentity:
    if credentials is None or credentials.scheme.lower() != "bearer":
        raise HTTPException(
            status_code=401,
            detail={"code": "invalid_token", "message": "Agent bearer token required"},
            headers={"WWW-Authenticate": "Bearer"},
        )
    try:
        principal = await run_blocking(
            _verify_and_limit, service, credentials.credentials
        )
        # Expose the authenticated device to the request-log middleware.
        request.state.device_id = principal.device_id
        return principal
    except IdentityError as exc:
        raise HTTPException(
            status_code=exc.status_code,
            detail={"code": exc.code, "message": exc.message},
            headers={"WWW-Authenticate": "Bearer"} if exc.status_code == 401 else None,
        ) from exc
    except RateLimitExceeded as exc:
        raise HTTPException(
            status_code=429,
            detail={
                "code": "rate_limited",
                "message": "Device request rate exceeded",
                "retry_after": exc.retry_after,
            },
            headers={"Retry-After": str(exc.retry_after)},
        ) from exc
    except BlockingQueueFull as exc:
        raise HTTPException(
            status_code=503,
            detail={"code": "unavailable", "message": "Service is busy"},
            headers={"Retry-After": str(RETRY_AFTER)},
        ) from exc
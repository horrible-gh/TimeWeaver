from datetime import datetime, timezone

from fastapi import APIRouter, Depends, HTTPException
from fastapi.security import OAuth2PasswordBearer

from routers.login.auth import decode_user_token
from services.security.revocation import (
    RevocationStoreUnavailable,
    get_revocation_store,
)
from util.safe_logging import safe_log


router = APIRouter()
oauth2_scheme = OAuth2PasswordBearer(tokenUrl="/token")


@router.post("/")
def logout(token: str = Depends(oauth2_scheme)):
    payload = decode_user_token(token)
    remaining = max(
        1,
        int(payload["exp"] - datetime.now(timezone.utc).timestamp()),
    )
    try:
        get_revocation_store().revoke(payload["jti"], remaining)
    except RevocationStoreUnavailable as exc:
        raise HTTPException(
            status_code=503,
            detail={
                "code": "unavailable",
                "message": "Authentication service unavailable",
            },
        ) from exc
    safe_log("debug", "logout_succeeded", {"user_id": payload["sub"]})
    return {"message": "Logged out successfully"}
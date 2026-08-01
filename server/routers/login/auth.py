from datetime import datetime, timezone

import jwt
from fastapi import Depends, HTTPException
from fastapi.security import OAuth2PasswordBearer

from config import settings
from services.security.revocation import (
    RevocationStoreUnavailable,
    get_revocation_store,
)


SECRET_KEY = settings.SECRET_KEY
ALGORITHM = "HS256"
USER_AUDIENCE = "timeweaver-dashboard"
USER_TOKEN_TYPE = "user"
oauth2_scheme = OAuth2PasswordBearer(tokenUrl="/token")


def _authentication_error(code="invalid_token", message="Invalid authentication credentials"):
    return HTTPException(
        status_code=401,
        detail={"code": code, "message": message},
        headers={"WWW-Authenticate": "Bearer"},
    )


def decode_user_token(token: str):
    try:
        payload = jwt.decode(
            token,
            SECRET_KEY,
            algorithms=[ALGORITHM],
            audience=USER_AUDIENCE,
            options={"require": ["sub", "exp", "aud", "typ", "jti"]},
        )
    except jwt.ExpiredSignatureError as exc:
        raise _authentication_error("token_expired", "Token has expired") from exc
    except jwt.InvalidTokenError as exc:
        raise _authentication_error() from exc
    if payload.get("typ") != USER_TOKEN_TYPE:
        raise _authentication_error()
    if not isinstance(payload.get("sub"), str) or not payload["sub"]:
        raise _authentication_error()
    if not isinstance(payload.get("jti"), str) or not payload["jti"]:
        raise _authentication_error()
    return payload


def verify_token(token: str = Depends(oauth2_scheme)):
    payload = decode_user_token(token)
    try:
        revoked = get_revocation_store().is_revoked(payload["jti"])
    except RevocationStoreUnavailable as exc:
        raise HTTPException(
            status_code=503,
            detail={
                "code": "unavailable",
                "message": "Authentication service unavailable",
            },
        ) from exc
    if revoked:
        raise _authentication_error("invalid_token", "Token has been revoked")
    return payload["sub"]
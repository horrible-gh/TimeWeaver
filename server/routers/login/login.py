import uuid
from datetime import datetime, timedelta, timezone

import jwt
from fastapi import APIRouter, Depends, HTTPException
from fastapi.security import OAuth2PasswordBearer, OAuth2PasswordRequestForm
from passlib.context import CryptContext

from config import db, settings
from services.blocking import BlockingQueueFull, RETRY_AFTER, run_blocking
from util.safe_logging import safe_log


db_instance = db.db_instance
sqloader = db.sqloader
SECRET_KEY = settings.SECRET_KEY
ALGORITHM = "HS256"
ACCESS_TOKEN_EXPIRE_MINUTES = settings.ACCESS_TOKEN_EXPIRE_MINUTES
USER_AUDIENCE = "timeweaver-dashboard"
USER_TOKEN_TYPE = "user"
pwd_context = CryptContext(schemes=["pbkdf2_sha256"], deprecated="auto")
router = APIRouter()
oauth2_scheme = OAuth2PasswordBearer(tokenUrl="/token")


def verify_password(plain_password, hashed_password):
    return pwd_context.verify(plain_password, hashed_password)


def authenticate_user(username: str, password: str):
    user_pw = db_instance.fetch_one(
        sqloader.load_sql("time_weaver", "get_password"), username
    )
    if not user_pw or not verify_password(password, user_pw.get("password", "")):
        return False
    return db_instance.fetch_one(sqloader.load_sql("time_weaver", "get_user"), username)


def create_access_token(data: dict, expires_delta: timedelta):
    now = datetime.now(timezone.utc)
    to_encode = data.copy()
    to_encode.update(
        {
            "aud": USER_AUDIENCE,
            "typ": USER_TOKEN_TYPE,
            "jti": str(uuid.uuid4()),
            "iat": now,
            "exp": now + expires_delta,
        }
    )
    return jwt.encode(to_encode, SECRET_KEY, algorithm=ALGORITHM)


def _queue_full(exc):
    raise HTTPException(
        status_code=503,
        detail={"code": "unavailable", "message": "Service is busy"},
        headers={"Retry-After": str(RETRY_AFTER)},
    ) from exc


@router.post("/")
@router.post("")
async def login(form_data: OAuth2PasswordRequestForm = Depends()):
    try:
        user = await run_blocking(
            authenticate_user, form_data.username, form_data.password
        )
    except BlockingQueueFull as exc:
        _queue_full(exc)
    if not user:
        raise HTTPException(status_code=400, detail="Invalid credentials")

    access_token = create_access_token(
        data={"sub": user["user_id"]},
        expires_delta=timedelta(minutes=ACCESS_TOKEN_EXPIRE_MINUTES),
    )
    safe_log("debug", "login_succeeded", {"user_id": user["user_id"]})
    return {"access_token": access_token, "token_type": "bearer", "user": user}
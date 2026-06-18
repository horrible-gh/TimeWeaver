from fastapi import APIRouter, Depends, HTTPException
from fastapi.security import OAuth2PasswordBearer, OAuth2PasswordRequestForm
from passlib.context import CryptContext
import jwt
from datetime import datetime, timedelta, timezone
from config import settings, db

import LogAssist.log as logger

db_instance = db.db_instance
sqloader = db.sqloader

# JWT Secret Key (실제 서비스에서는 환경변수 사용 권장)
SECRET_KEY = settings.SECRET_KEY
ALGORITHM = "HS256"
ACCESS_TOKEN_EXPIRE_MINUTES = settings.ACCESS_TOKEN_EXPIRE_MINUTES

# 비밀번호 해싱 설정
pwd_context = CryptContext(schemes=["pbkdf2_sha256"], deprecated="auto")

router = APIRouter()

# OAuth2 방식으로 토큰 받기
oauth2_scheme = OAuth2PasswordBearer(tokenUrl="/token")

def verify_password(plain_password, hashed_password):
    return pwd_context.verify(plain_password, hashed_password)

def authenticate_user(username: str, password: str):
    user_pw = db_instance.fetch_one(sqloader.load_sql("time_weaver", "get_password"), username)
    if not user_pw or not verify_password(password, user_pw.get("password", "")):
        return False
    result =  db_instance.fetch_one(sqloader.load_sql("time_weaver", "get_user"), username)
    logger.debug(result)
    return result


def create_access_token(data: dict, expires_delta: timedelta):
    to_encode = data.copy()
    expire = datetime.now(timezone.utc) + expires_delta
    to_encode.update({"exp": expire})
    encoded_jwt = jwt.encode(to_encode, SECRET_KEY, algorithm=ALGORITHM)

    return encoded_jwt


@router.post("/")
@router.post("")
async def login(form_data: OAuth2PasswordRequestForm = Depends()):
    user = authenticate_user(form_data.username, form_data.password)
    if not user:
        raise HTTPException(status_code=400, detail="Invalid credentials")

    access_token_expires = timedelta(minutes=ACCESS_TOKEN_EXPIRE_MINUTES)
    access_token = create_access_token(
        data={"sub": user["user_id"]}, expires_delta=access_token_expires
    )
    logger.debug({"access_token": access_token, "token_type": "bearer", "user": user})
    return {"access_token": access_token, "token_type": "bearer", "user": user}

from fastapi import APIRouter, Depends, HTTPException
from fastapi.security import OAuth2PasswordBearer, OAuth2PasswordRequestForm
from passlib.context import CryptContext
import jwt
from datetime import datetime, timedelta, timezone
from config import settings, db
import redis

db_instance = db.db_instance
sqloader = db.sqloader

router = APIRouter()

# Receive token through OAuth2
oauth2_scheme = OAuth2PasswordBearer(tokenUrl="/token")

# JWT Secret Key (Use environment variables in production)
SECRET_KEY = settings.SECRET_KEY
ALGORITHM = "HS256"
ACCESS_TOKEN_EXPIRE_MINUTES = settings.ACCESS_TOKEN_EXPIRE_MINUTES


redis_client = redis.Redis(host='localhost', port=6379, db=0, decode_responses=True)

@router.post("/")
async def logout(token: str = Depends(oauth2_scheme)):
    """ Add the current JWT token to the blacklist for logout """
    exp_time = jwt.decode(token, SECRET_KEY, algorithms=[ALGORITHM])["exp"]
    remaining_time = exp_time - datetime.now(timezone.utc).timestamp()

    # ✅ Store the token in Redis until it expires
    redis_client.setex(f"blacklist:{token}", int(remaining_time), "1")

    return {"message": "Logged out successfully"}

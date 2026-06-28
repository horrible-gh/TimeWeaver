from fastapi import APIRouter, Depends
from fastapi.security import OAuth2PasswordBearer
import jwt
from datetime import datetime, timezone
from config import settings
from routers.login.auth import token_blacklist
import LogAssist.log as Logger

try:
    import redis
except ImportError:  # redis package missing -> in-process blacklist still works
    redis = None

router = APIRouter()

# Receive token through OAuth2
oauth2_scheme = OAuth2PasswordBearer(tokenUrl="/token")

# JWT Secret Key (Use environment variables in production)
SECRET_KEY = settings.SECRET_KEY
ALGORITHM = "HS256"
ACCESS_TOKEN_EXPIRE_MINUTES = settings.ACCESS_TOKEN_EXPIRE_MINUTES

# Redis is OPTIONAL. It is only a shared blacklist store for multi-worker /
# multi-host deployments. When it is not installed or not reachable we fall back
# to the in-process blacklist that verify_token already consults, so logout
# works out of the box with zero extra services to install or configure.
_redis_client = None
if redis is not None:
    try:
        _redis_client = redis.Redis(
            host=settings.REDIS_HOST,
            port=settings.REDIS_PORT,
            db=settings.REDIS_DB,
            decode_responses=True,
            socket_connect_timeout=0.5,
        )
    except Exception:  # pragma: no cover - construction is lazy, but be safe
        _redis_client = None


@router.post("/")
async def logout(token: str = Depends(oauth2_scheme)):
    """ Add the current JWT token to the blacklist for logout """
    exp_time = jwt.decode(token, SECRET_KEY, algorithms=[ALGORITHM])["exp"]
    remaining_time = max(int(exp_time - datetime.now(timezone.utc).timestamp()), 1)

    # ✅ Always enforce logout via the in-process blacklist (checked by
    #    verify_token). This is what actually makes the token stop working.
    token_blacklist.add(token)

    # ✅ Best-effort shared blacklist. If Redis is down/absent we already logged
    #    the user out above, so we just note it and carry on (no 500).
    if _redis_client is not None:
        try:
            _redis_client.setex(f"blacklist:{token}", remaining_time, "1")
        except Exception as exc:
            Logger.debug(f"Redis blacklist unavailable, using in-process store: {exc}")

    return {"message": "Logged out successfully"}

"""Fail-closed shared revocation storage for dashboard JWTs."""
import hashlib

import redis

from config import settings


class RevocationStoreUnavailable(Exception):
    pass


def _key(jti: str) -> str:
    digest = hashlib.sha256(jti.encode("utf-8")).hexdigest()
    return f"timeweaver:user-revoked:{digest}"


class UserTokenRevocationStore:
    def __init__(self, client=None):
        self.client = client or redis.Redis(
            host=settings.REDIS_HOST,
            port=settings.REDIS_PORT,
            db=settings.REDIS_DB,
            decode_responses=True,
            socket_connect_timeout=0.5,
            socket_timeout=0.5,
        )

    def is_revoked(self, jti: str) -> bool:
        try:
            return self.client.get(_key(jti)) is not None
        except Exception as exc:
            raise RevocationStoreUnavailable() from exc

    def revoke(self, jti: str, ttl_seconds: int) -> None:
        try:
            self.client.setex(_key(jti), max(1, int(ttl_seconds)), "1")
        except Exception as exc:
            raise RevocationStoreUnavailable() from exc


_store = None


def get_revocation_store() -> UserTokenRevocationStore:
    global _store
    if _store is None:
        _store = UserTokenRevocationStore()
    return _store
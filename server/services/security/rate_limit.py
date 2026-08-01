"""Redis-backed token buckets with the protocol's fail-open policy."""
import hashlib
import ipaddress
import math
import threading
import time

import redis

from config import settings
from util.safe_logging import safe_log


_BUCKET_LUA = r"""
local now = redis.call('TIME')
local now_ms = tonumber(now[1]) * 1000 + math.floor(tonumber(now[2]) / 1000)
local data = redis.call('HMGET', KEYS[1], 'tokens', 'updated_ms')
local tokens = tonumber(data[1]) or tonumber(ARGV[1])
local updated_ms = tonumber(data[2]) or now_ms
local elapsed = math.max(0, now_ms - updated_ms) / 1000
local capacity = tonumber(ARGV[1])
local refill = tonumber(ARGV[2])
tokens = math.min(capacity, tokens + elapsed * refill)
local allowed = 0
local retry_after = 0
if tokens >= 1 then
  tokens = tokens - 1
  allowed = 1
else
  retry_after = math.max(1, math.ceil((1 - tokens) / refill))
end
redis.call('HMSET', KEYS[1], 'tokens', tokens, 'updated_ms', now_ms)
redis.call('EXPIRE', KEYS[1], tonumber(ARGV[3]))
return {allowed, retry_after}
"""


class RateLimitExceeded(Exception):
    def __init__(self, retry_after: int):
        super().__init__("rate limited")
        self.retry_after = max(1, int(retry_after))


class DeviceRateLimiter:
    def __init__(self, client=None, capacity=None, refill=None):
        self.capacity = int(capacity or getattr(settings, "RATE_LIMIT_CAPACITY", 60))
        self.refill = float(refill or getattr(settings, "RATE_LIMIT_REFILL", 1.0))
        self.client = client or redis.Redis(
            host=getattr(settings, "REDIS_HOST", "localhost"),
            port=getattr(settings, "REDIS_PORT", 6379),
            db=getattr(settings, "REDIS_DB", 0),
            decode_responses=True,
            socket_connect_timeout=0.2,
            socket_timeout=0.2,
        )
        self._warning_lock = threading.Lock()
        self._unavailable_until = 0.0

    def enforce(self, bucket: str) -> None:
        now = time.monotonic()
        if now < self._unavailable_until:
            return
        digest = hashlib.sha256(bucket.encode("utf-8")).hexdigest()
        ttl = max(1, math.ceil((self.capacity / self.refill) * 2))
        try:
            result = self.client.eval(
                _BUCKET_LUA,
                1,
                f"timeweaver:rate:{digest}",
                self.capacity,
                self.refill,
                ttl,
            )
        except Exception:
            self._warn_fail_open(now)
            return
        if int(result[0]) != 1:
            raise RateLimitExceeded(int(result[1]))

    def _warn_fail_open(self, now: float) -> None:
        with self._warning_lock:
            if now < self._unavailable_until:
                return
            self._unavailable_until = now + 60.0
            safe_log(
                "warning",
                "rate_limit_store_unavailable",
                {"code": "rate_limit_fail_open"},
            )


_rate_limiter = None


def get_rate_limiter() -> DeviceRateLimiter:
    global _rate_limiter
    if _rate_limiter is None:
        _rate_limiter = DeviceRateLimiter()
    return _rate_limiter


def enforce_device_rate_limit(device_id: int) -> None:
    get_rate_limiter().enforce(f"device:{device_id}")


def _trusted_networks():
    configured = getattr(settings, "TRUSTED_PROXIES", "")
    values = configured if isinstance(configured, list) else str(configured).split(",")
    networks = []
    for value in values:
        value = str(value).strip()
        if not value:
            continue
        try:
            networks.append(ipaddress.ip_network(value, strict=False))
        except ValueError:
            continue
    return networks


def request_address(request) -> str:
    peer = request.client.host if request.client else "unknown"
    try:
        trusted = any(ipaddress.ip_address(peer) in network for network in _trusted_networks())
    except ValueError:
        trusted = False
    if trusted:
        forwarded = request.headers.get("x-forwarded-for", "").split(",", 1)[0].strip()
        if forwarded:
            try:
                return str(ipaddress.ip_address(forwarded))
            except ValueError:
                pass
    return peer


def enforce_untrusted_rate_limit(request, endpoint: str) -> None:
    get_rate_limiter().enforce(f"unauthenticated:{endpoint}:{request_address(request)}")
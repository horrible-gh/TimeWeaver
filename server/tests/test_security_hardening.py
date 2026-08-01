import asyncio
import os
import subprocess
import sys
import threading
import time
from concurrent.futures import ThreadPoolExecutor
from datetime import timedelta
from pathlib import Path
from types import SimpleNamespace

import jwt
import pytest
from fastapi import Depends, FastAPI, HTTPException
from fastapi.testclient import TestClient
from fastapi.exceptions import RequestValidationError
from starlette.requests import Request

from routers.login import auth as user_auth
from routers.login import login as login_router
from routers.login import logout as logout_router
from services.security import revocation
from services.security.rate_limit import DeviceRateLimiter
from util.safe_logging import redact


class MemoryRevocationStore:
    def __init__(self, revoked=None, fail=False):
        self.revoked = revoked if revoked is not None else set()
        self.fail = fail

    def is_revoked(self, jti):
        if self.fail:
            raise revocation.RevocationStoreUnavailable()
        return jti in self.revoked

    def revoke(self, jti, ttl_seconds):
        if self.fail:
            raise revocation.RevocationStoreUnavailable()
        assert ttl_seconds > 0
        self.revoked.add(jti)


def user_token(**claims):
    data = {"sub": "admin"}
    data.update(claims)
    return login_router.create_access_token(data, timedelta(minutes=5))


def auth_header(token):
    return {"Authorization": f"Bearer {token}"}


def auth_app(monkeypatch, store):
    monkeypatch.setattr(user_auth, "get_revocation_store", lambda: store)
    monkeypatch.setattr(logout_router, "get_revocation_store", lambda: store)
    app = FastAPI()

    @app.get("/protected")
    def protected(user_id=Depends(user_auth.verify_token)):
        return {"user_id": user_id}

    app.include_router(logout_router.router, prefix="/logout")
    return TestClient(app)


def test_user_jwt_claims_cross_type_rejection_and_shared_logout(monkeypatch):
    shared = set()
    store = MemoryRevocationStore(shared)
    client = auth_app(monkeypatch, store)
    token = user_token()
    claims = jwt.decode(
        token,
        "test-secret-key",
        algorithms=["HS256"],
        audience="timeweaver-dashboard",
    )
    assert claims["aud"] == "timeweaver-dashboard"
    assert claims["typ"] == "user"
    assert claims["jti"]
    assert claims["jti"] not in revocation._key(claims["jti"])
    assert client.get("/protected", headers=auth_header(token)).status_code == 200

    # The logout writer and a later verifier consult the same shared state.
    assert client.post("/logout/", headers=auth_header(token)).status_code == 200
    assert client.get("/protected", headers=auth_header(token)).status_code == 401

    legacy = jwt.encode(
        {"sub": "admin", "exp": claims["exp"]},
        "test-secret-key",
        algorithm="HS256",
    )
    wrong_type = jwt.encode(
        {**claims, "typ": "agent", "aud": "timeweaver-agent"},
        "test-secret-key",
        algorithm="HS256",
    )
    from services.agent.identity_service import _agent_secret
    actual_agent = jwt.encode(
        {
            "sub": "device:7",
            "aud": "timeweaver-agent",
            "typ": "agent",
            "jti": "agent-jti",
            "credential_id": 4,
            "exp": claims["exp"],
        },
        _agent_secret(),
        algorithm="HS256",
    )
    for invalid in (legacy, wrong_type, actual_agent):
        response = client.get("/protected", headers=auth_header(invalid))
        assert response.status_code == 401
        assert response.json()["detail"]["code"] == "invalid_token"


def test_revocation_storage_failure_is_fail_closed(monkeypatch):
    client = auth_app(monkeypatch, MemoryRevocationStore(fail=True))
    token = user_token()
    protected = client.get("/protected", headers=auth_header(token))
    logged_out = client.post("/logout/", headers=auth_header(token))
    assert protected.status_code == logged_out.status_code == 503
    assert protected.json()["detail"]["code"] == "unavailable"
    assert logged_out.json()["detail"]["code"] == "unavailable"


class FakeBucketRedis:
    def __init__(self, fail=False):
        self.fail = fail
        self.state = {}

    def eval(self, _script, _keys, key, capacity, refill, _ttl):
        if self.fail:
            raise ConnectionError("redis unavailable")
        tokens = self.state.get(key, float(capacity))
        if tokens >= 1:
            self.state[key] = tokens - 1
            return [1, 0]
        return [0, 1]


def test_device_token_bucket_limits_sixty_first_and_fails_open(monkeypatch):
    limiter = DeviceRateLimiter(client=FakeBucketRedis(), capacity=60, refill=1)
    for _ in range(60):
        limiter.enforce("device:7")
    from services.security.rate_limit import RateLimitExceeded
    with pytest.raises(RateLimitExceeded) as limited:
        limiter.enforce("device:7")
    assert limited.value.retry_after == 1

    warnings = []
    monkeypatch.setattr(
        "services.security.rate_limit.safe_log",
        lambda level, event, fields: warnings.append((level, event, fields)),
    )
    fail_open = DeviceRateLimiter(client=FakeBucketRedis(fail=True), capacity=60, refill=1)
    fail_open.enforce("device:7")
    fail_open.enforce("device:7")
    assert len(warnings) == 1


def test_debug_route_removed_and_validation_logging_omits_secrets(monkeypatch):
    from routers import main

    assert all("debug-headers" not in getattr(route, "path", "") for route in main.app.routes)
    captured = []
    monkeypatch.setattr(main, "safe_log", lambda level, event, fields: captured.append((level, event, fields)))
    secret = "a" * 64
    scope = {
        "type": "http",
        "method": "POST",
        "path": "/api/agent/v1/events",
        "raw_path": b"/api/agent/v1/events",
        "query_string": b"",
        "headers": [(b"authorization", b"Bearer eyJ.secret.value")],
        "scheme": "http",
        "server": ("test", 80),
        "client": ("127.0.0.1", 1234),
    }
    request = Request(scope)
    exc = RequestValidationError(
        [{"type": "value_error", "loc": ("body", "claim_token"), "msg": "bad", "input": secret}]
    )
    asyncio.run(main.validation_exception_handler(request, exc))
    serialized = repr(captured)
    assert secret not in serialized
    assert "eyJ.secret.value" not in serialized
    assert "authorization" not in serialized.lower()

    scrubbed = redact(
        {
            "Authorization": "Bearer eyJabc.def.ghi",
            "claim-token": secret,
            "message": "credential enr_abcdefghijklmnopqrstuvwxyz leaked",
        }
    )
    assert repr(scrubbed).count("[REDACTED]") >= 3


def test_health_is_clean_and_independent_of_blocking_worker(monkeypatch):
    from routers.agent import health as health_router
    from services import blocking

    new_executor = ThreadPoolExecutor(max_workers=1)
    monkeypatch.setattr(blocking, "_executor", new_executor)
    monkeypatch.setattr(blocking, "_capacity", threading.BoundedSemaphore(1))
    started = threading.Event()
    release = threading.Event()

    def slow_database_call():
        started.set()
        release.wait(timeout=2)
        return "done"

    async def scenario():
        busy = asyncio.create_task(blocking.run_blocking(slow_database_call))
        while not started.is_set():
            await asyncio.sleep(0.001)
        with pytest.raises(blocking.BlockingQueueFull):
            await blocking.run_blocking(lambda: None)
        monkeypatch.setattr(health_router, "database_ready", lambda: True)
        result = await health_router.health()
        assert result == {"ready": True}
        release.set()
        assert await busy == "done"

    try:
        asyncio.run(scenario())
    finally:
        release.set()
        new_executor.shutdown(wait=True)

    app = FastAPI()
    app.include_router(health_router.router, prefix="/api/agent/v1")
    client = TestClient(app)
    monkeypatch.setattr(health_router, "database_ready", lambda: False)
    unavailable = client.get("/api/agent/v1/health")
    assert unavailable.status_code == 503
    assert unavailable.json() == {"ready": False}
    assert unavailable.headers["Retry-After"] == "5"
    assert all(word not in unavailable.text.lower() for word in ("mysql", "table", "schema", "exception"))


def test_dashboard_task_queue_saturation_returns_503(monkeypatch):
    from routers.dashboard import tasks
    from services.blocking import BlockingQueueFull

    async def full(*_args, **_kwargs):
        raise BlockingQueueFull()

    monkeypatch.setattr(tasks, "run_blocking", full)
    monkeypatch.setattr(tasks, "db_instance", SimpleNamespace(fetch_all=lambda *_: []))
    request = SimpleNamespace(model_dump=lambda: {"group_id": 0})
    with pytest.raises(HTTPException) as response:
        asyncio.run(tasks.get_Tasks(request))
    assert response.value.status_code == 503
    assert response.value.headers["Retry-After"] == "5"


@pytest.mark.parametrize(
    "updates,reason",
    [
        ({"SECRET_KEY": "short-secret"}, "SECRET_KEY"),
        ({"ALLOWED_ORIGIN": "*"}, "wildcard"),
        ({"RATE_LIMIT_REFILL": "0"}, "RATE_LIMIT_REFILL"),
    ],
)
def test_unsafe_startup_configuration_is_rejected_without_secret_echo(updates, reason):
    server_root = Path(__file__).resolve().parents[1]
    environment = os.environ.copy()
    environment.update(
        {
            "ALLOWED_ORIGIN": "http://127.0.0.1:3000",
            "SECRET_KEY": "s" * 32,
            "AGENT_SECRET_KEY": "",
            "CONTEXT": "/timeweaver",
            "DB_TYPE": "sqlite",
            "DB_PATH": ":memory:",
            "RATE_LIMIT_REFILL": "1",
        }
    )
    environment.update(updates)
    result = subprocess.run(
        [sys.executable, "-c", "import config"],
        cwd=server_root,
        env=environment,
        capture_output=True,
        text=True,
        timeout=10,
    )
    output = result.stdout + result.stderr
    assert result.returncode != 0
    assert reason.lower() in output.lower()
    assert environment["SECRET_KEY"] not in output
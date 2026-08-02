"""T0004 6.1 T8: agent API request logging middleware."""

import pytest
from fastapi import FastAPI, HTTPException, Request
from fastapi.testclient import TestClient

import LogAssist.log as Logger
from util.agent_request_logging import register_agent_request_logging


AGENT_TOKEN = "eyJhbGciOiJIUzI1NiJ9.agentpayload.agentsignature"


@pytest.fixture
def captured(monkeypatch):
    records = []

    def recorder(level):
        def _log(tag="", msg=None):
            records.append((level, f"{tag} {msg}" if msg else str(tag)))

        return _log

    for level in ("debug", "info", "warn", "error"):
        monkeypatch.setattr(Logger, level, recorder(level))
    return records


def make_app():
    app = FastAPI()
    register_agent_request_logging(app)

    @app.post("/time_weaver/api/agent/v1/heartbeat")
    async def heartbeat(request: Request):
        if request.headers.get("authorization") != f"Bearer {AGENT_TOKEN}":
            raise HTTPException(status_code=401, detail={"code": "invalid_token"})
        request.state.device_id = 7
        return {"schema_version": "1", "server_time": "now", "data": {}}

    @app.get("/time_weaver/api/agent/v1/crash")
    async def crash():
        raise RuntimeError("synthetic server failure")

    return app


def test_2xx_401_and_context_missing_404_are_recorded_without_authorization(captured):
    client = TestClient(make_app())

    ok = client.post(
        "/time_weaver/api/agent/v1/heartbeat",
        headers={"Authorization": f"Bearer {AGENT_TOKEN}", "x-request-id": "req-1"},
    )
    assert ok.status_code == 200

    unauthorized = client.post(
        "/time_weaver/api/agent/v1/heartbeat",
        headers={"Authorization": "Bearer wrong-token"},
    )
    assert unauthorized.status_code == 401

    context_missing = client.post(
        "/api/agent/v1/heartbeat",
        headers={"Authorization": f"Bearer {AGENT_TOKEN}"},
    )
    assert context_missing.status_code == 404

    agent_logs = [
        (level, line) for level, line in captured if "agent_api_request" in line
    ]
    assert len(agent_logs) == 3
    assert [level for level, _ in agent_logs] == ["debug", "warn", "warn"]

    assert '"status_code": 200' in agent_logs[0][1]
    assert '"device_id": 7' in agent_logs[0][1]
    assert '"request_id": "req-1"' in agent_logs[0][1]
    assert '"status_code": 401' in agent_logs[1][1]
    # A CONTEXT-missing request must still leave its 404 (task 6-2).
    assert '"status_code": 404' in agent_logs[2][1]
    assert '"path": "/api/agent/v1/heartbeat"' in agent_logs[2][1]

    joined = "\n".join(line for _, line in captured)
    assert AGENT_TOKEN not in joined
    assert "wrong-token" not in joined


def test_5xx_is_logged_at_error_without_authorization(captured):
    client = TestClient(make_app(), raise_server_exceptions=False)

    response = client.get(
        "/time_weaver/api/agent/v1/crash",
        headers={"Authorization": f"Bearer {AGENT_TOKEN}"},
    )

    assert response.status_code == 500
    agent_logs = [
        (level, line) for level, line in captured if "agent_api_request" in line
    ]
    assert len(agent_logs) == 1
    assert agent_logs[0][0] == "error"
    assert '"status_code": 500' in agent_logs[0][1]
    assert AGENT_TOKEN not in agent_logs[0][1]


def test_non_agent_paths_are_not_logged(captured):
    client = TestClient(make_app())

    response = client.get("/time_weaver/dashboard/devices/get_devices")
    assert response.status_code in (404, 405)
    assert not [line for _, line in captured if "agent_api_request" in line]

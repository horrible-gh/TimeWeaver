"""In-process FIFO transport and exact protocol scenarios; opens no sockets."""

from __future__ import annotations

from collections import deque
from dataclasses import dataclass, field
from typing import Any, Mapping


SERVER_TIME = "2026-08-01T05:00:00Z"


@dataclass(slots=True)
class MockResponse:
    status_code: int = 200
    payload: Any = field(default_factory=lambda: envelope({}))
    headers: Mapping[str, str] = field(default_factory=dict)

    def json(self) -> Any:
        if isinstance(self.payload, BaseException):
            raise self.payload
        return self.payload


@dataclass(frozen=True, slots=True)
class RecordedRequest:
    method: str
    url: str
    kwargs: Mapping[str, Any]


class MockTransport:
    def __init__(self, *outcomes: MockResponse | BaseException) -> None:
        self._outcomes = deque(outcomes)
        self.requests: list[RecordedRequest] = []
        self._lock = __import__("threading").Lock()

    def enqueue(self, outcome: MockResponse | BaseException) -> None:
        with self._lock:
            self._outcomes.append(outcome)

    def queue_timeout(self) -> None:
        self.enqueue(TimeoutError("mock timeout"))

    def queue_connection_refused(self) -> None:
        self.enqueue(ConnectionError("mock connection refused"))

    def request(self, method: str, url: str, **kwargs: Any) -> MockResponse:
        with self._lock:
            self.requests.append(RecordedRequest(method=method, url=url, kwargs=kwargs))
            if not self._outcomes:
                raise AssertionError("mock transport has no queued outcome")
            outcome = self._outcomes.popleft()
        if isinstance(outcome, BaseException):
            raise outcome
        return outcome


def envelope(data: Any, *, schema_version: str = "1", server_time: str = SERVER_TIME) -> dict[str, Any]:
    return {"schema_version": schema_version, "server_time": server_time, "data": data}


def error_envelope(
    code: str,
    *,
    message: str = "mock protocol error",
    retry_after: int | None = None,
    schema_version: str = "1",
) -> dict[str, Any]:
    return {
        "schema_version": schema_version,
        "server_time": SERVER_TIME,
        "error": {"code": code, "message": message, "retry_after": retry_after},
    }


def credential_data(*, include_identity: bool = False) -> dict[str, Any]:
    data: dict[str, Any] = {
        "access_token": "access-value",
        "access_token_expires_at": "2026-08-01T05:15:00Z",
        "refresh_token": "refresh-value",
        "refresh_token_expires_at": "2026-10-30T05:00:00Z",
    }
    if include_identity:
        data.update({"device_id": 7, "device_name": "batch-01"})
    return data


def enroll_success() -> MockResponse:
    return MockResponse(payload=envelope(credential_data(include_identity=True)))


def token_success() -> MockResponse:
    return MockResponse(payload=envelope(credential_data()))


def heartbeat_success() -> MockResponse:
    return MockResponse(payload=envelope({
        "device_id": 7, "device_status": "active", "server_time": SERVER_TIME
    }))


def snapshot_success() -> MockResponse:
    digest = "a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f9"
    return MockResponse(
        headers={"ETag": f'W/"{digest[:16]}"'},
        payload=envelope({
            "revision": f"sha256:{digest}",
            "generated_at": "2026-08-01T05:10:00Z",
            "device": {
                "device_id": 7,
                "device_name": "batch-01",
                "status": "active",
                "known_agent_version": "v0.1.79-20251123.0",
            },
            "schedules": [],
            "manual_runs": [],
        }),
    )


def not_modified() -> MockResponse:
    return MockResponse(status_code=304, payload=None, headers={"ETag": 'W/"a1b2c3d4e5f60718"'})
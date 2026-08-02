"""Agent API request observability middleware.

Records every request whose path contains the agent API marker - membership,
not a CONTEXT-prefixed match, so a request that misses the CONTEXT prefix
(e.g. /api/agent/v1/heartbeat instead of /time_weaver/api/agent/v1/heartbeat)
still leaves its 404 in the log. Bodies and the Authorization header are never
recorded; everything goes through safe_log redaction.
"""

import time

from util.safe_logging import safe_log

AGENT_API_MARKER = "/api/agent/v1"


def is_agent_api_path(path: str) -> bool:
    return AGENT_API_MARKER in path


def level_for_status(status_code: int) -> str:
    if status_code >= 500:
        return "error"
    if status_code >= 400:
        return "warn"
    return "debug"


def register_agent_request_logging(app) -> None:
    @app.middleware("http")
    async def agent_api_request_logger(request, call_next):
        if not is_agent_api_path(request.url.path):
            return await call_next(request)
        started = time.perf_counter()
        try:
            response = await call_next(request)
        except Exception:
            _log_request(request, 500, started)
            raise
        _log_request(request, response.status_code, started)
        return response


def _log_request(request, status_code: int, started: float) -> None:
    safe_log(
        level_for_status(status_code),
        "agent_api_request",
        {
            "method": request.method,
            "path": request.url.path,
            "status_code": status_code,
            "elapsed_ms": round((time.perf_counter() - started) * 1000, 1),
            "request_id": request.headers.get("x-request-id"),
            # Set by verify_agent_token after successful authentication.
            "device_id": getattr(request.state, "device_id", None),
        },
    )

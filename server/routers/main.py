from fastapi import FastAPI, Request
from fastapi.exceptions import RequestValidationError
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import JSONResponse

from config import settings
from services.agent.claim_sweeper import start_claim_sweeper, stop_claim_sweeper
from util.safe_logging import safe_log
from .agent import (
    enroll as agent_enroll,
    execution as agent_execution,
    health as agent_health,
    runtime as agent_runtime,
)
from .dashboard import (
    agent_enrollment_tokens,
    charts,
    dashboard,
    devices,
    groups,
    manual_execution,
    schedule,
    tasks,
)
from .login import login, logout

import LogAssist.log as Logger


Logger.logger_init()
ALLOWED_ORIGIN = [item.strip() for item in settings.ALLOWED_ORIGIN.split(",") if item.strip()]
CONTEXT = settings.CONTEXT

app = FastAPI()
app.include_router(login.router, prefix=f"{CONTEXT}/login", tags=["Login"])
app.include_router(logout.router, prefix=f"{CONTEXT}/logout", tags=["Logout"])
app.include_router(charts.router, prefix=f"{CONTEXT}/dashboard/charts", tags=["Charts"])
app.include_router(dashboard.router, prefix=f"{CONTEXT}/dashboard", tags=["Dashboard"])
app.include_router(devices.router, prefix=f"{CONTEXT}/dashboard/devices", tags=["Devices"])
app.include_router(schedule.router, prefix=f"{CONTEXT}/dashboard/schedule", tags=["Schedules"])
app.include_router(tasks.router, prefix=f"{CONTEXT}/dashboard/tasks", tags=["Tasks"])
app.include_router(groups.router, prefix=f"{CONTEXT}/dashboard/groups", tags=["Groups"])
app.include_router(manual_execution.router, prefix=f"{CONTEXT}/dashboard/manual_execution", tags=["Groups"])
app.include_router(
    agent_enrollment_tokens.router,
    prefix=f"{CONTEXT}/dashboard/agent-enrollment-tokens",
    tags=["Agent enrollment"],
)
for agent_router, tag in (
    (agent_enroll.router, "Agent identity"),
    (agent_runtime.router, "Agent runtime"),
    (agent_execution.router, "Agent execution"),
    (agent_health.router, "Agent health"),
):
    app.include_router(agent_router, prefix=f"{CONTEXT}/api/agent/v1", tags=[tag])


@app.on_event("startup")
def start_background_jobs():
    start_claim_sweeper()


@app.on_event("shutdown")
def stop_background_jobs():
    stop_claim_sweeper()


app.add_middleware(
    CORSMiddleware,
    allow_origins=ALLOWED_ORIGIN,
    allow_credentials=getattr(settings, "CORS_ALLOW_CREDENTIALS", True),
    allow_methods=["*"],
    allow_headers=["*"],
)


@app.get(CONTEXT + "/")
async def read_root():
    return {"message": "Hello FastAPI"}


@app.get(CONTEXT + "/items/{item_id}")
async def read_item(item_id: int, q: str = None):
    return {"item_id": item_id, "query": q}


@app.exception_handler(RequestValidationError)
async def validation_exception_handler(request: Request, exc: RequestValidationError):
    safe_log(
        "debug",
        "request_validation_failed",
        {
            "request_id": request.headers.get("x-request-id"),
            "method": request.method,
            "path": request.url.path,
            "status_code": 422,
            "error_code": "invalid_request",
            "error_count": len(exc.errors()),
        },
    )
    return JSONResponse(
        status_code=422,
        content={
            "detail": {
                "code": "invalid_request",
                "message": "Request validation failed",
            }
        },
    )
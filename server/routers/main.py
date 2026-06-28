from fastapi import FastAPI, Depends, Request
from fastapi.middleware.cors import CORSMiddleware
from fastapi.exceptions import RequestValidationError
from .login import login, logout
from .dashboard import charts, dashboard, devices, schedule, groups, tasks, manual_execution
from routers.login.auth import verify_token, token_blacklist
import redis

from config import settings

import LogAssist.log as Logger

Logger.logger_init()

ALLOWED_ORIGIN = settings.ALLOWED_ORIGIN.split(",")
CONTEXT = settings.CONTEXT

redis_client = redis.Redis(
    host=settings.REDIS_HOST,
    port=settings.REDIS_PORT,
    db=settings.REDIS_DB,
    decode_responses=True,
)

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

app.add_middleware(
    CORSMiddleware,
    allow_origins=ALLOWED_ORIGIN,  # Allow all domains; tighten this for production
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

@app.get(CONTEXT + "/")
async def read_root():
    return {"message": "Hello FastAPI"}

@app.get(CONTEXT + "/items/{item_id}")
async def read_item(item_id: int, q: str = None):
    return {"item_id": item_id, "query": q}


@app.get(CONTEXT + "/debug-headers")
async def debug_headers(request: Request):
    Logger.debug(f"🔍 Request Headers: {request.headers}")  # ✅ Log all headers
    return {"headers": dict(request.headers)}


@app.exception_handler(RequestValidationError)
async def validation_exception_handler(request: Request, exc: RequestValidationError):
    Logger.debug("💥 Validation error occurred")
    Logger.debug("⛳ Path:", request.url)
    Logger.debug("📦 Details:\n", exc.errors())
    Logger.debug("📨 Raw body:\n", await request.body())

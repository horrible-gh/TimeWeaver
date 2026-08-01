"""Bounded executor for synchronous database work called by async routes."""
import asyncio
import functools
import os
import threading
from concurrent.futures import ThreadPoolExecutor

from config import settings


DEFAULT_WORKERS = max(4, min(32, 4 * (os.cpu_count() or 1)))
WORKER_COUNT = int(getattr(settings, "BLOCKING_DB_WORKER_COUNT", DEFAULT_WORKERS))
QUEUE_CAPACITY = int(
    getattr(settings, "BLOCKING_DB_QUEUE_CAPACITY", 2 * WORKER_COUNT)
)
RETRY_AFTER = 5

_executor = ThreadPoolExecutor(
    max_workers=WORKER_COUNT,
    thread_name_prefix="timeweaver-db",
)
# Permits cover running work and the explicitly bounded waiting queue.
_capacity = threading.BoundedSemaphore(WORKER_COUNT + QUEUE_CAPACITY)


class BlockingQueueFull(Exception):
    """Raised before submission when the dedicated DB queue has no capacity."""


async def run_blocking(function, *args, **kwargs):
    if not _capacity.acquire(blocking=False):
        raise BlockingQueueFull("blocking database queue is full")
    try:
        loop = asyncio.get_running_loop()
        call = functools.partial(function, *args, **kwargs)
        return await loop.run_in_executor(_executor, call)
    finally:
        _capacity.release()
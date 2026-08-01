import threading

from config import db, settings
from repositories.agent_execution import (
    AgentExecutionRepository,
    ExecutionRepositoryError,
)


EXPIRED_CLAIM_SWEEP_INTERVAL = 60

_stop = threading.Event()
_thread: threading.Thread | None = None
_guard = threading.Lock()


def sweep_expired_claims() -> int:
    repository = AgentExecutionRepository(db.db_instance)
    return repository.sweep_expired_claims()


def _sweep_loop():
    while not _stop.wait(EXPIRED_CLAIM_SWEEP_INTERVAL):
        try:
            sweep_expired_claims()
        except ExecutionRepositoryError:
            # A later interval retries. Request handlers still reclaim their
            # own expired row synchronously, so a transient sweep failure does
            # not block manual execution.
            continue


def start_claim_sweeper():
    global _thread
    if getattr(settings.DB_TYPE, "value", settings.DB_TYPE) != "mysql":
        return
    with _guard:
        if _thread is not None and _thread.is_alive():
            return
        _stop.clear()
        _thread = threading.Thread(
            target=_sweep_loop,
            name="timeweaver-expired-claim-sweeper",
            daemon=True,
        )
        _thread.start()


def stop_claim_sweeper():
    global _thread
    with _guard:
        _stop.set()
        thread = _thread
        _thread = None
    if thread is not None:
        thread.join(timeout=2)
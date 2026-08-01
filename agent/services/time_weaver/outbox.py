"""In-memory result outbox with reservation and per-execution FIFO barriers."""

from __future__ import annotations

from collections import defaultdict, deque
from collections.abc import Callable, Mapping
from dataclasses import dataclass
import json
from queue import Empty, Queue
import random
import threading
import time
from datetime import datetime
from typing import Any

from .api_client import (
    ApiClientError,
    AuthenticationError,
    ClientRejectedError,
    CommunicationError,
    DeviceInactiveError,
    MalformedResponseError,
    RateLimitError,
    SchemaMismatchError,
    TransientServerError,
)


PERMANENT_CODES = frozenset({
    "claim_expired", "not_found", "invalid_request", "schema_mismatch"
})
RETRYABLE_CODES = frozenset({
    "rate_limited", "server_error", "unavailable", "malformed_response"
})
ENVIRONMENT_INFO_MAX = 32_768


class RetryBackoff:
    """Shared exponential backoff policy for every transient delivery channel."""

    def __init__(
        self,
        *,
        initial_delay: float = 1.0,
        multiplier: float = 2.0,
        max_delay: float = 60.0,
        jitter_ratio: float = 0.20,
        random_uniform: Callable[[float, float], float] = random.uniform,
    ) -> None:
        if initial_delay <= 0 or multiplier < 1 or max_delay <= 0:
            raise ValueError("retry settings are invalid")
        if not 0 <= jitter_ratio <= 1:
            raise ValueError("retry_jitter_ratio must be between zero and one")
        self.initial_delay = initial_delay
        self.multiplier = multiplier
        self.max_delay = max_delay
        self.jitter_ratio = jitter_ratio
        self._random_uniform = random_uniform
        self.failures = 0

    def failure_delay(self, retry_after: float | None = None) -> float:
        self.failures += 1
        if retry_after is not None:
            return max(float(retry_after), 0.0)
        raw = min(
            self.max_delay,
            self.initial_delay * self.multiplier ** (self.failures - 1),
        )
        return raw * self._random_uniform(
            1 - self.jitter_ratio, 1 + self.jitter_ratio
        )

    def reset(self) -> None:
        self.failures = 0


class _DaemonExecutor:
    """Small daemon worker pool so a timed-out shutdown cannot pin the process."""

    def __init__(self, workers: int) -> None:
        self._queue: Queue = Queue()
        self._closed = False
        self._lock = threading.Lock()
        self._threads = []
        for index in range(workers):
            thread = threading.Thread(
                target=self._worker,
                name=f"timeweaver-outbox-{index}",
                daemon=True,
            )
            thread.start()
            self._threads.append(thread)

    def submit(self, func, *args) -> None:
        with self._lock:
            if self._closed:
                raise RuntimeError("executor is closed")
            self._queue.put((func, args))

    def shutdown(self, *, wait: bool = False, cancel_futures: bool = False) -> None:
        with self._lock:
            if self._closed:
                return
            self._closed = True
        if cancel_futures:
            while True:
                try:
                    self._queue.get_nowait()
                    self._queue.task_done()
                except Empty:
                    break
        for _thread in self._threads:
            self._queue.put(None)
        if wait:
            for thread in self._threads:
                thread.join()

    def _worker(self) -> None:
        while True:
            work = self._queue.get()
            try:
                if work is None:
                    return
                func, args = work
                func(*args)
            finally:
                self._queue.task_done()


@dataclass(frozen=True, slots=True)
class ResultEnvelope:
    execution_grp_id: str
    schedule_id: int
    detail_id: str
    attempt: int
    manual_id: int | None
    claim_token: str | None
    started_at: str
    finished_at: str
    result_code: int
    result_message: str | None
    environment_info: Mapping[str, Any]
    sequence: int

    def payload(self) -> dict[str, Any]:
        return {
            "execution_grp_id": self.execution_grp_id,
            "schedule_id": self.schedule_id,
            "detail_id": self.detail_id,
            "attempt": self.attempt,
            "manual_id": self.manual_id,
            "claim_token": self.claim_token,
            "started_at": self.started_at,
            "finished_at": self.finished_at,
            "result_code": self.result_code,
            "result_message": self.result_message,
            "environment_info": dict(self.environment_info),
        }


@dataclass(frozen=True, slots=True)
class OutboxItem:
    envelope: ResultEnvelope
    idempotency_key: str


class ResultOutbox:
    """Own reserved capacity, result queues and acknowledgement-driven delivery."""

    def __init__(
        self,
        client,
        *,
        state_manager=None,
        capacity: int = 10_000,
        high_watermark: int = 8_000,
        low_watermark: int = 5_000,
        sender_workers: int = 4,
        retry_initial_delay: float = 1.0,
        retry_multiplier: float = 2.0,
        retry_max_delay: float = 60.0,
        retry_jitter_ratio: float = 0.20,
        random_uniform: Callable[[float, float], float] = random.uniform,
        access_token_provider: Callable[[], str] | None = None,
        event_reporter: Callable[[str, str, str, str], None] | None = None,
        ack_callback: Callable[[ResultEnvelope, tuple[Mapping[str, Any], ...]], None] | None = None,
        rejection_callback: Callable[[ResultEnvelope, str], None] | None = None,
        executor=None,
        timer_factory: Callable[[float, Callable[[], None]], Any] | None = None,
    ) -> None:
        if capacity <= 0:
            raise ValueError("outbox_capacity must be positive")
        if not 0 <= low_watermark < high_watermark <= capacity:
            raise ValueError("outbox watermarks must satisfy 0 <= low < high <= capacity")
        if sender_workers < 1:
            raise ValueError("outbox_sender_workers must be positive")
        if retry_initial_delay <= 0 or retry_multiplier < 1 or retry_max_delay <= 0:
            raise ValueError("outbox retry settings are invalid")
        if not 0 <= retry_jitter_ratio <= 1:
            raise ValueError("retry_jitter_ratio must be between zero and one")
        self.client = client
        self.state_manager = state_manager
        self.capacity = capacity
        self.high_watermark = high_watermark
        self.low_watermark = low_watermark
        self.sender_workers = sender_workers
        self.retry_initial_delay = retry_initial_delay
        self.retry_multiplier = retry_multiplier
        self.retry_max_delay = retry_max_delay
        self.retry_jitter_ratio = retry_jitter_ratio
        self._random_uniform = random_uniform
        self._access_token_provider = access_token_provider
        self._event_reporter = event_reporter
        self._ack_callback = ack_callback
        self._rejection_callback = rejection_callback
        self._executor = executor or _DaemonExecutor(sender_workers)
        self._owns_executor = executor is None
        self._timer_factory = timer_factory or self._make_timer
        self._lock = threading.RLock()
        self._queues: dict[str, deque[OutboxItem]] = defaultdict(deque)
        self._reserved_slots = 0
        self._queued_results = 0
        self._active_groups: set[str] = set()
        self._sending_groups: set[str] = set()
        self._idle = threading.Condition(self._lock)
        self._retry_timers: dict[str, Any] = {}
        self._retry_backoffs: dict[str, RetryBackoff] = {}
        self._backlog_latched = False
        self._event_latches: set[str] = set()
        self._closed = False
        self.event_delivery_failures = 0

    @property
    def reserved_slots(self) -> int:
        with self._lock:
            return self._reserved_slots

    @property
    def queued_results(self) -> int:
        with self._lock:
            return self._queued_results

    @property
    def size(self) -> int:
        with self._lock:
            return self._reserved_slots + self._queued_results

    def reserve_slot(self) -> bool:
        emit = False
        with self._lock:
            if self._closed or self._reserved_slots + self._queued_results >= self.capacity:
                return False
            self._reserved_slots += 1
            emit = self._update_backlog_latch_locked()
        if emit:
            self._emit_event_once(
                "outbox:backlog",
                "outbox_backlog",
                "warning",
                "Result outbox entered backlog mode.",
            )
        return True

    def release_slot(self) -> None:
        with self._lock:
            if self._reserved_slots <= 0:
                raise RuntimeError("no reserved outbox slot to release")
            self._reserved_slots -= 1
            self._update_backlog_latch_locked()

    def enqueue(self, envelope: ResultEnvelope) -> OutboxItem:
        envelope = _bounded_envelope(envelope)
        key = f"{envelope.execution_grp_id}:{envelope.detail_id}:{envelope.attempt}"
        item = OutboxItem(envelope=envelope, idempotency_key=key)
        emit = False
        with self._lock:
            if self._reserved_slots <= 0:
                raise RuntimeError("an outbox slot must be reserved before enqueue")
            self._reserved_slots -= 1
            self._queues[envelope.execution_grp_id].append(item)
            self._queued_results += 1
            emit = self._update_backlog_latch_locked()
        if emit:
            self._emit_event_once(
                "outbox:backlog",
                "outbox_backlog",
                "warning",
                "Result outbox entered backlog mode.",
            )
        self._schedule_group(envelope.execution_grp_id)
        return item

    def peek(self, execution_grp_id: str) -> OutboxItem | None:
        with self._lock:
            queue = self._queues.get(execution_grp_id)
            return None if not queue else queue[0]

    def wake(self) -> None:
        with self._lock:
            groups = tuple(group for group, queue in self._queues.items() if queue)
        for group in groups:
            self._schedule_group(group)

    def close(
        self, *, wait: bool = False, timeout: float | None = None
    ) -> bool:
        """Stop new sends and optionally wait only for sends already in progress."""
        if timeout is not None and timeout < 0:
            raise ValueError("shutdown timeout must be non-negative")
        with self._idle:
            self._closed = True
            timers = tuple(self._retry_timers.values())
            self._retry_timers.clear()
        for timer in timers:
            cancel = getattr(timer, "cancel", None)
            if cancel is not None:
                cancel()
        drained = True
        if wait:
            deadline = None if timeout is None else time.monotonic() + timeout
            with self._idle:
                while self._sending_groups:
                    remaining = None if deadline is None else deadline - time.monotonic()
                    if remaining is not None and remaining <= 0:
                        drained = False
                        break
                    self._idle.wait(remaining)
        if self._owns_executor:
            self._executor.shutdown(wait=wait and drained, cancel_futures=True)
        return drained

    def _schedule_group(self, group: str) -> None:
        with self._lock:
            if (
                self._closed
                or self._is_halted()
                or group in self._active_groups
                or group in self._retry_timers
                or not self._queues.get(group)
            ):
                return
            self._active_groups.add(group)
        self._executor.submit(self._drain_group, group)

    def _drain_group(self, group: str) -> None:
        reschedule = False
        with self._idle:
            if self._closed:
                self._active_groups.discard(group)
                self._idle.notify_all()
                return
            self._sending_groups.add(group)
        try:
            while True:
                with self._lock:
                    if self._closed or self._is_halted():
                        return
                    queue = self._queues.get(group)
                    if not queue:
                        return
                    item = queue[0]
                try:
                    call_kwargs = {"idempotency_key": item.idempotency_key}
                    if self._access_token_provider is not None:
                        call_kwargs["access_token"] = self._access_token_provider()
                    response = self.client.report_execution_results(
                        item.envelope.execution_grp_id,
                        item.envelope.payload(),
                        **call_kwargs,
                    )
                    if not isinstance(response, Mapping):
                        raise MalformedResponseError("execution result data must be an object")
                except Exception as exc:
                    action = self._classify_failure(exc)
                    if action == "retry":
                        self._defer_group(group, getattr(exc, "retry_after", None))
                        return
                    if action == "halt":
                        return
                    code = getattr(exc, "code", "invalid_request")
                    self._remove_head(group, item)
                    self._notify_rejection(item.envelope, code)
                    self._emit_event_once(
                        f"result:{code}:{item.idempotency_key}",
                        "sync_error",
                        "error",
                        f"Execution result was permanently rejected ({code}).",
                    )
                    continue

                transitions = response.get("applied_transitions", ())
                if not isinstance(transitions, (list, tuple)):
                    transitions = ()
                normalized = tuple(value for value in transitions if isinstance(value, Mapping))
                self._remove_head(group, item)
                with self._lock:
                    self._retry_backoffs.pop(group, None)
                self._notify_ack(item.envelope, normalized)
        finally:
            with self._idle:
                self._active_groups.discard(group)
                self._sending_groups.discard(group)
                self._idle.notify_all()
                reschedule = (
                    not self._closed
                    and group not in self._retry_timers
                    and bool(self._queues.get(group))
                    and not self._is_halted()
                )
            if reschedule:
                self._schedule_group(group)

    def _classify_failure(self, exc: Exception) -> str:
        code = getattr(exc, "code", None)
        if isinstance(exc, DeviceInactiveError):
            if self.state_manager is not None:
                self.state_manager.device_status("inactive")
            return "halt"
        if isinstance(exc, AuthenticationError):
            if self.state_manager is not None:
                self.state_manager.credential_failed("needs_enrollment")
            return "halt" if self._is_halted() else "retry"
        if code in PERMANENT_CODES or isinstance(exc, (SchemaMismatchError, ClientRejectedError)):
            return "remove"
        if (
            code in RETRYABLE_CODES
            or isinstance(exc, (
                CommunicationError, TransientServerError, RateLimitError,
                MalformedResponseError,
            ))
        ):
            return "retry"
        if isinstance(exc, ApiClientError):
            return "remove"
        return "remove"

    def _remove_head(self, group: str, expected: OutboxItem) -> None:
        with self._lock:
            queue = self._queues.get(group)
            if not queue or queue[0] is not expected:
                return
            queue.popleft()
            self._queued_results -= 1
            if not queue:
                self._queues.pop(group, None)
            self._update_backlog_latch_locked()

    def _defer_group(self, group: str, retry_after: float | None) -> None:
        with self._idle:
            self._active_groups.discard(group)
            self._idle.notify_all()
            backoff = self._retry_backoffs.get(group)
            if backoff is None:
                backoff = RetryBackoff(
                    initial_delay=self.retry_initial_delay,
                    multiplier=self.retry_multiplier,
                    max_delay=self.retry_max_delay,
                    jitter_ratio=self.retry_jitter_ratio,
                    random_uniform=self._random_uniform,
                )
                self._retry_backoffs[group] = backoff
            delay = backoff.failure_delay(retry_after)
            timer = self._timer_factory(delay, lambda: self._retry_group(group))
            self._retry_timers[group] = timer
        start = getattr(timer, "start", None)
        if start is not None:
            start()

    def _retry_group(self, group: str) -> None:
        with self._lock:
            self._retry_timers.pop(group, None)
        self._schedule_group(group)

    def _update_backlog_latch_locked(self) -> bool:
        size = self._reserved_slots + self._queued_results
        if not self._backlog_latched and size >= self.high_watermark:
            self._backlog_latched = True
            return True
        if self._backlog_latched and size < self.low_watermark:
            self._backlog_latched = False
            self._event_latches.discard("outbox:backlog")
        return False

    def _emit_event_once(
        self, cause: str, event_type: str, severity: str, message: str
    ) -> None:
        with self._lock:
            if cause in self._event_latches:
                return
            self._event_latches.add(cause)
        if self._event_reporter is None:
            return
        try:
            self._event_reporter(event_type, severity, message, cause)
        except Exception:
            self.event_delivery_failures += 1

    def _notify_ack(
        self, envelope: ResultEnvelope, transitions: tuple[Mapping[str, Any], ...]
    ) -> None:
        if self._ack_callback is not None:
            try:
                self._ack_callback(envelope, transitions)
            except Exception:
                pass

    def _notify_rejection(self, envelope: ResultEnvelope, code: str) -> None:
        if self._rejection_callback is not None:
            try:
                self._rejection_callback(envelope, code)
            except Exception:
                pass

    def _is_halted(self) -> bool:
        if self.state_manager is None:
            return False
        state = getattr(self.state_manager, "state", None)
        return getattr(state, "value", state) == "HALTED"

    @staticmethod
    def _make_timer(delay: float, callback: Callable[[], None]):
        timer = threading.Timer(delay, callback)
        timer.daemon = True
        return timer


def _bounded_envelope(envelope: ResultEnvelope) -> ResultEnvelope:
    # The protocol defines no result_message byte ceiling. Preserve it exactly;
    # invalid_request remains a permanent item rejection under the A6 contract.
    message = envelope.result_message
    environment = dict(envelope.environment_info)
    for name in ("device_name", "user", "ip", "os", "host"):
        if _json_size(environment) <= ENVIRONMENT_INFO_MAX:
            break
        environment.pop(name, None)
    if _json_size(environment) > ENVIRONMENT_INFO_MAX:
        environment = {}
    return ResultEnvelope(
        execution_grp_id=envelope.execution_grp_id,
        schedule_id=envelope.schedule_id,
        detail_id=str(envelope.detail_id),
        attempt=envelope.attempt,
        manual_id=envelope.manual_id,
        claim_token=envelope.claim_token,
        started_at=_utc_text(envelope.started_at),
        finished_at=_utc_text(envelope.finished_at),
        result_code=envelope.result_code,
        result_message=message,
        environment_info=environment,
        sequence=envelope.sequence,
    )


def _json_size(value: Mapping[str, Any]) -> int:
    return len(json.dumps(value, ensure_ascii=False, separators=(",", ":")).encode("utf-8"))


def _utc_text(value: str | datetime) -> str:
    if isinstance(value, str):
        return value
    return value.isoformat().replace("+00:00", "Z")
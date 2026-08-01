"""Derived agent operating state and event-driven reason management."""

from __future__ import annotations

from collections.abc import Callable
from dataclasses import dataclass
from enum import Enum
import threading


class OperatingState(str, Enum):
    BOOTSTRAP = "BOOTSTRAP"
    HEALTHY = "HEALTHY"
    DEGRADED = "DEGRADED"
    HALTED = "HALTED"


FAIL_CLOSED_REASONS = frozenset({
    "needs_enrollment",
    "device_inactive",
    "device_revoked",
    "credential_persist_failed",
    "reconciliation_restore_failed",
})


@dataclass(frozen=True, slots=True)
class StateValue:
    state: OperatingState
    reasons: tuple[str, ...]
    execution_allowed: bool


@dataclass(frozen=True, slots=True)
class StateTransition:
    previous: StateValue
    current: StateValue


class OperatingStateManager:
    """Stores facts/reasons and publishes state changes without owning side effects."""

    def __init__(self) -> None:
        self._lock = threading.RLock()
        self._has_valid_snapshot = False
        self._fail_closed_reasons: set[str] = set()
        self._degraded_reasons: set[str] = set()
        self._transition_listeners: list[Callable[[StateTransition], None]] = []

    def value(self) -> StateValue:
        with self._lock:
            if self._fail_closed_reasons:
                state = OperatingState.HALTED
                reasons = self._fail_closed_reasons | self._degraded_reasons
            elif not self._has_valid_snapshot:
                state = OperatingState.BOOTSTRAP
                reasons = self._degraded_reasons
            elif self._degraded_reasons:
                state = OperatingState.DEGRADED
                reasons = self._degraded_reasons
            else:
                state = OperatingState.HEALTHY
                reasons = set()
            return StateValue(
                state=state,
                reasons=tuple(sorted(reasons)),
                execution_allowed=state in {OperatingState.HEALTHY, OperatingState.DEGRADED},
            )

    @property
    def state(self) -> OperatingState:
        return self.value().state

    def execution_allowed(self) -> bool:
        return self.value().execution_allowed

    def add_transition_listener(
        self, listener: Callable[[StateTransition], None]
    ) -> None:
        with self._lock:
            if listener not in self._transition_listeners:
                self._transition_listeners.append(listener)

    def credential_succeeded(self) -> StateValue:
        previous = self.value()
        with self._lock:
            self._clear_prefixed("credential:")
            self._fail_closed_reasons.discard("needs_enrollment")
        return self._publish(previous)

    def credential_failed(self, reason: str) -> StateValue:
        previous = self.value()
        with self._lock:
            if reason in FAIL_CLOSED_REASONS:
                self._fail_closed_reasons.add(reason)
                self._has_valid_snapshot = False
            else:
                self._degraded_reasons.add(f"credential:{reason}")
        return self._publish(previous)

    def enrollment_succeeded(self) -> StateValue:
        previous = self.value()
        with self._lock:
            self._fail_closed_reasons.difference_update({
                "needs_enrollment", "credential_persist_failed"
            })
            self._clear_prefixed("credential:")
        return self._publish(previous)

    def heartbeat_succeeded(self) -> StateValue:
        previous = self.value()
        with self._lock:
            self._clear_prefixed("heartbeat:")
        return self._publish(previous)

    def heartbeat_failed(self, reason: str) -> StateValue:
        previous = self.value()
        with self._lock:
            if reason in {"device_inactive", "device_revoked"}:
                self._fail_closed_reasons.add(reason)
                self._has_valid_snapshot = False
            elif reason in {"invalid_token", "token_expired", "needs_enrollment"}:
                self._fail_closed_reasons.add("needs_enrollment")
                self._has_valid_snapshot = False
            else:
                self._degraded_reasons.add(f"heartbeat:{reason}")
        return self._publish(previous)

    def snapshot_succeeded(self) -> StateValue:
        previous = self.value()
        with self._lock:
            self._has_valid_snapshot = True
            self._clear_prefixed("snapshot:")
            self._degraded_reasons.discard("reconciliation_failed")
        return self._publish(previous)

    def snapshot_failed(self, reason: str) -> StateValue:
        previous = self.value()
        with self._lock:
            self._degraded_reasons.add(f"snapshot:{reason}")
        return self._publish(previous)

    def clock_warning(self, active: bool) -> StateValue:
        previous = self.value()
        with self._lock:
            if active:
                self._degraded_reasons.add("clock_skew")
            else:
                self._degraded_reasons.discard("clock_skew")
        return self._publish(previous)

    def device_status(self, status: str) -> StateValue:
        previous = self.value()
        with self._lock:
            if status == "active":
                self._fail_closed_reasons.difference_update({
                    "device_inactive", "device_revoked"
                })
            elif status in {"inactive", "revoked"}:
                self._fail_closed_reasons.add(f"device_{status}")
                self._has_valid_snapshot = False
            else:
                self._degraded_reasons.add("device_status_unknown")
        return self._publish(previous)

    def reconciliation_failed(self, *, restored: bool) -> StateValue:
        previous = self.value()
        with self._lock:
            if restored:
                self._degraded_reasons.add("reconciliation_failed")
            else:
                self._fail_closed_reasons.add("reconciliation_restore_failed")
                self._has_valid_snapshot = False
        return self._publish(previous)

    def reconciliation_succeeded(self) -> StateValue:
        previous = self.value()
        with self._lock:
            self._degraded_reasons.discard("reconciliation_failed")
        return self._publish(previous)

    def clear_fail_closed_reason(self, reason: str) -> StateValue:
        previous = self.value()
        with self._lock:
            self._fail_closed_reasons.discard(reason)
        return self._publish(previous)

    def _publish(self, previous: StateValue) -> StateValue:
        current = self.value()
        if current.state is previous.state:
            return current
        with self._lock:
            listeners = tuple(self._transition_listeners)
        transition = StateTransition(previous, current)
        for listener in listeners:
            try:
                listener(transition)
            except Exception:
                continue
        return current

    def _clear_prefixed(self, prefix: str) -> None:
        self._degraded_reasons = {
            reason for reason in self._degraded_reasons if not reason.startswith(prefix)
        }

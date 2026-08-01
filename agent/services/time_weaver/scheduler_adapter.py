"""APScheduler implementation of the snapshot reconciliation adapter."""

from __future__ import annotations

from collections.abc import Callable, Mapping
from datetime import datetime, timedelta, timezone
import threading

from apscheduler.jobstores.base import JobLookupError
from apscheduler.triggers.cron import CronTrigger

from .models import ManualExecution, ScheduleGroup
from .sync_coordinator import JobSpec


REGULAR_MISFIRE_GRACE = 60
MANUAL_DISPATCH_DELAY = 1.0


class ApSchedulerAdapter:
    """Keep exact managed specs and use distinct runtime job namespaces."""

    def __init__(
        self,
        scheduler,
        on_schedule_trigger: Callable[[int], None],
        on_manual_trigger: Callable[[int], None] | None = None,
        *,
        misfire_grace_time: int = REGULAR_MISFIRE_GRACE,
        manual_dispatch_delay: float = MANUAL_DISPATCH_DELAY,
        now: Callable[[], datetime] | None = None,
    ) -> None:
        if misfire_grace_time < 1:
            raise ValueError("misfire_grace_time must be positive")
        if manual_dispatch_delay < 0:
            raise ValueError("manual_dispatch_delay must be non-negative")
        self._scheduler = scheduler
        self._on_schedule_trigger = on_schedule_trigger
        self._on_manual_trigger = on_manual_trigger or (lambda manual_id: None)
        self._misfire_grace_time = misfire_grace_time
        self._manual_dispatch_delay = manual_dispatch_delay
        self._now = now or (lambda: datetime.now(timezone.utc))
        self._specs: dict[str, JobSpec] = {}
        self._manual_inflight: set[int] = set()
        self._lock = threading.RLock()

    def add_job(self, key: str, spec: JobSpec) -> None:
        self._validate(key, spec)
        with self._lock:
            if spec.kind == "schedule":
                self._add_schedule(spec)
            else:
                self._specs[key] = spec
                self._schedule_manual_locked(spec)
            self._specs[key] = spec

    def update_job(self, key: str, spec: JobSpec) -> None:
        self._validate(key, spec)
        with self._lock:
            if spec.kind == "manual":
                manual = spec.value
                assert isinstance(manual, ManualExecution)
                self._remove_scheduler_job(self._dispatch_key(manual.manual_id))
                self._specs[key] = spec
                self._schedule_manual_locked(spec)
                return
            self.remove_job(key)
            self.add_job(key, spec)

    def remove_job(self, key: str) -> None:
        with self._lock:
            spec = self._specs.get(key)
            if key.startswith("schedule:") or (
                spec is not None and spec.kind == "schedule"
            ):
                self._remove_scheduler_job(key)
            if key.startswith("manual:") or (
                spec is not None and spec.kind == "manual"
            ):
                manual_id = self._manual_id(key, spec)
                self._remove_scheduler_job(self._dispatch_key(manual_id))
                self._manual_inflight.discard(manual_id)
            self._specs.pop(key, None)

    def list_jobs(self) -> Mapping[str, JobSpec]:
        with self._lock:
            return dict(self._specs)

    def schedule_pending_manuals(self) -> None:
        """Retry eligible one-shot dispatch only when a fresh snapshot is applied."""
        with self._lock:
            specs = tuple(
                spec for spec in self._specs.values() if spec.kind == "manual"
            )
            for spec in specs:
                self._schedule_manual_locked(spec)

    def mark_manual_attempted(self, manual_id: int) -> None:
        with self._lock:
            self._manual_inflight.add(int(manual_id))

    def release_manual(self, manual_id: int) -> None:
        """Allow a new snapshot to schedule a claim after a quiet/transient skip."""
        with self._lock:
            self._manual_inflight.discard(int(manual_id))

    def manual_inflight(self, manual_id: int) -> bool:
        with self._lock:
            return int(manual_id) in self._manual_inflight

    def _add_schedule(self, spec: JobSpec) -> None:
        schedule = spec.value
        assert isinstance(schedule, ScheduleGroup)
        cron = schedule.cron
        self._scheduler.add_job(
            func=self._on_schedule_trigger,
            trigger=CronTrigger(
                year=cron.year,
                month=cron.month,
                day_of_week=cron.day_of_week,
                day=cron.day,
                hour=cron.hour,
                minute=cron.minute,
                second=cron.second,
            ),
            args=[schedule.schedule_id],
            id=spec.key,
            replace_existing=True,
            misfire_grace_time=self._misfire_grace_time,
        )

    def _schedule_manual_locked(self, spec: JobSpec) -> None:
        manual = spec.value
        assert isinstance(manual, ManualExecution)
        if manual.manual_id in self._manual_inflight:
            return
        dispatch_key = self._dispatch_key(manual.manual_id)
        if self._scheduler.get_job(dispatch_key) is not None:
            return
        self._scheduler.add_job(
            func=self._dispatch_manual,
            trigger="date",
            run_date=self._now() + timedelta(seconds=self._manual_dispatch_delay),
            args=[manual.manual_id],
            id=dispatch_key,
            replace_existing=True,
        )

    def _dispatch_manual(self, manual_id: int) -> None:
        with self._lock:
            if manual_id in self._manual_inflight:
                return
        self._on_manual_trigger(manual_id)

    def _remove_scheduler_job(self, key: str) -> None:
        try:
            self._scheduler.remove_job(key)
        except JobLookupError:
            pass

    @staticmethod
    def _dispatch_key(manual_id: int) -> str:
        return f"manual_dispatch:{manual_id}"

    @staticmethod
    def _manual_id(key: str, spec: JobSpec | None) -> int:
        if spec is not None and isinstance(spec.value, ManualExecution):
            return spec.value.manual_id
        return int(key.split(":", 1)[1])

    @staticmethod
    def _validate(key: str, spec: JobSpec) -> None:
        if key != spec.key:
            raise ValueError("adapter key must match JobSpec.key")
        if spec.kind == "schedule":
            if not key.startswith("schedule:") or not isinstance(spec.value, ScheduleGroup):
                raise ValueError("schedule JobSpec has an invalid key or value")
        elif spec.kind == "manual":
            if not key.startswith("manual:") or not isinstance(spec.value, ManualExecution):
                raise ValueError("manual JobSpec has an invalid key or value")
        else:
            raise ValueError(f"unsupported managed job kind: {spec.kind}")
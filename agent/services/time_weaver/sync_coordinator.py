"""Snapshot validation, deterministic reconciliation and scheduler compensation."""

from __future__ import annotations

from dataclasses import asdict, dataclass
from datetime import datetime, timezone
import hashlib
import json
import re
import threading
from typing import Any, Iterable, Mapping, Protocol

from .api_client import SnapshotResponse
from .models import (
    CronFields,
    ManualExecution,
    ModelValidationError,
    ScheduleGroup,
    ScheduleSnapshot,
    parse_datetime,
    require_mapping,
)
from .operating_state import OperatingStateManager


SCHEMA_VERSION = "1"
REVISION_PATTERN = re.compile(r"^sha256:[0-9a-f]{64}$")
ETAG_PATTERN = 'W/"{}"'
MANAGED_PREFIXES = ("schedule:", "manual:")
snapshot_apply_lock = threading.RLock()


class SnapshotValidationError(ValueError):
    """The complete snapshot is rejected; no local state was changed."""


class StaleReconcilePlan(RuntimeError):
    pass


class ReconciliationApplyError(RuntimeError):
    pass


class ReconciliationRestoreError(RuntimeError):
    pass


@dataclass(frozen=True, slots=True)
class LocalIdentity:
    device_id: int
    device_name: str


@dataclass(frozen=True, slots=True)
class JobSpec:
    key: str
    kind: str
    fingerprint: str
    value: ScheduleGroup | ManualExecution


@dataclass(frozen=True, slots=True)
class ReconcilePlan:
    old_revision: str | None
    new_revision: str
    removals: tuple[str, ...]
    updates: tuple[JobSpec, ...]
    additions: tuple[JobSpec, ...]
    stop_after_current_sequence: tuple[str, ...]
    old_snapshot: ScheduleSnapshot | None = None

    @property
    def ordered_actions(self) -> tuple[tuple[str, str], ...]:
        return (
            tuple(("remove", key) for key in self.removals)
            + tuple(("update", spec.key) for spec in self.updates)
            + tuple(("add", spec.key) for spec in self.additions)
        )


@dataclass(slots=True)
class RunningContext:
    context_id: str
    kind: str
    schedule_id: int | None = None
    stop_after_current_sequence: bool = False


class SchedulerAdapter(Protocol):
    def add_job(self, key: str, spec: JobSpec) -> None: ...
    def update_job(self, key: str, spec: JobSpec) -> None: ...
    def remove_job(self, key: str) -> None: ...
    def list_jobs(self) -> Mapping[str, JobSpec]: ...


def validate_snapshot(
    response: SnapshotResponse | Mapping[str, Any],
    local_identity: LocalIdentity | tuple[int, str] | Mapping[str, Any],
) -> ScheduleSnapshot:
    """Validate every contract invariant before constructing an immutable snapshot."""
    try:
        envelope, etag = _response_parts(response)
        if envelope.get("schema_version") != SCHEMA_VERSION:
            raise SnapshotValidationError("schema_version mismatch")
        server_time = _utc_datetime(envelope.get("server_time"), "server_time")
        data = require_mapping(envelope.get("data"), "data")
        revision = data.get("revision")
        if not isinstance(revision, str) or not REVISION_PATTERN.fullmatch(revision):
            raise SnapshotValidationError("revision has an invalid format")
        expected_etag = ETAG_PATTERN.format(revision.removeprefix("sha256:")[:16])
        if etag != expected_etag:
            raise SnapshotValidationError("ETag does not match revision")
        generated_at = _utc_datetime(data.get("generated_at"), "generated_at")

        identity = _identity(local_identity)
        device_data = require_mapping(data.get("device"), "device")
        device_id = device_data.get("device_id")
        device_name = device_data.get("device_name")
        if device_id != identity.device_id or device_name != identity.device_name:
            raise SnapshotValidationError("snapshot device does not match local identity")
        if device_data.get("status") != "active":
            raise SnapshotValidationError("snapshot device is not active")

        schedules_data = data.get("schedules")
        manuals_data = data.get("manual_runs", [])
        if not isinstance(schedules_data, list) or not isinstance(manuals_data, list):
            raise SnapshotValidationError("schedules and manual_runs must be arrays")

        schedules: list[ScheduleGroup] = []
        schedule_ids: set[int] = set()
        detail_ids: set[str] = set()
        schedule_details: dict[int, set[str]] = {}
        for index, item in enumerate(schedules_data):
            schedule_data = require_mapping(item, f"schedules[{index}]")
            _require_fields(
                schedule_data,
                {"schedule_id", "name", "cron", "is_error_stop", "details"},
                f"schedules[{index}]",
            )
            _require_cron_shape(schedule_data["cron"], f"schedules[{index}].cron")
            schedule = ScheduleGroup.from_dict(schedule_data)
            if schedule.schedule_id in schedule_ids:
                raise SnapshotValidationError(f"schedules[{index}].schedule_id is duplicated")
            schedule_ids.add(schedule.schedule_id)
            _validate_cron(schedule.cron, f"schedules[{index}].cron")
            current_details: set[str] = set()
            for detail_index, (detail, detail_value) in enumerate(
                zip(schedule.details, schedule_data["details"])
            ):
                detail_data = require_mapping(
                    detail_value, f"schedules[{index}].details[{detail_index}]"
                )
                _require_fields(
                    detail_data,
                    {
                        "detail_id", "schedule_name", "cron", "is_error_stop",
                        "sequence", "exec_sequence", "retry_count", "task",
                    },
                    f"schedules[{index}].details[{detail_index}]",
                )
                _require_cron_shape(
                    detail_data["cron"], f"schedules[{index}].details[{detail_index}].cron"
                )
                task_data = require_mapping(
                    detail_data["task"], f"schedules[{index}].details[{detail_index}].task"
                )
                _require_fields(
                    task_data,
                    {
                        "task_type", "command", "archive_type", "source_path",
                        "error_on_missing_source", "destination_path", "date_format",
                        "target_date_format", "destination_date_format", "house_keep_days",
                    },
                    f"schedules[{index}].details[{detail_index}].task",
                )
                detail_key = str(detail.detail_id)
                if detail_key in detail_ids:
                    raise SnapshotValidationError(
                        f"schedules[{index}].details[{detail_index}].detail_id is duplicated"
                    )
                detail_ids.add(detail_key)
                current_details.add(detail_key)
                _validate_cron(
                    detail.cron, f"schedules[{index}].details[{detail_index}].cron"
                )
                _validate_task(detail, index, detail_index)
            schedule_details[schedule.schedule_id] = current_details
            ordered_details = sorted(
                schedule.details, key=lambda detail: (detail.sequence, str(detail.detail_id))
            )
            if any(
                left.exec_sequence > right.exec_sequence
                for left, right in zip(ordered_details, ordered_details[1:])
            ):
                raise SnapshotValidationError(
                    f"schedules[{index}].details exec_sequence must be nondecreasing"
                )
            schedules.append(schedule)

        manuals: list[ManualExecution] = []
        manual_ids: set[int] = set()
        for index, item in enumerate(manuals_data):
            manual_data = require_mapping(item, f"manual_runs[{index}]")
            _require_fields(
                manual_data,
                {
                    "manual_id", "schedule_id", "detail_id", "status",
                    "is_immediate", "schedule_datetime", "claimable",
                },
                f"manual_runs[{index}]",
            )
            manual = ManualExecution.from_dict(manual_data)
            if manual.manual_id in manual_ids:
                raise SnapshotValidationError(f"manual_runs[{index}].manual_id is duplicated")
            manual_ids.add(manual.manual_id)
            _require_utc(manual.schedule_datetime, f"manual_runs[{index}].schedule_datetime")
            if (
                manual.schedule_id not in schedule_details
                or str(manual.detail_id) not in schedule_details[manual.schedule_id]
            ):
                raise SnapshotValidationError(
                    f"manual_runs[{index}] references a missing schedule/detail pair"
                )
            manuals.append(manual)

        from .models import Device

        return ScheduleSnapshot(
            schema_version=SCHEMA_VERSION,
            revision=revision,
            etag=etag,
            server_time=server_time,
            generated_at=generated_at,
            device=Device.from_dict(device_data),
            schedules=tuple(schedules),
            manual_runs=tuple(manuals),
        )
    except SnapshotValidationError:
        raise
    except (ModelValidationError, TypeError, ValueError) as exc:
        raise SnapshotValidationError(str(exc)) from exc


def build_reconcile_plan(
    old: ScheduleSnapshot | None,
    new: ScheduleSnapshot,
    running_contexts: Iterable[RunningContext | Mapping[str, Any]] = (),
) -> ReconcilePlan:
    old_schedules = {} if old is None else {item.schedule_id: item for item in old.schedules}
    new_schedules = {item.schedule_id: item for item in new.schedules}
    old_manuals = {} if old is None else {item.manual_id: item for item in old.manual_runs}
    new_manuals = {item.manual_id: item for item in new.manual_runs}

    removal_keys: set[str] = set()
    updates: list[JobSpec] = []
    additions: list[JobSpec] = []

    for schedule_id in sorted(old_schedules.keys() - new_schedules.keys()):
        removal_keys.add(f"schedule:{schedule_id}")
    for schedule_id in sorted(new_schedules.keys() - old_schedules.keys()):
        additions.append(_schedule_spec(new_schedules[schedule_id]))
    for schedule_id in sorted(old_schedules.keys() & new_schedules.keys()):
        if _schedule_fingerprint(old_schedules[schedule_id]) != _schedule_fingerprint(new_schedules[schedule_id]):
            updates.append(_schedule_spec(new_schedules[schedule_id]))

    old_managed_manuals = {
        key: item for key, item in old_manuals.items() if _manual_managed(item)
    }
    new_managed_manuals = {
        key: item for key, item in new_manuals.items() if _manual_managed(item)
    }
    for manual_id in sorted(old_managed_manuals.keys() - new_managed_manuals.keys()):
        removal_keys.add(f"manual:{manual_id}")
    for manual_id in sorted(new_managed_manuals.keys() - old_managed_manuals.keys()):
        additions.append(_manual_spec(new_managed_manuals[manual_id]))
    for manual_id in sorted(old_managed_manuals.keys() & new_managed_manuals.keys()):
        if _manual_fingerprint(old_managed_manuals[manual_id]) != _manual_fingerprint(new_managed_manuals[manual_id]):
            updates.append(_manual_spec(new_managed_manuals[manual_id]))

    changed_schedule_ids = {
        schedule_id
        for schedule_id in old_schedules
        if schedule_id not in new_schedules
        or _schedule_fingerprint(old_schedules[schedule_id])
        != _schedule_fingerprint(new_schedules[schedule_id])
    }
    markers: list[str] = []
    for context in running_contexts:
        kind, schedule_id, context_id = _context_parts(context)
        if kind in {"regular", "schedule"} and schedule_id in changed_schedule_ids:
            markers.append(context_id)

    updates.sort(key=lambda spec: spec.key)
    additions.sort(key=lambda spec: spec.key)
    return ReconcilePlan(
        old_revision=None if old is None else old.revision,
        new_revision=new.revision,
        removals=tuple(sorted(removal_keys)),
        updates=tuple(updates),
        additions=tuple(additions),
        stop_after_current_sequence=tuple(sorted(markers)),
        old_snapshot=old,
    )


class SyncCoordinator:
    """Owns the last accepted snapshot and serializes short scheduler mutations."""

    def __init__(
        self,
        current_snapshot: ScheduleSnapshot | None = None,
        *,
        state_manager: OperatingStateManager | None = None,
    ) -> None:
        self._current_snapshot = current_snapshot
        self._state_manager = state_manager
        self.snapshot_apply_lock = snapshot_apply_lock

    @property
    def current_snapshot(self) -> ScheduleSnapshot | None:
        return self._current_snapshot

    @property
    def current_revision(self) -> str | None:
        return None if self._current_snapshot is None else self._current_snapshot.revision

    def remove_all_managed(self, adapter: SchedulerAdapter) -> None:
        """Remove future triggers and invalidate the snapshot for full recovery."""
        with self.snapshot_apply_lock:
            _remove_all_managed(adapter)
            self._current_snapshot = None

    def apply_reconcile_plan(
        self,
        expected_old_revision: str | None,
        new_snapshot: ScheduleSnapshot,
        plan: ReconcilePlan,
        adapter: SchedulerAdapter,
    ) -> ScheduleSnapshot:
        with self.snapshot_apply_lock:
            if self.current_revision != expected_old_revision or plan.old_revision != expected_old_revision:
                raise StaleReconcilePlan("reconcile plan was built from a stale revision")
            previous = _managed_jobs(adapter.list_jobs())
            try:
                for key in plan.removals:
                    adapter.remove_job(key)
                for spec in plan.updates:
                    adapter.update_job(spec.key, spec)
                for spec in plan.additions:
                    adapter.add_job(spec.key, spec)
            except Exception as mutation_error:
                try:
                    _restore_jobs(adapter, previous)
                except Exception as restore_error:
                    _remove_all_managed(adapter)
                    if self._state_manager is not None:
                        self._state_manager.reconciliation_failed(restored=False)
                    raise ReconciliationRestoreError(
                        "scheduler mutation and compensation both failed"
                    ) from restore_error
                if self._state_manager is not None:
                    self._state_manager.reconciliation_failed(restored=True)
                raise ReconciliationApplyError(
                    "scheduler mutation failed and previous jobs were restored"
                ) from mutation_error
            self._current_snapshot = new_snapshot
            if self._state_manager is not None:
                self._state_manager.snapshot_succeeded()
                self._state_manager.reconciliation_succeeded()
            return new_snapshot


def apply_reconcile_plan(
    expected_old_revision: str | None,
    new_snapshot: ScheduleSnapshot,
    plan: ReconcilePlan,
    adapter: SchedulerAdapter,
    *,
    coordinator: SyncCoordinator | None = None,
) -> ScheduleSnapshot:
    owner = coordinator or SyncCoordinator(plan.old_snapshot)
    return owner.apply_reconcile_plan(expected_old_revision, new_snapshot, plan, adapter)


def _response_parts(
    response: SnapshotResponse | Mapping[str, Any],
) -> tuple[Mapping[str, Any], str]:
    if isinstance(response, SnapshotResponse):
        return response.envelope, response.etag
    envelope = response.get("envelope", response)
    if not isinstance(envelope, Mapping):
        raise SnapshotValidationError("snapshot envelope must be an object")
    etag = response.get("etag") or response.get("ETag")
    if not isinstance(etag, str):
        raise SnapshotValidationError("snapshot response is missing ETag")
    return envelope, etag


def _identity(value: LocalIdentity | tuple[int, str] | Mapping[str, Any]) -> LocalIdentity:
    if isinstance(value, LocalIdentity):
        return value
    if isinstance(value, tuple) and len(value) == 2:
        return LocalIdentity(value[0], value[1])
    if isinstance(value, Mapping):
        return LocalIdentity(value["device_id"], value["device_name"])
    raise SnapshotValidationError("local_identity has an invalid shape")


def _utc_datetime(value: Any, field_name: str) -> datetime:
    parsed = parse_datetime(value, field_name)
    _require_utc(parsed, field_name)
    return parsed


def _require_utc(value: datetime, field_name: str) -> None:
    if value.utcoffset() != timezone.utc.utcoffset(value):
        raise SnapshotValidationError(f"{field_name} must be UTC")


def _require_fields(data: Mapping[str, Any], names: set[str], field_name: str) -> None:
    missing = sorted(names - data.keys())
    if missing:
        raise SnapshotValidationError(
            f"{field_name} is missing required field {missing[0]}"
        )


def _require_cron_shape(value: Any, field_name: str) -> None:
    data = require_mapping(value, field_name)
    _require_fields(
        data,
        {"year", "month", "day_of_week", "day", "hour", "minute", "second"},
        field_name,
    )


def _validate_cron(cron: CronFields, field_name: str) -> None:
    bounds = {
        "year": (1970, 9999), "month": (1, 12), "day_of_week": (0, 7),
        "day": (1, 31), "hour": (0, 23), "minute": (0, 59), "second": (0, 59),
    }
    allowed = re.compile(r"^[A-Za-z0-9*/,?\-#LW]+$")
    for name, (minimum, maximum) in bounds.items():
        expression = getattr(cron, name)
        if not allowed.fullmatch(expression):
            raise SnapshotValidationError(f"{field_name}.{name} is not parseable")
        letters = re.findall(r"[A-Za-z]+", expression)
        if name == "month":
            valid_words = {"jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"}
        elif name == "day_of_week":
            valid_words = {"mon", "tue", "wed", "thu", "fri", "sat", "sun"}
        elif name == "day":
            valid_words = {"L", "W", "LW"}
        else:
            valid_words = set()
        if any(word not in valid_words and word.lower() not in valid_words for word in letters):
            raise SnapshotValidationError(f"{field_name}.{name} is not parseable")
        for number in re.findall(r"\d+", expression):
            numeric = int(number)
            if "/" in expression and expression.endswith(number):
                if numeric < 1:
                    raise SnapshotValidationError(f"{field_name}.{name} has an invalid step")
            elif numeric < minimum or numeric > maximum:
                raise SnapshotValidationError(f"{field_name}.{name} is outside its range")


def _validate_task(detail: Any, schedule_index: int, detail_index: int) -> None:
    prefix = f"schedules[{schedule_index}].details[{detail_index}].task"
    task_type = detail.task_type
    values = {
        "command": detail.command,
        "archive_type": detail.archive_type,
        "source_path": detail.source_path,
        "destination_path": detail.destination_path,
        "house_keep_days": detail.house_keep_days,
    }
    required: dict[str, set[str]] = {
        "command": {"command"},
        "copy": {"source_path", "destination_path"},
        "archive": {"archive_type", "source_path", "destination_path"},
        "housekeep": {"destination_path", "house_keep_days"},
    }
    forbidden: dict[str, set[str]] = {
        "command": {"archive_type", "source_path", "destination_path", "house_keep_days"},
        "copy": {"command", "archive_type", "house_keep_days"},
        "archive": {"command", "house_keep_days"},
        "housekeep": {"command", "archive_type", "source_path"},
    }
    if task_type not in required:
        raise SnapshotValidationError(f"{prefix}.task_type is unsupported")
    missing = [name for name in required[task_type] if values[name] is None]
    present_forbidden = [name for name in forbidden[task_type] if values[name] is not None]
    if missing or present_forbidden:
        raise SnapshotValidationError(f"{prefix} fields are inconsistent with task_type")


def _schedule_fingerprint(schedule: ScheduleGroup) -> str:
    value = asdict(schedule)
    return _fingerprint(value)


def _manual_fingerprint(manual: ManualExecution) -> str:
    value = asdict(manual)
    value["detail_id"] = str(manual.detail_id)
    value["schedule_datetime"] = manual.schedule_datetime.astimezone(timezone.utc).isoformat()
    return _fingerprint(value)


def _fingerprint(value: Any) -> str:
    return hashlib.sha256(
        json.dumps(value, sort_keys=True, separators=(",", ":"), default=str).encode("utf-8")
    ).hexdigest()


def _schedule_spec(schedule: ScheduleGroup) -> JobSpec:
    return JobSpec(
        key=f"schedule:{schedule.schedule_id}",
        kind="schedule",
        fingerprint=_schedule_fingerprint(schedule),
        value=schedule,
    )


def _manual_spec(manual: ManualExecution) -> JobSpec:
    return JobSpec(
        key=f"manual:{manual.manual_id}",
        kind="manual",
        fingerprint=_manual_fingerprint(manual),
        value=manual,
    )


def _manual_managed(manual: ManualExecution) -> bool:
    return manual.claimable and manual.status not in {"done", "failed", "cancelled", "claimed"}


def _context_parts(context: RunningContext | Mapping[str, Any]) -> tuple[str, int | None, str]:
    if isinstance(context, RunningContext):
        return context.kind, context.schedule_id, context.context_id
    return (
        str(context.get("kind", context.get("execution_type", ""))),
        context.get("schedule_id"),
        str(context.get("context_id", context.get("execution_grp_id", "unknown"))),
    )


def _managed_jobs(jobs: Mapping[str, JobSpec]) -> dict[str, JobSpec]:
    return {key: value for key, value in jobs.items() if key.startswith(MANAGED_PREFIXES)}


def _restore_jobs(adapter: SchedulerAdapter, previous: Mapping[str, JobSpec]) -> None:
    current = _managed_jobs(adapter.list_jobs())
    for key in sorted(current):
        adapter.remove_job(key)
    for key in sorted(previous):
        adapter.add_job(key, previous[key])


def _remove_all_managed(adapter: SchedulerAdapter) -> None:
    try:
        keys = sorted(_managed_jobs(adapter.list_jobs()))
    except Exception:
        return
    for key in keys:
        try:
            adapter.remove_job(key)
        except Exception:
            continue
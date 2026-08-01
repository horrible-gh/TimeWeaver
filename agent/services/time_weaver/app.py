"""Snapshot-backed scheduling, manual claims and acknowledged result delivery."""

from __future__ import annotations

import datetime
from collections.abc import Mapping
from datetime import timedelta, timezone
import getpass
import os
import socket
from threading import RLock
import uuid

import LogAssist.log as Logger
from apscheduler.jobstores.base import JobLookupError
from apscheduler.schedulers.background import BackgroundScheduler


try:
    from agent.configure import agent_api_config, time_weaver_config as twconfig
    from agent.services.time_weaver import task
    from agent.services.time_weaver.api_client import (
        ApiClientError,
        AuthenticationError,
        ClientRejectedError,
        DeviceInactiveError,
        TransientServerError,
    )
    from agent.services.time_weaver.models import (
        ManualExecution, ScheduleDetail, ScheduleGroup, ScheduleSnapshot,
    )
    from agent.services.time_weaver.outbox import ResultEnvelope, ResultOutbox
    from agent.services.time_weaver.scheduler_adapter import ApSchedulerAdapter
    from agent.services.time_weaver.sync_coordinator import RunningContext, build_reconcile_plan
except ImportError:  # Script execution from the agent directory.
    from configure import agent_api_config, time_weaver_config as twconfig
    import services.time_weaver.task as task
    from services.time_weaver.api_client import (
        ApiClientError,
        AuthenticationError,
        ClientRejectedError,
        DeviceInactiveError,
        TransientServerError,
    )
    from services.time_weaver.models import (
        ManualExecution, ScheduleDetail, ScheduleGroup, ScheduleSnapshot,
    )
    from services.time_weaver.outbox import ResultEnvelope, ResultOutbox
    from services.time_weaver.scheduler_adapter import ApSchedulerAdapter
    from services.time_weaver.sync_coordinator import RunningContext, build_reconcile_plan


service_name = "time_weaver"
device_name = ""

credential_manager = None
operating_state_manager = None
api_client = None
sync_coordinator = None
scheduler_adapter: ApSchedulerAdapter | None = None
result_outbox: ResultOutbox | None = None

running_tasks: dict[int, bool] = {}
group_execution_status: dict[int, dict] = {}
task_completion_status: dict[int, dict[int, list[str]]] = {}
manual_execution_status: dict[int, dict] = {}
_event_causes: set[str] = set()
_event_suppressed_counts: dict[str, int] = {}
event_delivery_failures = 0

scheduler = BackgroundScheduler()
execution_lock = RLock()


def configure_sync(
    _credential_manager,
    _operating_state_manager,
    _api_client,
    _sync_coordinator,
    _scheduler_adapter: ApSchedulerAdapter | None = None,
    _result_outbox: ResultOutbox | None = None,
) -> ApSchedulerAdapter:
    """Wire snapshot, execution and result-delivery boundaries."""
    global device_name
    global credential_manager, operating_state_manager, api_client, sync_coordinator
    global scheduler_adapter, result_outbox

    if result_outbox is not None and result_outbox is not _result_outbox:
        result_outbox.close(wait=False)

    device_name = twconfig["device"]
    credential_manager = _credential_manager
    operating_state_manager = _operating_state_manager
    api_client = _api_client
    sync_coordinator = _sync_coordinator
    scheduler_adapter = _scheduler_adapter or ApSchedulerAdapter(
        scheduler,
        start_group_execution,
        dispatch_manual,
        manual_dispatch_delay=agent_api_config["manual_dispatch_delay"],
    )
    result_outbox = _result_outbox or ResultOutbox(
        api_client,
        state_manager=operating_state_manager,
        capacity=agent_api_config["outbox_capacity"],
        high_watermark=agent_api_config["outbox_high_watermark"],
        low_watermark=agent_api_config["outbox_low_watermark"],
        sender_workers=agent_api_config["outbox_sender_workers"],
        retry_initial_delay=agent_api_config["retry_initial_delay"],
        retry_multiplier=agent_api_config["retry_multiplier"],
        retry_max_delay=agent_api_config["retry_max_delay"],
        retry_jitter_ratio=agent_api_config["retry_jitter_ratio"],
        access_token_provider=_result_access_token,
        event_reporter=report_event_once,
        ack_callback=handle_result_ack,
        rejection_callback=handle_result_rejection,
    )
    add_listener = getattr(operating_state_manager, "add_transition_listener", None)
    if add_listener is not None:
        add_listener(_handle_operating_transition)
    return scheduler_adapter


def apply_snapshot(snapshot: ScheduleSnapshot) -> ScheduleSnapshot:
    """Build and apply a deterministic reconcile plan in a short lock section."""
    if sync_coordinator is None or scheduler_adapter is None:
        raise RuntimeError("configure_sync must be called before applying a snapshot")

    with execution_lock:
        contexts = []
        for schedule_id, status in group_execution_status.items():
            if running_tasks.get(schedule_id):
                contexts.append(
                    RunningContext(
                        context_id=str(status["context_id"]),
                        kind="schedule",
                        schedule_id=schedule_id,
                    )
                )
        for manual_id, status in manual_execution_status.items():
            contexts.append(
                RunningContext(
                    context_id=str(status["execution_grp_id"]),
                    kind="manual",
                    schedule_id=status["schedule_id"],
                )
            )
        old_snapshot = sync_coordinator.current_snapshot
        plan = build_reconcile_plan(old_snapshot, snapshot, contexts)
        applied = sync_coordinator.apply_reconcile_plan(
            None if old_snapshot is None else old_snapshot.revision,
            snapshot,
            plan,
            scheduler_adapter,
        )
        markers = set(plan.stop_after_current_sequence)
        for status in group_execution_status.values():
            if str(status["context_id"]) in markers:
                status["stop_after_current_sequence"] = True
    scheduler_adapter.schedule_pending_manuals()
    return applied


def get_current_snapshot() -> ScheduleSnapshot | None:
    return None if sync_coordinator is None else sync_coordinator.current_snapshot


def start_group_execution(group_id: int) -> bool:
    """Copy a regular group from the current snapshot into an execution context."""
    if operating_state_manager is None or not operating_state_manager.execution_allowed():
        Logger.warn(f"[start_group_execution] execution blocked for Group={group_id}")
        return False

    with execution_lock:
        if running_tasks.get(group_id):
            return False
        schedule_group = _find_schedule(group_id)
        if schedule_group is None:
            Logger.warn(f"[start_group_execution] schedule missing for Group={group_id}")
            return False
        sequences: dict[int, list[ScheduleDetail]] = {}
        for detail in schedule_group.details:
            sequences.setdefault(detail.exec_sequence, []).append(detail)
        if not sequences:
            return False
        context_id = uuid.uuid4()
        group_execution_status[group_id] = {
            "context_id": context_id,
            "schedule": schedule_group,
            "sequences": {key: tuple(value) for key, value in sequences.items()},
            "stop_after_current_sequence": False,
        }
        task_completion_status[group_id] = {}
        running_tasks[group_id] = True
        first_sequence = min(sequences)

    Logger.debug(f"[start_group_execution] Starting Group={group_id}")
    if not execute_next_task(group_id, first_sequence):
        with execution_lock:
            running_tasks[group_id] = False
        return False
    return True


def execute_next_task(group_id: int, sequence: int) -> bool:
    """Reserve result capacity, then register an immutable execution sequence."""
    if result_outbox is None:
        raise RuntimeError("result outbox is not configured")
    with execution_lock:
        status = group_execution_status.get(group_id)
        if status is None:
            return False
        tasks = status["sequences"].get(sequence, ())
        if not tasks:
            return False
        schedule_group: ScheduleGroup = status["schedule"]

    reserved = 0
    for _detail in tasks:
        if not result_outbox.reserve_slot():
            for _ in range(reserved):
                result_outbox.release_slot()
            with execution_lock:
                status["stop_after_current_sequence"] = True
            return False
        reserved += 1

    with execution_lock:
        task_completion_status[group_id][sequence] = [
            str(detail.detail_id) for detail in tasks
        ]
    scheduled_job_ids: list[str] = []
    try:
        for detail in tasks:
            run_time = datetime.datetime.now() + timedelta(seconds=1)
            detail_id = str(detail.detail_id)
            job_id = f"{group_id}_{detail_id}_{sequence}"
            scheduler.add_job(
                func=execute_task,
                id=job_id,
                args=[
                    detail_id,
                    group_id,
                    sequence,
                    _detail_task_data(detail),
                    schedule_group.is_error_stop,
                    detail.is_error_stop,
                    None,
                    None,
                    str(status["context_id"]),
                ],
                trigger="date",
                run_date=run_time,
                replace_existing=True,
            )
            scheduled_job_ids.append(job_id)
            Logger.debug(
                f"[execute_next_task] Detail={detail_id} scheduled at {run_time}"
            )
    except Exception:
        for job_id in scheduled_job_ids:
            try:
                scheduler.remove_job(job_id)
            except JobLookupError:
                pass
        for _ in range(reserved):
            result_outbox.release_slot()
        with execution_lock:
            task_completion_status[group_id].pop(sequence, None)
            status["stop_after_current_sequence"] = True
        raise
    return True


def dispatch_manual(manual_id: int) -> bool:
    """Claim one snapshot-listed manual run immediately before local dispatch."""
    if (
        operating_state_manager is None
        or not operating_state_manager.execution_allowed()
        or scheduler_adapter is None
        or result_outbox is None
    ):
        if scheduler_adapter is not None:
            scheduler_adapter.release_manual(manual_id)
        return False

    if scheduler_adapter.manual_inflight(manual_id):
        return False
    scheduler_adapter.mark_manual_attempted(manual_id)
    with execution_lock:
        manual = _find_manual(manual_id)
        if manual is None or not manual.claimable or manual.status in {
            "done", "failed", "cancelled", "claimed"
        }:
            scheduler_adapter.release_manual(manual_id)
            return False
        detail = _find_detail(manual.schedule_id, manual.detail_id)
        schedule_group = _find_schedule(manual.schedule_id)
        if detail is None or schedule_group is None:
            scheduler_adapter.release_manual(manual_id)
            return False

    if not result_outbox.reserve_slot():
        scheduler_adapter.release_manual(manual_id)
        return False

    try:
        claim = api_client.claim_manual_execution(manual_id)
    except ClientRejectedError as exc:
        result_outbox.release_slot()
        scheduler_adapter.release_manual(manual_id)
        if exc.code not in {"already_claimed", "not_found"}:
            _record_api_failure(exc)
        return False
    except TransientServerError:
        result_outbox.release_slot()
        scheduler_adapter.release_manual(manual_id)
        return False
    except ApiClientError as exc:
        result_outbox.release_slot()
        scheduler_adapter.release_manual(manual_id)
        _record_api_failure(exc)
        return False
    except Exception:
        result_outbox.release_slot()
        scheduler_adapter.release_manual(manual_id)
        return False

    claim_token = claim.get("claim_token") if isinstance(claim, Mapping) else None
    if not isinstance(claim_token, str) or not claim_token:
        result_outbox.release_slot()
        scheduler_adapter.release_manual(manual_id)
        report_event_once(
            "sync_error", "error", "Manual claim response was invalid.",
            f"manual-claim-shape:{manual_id}",
        )
        return False

    execution_grp_id = str(uuid.uuid4())
    with execution_lock:
        manual_execution_status[manual_id] = {
            "execution_grp_id": execution_grp_id,
            "schedule_id": manual.schedule_id,
            "detail_id": str(manual.detail_id),
            "claim_token": claim_token,
        }
    run_time = datetime.datetime.now() + timedelta(seconds=1)
    kwargs = {
        "func": execute_task,
        "id": f"manual_task:{manual_id}:{execution_grp_id}",
        "args": [
            str(detail.detail_id),
            manual.schedule_id,
            1,
            _detail_task_data(detail),
            schedule_group.is_error_stop,
            detail.is_error_stop,
            manual_id,
            claim_token,
            execution_grp_id,
        ],
        "trigger": "date",
        "run_date": run_time,
        "replace_existing": False,
    }
    try:
        scheduler.add_job(**kwargs)
    except Exception:
        execute_task(*kwargs["args"])
    return True


def execute_task(
    detail_id: str,
    group_id: int,
    sequence: int,
    task_data: dict,
    is_error_stop_group: bool,
    is_error_stop_detail: bool,
    manual_id=None,
    claim_token: str | None = None,
    execution_grp_id: str | None = None,
) -> None:
    """Execute locally and enqueue a result; server ACK decides progression."""
    del is_error_stop_group, is_error_stop_detail
    if result_outbox is None:
        raise RuntimeError("result outbox is not configured")
    if manual_id is None:
        running_tasks[group_id] = True

    started_at = datetime.datetime.now(timezone.utc)
    result = -1
    message = None
    try:
        result, message = task.task_run(task_data)
    except Exception:
        message = "Unexpected error during task execution."
        Logger.error(
            f"[execute_task] unexpected failure Group={group_id}, Detail={detail_id}"
        )
    finished_at = datetime.datetime.now(timezone.utc)

    if execution_grp_id is None:
        with execution_lock:
            status = group_execution_status.get(group_id)
            if status is None:
                raise RuntimeError("execution context is missing")
            execution_grp_id = str(status["context_id"])
    try:
        environment = get_environment_info(device_name)
    except Exception:
        Logger.warn("[execute_task] environment collection failed")
        environment = {}

    result_outbox.enqueue(
        ResultEnvelope(
            execution_grp_id=str(execution_grp_id),
            schedule_id=int(group_id),
            detail_id=str(detail_id),
            attempt=1,
            manual_id=manual_id,
            claim_token=claim_token,
            started_at=started_at.isoformat().replace("+00:00", "Z"),
            finished_at=finished_at.isoformat().replace("+00:00", "Z"),
            result_code=int(result),
            result_message=message,
            environment_info=environment,
            sequence=int(sequence),
        )
    )
    if result != 0:
        Logger.error(
            f"[execute_task] task failed Group={group_id}, Detail={detail_id}, Code={result}"
        )


def handle_result_ack(
    envelope: ResultEnvelope, transitions: tuple[dict, ...] | tuple
) -> None:
    """Apply server-owned transitions, then cross the sequence ACK barrier."""
    if envelope.manual_id is not None:
        with execution_lock:
            manual_execution_status.pop(envelope.manual_id, None)
        return

    with execution_lock:
        status = group_execution_status.get(envelope.schedule_id)
        if status is None:
            return
        for transition in transitions:
            target = transition.get("target")
            if target == "schedule_detail":
                exclude_task(envelope.schedule_id, str(transition.get("id", envelope.detail_id)))
            elif target == "schedule_group":
                status["stop_after_current_sequence"] = True
                try:
                    scheduler.remove_job(f"schedule:{envelope.schedule_id}")
                except JobLookupError:
                    pass
    _finish_detail(
        envelope.schedule_id, envelope.sequence, envelope.detail_id, True
    )


def handle_result_rejection(envelope: ResultEnvelope, code: str) -> None:
    """Fail one execution context closed after a permanent A6 rejection."""
    if envelope.manual_id is not None:
        with execution_lock:
            manual_execution_status.pop(envelope.manual_id, None)
        return
    with execution_lock:
        status = group_execution_status.get(envelope.schedule_id)
        if status is not None:
            status["stop_after_current_sequence"] = True
    _finish_detail(
        envelope.schedule_id, envelope.sequence, envelope.detail_id, False
    )


def _finish_detail(group_id: int, sequence: int, detail_id: str, next_task_run: bool) -> None:
    next_sequence = None
    with execution_lock:
        group_completion = task_completion_status.get(group_id)
        if group_completion is None or sequence not in group_completion:
            return
        remaining = group_completion[sequence]
        if detail_id in remaining:
            remaining.remove(detail_id)
        if not remaining:
            del group_completion[sequence]
            status = group_execution_status[group_id]
            if next_task_run and not status["stop_after_current_sequence"]:
                later = [value for value in status["sequences"] if value > sequence]
                if later:
                    next_sequence = min(later)
        if next_sequence is None and not group_completion:
            running_tasks[group_id] = False

    if next_sequence is not None and not execute_next_task(group_id, next_sequence):
        with execution_lock:
            running_tasks[group_id] = False


def _find_schedule(group_id: int) -> ScheduleGroup | None:
    snapshot = get_current_snapshot()
    if snapshot is None:
        return None
    return next(
        (group for group in snapshot.schedules if group.schedule_id == group_id),
        None,
    )


def _find_manual(manual_id: int) -> ManualExecution | None:
    snapshot = get_current_snapshot()
    if snapshot is None:
        return None
    return next((item for item in snapshot.manual_runs if item.manual_id == manual_id), None)


def _find_detail(schedule_id: int, detail_id) -> ScheduleDetail | None:
    schedule_group = _find_schedule(schedule_id)
    if schedule_group is None:
        return None
    return next(
        (detail for detail in schedule_group.details if str(detail.detail_id) == str(detail_id)),
        None,
    )


def _detail_task_data(detail: ScheduleDetail) -> dict:
    """Adapt the typed contract to task.task_run's legacy mapping boundary."""
    return {
        "detail_id": str(detail.detail_id),
        "task_type": detail.task_type,
        "command": detail.command,
        "archive_type": detail.archive_type,
        "source_path": detail.source_path,
        "error_on_missing_source": detail.error_on_missing_source,
        "destination_path": detail.destination_path,
        "date_format": detail.date_format,
        "target_date_format": detail.target_date_format,
        "destination_date_format": detail.destination_date_format,
        "house_keep_days": detail.house_keep_days,
    }


def get_environment_info(configured_device_name: str) -> dict[str, str]:
    return {
        "host": socket.gethostname(),
        "ip": socket.gethostbyname(socket.gethostname()),
        "os": os.name,
        "user": getpass.getuser(),
        "device_name": configured_device_name,
    }


def exclude_task(group_id: int, exclude_detail_id: str) -> None:
    """Exclude a failed detail only from this immutable execution context."""
    with execution_lock:
        status = group_execution_status.get(group_id)
        if status is None:
            return
        status["sequences"] = {
            sequence: tuple(
                detail
                for detail in details
                if str(detail.detail_id) != str(exclude_detail_id)
            )
            for sequence, details in status["sequences"].items()
        }
    Logger.warn(
        f"[exclude_task] Group={group_id}, Detail={exclude_detail_id} Task was excluded"
    )


def clear_event_cause(cause: str) -> None:
    """Release a cause latch after recovery so a later recurrence is observable."""
    with execution_lock:
        _event_causes.discard(cause)


def _handle_operating_transition(transition) -> None:
    """Own network/scheduler side effects outside the state manager boundary."""
    previous = getattr(transition.previous.state, "value", transition.previous.state)
    current = getattr(transition.current.state, "value", transition.current.state)
    if current == "HALTED":
        if sync_coordinator is not None and scheduler_adapter is not None:
            sync_coordinator.remove_all_managed(scheduler_adapter)
        with execution_lock:
            for status in group_execution_status.values():
                status["stop_after_current_sequence"] = True
    if previous == "HEALTHY" and current == "DEGRADED":
        clear_event_cause("state:recovered")
        report_event_once(
            "degraded", "warning", "Agent entered degraded operation.", "state:degraded"
        )
    elif previous == "DEGRADED" and current == "HEALTHY":
        clear_event_cause("state:degraded")
        report_event_once(
            "recovered", "info", "Agent recovered healthy operation.", "state:recovered"
        )


def report_event_once(
    event_type: str, severity: str, message: str, cause: str | None = None
) -> None:
    """Send one sanitized A7 event per cause; event delivery itself is not retried."""
    global event_delivery_failures
    if api_client is None:
        return
    key = cause or f"{event_type}:{message}"
    with execution_lock:
        # The outbox owns the high/low-watermark latch and deliberately reuses
        # this cause after recovery below the low watermark.
        if key != "outbox:backlog":
            if key in _event_causes:
                _event_suppressed_counts[key] = _event_suppressed_counts.get(key, 0) + 1
                return
            _event_causes.add(key)
    try:
        api_client.report_execution_event(
            {
                "event_type": event_type,
                "severity": severity,
                "occurred_at": datetime.datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
                "message": message,
                "environment_info": get_environment_info(device_name),
            }
        )
    except Exception:
        event_delivery_failures += 1


def _result_access_token() -> str:
    if credential_manager is None:
        raise AuthenticationError("credential manager is unavailable", code="invalid_token")
    outcome = credential_manager.ensure_access_token()
    if not outcome.ok:
        reason = outcome.reason or "unavailable"
        if reason in {"needs_enrollment", "credential_persist_failed", "device_inactive"}:
            if operating_state_manager is not None:
                operating_state_manager.credential_failed(reason)
            raise AuthenticationError("result delivery credential unavailable", code="invalid_token")
        raise TransientServerError("credential refresh temporarily unavailable", code="unavailable")
    if operating_state_manager is not None:
        operating_state_manager.credential_succeeded()
    api_client.set_access_token(outcome.access_token)
    return outcome.access_token


def _record_api_failure(exc: ApiClientError) -> None:
    if operating_state_manager is None:
        return
    if isinstance(exc, DeviceInactiveError):
        operating_state_manager.device_status("inactive")
    elif isinstance(exc, AuthenticationError):
        operating_state_manager.credential_failed("needs_enrollment")
    else:
        operating_state_manager.snapshot_failed(exc.code)


def error_handle(msg: str) -> None:
    """Report agent-level failures without storing raw business values."""
    del msg
    Logger.error("[error_handle] agent operation failed")
    report_event_once(
        "startup_error", "error", "Agent operation failed.", "agent-operation-failed"
    )
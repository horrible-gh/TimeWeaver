from datetime import datetime, timedelta, timezone
from uuid import UUID, uuid4

from apscheduler.schedulers.background import BackgroundScheduler
import pytest

from agent.services.time_weaver import app
from agent.services.time_weaver.api_client import (
    ClientRejectedError,
    CommunicationError,
    DeviceInactiveError,
    NotModified,
    TransientServerError,
)
from agent.services.time_weaver.credential_manager import CredentialOutcome
from agent.services.time_weaver.models import (
    CronFields,
    Device,
    ManualExecution,
    ScheduleDetail,
    ScheduleGroup,
    ScheduleSnapshot,
)
from agent.services.time_weaver.operating_state import OperatingState, OperatingStateManager
from agent.services.time_weaver.outbox import ResultEnvelope, ResultOutbox
from agent.services.time_weaver.scheduler_adapter import ApSchedulerAdapter
from agent.services.time_weaver.sync_coordinator import SyncCoordinator
from agent.timeweaver import AgentRuntime


DETAIL_ID = UUID("00000000-0000-0000-0000-000000000012")
DETAIL_ID_2 = UUID("00000000-0000-0000-0000-000000000013")
NOW = datetime(2026, 8, 1, 10, 0, tzinfo=timezone.utc)


def snapshot(seed="a", *, hour="2", include_manual=True, two_sequences=False):
    detail = ScheduleDetail(
        detail_id=DETAIL_ID,
        schedule_name="copy",
        cron=CronFields(second="0"),
        is_error_stop=True,
        sequence=1,
        exec_sequence=1,
        retry_count=0,
        task_type="copy",
        source_path="/source",
        destination_path="/destination",
    )
    details = [detail]
    if two_sequences:
        details.append(ScheduleDetail(
            detail_id=DETAIL_ID_2,
            schedule_name="copy-next",
            cron=CronFields(second="0"),
            is_error_stop=False,
            sequence=2,
            exec_sequence=2,
            retry_count=0,
            task_type="copy",
            source_path="/source-2",
            destination_path="/destination-2",
        ))
    group = ScheduleGroup(
        schedule_id=12,
        name="nightly",
        cron=CronFields(hour=hour, minute="0", second="0"),
        is_error_stop=True,
        details=tuple(details),
    )
    manuals = ()
    if include_manual:
        manuals = (ManualExecution(
            manual_id=41,
            schedule_id=12,
            detail_id=DETAIL_ID,
            status="wait",
            is_immediate=True,
            schedule_datetime=NOW,
            claimable=True,
        ),)
    digest = seed * 64
    return ScheduleSnapshot(
        schema_version="1",
        revision=f"sha256:{digest}",
        etag=f'W/"{digest[:16]}"',
        server_time=NOW,
        generated_at=NOW,
        device=Device(7, "batch-01", "active", "v1"),
        schedules=(group,),
        manual_runs=manuals,
    )



class QueuedExecutor:
    def __init__(self):
        self.calls = []

    def submit(self, func, *args):
        self.calls.append((func, args))


class FakeClient:
    def __init__(self):
        self.claims = []
        self.claim_error = None
        self.starts = []
        self.start_error = None
        self.events = []

    def claim_manual_execution(self, manual_id):
        self.claims.append(manual_id)
        if self.claim_error:
            raise self.claim_error
        return {"manual_id": manual_id, "claim_token": "claim-token"}

    def report_execution_start(self, execution_grp_id, start):
        self.starts.append((execution_grp_id, start))
        if self.start_error:
            raise self.start_error
        return {"accepted": True}

    def report_execution_event(self, event):
        self.events.append(event)
        return {"accepted": True}

    def report_execution_results(self, *args, **kwargs):
        return {"duplicate": False, "applied_transitions": []}


@pytest.fixture
def wired_app(monkeypatch):
    scheduler = BackgroundScheduler()
    scheduler.start(paused=True)
    monkeypatch.setattr(app, "scheduler", scheduler)
    app.running_tasks.clear()
    app.group_execution_status.clear()
    app.task_completion_status.clear()
    app.manual_execution_status.clear()
    app._event_causes.clear()
    app._event_suppressed_counts.clear()
    state = OperatingStateManager()
    coordinator = SyncCoordinator(state_manager=state)
    client = FakeClient()
    adapter = ApSchedulerAdapter(
        scheduler, app.start_group_execution, app.dispatch_manual,
        manual_dispatch_delay=1, now=lambda: NOW,
    )
    executor = QueuedExecutor()
    outbox = ResultOutbox(
        client, state_manager=state, executor=executor,
        capacity=20, high_watermark=15, low_watermark=10,
        event_reporter=app.report_event_once,
        ack_callback=app.handle_result_ack,
        rejection_callback=app.handle_result_rejection,
    )
    app.configure_sync(
        object(), state, client, coordinator, adapter, outbox
    )
    yield scheduler, state, coordinator, adapter, client, outbox
    outbox.close(wait=False)
    scheduler.shutdown(wait=False)


def test_apply_snapshot_reconciles_regular_and_schedules_manual_dispatch(wired_app):
    scheduler, state, coordinator, adapter, _client, _outbox = wired_app
    current = snapshot()

    assert app.apply_snapshot(current) is current
    assert coordinator.current_snapshot is current
    assert set(adapter.list_jobs()) == {"schedule:12", "manual:41"}
    assert scheduler.get_job("schedule:12") is not None
    assert scheduler.get_job("manual:41") is None
    assert scheduler.get_job("manual_dispatch:41") is not None
    assert state.state is OperatingState.HEALTHY


def test_changed_running_group_is_marked_to_stop_after_current_sequence(wired_app):
    _scheduler, _state, _coordinator, _adapter, _client, _outbox = wired_app
    old = snapshot("a", hour="2", include_manual=False)
    app.apply_snapshot(old)
    context_id = uuid4()
    app.group_execution_status[12] = {
        "context_id": context_id,
        "schedule": old.schedules[0],
        "sequences": {1: old.schedules[0].details},
        "stop_after_current_sequence": False,
    }
    app.running_tasks[12] = True

    app.apply_snapshot(snapshot("b", hour="3", include_manual=False))
    assert app.group_execution_status[12]["stop_after_current_sequence"] is True


def test_inactive_device_blocks_execution_without_process_exit(wired_app):
    _scheduler, state, _coordinator, _adapter, _client, _outbox = wired_app
    app.apply_snapshot(snapshot(include_manual=False))
    state.device_status("inactive")

    assert app.start_group_execution(12) is False
    assert state.state is OperatingState.HALTED


@pytest.mark.parametrize(
    "halt",
    [
        lambda state: state.credential_failed("needs_enrollment"),
        lambda state: state.device_status("revoked"),
        lambda state: state.reconciliation_failed(restored=False),
    ],
)
def test_every_halted_path_removes_managed_jobs_but_preserves_running_sequence(
    wired_app, halt
):
    scheduler, state, coordinator, adapter, _client, outbox = wired_app
    current = snapshot()
    app.apply_snapshot(current)
    _regular_context(current)
    assert outbox.reserve_slot()

    halt(state)

    assert state.state is OperatingState.HALTED
    assert adapter.list_jobs() == {}
    assert scheduler.get_job("schedule:12") is None
    assert scheduler.get_job("manual_dispatch:41") is None
    assert coordinator.current_snapshot is None
    assert app.running_tasks[12] is True
    assert app.group_execution_status[12]["stop_after_current_sequence"] is True

    outbox.enqueue(ResultEnvelope(
        execution_grp_id=str(app.group_execution_status[12]["context_id"]),
        schedule_id=12,
        detail_id=str(DETAIL_ID),
        attempt=1,
        manual_id=None,
        claim_token=None,
        started_at="2026-08-01T10:00:00Z",
        finished_at="2026-08-01T10:00:01Z",
        result_code=0,
        result_message=None,
        environment_info={},
        sequence=1,
    ))
    assert outbox.queued_results == 1


def test_degraded_and_recovered_events_relatch_after_recovery(wired_app):
    _scheduler, state, _coordinator, _adapter, client, _outbox = wired_app
    app.apply_snapshot(snapshot(include_manual=False))

    state.heartbeat_failed("timeout")
    state.heartbeat_failed("timeout")
    state.heartbeat_succeeded()
    state.heartbeat_failed("timeout")

    assert [event["event_type"] for event in client.events] == [
        "degraded", "recovered", "degraded"
    ]


def _regular_context(current):
    app.group_execution_status[12] = {
        "context_id": uuid4(),
        "schedule": current.schedules[0],
        "sequences": {
            detail.exec_sequence: tuple(
                value for value in current.schedules[0].details
                if value.exec_sequence == detail.exec_sequence
            )
            for detail in current.schedules[0].details
        },
        "stop_after_current_sequence": False,
    }
    app.task_completion_status[12] = {1: [str(DETAIL_ID)]}
    app.running_tasks[12] = True


def test_execute_task_writes_only_to_outbox_and_waits_for_ack(wired_app, monkeypatch):
    _scheduler, _state, _coordinator, _adapter, client, outbox = wired_app
    current = snapshot(include_manual=False)
    app.apply_snapshot(current)
    _regular_context(current)
    monkeypatch.setattr(app.task, "task_run", lambda value: (0, None))
    monkeypatch.setattr(app, "get_environment_info", lambda value: {"host": "batch-01"})
    assert outbox.reserve_slot()

    app.execute_task(
        str(DETAIL_ID), 12, 1,
        app._detail_task_data(current.schedules[0].details[0]),
        False, False, None,
    )


    assert len(client.starts) == 1
    execution_grp_id, start = client.starts[0]
    assert execution_grp_id == str(app.group_execution_status[12]["context_id"])
    assert start["detail_id"] == str(DETAIL_ID)
    assert start["attempt"] == 1
    assert outbox.queued_results == 1
    assert app.running_tasks[12] is True
    item = outbox.peek(str(app.group_execution_status[12]["context_id"]))
    assert item is not None
    app.handle_result_ack(item.envelope, ())
    assert app.running_tasks[12] is False


def test_start_signal_failure_never_blocks_task_execution(wired_app, monkeypatch):
    _scheduler, _state, _coordinator, _adapter, client, outbox = wired_app
    current = snapshot(include_manual=False)
    app.apply_snapshot(current)
    _regular_context(current)
    client.start_error = CommunicationError("offline", code="unavailable")
    ran = []
    monkeypatch.setattr(app.task, "task_run", lambda value: (ran.append(value) or 0, None))
    monkeypatch.setattr(app, "get_environment_info", lambda value: {})
    assert outbox.reserve_slot()

    app.execute_task(
        str(DETAIL_ID), 12, 1,
        app._detail_task_data(current.schedules[0].details[0]),
        False, False, None,
    )

    assert len(client.starts) == 1
    assert len(ran) == 1
    assert outbox.queued_results == 1


@pytest.mark.parametrize(
    "error", [
        ClientRejectedError("claimed", code="already_claimed"),
        ClientRejectedError("missing", code="not_found"),
        TransientServerError("offline", code="unavailable"),
        ClientRejectedError("invalid", code="invalid_request"),
    ],
)
def test_manual_dispatch_never_runs_when_claim_fails(wired_app, error):
    scheduler, _state, _coordinator, adapter, client, outbox = wired_app
    app.apply_snapshot(snapshot())
    client.claim_error = error

    assert app.dispatch_manual(41) is False
    assert outbox.reserved_slots == 0
    assert not adapter.manual_inflight(41)
    assert not [job for job in scheduler.get_jobs() if job.id.startswith("manual_task:")]


def test_manual_claim_success_dispatches_once_and_captures_immutable_context(wired_app):
    scheduler, _state, _coordinator, adapter, client, outbox = wired_app
    app.apply_snapshot(snapshot())

    assert app.dispatch_manual(41) is True
    assert app.dispatch_manual(41) is False
    assert client.claims == [41]
    assert adapter.manual_inflight(41)
    assert outbox.reserved_slots == 1
    context = app.manual_execution_status[41]
    assert context["detail_id"] == str(DETAIL_ID)
    jobs = [job for job in scheduler.get_jobs() if job.id.startswith("manual_task:41:")]
    assert len(jobs) == 1


def test_applied_transitions_control_next_sequence_and_error_stop(wired_app, monkeypatch):
    scheduler, _state, _coordinator, _adapter, _client, outbox = wired_app
    current = snapshot(include_manual=False, two_sequences=True)
    app.apply_snapshot(current)
    _regular_context(current)
    monkeypatch.setattr(app.task, "task_run", lambda value: (1, "business detail"))
    monkeypatch.setattr(app, "get_environment_info", lambda value: {})
    assert outbox.reserve_slot()
    app.execute_task(
        str(DETAIL_ID), 12, 1, app._detail_task_data(current.schedules[0].details[0]),
        True, True, None,
    )
    item = outbox.peek(str(app.group_execution_status[12]["context_id"]))
    assert item is not None
    assert scheduler.get_job(f"12_{DETAIL_ID_2}_2") is None
    assert app.group_execution_status[12]["stop_after_current_sequence"] is False

    app.handle_result_ack(item.envelope, ())
    assert scheduler.get_job(f"12_{DETAIL_ID_2}_2") is not None

    # A server group transition, unlike the local result code alone, closes progression.
    current2 = snapshot("b", include_manual=False, two_sequences=True)
    app.apply_snapshot(current2)
    _regular_context(current2)
    envelope = item.envelope
    app.handle_result_ack(envelope, ({"target": "schedule_group", "id": 12, "status": "error"},))
    assert app.group_execution_status[12]["stop_after_current_sequence"] is True


def test_detail_and_manual_transitions_map_to_local_contexts(wired_app):
    _scheduler, _state, _coordinator, _adapter, _client, _outbox = wired_app
    current = snapshot(two_sequences=True)
    app.apply_snapshot(current)
    _regular_context(current)
    envelope = type("Envelope", (), {
        "manual_id": None, "schedule_id": 12, "sequence": 1,
        "detail_id": str(DETAIL_ID),
    })()
    app.handle_result_ack(
        envelope,
        ({"target": "schedule_detail", "id": str(DETAIL_ID), "status": "error"},),
    )
    assert all(
        str(detail.detail_id) != str(DETAIL_ID)
        for values in app.group_execution_status[12]["sequences"].values()
        for detail in values
    )

    app.manual_execution_status[41] = {
        "execution_grp_id": "manual-exec", "schedule_id": 12,
        "detail_id": str(DETAIL_ID), "claim_token": "secret",
    }
    manual_envelope = type("Envelope", (), {"manual_id": 41})()
    app.handle_result_ack(
        manual_envelope,
        ({"target": "manual_execution", "id": 41, "status": "failed"},),
    )
    assert 41 not in app.manual_execution_status


class FakeScheduler:
    def __init__(self):
        self.running = False
        self.paused = False
        self.jobs = []

    def start(self, paused=False):
        self.running = True
        self.paused = paused

    def add_job(self, *args, **kwargs):
        self.jobs.append((args, kwargs))

    def resume(self):
        self.paused = False


class FakeCredentials:
    credential_existed_at_start = False

    def ensure_access_token(self):
        return CredentialOutcome("access", NOW + timedelta(minutes=10))

    def identity(self):
        return (7, "batch-01")


class FailingClient:
    def __init__(self, error):
        self.error = error

    def set_access_token(self, token):
        self.token = token

    def send_heartbeat(self, *args, **kwargs):
        raise self.error

    def get_schedule_snapshot(self, *args, **kwargs):
        raise self.error


def runtime_for(error):
    state = OperatingStateManager()
    return AgentRuntime(
        FailingClient(error), FakeCredentials(), state,
        SyncCoordinator(state_manager=state), FakeScheduler(), agent_version="v1",
    )


def test_bootstrap_inactive_halts_without_exiting():
    runtime = runtime_for(DeviceInactiveError("inactive", code="device_inactive"))
    assert runtime.bootstrap() is False
    assert runtime.state_manager.state is OperatingState.HALTED


def test_bootstrap_communication_failure_stays_alive_in_bootstrap():
    runtime = runtime_for(CommunicationError("offline", code="unavailable"))
    assert runtime.bootstrap() is False
    assert runtime.state_manager.state is OperatingState.BOOTSTRAP


class ActiveClient:
    def set_access_token(self, token):
        self.token = token

    def send_heartbeat(self, *args, **kwargs):
        return {
            "device_id": 7,
            "device_status": "active",
            "server_time": "2026-08-01T10:00:00Z",
        }

    def get_schedule_snapshot(self, *args, **kwargs):
        return NotModified(kwargs.get("etag"))


def test_successful_bootstrap_resumes_and_installs_independent_single_flight_jobs(monkeypatch):
    monkeypatch.setattr(app, "result_outbox", None)
    state = OperatingStateManager()
    coordinator = SyncCoordinator(snapshot(include_manual=False), state_manager=state)
    scheduler = FakeScheduler()
    runtime = AgentRuntime(
        ActiveClient(), FakeCredentials(), state, coordinator, scheduler,
        agent_version="v1", heartbeat_interval=30, snapshot_sync_interval=60,
        now=lambda: NOW,
    )

    assert runtime.bootstrap() is True
    assert state.state is OperatingState.HEALTHY
    assert scheduler.paused is False
    jobs = {entry[1]["id"]: entry[1] for entry in scheduler.jobs}
    assert set(jobs) == {"__heartbeat__", "__snapshot__"}
    assert jobs["__heartbeat__"]["seconds"] == 30
    assert jobs["__snapshot__"]["seconds"] == 60
    assert all(job["max_instances"] == 1 for job in jobs.values())
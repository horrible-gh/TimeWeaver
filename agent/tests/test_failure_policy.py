from datetime import datetime, timedelta, timezone

from agent.services.time_weaver import app
from agent.services.time_weaver.api_client import RateLimitError
from agent.services.time_weaver.credential_manager import CredentialOutcome
from agent.services.time_weaver.operating_state import OperatingStateManager
from agent.services.time_weaver.outbox import RetryBackoff
from agent.services.time_weaver.sync_coordinator import SyncCoordinator
from agent.timeweaver import AgentRuntime


NOW = datetime(2026, 8, 1, 10, 0, tzinfo=timezone.utc)


class FakeCredentials:
    credential_existed_at_start = False

    def ensure_access_token(self):
        return CredentialOutcome("access", NOW + timedelta(minutes=10))

    def identity(self):
        return (7, "batch-01")


class FakeScheduler:
    def __init__(self):
        self.running = True
        self.paused = False
        self.modified = []

    def modify_job(self, job_id, **kwargs):
        self.modified.append((job_id, kwargs))

    def pause(self):
        self.paused = True


class RateLimitedHeartbeatClient:
    def set_access_token(self, token):
        self.token = token

    def send_heartbeat(self, *args, **kwargs):
        raise RateLimitError(
            "slow down", code="rate_limited", retry_after=17
        )


def runtime(client=None, scheduler=None, **kwargs):
    state = OperatingStateManager()
    return AgentRuntime(
        client or RateLimitedHeartbeatClient(),
        FakeCredentials(),
        state,
        SyncCoordinator(state_manager=state),
        scheduler or FakeScheduler(),
        agent_version="v1",
        retry_initial_delay=1,
        retry_multiplier=2,
        retry_max_delay=4,
        retry_jitter_ratio=0.2,
        random_uniform=lambda low, high: 1.0,
        now=lambda: NOW,
        **kwargs,
    )


def test_shared_retry_policy_is_exponential_capped_and_retry_after_has_no_jitter():
    value = RetryBackoff(
        initial_delay=1,
        multiplier=2,
        max_delay=4,
        jitter_ratio=0.2,
        random_uniform=lambda low, high: 1.2,
    )
    assert value.failure_delay() == 1.2
    assert value.failure_delay() == 2.4
    assert value.failure_delay() == 4.8
    assert value.failure_delay(17) == 17
    value.reset()
    assert value.failures == 0


def test_heartbeat_retry_after_delays_only_heartbeat_channel():
    scheduler = FakeScheduler()
    value = runtime(scheduler=scheduler)

    value._heartbeat_job()

    assert value._next_delays["heartbeat"] == 17
    assert value._channel_backoffs["heartbeat"].failures == 1
    assert value._channel_backoffs["snapshot"].failures == 0
    assert scheduler.modified == [
        ("__heartbeat__", {"next_run_time": NOW + timedelta(seconds=17)})
    ]


def test_degraded_recovery_resets_both_channel_backoffs():
    value = runtime()
    value.state_manager.snapshot_succeeded()
    value._channel_failed("heartbeat")
    value._channel_failed("snapshot")
    value.state_manager.heartbeat_failed("timeout")
    value.state_manager.heartbeat_succeeded()
    value.state_manager.snapshot_succeeded()

    assert value._channel_backoffs["heartbeat"].failures == 0
    assert value._channel_backoffs["snapshot"].failures == 0
    assert value._next_delays == {"heartbeat": 30.0, "snapshot": 60.0}


class InvalidSnapshotClient:
    def __init__(self):
        self.seed = "a"
        self.events = []

    def set_access_token(self, token):
        self.token = token

    def get_schedule_snapshot(self, *args, **kwargs):
        return {
            "etag": 'W/"invalid"',
            "schema_version": "1",
            "server_time": "2026-08-01T10:00:00Z",
            "data": {
                "revision": self.seed,
                "generated_at": "2026-08-01T10:00:00Z",
                "device": {
                    "device_id": 7,
                    "device_name": "batch-01",
                    "status": "active",
                    "known_agent_version": "v1",
                },
                "schedules": [],
                "manual_runs": [],
            },
        }

    def report_execution_event(self, event):
        self.events.append(event)
        return {"accepted": True}


def test_invalid_snapshot_cause_is_suppressed_counted_and_relatches_on_change(
    monkeypatch,
):
    client = InvalidSnapshotClient()
    monkeypatch.setattr(app, "api_client", client)
    monkeypatch.setattr(app, "result_outbox", None)
    monkeypatch.setattr(app, "_event_causes", set())
    monkeypatch.setattr(app, "_event_suppressed_counts", {})
    value = runtime(client=client)

    assert value.snapshot_once() is False
    assert value.snapshot_once() is False
    assert len(client.events) == 1
    cause = next(iter(app._event_causes))
    assert app._event_suppressed_counts[cause] == 1

    client.seed = "b"
    assert value.snapshot_once() is False
    assert len(client.events) == 2


class ClosingOutbox:
    def __init__(self, result):
        self.result = result
        self.calls = []

    def close(self, **kwargs):
        self.calls.append(kwargs)
        return self.result


def test_shutdown_pauses_new_triggers_and_bounds_outbox_wait(monkeypatch):
    scheduler = FakeScheduler()
    value = runtime(scheduler=scheduler, shutdown_grace=0.25)
    outbox = ClosingOutbox(False)
    monkeypatch.setattr(app, "result_outbox", outbox)

    assert value.shutdown() is False
    assert scheduler.paused
    assert outbox.calls == [{"wait": True, "timeout": 0.25}]

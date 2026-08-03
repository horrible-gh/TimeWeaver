"""T0004 6.1 T3-T7: base_url validation, silent-failure logging, status file."""

from datetime import datetime, timedelta, timezone
import json

import pytest

import agent.timeweaver as tw
from agent.services.time_weaver.api_client import AgentApiClient, CommunicationError
from agent.services.time_weaver.credential_manager import (
    CredentialManager,
    CredentialOutcome,
)
from agent.services.time_weaver.operating_state import OperatingStateManager
from agent.services.time_weaver.sync_coordinator import SyncCoordinator
from agent.timeweaver import AgentRuntime


NOW = datetime(2026, 8, 1, 10, 0, tzinfo=timezone.utc)
ACCESS_TOKEN = "secret-access-token-value"
DEFAULT_BASE_URL = "http://127.0.0.1:8000/time_weaver"


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
        return CredentialOutcome(ACCESS_TOKEN, NOW + timedelta(minutes=10))

    def identity(self):
        return (7, "batch-01")


def make_runtime(client, *, credentials=None, **kwargs):
    state = OperatingStateManager()
    return AgentRuntime(
        client,
        credentials or FakeCredentials(),
        state,
        SyncCoordinator(state_manager=state),
        FakeScheduler(),
        agent_version="v1",
        now=lambda: NOW,
        **kwargs,
    )


@pytest.fixture
def captured_logs(monkeypatch):
    monkeypatch.setattr(tw.service, "result_outbox", None)
    records = {"debug": [], "info": [], "warn": [], "error": []}

    def recorder(level):
        def _log(tag="", msg=None):
            records[level].append(f"{tag} {msg}" if msg else str(tag))

        return _log

    for level in records:
        monkeypatch.setattr(tw.Logger, level, recorder(level))
    return records


# --- T3: AgentApiClient base_url validation ---

@pytest.mark.parametrize(
    "bad",
    [
        "127.0.0.1:8000",                      # no scheme
        "http://",                             # empty host
        "ftp://127.0.0.1/time_weaver",         # unsupported scheme
        "http://127.0.0.1:8000/tw?verbose=1",  # query
        "http://127.0.0.1:8000/tw#frag",       # fragment
    ],
)
def test_invalid_base_url_is_rejected(bad):
    with pytest.raises(ValueError):
        AgentApiClient(bad)


def test_default_base_url_is_accepted_and_joins_paths_like_request():
    client = AgentApiClient(f"  {DEFAULT_BASE_URL}/  ")
    assert (
        client.endpoint_url(client.endpoints.heartbeat)
        == "http://127.0.0.1:8000/time_weaver/api/agent/v1/heartbeat"
    )


# --- T4: no credential + no enrollment token sends nothing but logs why ---

class CountingTransport:
    def __init__(self):
        self.calls = 0

    def request(self, method, url, **kwargs):
        self.calls += 1
        raise AssertionError("no network request expected")


def test_bootstrap_without_credential_or_token_sends_nothing_and_logs(
    tmp_path, captured_logs
):
    transport = CountingTransport()
    client = AgentApiClient(DEFAULT_BASE_URL, transport=transport, retries=0)
    state = OperatingStateManager()
    credentials = CredentialManager(client, tmp_path / "agent_credential.json")
    runtime = AgentRuntime(
        client,
        credentials,
        state,
        SyncCoordinator(state_manager=state),
        FakeScheduler(),
        agent_version="v1",
        now=lambda: NOW,
    )

    assert runtime.bootstrap(None) is False
    assert transport.calls == 0
    assert [line for line in captured_logs["warn"] if "needs_enrollment" in line]


# --- T5: unexpected exceptions keep their diagnosis without contract change ---

class BoomClient:
    def set_access_token(self, token):
        self.token = token

    def send_heartbeat(self, *args, **kwargs):
        raise ValueError("boom-heartbeat")

    def get_schedule_snapshot(self, *args, **kwargs):
        raise ValueError("boom-snapshot")


def test_unexpected_exception_logs_type_and_message_without_contract_change(
    captured_logs
):
    runtime = make_runtime(BoomClient())

    assert runtime.heartbeat_once() is False
    assert "heartbeat:unexpected" in runtime.state_manager.value().reasons
    assert any(
        "ValueError" in line and "boom-heartbeat" in line
        for line in captured_logs["error"]
    )

    assert runtime.snapshot_once() is False
    assert "snapshot:unexpected" in runtime.state_manager.value().reasons
    assert any(
        "ValueError" in line and "boom-snapshot" in line
        for line in captured_logs["error"]
    )


class InvalidSnapshotClient:
    def set_access_token(self, token):
        self.token = token

    def get_schedule_snapshot(self, *args, **kwargs):
        return {
            "envelope": {"schema_version": "0"},
            "etag": 'W/"invalid"',
        }


def test_snapshot_validation_reason_is_published_in_status(
    tmp_path, captured_logs, monkeypatch
):
    status_path = tmp_path / "agent_status.json"
    monkeypatch.setattr(tw.service, "report_event_once", lambda *args: None)
    runtime = make_runtime(InvalidSnapshotClient(), status_path=status_path)

    assert runtime.snapshot_once() is False

    payload = json.loads(status_path.read_text(encoding="utf-8"))
    assert payload["channels"]["snapshot"]["last_reason"] == "schema_version mismatch"
    assert "snapshot:sync_error" in payload["reasons"]


# --- T6: status file lifecycle ---

class FlakyHeartbeatClient:
    def __init__(self, failures):
        self.failures = failures

    def set_access_token(self, token):
        self.token = token

    def send_heartbeat(self, *args, **kwargs):
        if self.failures > 0:
            self.failures -= 1
            raise CommunicationError("offline", code="unavailable")
        return {"device_status": "active", "server_time": "2026-08-01T10:00:00Z"}


def test_status_file_tracks_consecutive_failures_and_holds_no_secrets(
    tmp_path, captured_logs
):
    status_path = tmp_path / "status" / "agent_status.json"
    runtime = make_runtime(
        FlakyHeartbeatClient(3),
        status_path=status_path,
        status_info={
            "device": "batch-01",
            "config_path": "conf/time_weaver.json",
            "base_url": DEFAULT_BASE_URL,
            "credential_path": str(tmp_path / "agent_credential.json"),
            "enrollment_token_env": "TIMEWEAVER_ENROLLMENT_TOKEN",
        },
    )

    for _ in range(3):
        assert runtime.heartbeat_once() is False

    content = status_path.read_text(encoding="utf-8")
    payload = json.loads(content)
    assert payload["channels"]["heartbeat"]["consecutive_failures"] == 3
    assert payload["channels"]["heartbeat"]["last_reason"] == "unavailable"
    assert payload["credential_present"] is False

    assert runtime.heartbeat_once() is True
    content = status_path.read_text(encoding="utf-8")
    payload = json.loads(content)
    assert payload["channels"]["heartbeat"]["consecutive_failures"] == 0
    assert payload["channels"]["heartbeat"]["last_success_at"] is not None

    for banned in (ACCESS_TOKEN, "eyJ", "enr_", "rft_"):
        assert banned not in content


# --- constraint 3: suppression gate marks long streaks ---

def test_failure_log_gate_marks_persistent_failures():
    from agent.services.time_weaver.log_gate import FailureLogGate

    gate = FailureLogGate(repeat_mark_every=10)
    assert gate.failure("hb", "x") == (True, 1)
    surfaced = [
        count for count in range(2, 21) if gate.failure("hb", "x")[0]
    ]
    assert surfaced == [10, 20]
    assert gate.failure("hb", "y") == (True, 1)  # reason change relatches
    assert gate.success("hb") is True
    assert gate.success("hb") is False


# --- T7: an unwritable status path never changes runtime behavior ---

def test_unwritable_status_path_does_not_change_outcome_or_state(
    tmp_path, captured_logs
):
    blocker = tmp_path / "blocker"
    blocker.write_text("plain file", encoding="utf-8")
    runtime = make_runtime(
        FlakyHeartbeatClient(0),
        status_path=blocker / "status.json",
    )

    assert runtime.heartbeat_once() is True
    assert "heartbeat:unexpected" not in runtime.state_manager.value().reasons
    assert any("status" in line for line in captured_logs["warn"])

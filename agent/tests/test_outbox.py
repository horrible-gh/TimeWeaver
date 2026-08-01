from collections import defaultdict, deque
from threading import Event
import time

import pytest

from agent.services.time_weaver.api_client import (
    AuthenticationError,
    ClientRejectedError,
    CommunicationError,
)
from agent.services.time_weaver.outbox import ResultEnvelope, ResultOutbox


class InlineExecutor:
    def submit(self, func, *args):
        func(*args)


class FakeTimer:
    def __init__(self, delay, callback):
        self.delay = delay
        self.callback = callback
        self.started = False

    def start(self):
        self.started = True

    def run(self):
        self.callback()

    def cancel(self):
        pass


class FakeState:
    def __init__(self, state="HEALTHY"):
        self.state = state

    def credential_failed(self, reason):
        self.state = "HALTED"

    def device_status(self, status):
        self.state = "HALTED"


class FakeClient:
    def __init__(self):
        self.outcomes = defaultdict(deque)
        self.calls = []

    def report_execution_results(self, group, payload, *, idempotency_key):
        self.calls.append((group, payload["detail_id"], idempotency_key, payload))
        outcome = self.outcomes[group].popleft() if self.outcomes[group] else {
            "duplicate": False, "applied_transitions": []
        }
        if isinstance(outcome, Exception):
            raise outcome
        return outcome


def envelope(group="group-a", detail="detail-1", *, manual_id=None):
    return ResultEnvelope(
        execution_grp_id=group,
        schedule_id=12,
        detail_id=detail,
        attempt=1,
        manual_id=manual_id,
        claim_token="claim" if manual_id else None,
        started_at="2026-08-01T00:00:00Z",
        finished_at="2026-08-01T00:00:01Z",
        result_code=0,
        result_message=None,
        environment_info={"host": "batch-01"},
        sequence=1,
    )


def make_outbox(client=None, **kwargs):
    timers = []

    def timer_factory(delay, callback):
        value = FakeTimer(delay, callback)
        timers.append(value)
        return value

    value = ResultOutbox(
        client or FakeClient(),
        executor=InlineExecutor(),
        timer_factory=timer_factory,
        random_uniform=lambda low, high: 1.0,
        **kwargs,
    )
    return value, timers


def test_slot_capacity_never_drops_completed_result_and_watermark_relatches():
    events = []
    state = FakeState("HALTED")
    outbox, _ = make_outbox(
        state_manager=state,
        capacity=3,
        high_watermark=2,
        low_watermark=1,
        event_reporter=lambda *args: events.append(args),
    )

    assert outbox.reserve_slot()
    assert outbox.reserve_slot()
    assert len(events) == 1
    assert outbox.reserve_slot()
    assert not outbox.reserve_slot()
    outbox.enqueue(envelope())
    assert outbox.queued_results == 1
    assert outbox.size == 3

    outbox.release_slot()
    outbox.release_slot()
    assert outbox.size == 1
    assert len(events) == 1
    state.state = "HEALTHY"
    outbox.wake()
    assert outbox.size == 0

    state.state = "HALTED"
    assert outbox.reserve_slot()
    assert outbox.reserve_slot()
    assert len(events) == 2


def test_group_fifo_retry_keeps_stable_idempotency_and_other_group_progresses():
    client = FakeClient()
    client.outcomes["group-a"].extend([
        CommunicationError("offline", code="unavailable"),
        {"duplicate": True, "applied_transitions": []},
    ])
    acknowledgements = []
    outbox, timers = make_outbox(
        client,
        ack_callback=lambda value, transitions: acknowledgements.append(value.detail_id),
    )

    assert outbox.reserve_slot()
    first = outbox.enqueue(envelope("group-a", "detail-1"))
    assert len(timers) == 1
    assert outbox.peek("group-a") is first
    assert outbox.reserve_slot()
    outbox.enqueue(envelope("group-a", "detail-2"))

    assert outbox.reserve_slot()
    outbox.enqueue(envelope("group-b", "detail-9"))
    assert acknowledgements == ["detail-9"]

    timers[0].run()
    assert acknowledgements == ["detail-9", "detail-1", "detail-2"]
    first_calls = [call for call in client.calls if call[1] == "detail-1"]
    assert len(first_calls) == 2
    assert first_calls[0][2] == first_calls[1][2]
    assert outbox.queued_results == 0


class BlockingClient:
    def __init__(self):
        self.first_started = Event()
        self.second_started = Event()
        self.release_first = Event()

    def report_execution_results(self, group, payload, *, idempotency_key):
        if group == "group-a":
            self.first_started.set()
            assert self.release_first.wait(2)
        else:
            self.second_started.set()
        return {"duplicate": False, "applied_transitions": []}


def test_different_groups_send_in_parallel_within_worker_limit():
    client = BlockingClient()
    outbox = ResultOutbox(
        client, capacity=10, high_watermark=8, low_watermark=5,
        sender_workers=2,
    )
    try:
        assert outbox.reserve_slot()
        outbox.enqueue(envelope("group-a", "detail-1"))
        assert client.first_started.wait(1)
        assert outbox.reserve_slot()
        outbox.enqueue(envelope("group-b", "detail-2"))
        assert client.second_started.wait(1)
    finally:
        client.release_first.set()
        outbox.close(wait=True)


@pytest.mark.parametrize(
    "code", ["claim_expired", "not_found", "invalid_request", "schema_mismatch"]
)
def test_permanent_result_rejections_remove_head_and_emit_one_event(code):
    client = FakeClient()
    client.outcomes["group-a"].append(ClientRejectedError("rejected", code=code))
    events = []
    rejections = []
    outbox, _ = make_outbox(
        client,
        event_reporter=lambda *args: events.append(args),
        rejection_callback=lambda value, reason: rejections.append((value.detail_id, reason)),
    )

    assert outbox.reserve_slot()
    outbox.enqueue(envelope())
    assert outbox.queued_results == 0
    assert rejections == [("detail-1", code)]
    assert len(events) == 1
    assert events[0][0] == "sync_error"


def test_authentication_halt_preserves_fifo_head_until_recovery():
    client = FakeClient()
    client.outcomes["group-a"].extend([
        AuthenticationError("expired", code="token_expired"),
        {"duplicate": False, "applied_transitions": []},
    ])
    state = FakeState()
    outbox, _ = make_outbox(client, state_manager=state)

    assert outbox.reserve_slot()
    item = outbox.enqueue(envelope())
    assert state.state == "HALTED"
    assert outbox.peek("group-a") is item
    state.state = "HEALTHY"
    outbox.wake()
    assert outbox.queued_results == 0


def test_sender_gets_fresh_access_token_immediately_before_delivery():
    seen = []

    class TokenClient:
        def report_execution_results(
            self, group, payload, *, idempotency_key, access_token
        ):
            seen.append(access_token)
            return {"duplicate": False, "applied_transitions": []}

    outbox, _ = make_outbox(
        TokenClient(), access_token_provider=lambda: "fresh-access"
    )
    assert outbox.reserve_slot()
    outbox.enqueue(envelope())
    assert seen == ["fresh-access"]


def test_close_wait_is_bounded_while_an_inflight_send_finishes_later():
    started = Event()
    release = Event()

    class SlowClient:
        def report_execution_results(self, *args, **kwargs):
            started.set()
            release.wait(1)
            return {"duplicate": False, "applied_transitions": []}

    outbox = ResultOutbox(
        SlowClient(), capacity=10, high_watermark=8, low_watermark=5,
        sender_workers=1,
    )
    assert outbox.reserve_slot()
    outbox.enqueue(envelope())
    assert started.wait(1)

    before = time.monotonic()
    assert outbox.close(wait=True, timeout=0.02) is False
    elapsed = time.monotonic() - before
    assert elapsed < 0.2
    release.set()


def test_environment_is_bounded_and_unbounded_result_message_is_preserved():
    client = FakeClient()
    outbox, _ = make_outbox(client)
    huge = "x" * 40_000
    value = envelope()
    value = ResultEnvelope(
        value.execution_grp_id, value.schedule_id, value.detail_id, value.attempt,
        value.manual_id, value.claim_token, value.started_at, value.finished_at,
        1, huge, {"host": huge, "ip": huge}, value.sequence,
    )
    assert outbox.reserve_slot()
    outbox.enqueue(value)
    payload = client.calls[0][3]
    assert payload["result_message"] == huge
    assert len(str(payload["environment_info"]).encode("utf-8")) < 32_768
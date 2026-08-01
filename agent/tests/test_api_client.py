import pytest

from agent.services.time_weaver.api_client import (
    AgentApiClient,
    AuthenticationError,
    CommunicationError,
    DeviceInactiveError,
    EnrollmentTokenInvalidError,
    MalformedResponseError,
    NotModified,
    RateLimitError,
    SchemaMismatchError,
    SnapshotResponse,
    TransientServerError,
)
from agent.tests.support.mock_server import (
    MockResponse,
    MockTransport,
    enroll_success,
    envelope,
    error_envelope,
    heartbeat_success,
    not_modified,
    snapshot_success,
    token_success,
)


def client(transport, **kwargs):
    return AgentApiClient(
        "https://timeweaver.invalid",
        "configured-access",
        agent_version="v1.2.3",
        transport=transport,
        retries=kwargs.pop("retries", 0),
        sleeper=kwargs.pop("sleeper", lambda _: None),
        **kwargs,
    )


def test_enroll_token_heartbeat_and_snapshot_match_protocol():
    transport = MockTransport(enroll_success(), token_success(), heartbeat_success(), snapshot_success())
    api = client(transport)

    enrolled = api.enroll("enrollment-value", "batch-01")
    refreshed = api.refresh_access_token("old-refresh")
    heartbeat = api.send_heartbeat("v1.2.3", "HEALTHY", None)
    snapshot = api.get_schedule_snapshot(etag='W/"oldoldoldoldold1"')

    assert enrolled.device_id == 7
    assert refreshed.refresh_token == "refresh-value"
    assert heartbeat["device_status"] == "active"
    assert isinstance(snapshot, SnapshotResponse)
    assert transport.requests[0].url.endswith("/api/agent/v1/enroll")
    assert "Authorization" not in transport.requests[0].kwargs["headers"]
    assert "Authorization" not in transport.requests[1].kwargs["headers"]
    assert transport.requests[2].url.endswith("/api/agent/v1/heartbeat")
    assert transport.requests[2].kwargs["headers"]["X-TW-Agent-Version"] == "v1.2.3"
    assert transport.requests[2].kwargs["headers"]["X-TW-Schema-Version"] == "1"
    assert transport.requests[3].kwargs["headers"]["If-None-Match"] == 'W/"oldoldoldoldold1"'


def test_snapshot_304_has_no_body_decode():
    result = client(MockTransport(not_modified())).get_schedule_snapshot()
    assert isinstance(result, NotModified)
    assert result.etag == 'W/"a1b2c3d4e5f60718"'


@pytest.mark.parametrize(("status", "code", "error_type"), [
    (401, "invalid_token", AuthenticationError),
    (401, "token_expired", AuthenticationError),
    (403, "device_inactive", DeviceInactiveError),
    (403, "device_revoked", DeviceInactiveError),
    (403, "enrollment_token_invalid", EnrollmentTokenInvalidError),
    (422, "schema_mismatch", SchemaMismatchError),
])
def test_error_code_drives_classification(status, code, error_type):
    transport = MockTransport(MockResponse(status_code=status, payload=error_envelope(code)))
    with pytest.raises(error_type) as caught:
        client(transport).enroll("enrollment", "batch-01")
    assert caught.value.code == code


def test_error_code_wins_over_http_status():
    transport = MockTransport(MockResponse(status_code=400, payload=error_envelope("device_inactive")))
    with pytest.raises(DeviceInactiveError):
        client(transport).send_heartbeat("v1", "HEALTHY", None)


def test_rate_limit_honors_retry_after_and_recovers():
    sleeps = []
    transport = MockTransport(
        MockResponse(status_code=429, headers={"Retry-After": "2"}, payload=error_envelope("rate_limited")),
        enroll_success(),
    )
    result = client(transport, retries=1, sleeper=sleeps.append).enroll("enrollment", "batch-01")
    assert result.device_id == 7
    assert sleeps == [2.0]


def test_rate_limit_exhaustion_is_distinct():
    transport = MockTransport(MockResponse(status_code=429, payload=error_envelope("rate_limited")))
    with pytest.raises(RateLimitError):
        client(transport).enroll("enrollment", "batch-01")


@pytest.mark.parametrize("failure", [TimeoutError("late"), ConnectionError("refused")])
def test_connection_failures_are_unavailable(failure):
    with pytest.raises(CommunicationError) as caught:
        client(MockTransport(failure)).enroll("enrollment", "batch-01")
    assert caught.value.code == "unavailable"


def test_server_error_retries_then_raises():
    response = MockResponse(status_code=503, payload=error_envelope("unavailable"))
    with pytest.raises(TransientServerError):
        client(MockTransport(response, response), retries=1).enroll("enrollment", "batch-01")


def test_malformed_json_and_success_schema_mismatch_are_distinct():
    with pytest.raises(MalformedResponseError):
        client(MockTransport(MockResponse(payload=ValueError("bad")))).enroll("enrollment", "batch-01")
    with pytest.raises(SchemaMismatchError):
        client(MockTransport(MockResponse(payload=envelope({}, schema_version="2")))).enroll("enrollment", "batch-01")
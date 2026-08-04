from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timezone

import pytest

from agent.services.time_weaver.api_client import AgentApiClient
from agent.services.time_weaver.credential_manager import (
    CredentialManager,
    CredentialStore,
    StoredCredential,
)
from agent.tests.support.mock_server import (
    MockResponse,
    MockTransport,
    error_envelope,
    token_success,
)


NOW = datetime(2026, 8, 1, 5, 0, tzinfo=timezone.utc)


def stored(expiry="2026-10-30T05:00:00Z"):
    from agent.services.time_weaver.models import parse_datetime
    return StoredCredential(
        device_id=7,
        device_name="batch-01",
        refresh_token="old-refresh",
        refresh_token_expires_at=parse_datetime(expiry, "refresh expiry"),
    )


def setup_manager(tmp_path, *outcomes, store=None):
    path = tmp_path / "credential.json"
    initial_store = CredentialStore(path)
    initial_store.write(stored())
    selected_store = store or initial_store
    selected_store.replace_count = 0
    transport = MockTransport(*outcomes)
    api = AgentApiClient(
        "https://timeweaver.invalid",
        transport=transport,
        retries=0,
        sleeper=lambda _: None,
    )
    return CredentialManager(api, path, store=selected_store, now=lambda: NOW), selected_store, transport, path


def test_100_concurrent_callers_refresh_and_replace_exactly_once(tmp_path):
    manager, store, transport, _ = setup_manager(tmp_path, token_success())
    with ThreadPoolExecutor(max_workers=100) as executor:
        results = list(executor.map(lambda _: manager.ensure_access_token(60), range(100)))
    assert all(result.ok and result.access_token == "access-value" for result in results)
    assert len(transport.requests) == 1
    assert store.replace_count == 1
    assert "Authorization" not in transport.requests[0].kwargs["headers"]


def test_sufficient_memory_token_skips_refresh(tmp_path):
    manager, store, transport, _ = setup_manager(tmp_path, token_success())
    assert manager.ensure_access_token(60).ok
    assert manager.ensure_access_token(60).ok
    assert len(transport.requests) == 1
    assert store.replace_count == 1


def test_atomic_replace_failure_never_installs_new_access_token(tmp_path):
    path = tmp_path / "credential.json"
    initial = CredentialStore(path)
    initial.write(stored())

    def fail_replace(_source, _target):
        raise OSError("injected replace failure")

    failing = CredentialStore(path, replace=fail_replace)
    transport = MockTransport(token_success())
    api = AgentApiClient("https://timeweaver.invalid", transport=transport, retries=0)
    manager = CredentialManager(api, path, store=failing, now=lambda: NOW)

    result = manager.ensure_access_token(60)
    assert result.access_token is None
    assert result.reason == "credential_persist_failed"
    assert failing.read().refresh_token == "old-refresh"
    assert len(transport.requests) == 1


@pytest.mark.parametrize("code", ["invalid_token", "token_expired"])
def test_permanent_refresh_failure_discards_credential(code, tmp_path):
    response = MockResponse(status_code=401, payload=error_envelope(code))
    manager, _, transport, path = setup_manager(tmp_path, response)
    result = manager.ensure_access_token(60)
    assert result.reason == "needs_enrollment"
    assert not path.exists()
    assert len(transport.requests) == 1


@pytest.mark.parametrize("code", ["device_inactive", "device_revoked"])
def test_inactive_device_preserves_refresh_file(code, tmp_path):
    response = MockResponse(status_code=403, payload=error_envelope(code))
    manager, _, _, path = setup_manager(tmp_path, response)
    result = manager.ensure_access_token(60)
    assert result.reason == "device_inactive"
    assert path.exists()


def test_transient_error_preserves_refresh_file(tmp_path):
    response = MockResponse(status_code=503, payload=error_envelope("unavailable"))
    manager, _, _, path = setup_manager(tmp_path, response)
    result = manager.ensure_access_token(60)
    assert result.reason == "transient"
    assert path.exists()


def test_transient_read_failure_preserves_credential_and_does_not_enroll(tmp_path):
    path = tmp_path / "credential.json"
    store = CredentialStore(path)
    store.write(stored())

    def fail_verify(_path):
        raise PermissionError("simulated ACL mismatch between writer and reader accounts")

    store._verify_owner_only = fail_verify
    transport = MockTransport()
    api = AgentApiClient("https://timeweaver.invalid", transport=transport, retries=0)
    manager = CredentialManager(api, path, store=store, now=lambda: NOW)

    result = manager.ensure_access_token(60)
    assert result.reason == "transient"
    assert path.exists()
    assert transport.requests == []


def test_corrupt_credential_file_is_discarded_and_needs_enrollment(tmp_path):
    path = tmp_path / "credential.json"
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text("not valid json", encoding="utf-8")
    store = CredentialStore(path)
    transport = MockTransport()
    api = AgentApiClient("https://timeweaver.invalid", transport=transport, retries=0)
    manager = CredentialManager(api, path, store=store, now=lambda: NOW)

    result = manager.ensure_access_token(60)
    assert result.reason == "needs_enrollment"
    assert not path.exists()
    assert transport.requests == []


def test_expired_local_refresh_is_discarded_without_network(tmp_path):
    path = tmp_path / "credential.json"
    store = CredentialStore(path)
    store.write(stored("2026-07-31T05:00:00Z"))
    transport = MockTransport()
    api = AgentApiClient("https://timeweaver.invalid", transport=transport, retries=0)
    result = CredentialManager(api, path, store=store, now=lambda: NOW).ensure_access_token(60)
    assert result.reason == "needs_enrollment"
    assert not path.exists()
    assert transport.requests == []
import pytest

from agent.services.time_weaver.operating_state import OperatingState, OperatingStateManager


def healthy_manager():
    manager = OperatingStateManager()
    manager.credential_succeeded()
    manager.heartbeat_succeeded()
    manager.snapshot_succeeded()
    assert manager.state is OperatingState.HEALTHY
    return manager


def test_bootstrap_has_priority_without_a_valid_snapshot():
    manager = OperatingStateManager()
    manager.heartbeat_failed("timeout")
    assert manager.state is OperatingState.BOOTSTRAP
    assert not manager.execution_allowed()


def test_bootstrap_to_healthy_after_credential_heartbeat_and_snapshot():
    manager = OperatingStateManager()
    manager.credential_succeeded()
    manager.heartbeat_succeeded()
    manager.snapshot_succeeded()
    assert manager.state is OperatingState.HEALTHY
    assert manager.execution_allowed()


@pytest.mark.parametrize("channel", ["heartbeat", "snapshot"])
def test_healthy_to_degraded_and_recovery(channel):
    manager = healthy_manager()
    getattr(manager, f"{channel}_failed")("timeout")
    assert manager.state is OperatingState.DEGRADED
    assert manager.execution_allowed()
    getattr(manager, f"{channel}_succeeded")()
    assert manager.state is OperatingState.HEALTHY


def test_clock_warning_degrades_and_clear_recovers():
    manager = healthy_manager()
    manager.clock_warning(True)
    assert manager.state is OperatingState.DEGRADED
    manager.clock_warning(False)
    assert manager.state is OperatingState.HEALTHY


@pytest.mark.parametrize("reason", ["invalid_token", "token_expired"])
def test_refresh_failure_halts_and_new_enrollment_recovers(reason):
    manager = healthy_manager()
    manager.heartbeat_failed(reason)
    assert manager.state is OperatingState.HALTED
    assert not manager.execution_allowed()
    manager.enrollment_succeeded()
    assert manager.state is OperatingState.BOOTSTRAP
    manager.snapshot_succeeded()
    assert manager.state is OperatingState.HEALTHY


@pytest.mark.parametrize("status", ["inactive", "revoked"])
def test_inactive_or_revoked_halts_and_active_snapshot_recovers(status):
    manager = healthy_manager()
    manager.device_status(status)
    assert manager.state is OperatingState.HALTED
    manager.device_status("active")
    manager.snapshot_succeeded()
    assert manager.state is OperatingState.HEALTHY


def test_persist_and_restore_failures_have_fail_closed_priority():
    manager = healthy_manager()
    manager.snapshot_failed("malformed_response")
    manager.credential_failed("credential_persist_failed")
    value = manager.value()
    assert value.state is OperatingState.HALTED
    assert "credential_persist_failed" in value.reasons

    second = healthy_manager()
    second.reconciliation_failed(restored=False)
    assert second.state is OperatingState.HALTED


def test_reconciliation_failure_with_successful_compensation_is_degraded():
    manager = healthy_manager()
    manager.reconciliation_failed(restored=True)
    assert manager.state is OperatingState.DEGRADED
    manager.reconciliation_succeeded()
    assert manager.state is OperatingState.HEALTHY
"""TimeWeaver agent bootstrap and independent heartbeat/snapshot loops."""

from __future__ import annotations

from collections.abc import Mapping
from datetime import datetime, timedelta, timezone
import hashlib
import json
import os
import random
import signal
import threading
import time



try:
    from agent.configure import (
        agent_api_config,
        time_weaver_config,

        version,
    )
    from agent.services.time_weaver import app as service
    from agent.services.time_weaver.api_client import (
        AgentApiClient,
        ApiClientError,
        NotModified,
        SnapshotResponse,
    )
    from agent.services.time_weaver.credential_manager import CredentialManager
    from agent.services.time_weaver.operating_state import (
        OperatingState,
        OperatingStateManager,
    )
    from agent.services.time_weaver.outbox import RetryBackoff
    from agent.services.time_weaver.scheduler_adapter import ApSchedulerAdapter
    from agent.services.time_weaver.sync_coordinator import (
        SnapshotValidationError,
        SyncCoordinator,
        validate_snapshot,
    )
except ImportError:  # Script execution from the agent directory.
    from configure import agent_api_config, time_weaver_config, version
    import services.time_weaver.app as service
    from services.time_weaver.api_client import (
        AgentApiClient,
        ApiClientError,
        NotModified,
        SnapshotResponse,
    )
    from services.time_weaver.credential_manager import CredentialManager
    from services.time_weaver.operating_state import OperatingState, OperatingStateManager
    from services.time_weaver.outbox import RetryBackoff
    from services.time_weaver.scheduler_adapter import ApSchedulerAdapter
    from services.time_weaver.sync_coordinator import (
        SnapshotValidationError,
        SyncCoordinator,
        validate_snapshot,
    )


CLOCK_SKEW_WARNING_SECONDS = 300
CREDENTIAL_PROBE_INTERVAL = 60
_shutdown_requested = threading.Event()


class AgentRuntime:
    def __init__(
        self,
        client,
        credentials,
        state_manager,
        coordinator,
        scheduler,
        *,
        agent_version: str,
        heartbeat_interval: int = 30,
        snapshot_sync_interval: int = 60,
        retry_initial_delay: float = 1.0,
        retry_multiplier: float = 2.0,
        retry_max_delay: float = 60.0,
        retry_jitter_ratio: float = 0.20,
        shutdown_grace: float = 30.0,
        credential_existed_at_start: bool = False,
        now=None,
        random_uniform=random.uniform,
    ) -> None:
        if heartbeat_interval < 1 or snapshot_sync_interval < 1:
            raise ValueError("periodic intervals must be at least one second")
        if shutdown_grace < 0:
            raise ValueError("shutdown_grace must be non-negative")
        self.client = client
        self.credentials = credentials
        self.state_manager = state_manager
        self.coordinator = coordinator
        self.scheduler = scheduler
        self.agent_version = agent_version
        self.heartbeat_interval = heartbeat_interval
        self.snapshot_sync_interval = snapshot_sync_interval
        self.shutdown_grace = shutdown_grace
        self.credential_existed_at_start = credential_existed_at_start
        self._now = now or (lambda: datetime.now(timezone.utc))
        self._periodic_installed = False
        self._startup_outbox_reported = False
        self._shutdown_started = False
        self._snapshot_failure_cause: str | None = None
        self._channel_backoffs = {
            channel: RetryBackoff(
                initial_delay=retry_initial_delay,
                multiplier=retry_multiplier,
                max_delay=retry_max_delay,
                jitter_ratio=retry_jitter_ratio,
                random_uniform=random_uniform,
            )
            for channel in ("heartbeat", "snapshot")
        }
        self._normal_delays = {
            "heartbeat": float(heartbeat_interval),
            "snapshot": float(snapshot_sync_interval),
        }
        self._next_delays = dict(self._normal_delays)
        add_listener = getattr(state_manager, "add_transition_listener", None)
        if add_listener is not None:
            add_listener(self._handle_state_transition)

    def bootstrap(self, enrollment_token: str | None = None) -> bool:
        """Attempt one cold-start cycle and never terminate the process on failure."""
        if not self.scheduler.running:
            self.scheduler.start(paused=True)

        outcome = self.credentials.ensure_access_token()
        if not outcome.ok and outcome.reason == "needs_enrollment" and enrollment_token:
            try:
                enrolled = self.client.enroll(
                    enrollment_token, time_weaver_config["device"]
                )
                outcome = self.credentials.install_enrollment(enrolled)
                if outcome.ok:
                    self.state_manager.enrollment_succeeded()
            except ApiClientError as exc:
                self._record_failure("credential", exc.code)
                return False
        if not outcome.ok:
            self.state_manager.credential_failed(outcome.reason or "transient")
            return False

        self.state_manager.credential_succeeded()
        self.client.set_access_token(outcome.access_token)
        heartbeat_ok = self.heartbeat_once()
        snapshot_ok = self.snapshot_once()
        if not (heartbeat_ok and snapshot_ok):
            return False

        if self.credential_existed_at_start and not self._startup_outbox_reported:
            service.report_event_once(
                "startup_error",
                "warning",
                "Previous in-memory result outbox cannot be recovered after restart.",
                "startup-outbox-not-recoverable",
            )
            self._startup_outbox_reported = True
        if service.result_outbox is not None:
            service.result_outbox.wake()
        self._install_periodic_jobs()
        self.scheduler.resume()
        return True

    def heartbeat_once(self) -> bool:
        outcome = self._access_token()
        if outcome is None:
            self._channel_failed("heartbeat")
            return False
        try:
            response = self.client.send_heartbeat(
                self.agent_version,
                self.state_manager.state.value,
                self.coordinator.current_revision,
                access_token=outcome,
            )
            status = _heartbeat_device_status(response)
            if status is not None:
                self.state_manager.device_status(status)
                if status != "active":
                    self._channel_failed("heartbeat")
                    return False
            self.state_manager.heartbeat_succeeded()
            if service.result_outbox is not None:
                service.result_outbox.wake()
            server_time = _heartbeat_server_time(response)
            if server_time is not None:
                skew = abs((server_time - self._now()).total_seconds())
                self.state_manager.clock_warning(skew > CLOCK_SKEW_WARNING_SECONDS)
            self._channel_succeeded("heartbeat")
            return True
        except ApiClientError as exc:
            self._record_failure("heartbeat", exc.code)
            self._channel_failed("heartbeat", exc.retry_after)
        except Exception:
            self.state_manager.heartbeat_failed("unexpected")
            self._channel_failed("heartbeat")
        return False

    def snapshot_once(self) -> bool:
        outcome = self._access_token()
        if outcome is None:
            self._channel_failed("snapshot")
            return False
        current = self.coordinator.current_snapshot
        response = None
        try:
            response = self.client.get_schedule_snapshot(
                etag=None if current is None else current.etag,
                access_token=outcome,
            )
            if isinstance(response, NotModified):
                if current is None:
                    self.state_manager.snapshot_failed("not_modified_without_snapshot")
                    self._channel_failed("snapshot")
                    return False
                self.state_manager.snapshot_succeeded()
                self._snapshot_recovered()
                self._channel_succeeded("snapshot")
                return True
            identity = self.credentials.identity()
            if identity is None:
                self.state_manager.credential_failed("needs_enrollment")
                self._channel_failed("snapshot")
                return False
            snapshot = validate_snapshot(response, identity)
            service.apply_snapshot(snapshot)
            if service.result_outbox is not None:
                service.result_outbox.wake()
            skew = abs((snapshot.server_time - self._now()).total_seconds())
            self.state_manager.clock_warning(skew > CLOCK_SKEW_WARNING_SECONDS)
            self._snapshot_recovered()
            self._channel_succeeded("snapshot")
            return True
        except SnapshotValidationError:
            cause = _snapshot_failure_key(response)
            self._snapshot_failure_cause = cause
            service.report_event_once(
                "sync_error",
                "error",
                "Schedule snapshot validation failed.",
                cause,
            )
            self.state_manager.snapshot_failed("sync_error")
            self._channel_failed("snapshot")
        except ApiClientError as exc:
            self._record_failure("snapshot", exc.code)
            self._channel_failed("snapshot", exc.retry_after)
        except Exception:
            self.state_manager.snapshot_failed("unexpected")
            self._channel_failed("snapshot")
        return False

    def bootstrap_retry_delay(self) -> float:
        return min(self._next_delays.values())

    def shutdown(self) -> bool:
        """Pause new triggers and bound result-delivery cleanup by shutdown_grace."""
        if self._shutdown_started:
            return True
        self._shutdown_started = True
        try:
            self.scheduler.pause()
        except Exception:
            pass
        if service.result_outbox is None:
            return True
        return service.result_outbox.close(wait=True, timeout=self.shutdown_grace)

    def _access_token(self) -> str | None:
        outcome = self.credentials.ensure_access_token()
        if not outcome.ok:
            self.state_manager.credential_failed(outcome.reason or "transient")
            return None
        self.state_manager.credential_succeeded()
        self.client.set_access_token(outcome.access_token)
        return outcome.access_token

    def _record_failure(self, channel: str, reason: str) -> None:
        if reason in {"device_inactive", "device_revoked"}:
            self.state_manager.device_status(reason.removeprefix("device_"))
        elif reason in {"invalid_token", "token_expired"}:
            self.state_manager.credential_failed("needs_enrollment")
        elif channel == "heartbeat":
            self.state_manager.heartbeat_failed(reason)
        elif channel == "snapshot":
            self.state_manager.snapshot_failed(reason)
        else:
            self.state_manager.credential_failed(reason)

    def _channel_failed(
        self, channel: str, retry_after: float | None = None
    ) -> None:
        self._next_delays[channel] = self._channel_backoffs[channel].failure_delay(
            retry_after
        )

    def _channel_succeeded(self, channel: str) -> None:
        self._channel_backoffs[channel].reset()
        self._next_delays[channel] = self._normal_delays[channel]

    def _handle_state_transition(self, transition) -> None:
        if (
            transition.previous.state is OperatingState.DEGRADED
            and transition.current.state is OperatingState.HEALTHY
        ):
            for channel in self._channel_backoffs:
                self._channel_succeeded(channel)

    def _snapshot_recovered(self) -> None:
        if self._snapshot_failure_cause is not None:
            service.clear_event_cause(self._snapshot_failure_cause)
            self._snapshot_failure_cause = None

    def _heartbeat_job(self) -> None:
        if not self.heartbeat_once():
            self._delay_periodic_job("__heartbeat__", self._next_delays["heartbeat"])

    def _snapshot_job(self) -> None:
        if not self.snapshot_once():
            self._delay_periodic_job("__snapshot__", self._next_delays["snapshot"])

    def _delay_periodic_job(self, job_id: str, delay: float) -> None:
        modify_job = getattr(self.scheduler, "modify_job", None)
        if modify_job is not None:
            modify_job(job_id, next_run_time=self._now() + timedelta(seconds=delay))

    def _install_periodic_jobs(self) -> None:
        self.scheduler.add_job(
            self._heartbeat_job,
            trigger="interval",
            seconds=self.heartbeat_interval,
            id="__heartbeat__",
            replace_existing=True,
            coalesce=True,
            max_instances=1,
        )
        self.scheduler.add_job(
            self._snapshot_job,
            trigger="interval",
            seconds=self.snapshot_sync_interval,
            id="__snapshot__",
            replace_existing=True,
            coalesce=True,
            max_instances=1,
        )
        self._periodic_installed = True


def build_runtime(*, scheduler=None) -> AgentRuntime:
    """Assemble the API-backed agent runtime."""
    selected_scheduler = scheduler or service.scheduler
    client = AgentApiClient(
        agent_api_config["base_url"],
        agent_version=version["version"],
        connect_timeout=agent_api_config["connect_timeout"],
        read_timeout=agent_api_config["read_timeout"],
        retries=0,
    )
    state_manager = OperatingStateManager()
    credentials = CredentialManager(client, agent_api_config["credential_path"])
    coordinator = SyncCoordinator(state_manager=state_manager)
    adapter = ApSchedulerAdapter(
        selected_scheduler,
        service.start_group_execution,
        service.dispatch_manual,
        manual_dispatch_delay=agent_api_config["manual_dispatch_delay"],
    )
    service.configure_sync(
        credentials,
        state_manager,
        client,
        coordinator,
        adapter,
    )
    return AgentRuntime(
        client,
        credentials,
        state_manager,
        coordinator,
        selected_scheduler,
        agent_version=version["version"],
        heartbeat_interval=agent_api_config["heartbeat_interval"],
        snapshot_sync_interval=agent_api_config["snapshot_sync_interval"],
        retry_initial_delay=agent_api_config["retry_initial_delay"],
        retry_multiplier=agent_api_config["retry_multiplier"],
        retry_max_delay=agent_api_config["retry_max_delay"],
        retry_jitter_ratio=agent_api_config["retry_jitter_ratio"],
        shutdown_grace=agent_api_config["shutdown_grace"],
        credential_existed_at_start=credentials.credential_existed_at_start,
    )


def run_forever() -> None:
    runtime = build_runtime()
    token = os.environ.get(agent_api_config["enrollment_token_env"])
    _shutdown_requested.clear()

    def request_shutdown(_signum, _frame) -> None:
        _shutdown_requested.set()

    signal.signal(signal.SIGTERM, request_shutdown)
    signal.signal(signal.SIGINT, request_shutdown)
    while not _shutdown_requested.is_set() and not runtime.bootstrap(token):
        _shutdown_requested.wait(runtime.bootstrap_retry_delay())
    while not _shutdown_requested.wait(60):
        pass
    runtime.shutdown()


def _snapshot_failure_key(response) -> str:
    if isinstance(response, SnapshotResponse):
        value = response.envelope
    elif isinstance(response, Mapping):
        value = response.get("envelope", response)
    else:
        value = {"response_type": type(response).__name__}
    try:
        encoded = json.dumps(
            value, sort_keys=True, separators=(",", ":"), default=str
        ).encode("utf-8")
    except Exception:
        encoded = type(response).__name__.encode("utf-8")
    return f"snapshot-validation:{hashlib.sha256(encoded).hexdigest()[:16]}"


def _heartbeat_device_status(response) -> str | None:
    if not isinstance(response, dict):
        return None
    device = response.get("device")
    if isinstance(device, dict) and isinstance(device.get("status"), str):
        return device["status"]
    for name in ("device_status", "status"):
        if isinstance(response.get(name), str):
            return response[name]
    return None


def _heartbeat_server_time(response) -> datetime | None:
    if not isinstance(response, dict) or not isinstance(response.get("server_time"), str):
        return None
    value = response["server_time"]
    try:
        parsed = datetime.fromisoformat(value[:-1] + "+00:00" if value.endswith("Z") else value)
    except ValueError:
        return None
    return parsed if parsed.tzinfo is not None else None


if __name__ == "__main__":
    run_forever()

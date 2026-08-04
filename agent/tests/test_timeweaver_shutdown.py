"""T0004: bounded shutdown-signal latency in run_forever's wait loops.

Regression coverage for the "slow Ctrl+C" bug (timeweaver.agent.0008.0001-B):
run_forever() used to block on a single threading.Event.wait(60) (and, on
the bootstrap retry path, .wait(bootstrap_retry_delay())) with no way to
notice a shutdown request before that call returned. _wait_for_shutdown
replaces both call sites and must bound the wait to short slices instead.
"""

import threading
import time

from agent import timeweaver


def test_wait_for_shutdown_returns_promptly_once_flag_is_set(monkeypatch):
    monkeypatch.setattr(timeweaver, "SHUTDOWN_POLL_INTERVAL_SECONDS", 0.05)
    timeweaver._shutdown_requested.clear()

    def set_flag_soon():
        time.sleep(0.1)
        timeweaver._shutdown_requested.set()

    threading.Thread(target=set_flag_soon, daemon=True).start()

    started = time.monotonic()
    result = timeweaver._wait_for_shutdown(60)
    elapsed = time.monotonic() - started

    timeweaver._shutdown_requested.clear()

    assert result is True
    # Bounded by poll interval, not by the 60s timeout requested.
    assert elapsed < 1.0


def test_wait_for_shutdown_waits_out_the_full_timeout_when_no_signal(monkeypatch):
    monkeypatch.setattr(timeweaver, "SHUTDOWN_POLL_INTERVAL_SECONDS", 0.05)
    timeweaver._shutdown_requested.clear()

    started = time.monotonic()
    result = timeweaver._wait_for_shutdown(0.2)
    elapsed = time.monotonic() - started

    assert result is False
    assert elapsed >= 0.2

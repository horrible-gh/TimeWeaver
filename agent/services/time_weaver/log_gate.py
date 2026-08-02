"""Change-driven log suppression shared by agent runtime modules.

Logging policy (T0004 constraint 3): a failure is logged once when it starts
or when its reason changes, repeats of the same reason stay silent, but every
`repeat_mark_every`-th repeat still emits one "same failure persisted N times"
marker so a long outage never becomes a fully silent log. Success reporting
tells the caller whether a failure streak just ended so recovery can be
logged exactly once.
"""

from __future__ import annotations

REPEAT_MARK_EVERY = 10


class FailureLogGate:
    """Per-channel decision helper: should this failure/success be logged?"""

    def __init__(self, repeat_mark_every: int = REPEAT_MARK_EVERY) -> None:
        self._repeat_mark_every = max(1, int(repeat_mark_every))
        self._keys: dict[str, str] = {}
        self._counts: dict[str, int] = {}

    def failure(self, channel: str, key: str) -> tuple[bool, int]:
        """Record one failure; return (should_log, consecutive_count)."""
        if self._keys.get(channel) != key:
            self._keys[channel] = key
            self._counts[channel] = 1
            return True, 1
        self._counts[channel] = self._counts.get(channel, 1) + 1
        count = self._counts[channel]
        return count % self._repeat_mark_every == 0, count

    def success(self, channel: str) -> bool:
        """Record success; return True when a failure streak just ended."""
        ended = channel in self._keys
        self._keys.pop(channel, None)
        self._counts.pop(channel, None)
        return ended

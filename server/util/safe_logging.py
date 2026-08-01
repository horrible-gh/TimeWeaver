"""Structured logging helpers that redact secrets by name and value pattern."""
import json
import re

import LogAssist.log as Logger


_SECRET_NAME = re.compile(
    r"(?:authorization|cookie|setcookie|accesstoken|refreshtoken|"
    r"enrollmenttoken|claimtoken|token|password|secret)",
    re.IGNORECASE,
)
_JWT = re.compile(r"\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b")
_PREFIXED_SECRET = re.compile(r"\b(?:enr_|rft_)[A-Za-z0-9_-]+\b")
_CLAIM = re.compile(r"(?<![0-9a-fA-F])[0-9a-fA-F]{64}(?![0-9a-fA-F])")


def _normalized_name(value) -> str:
    return re.sub(r"[_-]", "", str(value))


def redact(value, field_name=None):
    if field_name is not None and _SECRET_NAME.search(_normalized_name(field_name)):
        return "[REDACTED]"
    if isinstance(value, dict):
        return {str(key): redact(item, key) for key, item in value.items()}
    if isinstance(value, (list, tuple)):
        return [redact(item) for item in value]
    if isinstance(value, str):
        value = _JWT.sub("[REDACTED]", value)
        value = _PREFIXED_SECRET.sub("[REDACTED]", value)
        return _CLAIM.sub("[REDACTED]", value)
    return value


_LEVEL_ALIASES = {"warning": "warn"}


def safe_log(level: str, event: str, fields=None) -> None:
    payload = {"event": event, **redact(fields or {})}
    message = json.dumps(payload, ensure_ascii=False, default=str, sort_keys=True)
    getattr(Logger, _LEVEL_ALIASES.get(level, level))(message)
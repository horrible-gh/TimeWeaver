from pathlib import Path

try:
    from agent.util import jsonutil
except ImportError:  # Script execution from the agent directory.
    from util import jsonutil
import LogAssist.log as Logger


_AGENT_ROOT = Path(__file__).resolve().parent


def _read_config(name: str, sample_name: str | None = None):
    candidates = [Path("conf") / name, _AGENT_ROOT / "conf" / name]
    if sample_name:
        candidates.append(_AGENT_ROOT / "conf" / sample_name)
    for candidate in candidates:
        if candidate.is_file():
            return jsonutil.json_read(str(candidate))
    raise FileNotFoundError(f"configuration file not found: {name}")


server_config = _read_config("server.json", "server.sample.json")
Logger.logger_init(server_config.get("log", None))


time_weaver_config = _read_config("time_weaver.json", "time_weaver.sample.json")
version = _read_config("version.json")

_api = time_weaver_config.get("api", {})
agent_api_config = {
    "base_url": _api.get("base_url", "http://127.0.0.1:8080"),
    "credential_path": _api.get("credential_path", "conf/agent_credential.json"),
    "enrollment_token_env": _api.get(
        "enrollment_token_env", "TIMEWEAVER_ENROLLMENT_TOKEN"
    ),
    "connect_timeout": float(_api.get("connect_timeout", 5)),
    "read_timeout": float(_api.get("read_timeout", 30)),
    "heartbeat_interval": int(_api.get("heartbeat_interval", 30)),
    "snapshot_sync_interval": int(_api.get("snapshot_sync_interval", 60)),
    "shutdown_grace": float(_api.get("shutdown_grace", 30)),
    "outbox_capacity": int(_api.get("outbox_capacity", 10_000)),
    "outbox_high_watermark": int(_api.get("outbox_high_watermark", 8_000)),
    "outbox_low_watermark": int(_api.get("outbox_low_watermark", 5_000)),
    "outbox_sender_workers": int(_api.get("outbox_sender_workers", 4)),
    "manual_dispatch_delay": float(_api.get("manual_dispatch_delay", 1)),
    "retry_initial_delay": float(_api.get("retry_initial_delay", 1)),
    "retry_multiplier": float(_api.get("retry_multiplier", 2)),
    "retry_max_delay": float(_api.get("retry_max_delay", 60)),
    "retry_jitter_ratio": float(_api.get("retry_jitter_ratio", 0.20)),
}

if agent_api_config["shutdown_grace"] < 0:
    raise ValueError("shutdown_grace must be non-negative")
if agent_api_config["outbox_capacity"] <= 0:
    raise ValueError("outbox_capacity must be positive")
if not (
    0 <= agent_api_config["outbox_low_watermark"]
    < agent_api_config["outbox_high_watermark"]
    <= agent_api_config["outbox_capacity"]
):
    raise ValueError("outbox watermarks must satisfy 0 <= low < high <= capacity")
from pathlib import Path

try:
    from agent.util import jsonutil
except ImportError:  # Script execution from the agent directory.
    from util import jsonutil
import LogAssist.log as Logger


_AGENT_ROOT = Path(__file__).resolve().parent

# Single source of truth for the API default. It must stay equal to what the
# setup scripts write (scripts/setup-windows.ps1 / scripts/setup-linux.sh) and
# to the server defaults (server/.env.sample: port 8000, CONTEXT=/time_weaver).
DEFAULT_BASE_URL = "http://127.0.0.1:8000/time_weaver"
DEFAULT_STATUS_PATH = "log/agent_status.json"

# Diagnostic record of which candidate file each configuration was read from:
# file name -> absolute path actually adopted.
config_sources: dict[str, str] = {}
# file name -> True when the packaged sample was the adopted candidate.
sample_adopted: dict[str, bool] = {}


def _read_config(name: str, sample_name: str | None = None):
    candidates = [Path("conf") / name, _AGENT_ROOT / "conf" / name]
    if sample_name:
        candidates.append(_AGENT_ROOT / "conf" / sample_name)
    for candidate in candidates:
        if candidate.is_file():
            config_sources[name] = str(candidate.resolve())
            sample_adopted[name] = sample_name is not None and candidate.name == sample_name
            return jsonutil.json_read(str(candidate))
    raise FileNotFoundError(f"configuration file not found: {name}")


def _log_config_source(name: str) -> None:
    if sample_adopted.get(name):
        Logger.warn(
            f"[configure] running on the packaged sample configuration for {name}: "
            f"{config_sources.get(name)}"
        )


server_config = _read_config("server.json", "server.sample.json")
Logger.logger_init(server_config.get("log", None))
# Anything logged before logger_init can be dropped, so the adoption
# diagnostics (including the one for server.json itself) are emitted here.
_log_config_source("server.json")

time_weaver_config = _read_config("time_weaver.json", "time_weaver.sample.json")
_log_config_source("time_weaver.json")
version = _read_config("version.json")


def build_agent_api_config(config) -> dict:
    """Map a raw time_weaver config dict onto the effective api settings."""
    api = config.get("api") if isinstance(config, dict) else None
    if not isinstance(api, dict):
        Logger.warn(
            "[configure] the 'api' section is missing from the time_weaver "
            f"configuration; applying the default base_url {DEFAULT_BASE_URL}"
        )
        api = {}
    elif "base_url" not in api:
        Logger.warn(
            "[configure] 'api.base_url' is not set; applying the default "
            f"base_url {DEFAULT_BASE_URL}"
        )
    return {
        "base_url": api.get("base_url", DEFAULT_BASE_URL),
        "credential_path": api.get("credential_path", "conf/agent_credential.json"),
        "status_path": api.get("status_path", DEFAULT_STATUS_PATH),
        "enrollment_token_env": api.get(
            "enrollment_token_env", "TIMEWEAVER_ENROLLMENT_TOKEN"
        ),
        "connect_timeout": float(api.get("connect_timeout", 5)),
        "read_timeout": float(api.get("read_timeout", 30)),
        "heartbeat_interval": int(api.get("heartbeat_interval", 30)),
        "snapshot_sync_interval": int(api.get("snapshot_sync_interval", 60)),
        "shutdown_grace": float(api.get("shutdown_grace", 30)),
        "outbox_capacity": int(api.get("outbox_capacity", 10_000)),
        "outbox_high_watermark": int(api.get("outbox_high_watermark", 8_000)),
        "outbox_low_watermark": int(api.get("outbox_low_watermark", 5_000)),
        "outbox_sender_workers": int(api.get("outbox_sender_workers", 4)),
        "manual_dispatch_delay": float(api.get("manual_dispatch_delay", 1)),
        "retry_initial_delay": float(api.get("retry_initial_delay", 1)),
        "retry_multiplier": float(api.get("retry_multiplier", 2)),
        "retry_max_delay": float(api.get("retry_max_delay", 60)),
        "retry_jitter_ratio": float(api.get("retry_jitter_ratio", 0.20)),
    }


agent_api_config = build_agent_api_config(time_weaver_config)

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

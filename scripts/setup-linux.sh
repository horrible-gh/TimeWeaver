#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PYTHON_BIN="${PYTHON_BIN:-python3}"
INSTALL_SERVICES="${INSTALL_SERVICES:-0}"
SERVICE_USER="${SERVICE_USER:-$USER}"
COMPONENT="${COMPONENT:-}"
RECONFIGURE="${RECONFIGURE:-0}"
NONINTERACTIVE="${NONINTERACTIVE:-0}"

# Config values (flags / env override prompts; prompts override defaults).
DB_TYPE="${DB_TYPE:-}"
DB_HOST="${DB_HOST:-}"
DB_PORT="${DB_PORT:-}"
DB_USER="${DB_USER:-}"
DB_PASSWORD="${DB_PASSWORD:-}"
DB_NAME="${DB_NAME:-}"
DB_SCHEMA="${DB_SCHEMA:-}"
DB_PATH="${DB_PATH:-}"
SECRET_KEY="${SECRET_KEY:-}"
ALLOWED_ORIGIN="${ALLOWED_ORIGIN:-}"
CONTEXT="${CONTEXT:-}"
ACCESS_TOKEN_EXPIRE_MINUTES="${ACCESS_TOKEN_EXPIRE_MINUTES:-}"
REDIS_HOST="${REDIS_HOST:-}"
REDIS_PORT="${REDIS_PORT:-}"
REDIS_DB="${REDIS_DB:-}"
DEVICE_NAME="${DEVICE_NAME:-}"
RESCHEDULE_MINUTE="${RESCHEDULE_MINUTE:-}"
LOG_LEVEL="${LOG_LEVEL:-}"
API_URL="${API_URL:-}"

usage() {
  cat <<'USAGE'
Usage: scripts/setup-linux.sh [options]

Components:
  --component all|server|agent|client   What to install (prompts if omitted; default all)

Config (optional; installer prompts interactively otherwise, sensible defaults applied):
  --db-type sqlite3|mysql               Server database type (default sqlite3)
  --db-host / --db-port / --db-user / --db-password / --db-name / --db-schema
  --db-path PATH                        sqlite file path (server, sqlite3 only)
  --secret-key KEY                      Server SECRET_KEY (auto-generated if omitted)
  --allowed-origin ORIGIN               Server CORS origin (default *)
  --context PATH                        Server API context (default /time_weaver)
  --access-token-expire-minutes N       JWT lifetime in minutes (default 30)
  --redis-host / --redis-port / --redis-db   Optional Redis blacklist store (defaults localhost:6379/0)
  --device-name NAME                    Agent device name (default: hostname)
  --reschedule-minute CRON              Agent poll cron minute field (default */5)
  --log-level LEVEL                     Agent log level (default debug)
  --api-url URL                         Client API server URL

Behaviour:
  --reconfigure                         Overwrite existing config files
  --non-interactive                     Never prompt; use flags/env/defaults
  --install-services                    Install systemd services (server/agent)
  --service-user USER                   systemd service user (default: current user)

Config is generated ready-to-run; no sample files to copy or hand-edit.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --component) COMPONENT="${2:?Missing component}"; shift 2 ;;
    --db-type) DB_TYPE="${2:?}"; shift 2 ;;
    --db-host) DB_HOST="${2:?}"; shift 2 ;;
    --db-port) DB_PORT="${2:?}"; shift 2 ;;
    --db-user) DB_USER="${2:?}"; shift 2 ;;
    --db-password) DB_PASSWORD="${2:?}"; shift 2 ;;
    --db-name) DB_NAME="${2:?}"; shift 2 ;;
    --db-schema) DB_SCHEMA="${2:?}"; shift 2 ;;
    --db-path) DB_PATH="${2:?}"; shift 2 ;;
    --secret-key) SECRET_KEY="${2:?}"; shift 2 ;;
    --allowed-origin) ALLOWED_ORIGIN="${2:?}"; shift 2 ;;
    --context) CONTEXT="${2:?}"; shift 2 ;;
    --access-token-expire-minutes) ACCESS_TOKEN_EXPIRE_MINUTES="${2:?}"; shift 2 ;;
    --redis-host) REDIS_HOST="${2:?}"; shift 2 ;;
    --redis-port) REDIS_PORT="${2:?}"; shift 2 ;;
    --redis-db) REDIS_DB="${2:?}"; shift 2 ;;
    --device-name) DEVICE_NAME="${2:?}"; shift 2 ;;
    --reschedule-minute) RESCHEDULE_MINUTE="${2:?}"; shift 2 ;;
    --log-level) LOG_LEVEL="${2:?}"; shift 2 ;;
    --api-url) API_URL="${2:?}"; shift 2 ;;
    --reconfigure) RECONFIGURE=1; shift ;;
    --non-interactive) NONINTERACTIVE=1; shift ;;
    --install-services) INSTALL_SERVICES=1; shift ;;
    --service-user) SERVICE_USER="${2:?Missing service user}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; usage; exit 2 ;;
  esac
done

# Auto-detect non-interactive (no TTY) so prompts never block.
if [[ ! -t 0 || -n "${CI:-}" ]]; then NONINTERACTIVE=1; fi

ask() {  # ask "Prompt" "default" -> echoes answer
  local prompt="$1" default="${2:-}" reply
  if [[ "$NONINTERACTIVE" == "1" ]]; then echo "$default"; return; fi
  if [[ -n "$default" ]]; then
    read -r -p "$prompt [$default]: " reply
  else
    read -r -p "$prompt: " reply
  fi
  echo "${reply:-$default}"
}

gen_secret() {
  if command -v openssl >/dev/null 2>&1; then openssl rand -hex 32; return; fi
  "$PYTHON_BIN" -c "import secrets; print(secrets.token_hex(32))"
}

if [[ -z "$COMPONENT" ]]; then
  COMPONENT="$(ask "Install which component? (all/server/agent/client)" "all")"
fi
case "$COMPONENT" in
  all|server|agent|client) ;;
  *) echo "Invalid component: $COMPONENT (expected all|server|agent|client)" >&2; exit 2 ;;
esac

DO_SERVER=0; DO_AGENT=0; DO_CLIENT=0
[[ "$COMPONENT" == "all" || "$COMPONENT" == "server" ]] && DO_SERVER=1
[[ "$COMPONENT" == "all" || "$COMPONENT" == "agent" ]] && DO_AGENT=1
[[ "$COMPONENT" == "all" || "$COMPONENT" == "client" ]] && DO_CLIENT=1

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Required command not found: $1" >&2
    exit 1
  fi
}

setup_python_project() {
  local project_dir="$1"
  pushd "$project_dir" >/dev/null
  "$PYTHON_BIN" -m venv .venv
  . .venv/bin/activate
  python -m pip install --upgrade pip
  pip install -r requirements.txt
  deactivate
  popd >/dev/null
}

write_server_env() {
  local target="$ROOT_DIR/server/.env"
  if [[ -f "$target" && "$RECONFIGURE" != "1" ]]; then
    echo "Using existing server/.env (pass --reconfigure to regenerate)."
    return
  fi
  local db_type="$DB_TYPE"
  [[ -z "$db_type" ]] && db_type="$(ask "Server database type (sqlite3/mysql)" "sqlite3")"
  local secret="$SECRET_KEY"
  [[ -z "$secret" ]] && secret="$(gen_secret)"
  local origin="${ALLOWED_ORIGIN:-*}"
  local ctx="${CONTEXT:-/time_weaver}"
  local expire="${ACCESS_TOKEN_EXPIRE_MINUTES:-30}"
  # Redis is optional at runtime (server falls back to an in-process token
  # blacklist). These values are only used when a Redis server is present.
  local rhost="${REDIS_HOST:-localhost}"
  local rport="${REDIS_PORT:-6379}"
  local rdb="${REDIS_DB:-0}"

  local h p u pw db sc dbpath
  if [[ "$db_type" == "mysql" ]]; then
    h="${DB_HOST:-$(ask "DB host" "127.0.0.1")}"
    p="${DB_PORT:-$(ask "DB port" "3306")}"
    u="${DB_USER:-$(ask "DB user" "timeweaver")}"
    pw="${DB_PASSWORD:-$(ask "DB password" "")}"
    db="${DB_NAME:-$(ask "DB name" "timeweaver")}"
    sc="${DB_SCHEMA:-$(ask "DB schema (blank if none)" "")}"
    dbpath=""
  else
    h="127.0.0.1"; p="0"; u=""; pw=""; db=""; sc=""
    dbpath="${DB_PATH:-./timeweaver.sqlite3}"
  fi

  cat > "$target" <<EOF
ALLOWED_ORIGIN=$origin
SECRET_KEY=$secret
ACCESS_TOKEN_EXPIRE_MINUTES=$expire
CONTEXT=$ctx
DB_TYPE=$db_type
DB_HOST=$h
DB_PORT=$p
DB_USER=$u
DB_PASSWORD=$pw
DB_DATABASE=$db
DB_SCHEMA=$sc
DB_PATH=$dbpath
REDIS_HOST=$rhost
REDIS_PORT=$rport
REDIS_DB=$rdb
EOF
  echo "Wrote server/.env (DB_TYPE=$db_type, generated SECRET_KEY, all keys populated)."
}

write_agent_config() {
  local server_target="$ROOT_DIR/agent/conf/server.json"
  local tw_target="$ROOT_DIR/agent/conf/time_weaver.json"
  if [[ -f "$server_target" && -f "$tw_target" && "$RECONFIGURE" != "1" ]]; then
    echo "Using existing agent config (pass --reconfigure to regenerate)."
    return
  fi
  # The agent connects to the shared MySQL database (it ships MySQL SQL only).
  local h p u pw db sc dev
  h="${DB_HOST:-$(ask "Agent DB host" "127.0.0.1")}"
  p="${DB_PORT:-$(ask "Agent DB port" "3306")}"
  u="${DB_USER:-$(ask "Agent DB user" "timeweaver")}"
  pw="${DB_PASSWORD:-$(ask "Agent DB password" "")}"
  db="${DB_NAME:-$(ask "Agent DB name" "timeweaver")}"
  sc="${DB_SCHEMA:-$(ask "Agent DB schema (blank if none)" "")}"
  dev="${DEVICE_NAME:-$(ask "Agent device name" "$(hostname)")}"
  local level="${LOG_LEVEL:-debug}"
  local rmin="${RESCHEDULE_MINUTE:-*/5}"

  DB_HOST="$h" DB_PORT="$p" DB_USER="$u" DB_PASSWORD="$pw" DB_NAME="$db" DB_SCHEMA="$sc" LOG_LEVEL="$level" \
  "$PYTHON_BIN" - "$ROOT_DIR/agent/conf/server.sample.json" "$server_target" <<'PY'
import json, os, sys
src, dst = sys.argv[1], sys.argv[2]
cfg = json.load(open(src))
my = cfg["databases"]["time_weaver"]["database"]["mysql"]
my.update({
    "host": os.environ["DB_HOST"],
    "port": int(os.environ["DB_PORT"]),
    "user": os.environ["DB_USER"],
    "password": os.environ["DB_PASSWORD"],
    "database": os.environ["DB_NAME"],
    "schema": os.environ["DB_SCHEMA"],
})
level = os.environ["LOG_LEVEL"]
for key in ("base", "console", "file_timed"):
    cfg["log"][key]["level"] = level
json.dump(cfg, open(dst, "w"), indent=4)
PY
  echo "Wrote agent/conf/server.json (host=$h db=$db, log level=$level)."

  DEVICE_NAME="$dev" RESCHEDULE_MINUTE="$rmin" "$PYTHON_BIN" - "$ROOT_DIR/agent/conf/time_weaver.sample.json" "$tw_target" <<'PY'
import json, os, sys
src, dst = sys.argv[1], sys.argv[2]
cfg = json.load(open(src))
cfg["device"] = os.environ["DEVICE_NAME"]
cfg["reschedule"]["minute"] = os.environ["RESCHEDULE_MINUTE"]
json.dump(cfg, open(dst, "w"), indent=4)
PY
  echo "Wrote agent/conf/time_weaver.json (device=$dev, reschedule minute=$rmin)."
  echo "  The agent registers this device automatically on first run (no manual DB seeding)."
}

write_client_config() {
  local target="$ROOT_DIR/client/config.js"
  if [[ -f "$target" && "$RECONFIGURE" != "1" ]]; then
    echo "Using existing client/config.js (pass --reconfigure to regenerate)."
    return
  fi
  local url="${API_URL:-$(ask "API server URL" "http://127.0.0.1:8000/time_weaver")}"
  cat > "$target" <<EOF
const config = {
    API_SERVER_URL: "$url"
};

export default config;
EOF
  echo "Wrote client/config.js (API_SERVER_URL=$url)."
}

install_systemd_service() {
  local template="$1" target_name="$2"
  local target="/etc/systemd/system/${target_name}"
  if [[ "$(id -u)" -ne 0 ]]; then
    echo "Installing systemd services requires root. Re-run with sudo." >&2
    exit 1
  fi
  sed \
    -e "s#__TIMEWEAVER_ROOT__#${ROOT_DIR}#g" \
    -e "s#__TIMEWEAVER_USER__#${SERVICE_USER}#g" \
    "$template" > "$target"
  systemctl daemon-reload
  systemctl enable "$target_name"
  echo "Installed and enabled $target_name"
}

require_command "$PYTHON_BIN"
if [[ "$DO_CLIENT" == "1" ]]; then require_command npm; fi

if [[ "$DO_SERVER" == "1" ]]; then
  mkdir -p "$ROOT_DIR/server/log"
  setup_python_project "$ROOT_DIR/server"
  write_server_env
fi

if [[ "$DO_AGENT" == "1" ]]; then
  mkdir -p "$ROOT_DIR/agent/log"
  setup_python_project "$ROOT_DIR/agent"
  write_agent_config
fi

if [[ "$DO_CLIENT" == "1" ]]; then
  pushd "$ROOT_DIR/client" >/dev/null
  npm ci
  write_client_config
  npm run build
  popd >/dev/null
fi

if [[ "$INSTALL_SERVICES" == "1" ]]; then
  if [[ "$(id -u)" -eq 0 ]]; then
    SERVICE_GROUP="$(id -gn "$SERVICE_USER")"
    CHOWN_TARGETS=()
    [[ "$DO_SERVER" == "1" ]] && CHOWN_TARGETS+=("$ROOT_DIR/server")
    [[ "$DO_AGENT" == "1" ]] && CHOWN_TARGETS+=("$ROOT_DIR/agent")
    [[ "$DO_CLIENT" == "1" ]] && CHOWN_TARGETS+=("$ROOT_DIR/client")
    if [[ ${#CHOWN_TARGETS[@]} -gt 0 ]]; then
      chown -R "$SERVICE_USER:$SERVICE_GROUP" "${CHOWN_TARGETS[@]}"
    fi
  fi
  if [[ "$DO_SERVER" == "1" ]]; then
    install_systemd_service "$ROOT_DIR/scripts/systemd/timeweaver-server.service" "timeweaver-server.service"
  fi
  if [[ "$DO_AGENT" == "1" ]]; then
    install_systemd_service "$ROOT_DIR/scripts/systemd/timeweaver-agent.service" "timeweaver-agent.service"
  fi
  echo "Start services with: sudo systemctl start timeweaver-server timeweaver-agent"
fi

cat <<EOF

TimeWeaver Linux setup complete (component: $COMPONENT).
Config was written automatically - no files to copy or edit.
Start:
EOF
[[ "$DO_SERVER" == "1" ]] && echo "  - cd \"$ROOT_DIR/server\" && . .venv/bin/activate && uvicorn app:app --host 0.0.0.0 --port 8000 --workers 1"
[[ "$DO_AGENT" == "1" ]]  && echo "  - cd \"$ROOT_DIR/agent\" && . .venv/bin/activate && python timeweaver.py   (needs the shared database reachable)"
[[ "$DO_CLIENT" == "1" ]] && echo "  - serve client/dist, or 'npm run serve' for development"

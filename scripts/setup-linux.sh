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
DB_LOG="${DB_LOG:-}"
SECRET_KEY="${SECRET_KEY:-}"
ALLOWED_ORIGIN="${ALLOWED_ORIGIN:-}"
CONTEXT="${CONTEXT:-}"
ACCESS_TOKEN_EXPIRE_MINUTES="${ACCESS_TOKEN_EXPIRE_MINUTES:-}"
SERVER_HOST="${SERVER_HOST:-}"
SERVER_PORT="${SERVER_PORT:-}"
REDIS_HOST="${REDIS_HOST:-}"
REDIS_PORT="${REDIS_PORT:-}"
REDIS_DB="${REDIS_DB:-}"
API_URL="${API_URL:-}"

usage() {
  cat <<'USAGE'
Usage: scripts/setup-linux.sh [options]

Components:
  --component all|server|client         What to install (prompts if omitted; default all)

Config (optional; installer prompts interactively otherwise, sensible defaults applied):
  --db-type sqlite3|mysql               Server database type (default sqlite3)
  --db-host / --db-port / --db-user / --db-password / --db-name / --db-schema
  --db-path PATH                        sqlite file path (server, sqlite3 only)
  --db-log true|false                   Log DB queries (default true)
  --secret-key KEY                      Server SECRET_KEY (auto-generated if omitted)
  --allowed-origin ORIGIN               Server CORS origin (default *)
  --context PATH                        Server API context (default /time_weaver)
  --access-token-expire-minutes N       JWT lifetime in minutes (default 30)
  --server-host HOST                    uvicorn bind host (default 0.0.0.0)
  --server-port PORT                    uvicorn bind port (default 8000)
  --redis-host / --redis-port / --redis-db   Optional Redis blacklist store (defaults localhost:6379/0)
  --api-url URL                         Client API server URL

Behaviour:
  --reconfigure                         (kept for compatibility; config always runs now)
  --non-interactive                     Never prompt; use flags/env/defaults
  --install-services                    Install the legacy Python server systemd service.
                                        Interactive installs also OFFER this as a
                                        prompt, so the flag is only needed for
                                        unattended/CI runs.
  --service-user USER                   systemd service user (default: current user)

Config is generated ready-to-run; no sample files to copy or hand-edit.
Install the Go agent separately: https://github.com/horrible-gh/TimeWeaver-Agent
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
    --db-log) DB_LOG="${2:?}"; shift 2 ;;
    --secret-key) SECRET_KEY="${2:?}"; shift 2 ;;
    --allowed-origin) ALLOWED_ORIGIN="${2:?}"; shift 2 ;;
    --context) CONTEXT="${2:?}"; shift 2 ;;
    --access-token-expire-minutes) ACCESS_TOKEN_EXPIRE_MINUTES="${2:?}"; shift 2 ;;
    --server-host) SERVER_HOST="${2:?}"; shift 2 ;;
    --server-port) SERVER_PORT="${2:?}"; shift 2 ;;
    --redis-host) REDIS_HOST="${2:?}"; shift 2 ;;
    --redis-port) REDIS_PORT="${2:?}"; shift 2 ;;
    --redis-db) REDIS_DB="${2:?}"; shift 2 ;;
    --api-url) API_URL="${2:?}"; shift 2 ;;
    --reconfigure) RECONFIGURE=1; shift ;;
    --non-interactive) NONINTERACTIVE=1; shift ;;
    --install-services) INSTALL_SERVICES=1; shift ;;
    --service-user) SERVICE_USER="${2:?Missing service user}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; usage; exit 2 ;;
  esac
done

# Honour non-interactive ONLY when explicitly requested (flag) or under CI. We no
# longer auto-disable prompts on a redirected stdin: a plain interactive install
# must let you set every value, never silently fall back to defaults.
if [[ -n "${CI:-}" ]]; then NONINTERACTIVE=1; fi

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

# pick FLAG "Prompt" EXISTING DEFAULT  -> flag wins, else prompt seeded by existing/default
pick() {
  local flag="$1" prompt="$2" existing="$3" default="$4"
  if [[ -n "$flag" ]]; then echo "$flag"; return; fi
  local seed="${existing:-$default}"
  ask "$prompt" "$seed"
}

gen_secret() {
  if command -v openssl >/dev/null 2>&1; then openssl rand -hex 32; return; fi
  "$PYTHON_BIN" -c "import secrets; print(secrets.token_hex(32))"
}

env_get() {  # env_get KEY FILE -> echoes value or empty
  local key="$1" file="$2"
  [[ -f "$file" ]] || { echo ""; return; }
  sed -n "s/^${key}=//p" "$file" | head -n1
}


backup_if_exists() {
  local path="$1"
  if [[ -f "$path" ]]; then
    local stamp backup_dir
    stamp="$(date +%Y%m%d-%H%M%S)"
    backup_dir="$ROOT_DIR/backups/$stamp"
    mkdir -p "$backup_dir"
    cp -f "$path" "$backup_dir/$(basename "$path")"
    echo "  Backed up existing $(basename "$path") -> backups/$stamp/"
  fi
}

finalize_run_script() {
  local target="$1"
  chmod +x "$target"
  echo "Created $target"
}

if [[ -z "$COMPONENT" ]]; then
  COMPONENT="$(ask "Install which component? (all/server/client)" "all")"
fi
case "$COMPONENT" in
  all|server|client) ;;
  agent) echo "The Python agent has been retired. Install the Go agent from https://github.com/horrible-gh/TimeWeaver-Agent" >&2; exit 2 ;;
  *) echo "Invalid component: $COMPONENT (expected all|server|client)" >&2; exit 2 ;;
esac

DO_SERVER=0; DO_CLIENT=0
[[ "$COMPONENT" == "all" || "$COMPONENT" == "server" ]] && DO_SERVER=1
[[ "$COMPONENT" == "all" || "$COMPONENT" == "client" ]] && DO_CLIENT=1

# --- Server database resolution -----------------------------------------------
ENV_FILE="$ROOT_DIR/server/.env"
RESOLVED_DB_TYPE="$DB_TYPE"
MY_HOST=""; MY_PORT=""; MY_USER=""; MY_PASSWORD=""; MY_DB=""; MY_SCHEMA=""

resolve_shared_db() {
  if [[ "$DO_SERVER" == "1" ]]; then
    local seed_type; seed_type="$(env_get DB_TYPE "$ENV_FILE")"; seed_type="${seed_type:-sqlite3}"
    RESOLVED_DB_TYPE="$(pick "$DB_TYPE" "Server database type (sqlite3/mysql)" "$seed_type" "sqlite3")"
  fi

  if [[ "$DO_SERVER" == "1" && "$RESOLVED_DB_TYPE" == "mysql" ]]; then
    MY_HOST="$(pick "$DB_HOST" "DB host" "$(env_get DB_HOST "$ENV_FILE")" "127.0.0.1")"
    MY_PORT="$(pick "$DB_PORT" "DB port" "$(env_get DB_PORT "$ENV_FILE")" "3306")"
    MY_USER="$(pick "$DB_USER" "DB user" "$(env_get DB_USER "$ENV_FILE")" "timeweaver")"
    MY_PASSWORD="$(pick "$DB_PASSWORD" "DB password" "$(env_get DB_PASSWORD "$ENV_FILE")" "")"
    MY_DB="$(pick "$DB_NAME" "DB name" "$(env_get DB_DATABASE "$ENV_FILE")" "timeweaver")"
    MY_SCHEMA="$(pick "$DB_SCHEMA" "DB schema (blank if none)" "$(env_get DB_SCHEMA "$ENV_FILE")" "")"
  fi
}

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
  # Existing values become defaults; the config step ALWAYS runs (no silent skip).
  local db_type="$RESOLVED_DB_TYPE"
  local secret="$SECRET_KEY"
  [[ -z "$secret" ]] && secret="$(env_get SECRET_KEY "$target")"
  [[ -z "$secret" ]] && secret="$(gen_secret)"

  local origin ctx expire dblog rhost rport rdb
  origin="$(pick "$ALLOWED_ORIGIN" "CORS allowed origin (ALLOWED_ORIGIN)" "$(env_get ALLOWED_ORIGIN "$target")" "*")"
  ctx="$(pick "$CONTEXT" "API context path (CONTEXT)" "$(env_get CONTEXT "$target")" "/time_weaver")"
  expire="$(pick "$ACCESS_TOKEN_EXPIRE_MINUTES" "Access token lifetime in minutes (ACCESS_TOKEN_EXPIRE_MINUTES)" "$(env_get ACCESS_TOKEN_EXPIRE_MINUTES "$target")" "30")"
  dblog="$(pick "$DB_LOG" "Log DB queries? (true/false; DB_LOG)" "$(env_get DB_LOG "$target")" "true")"
  rhost="$(pick "$REDIS_HOST" "Redis host (optional; REDIS_HOST)" "$(env_get REDIS_HOST "$target")" "localhost")"
  rport="$(pick "$REDIS_PORT" "Redis port (REDIS_PORT)" "$(env_get REDIS_PORT "$target")" "6379")"
  rdb="$(pick "$REDIS_DB" "Redis db index (REDIS_DB)" "$(env_get REDIS_DB "$target")" "0")"

  local h p u pw db sc dbpath
  if [[ "$db_type" == "mysql" ]]; then
    h="$MY_HOST"; p="$MY_PORT"; u="$MY_USER"; pw="$MY_PASSWORD"; db="$MY_DB"; sc="$MY_SCHEMA"; dbpath=""
  else
    h="127.0.0.1"; p="0"; u=""; pw=""; db=""; sc=""
    dbpath="$(pick "$DB_PATH" "SQLite file path (DB_PATH)" "$(env_get DB_PATH "$target")" "./timeweaver.sqlite3")"
  fi

  backup_if_exists "$target"
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
DB_LOG=$dblog
DB_PATH=$dbpath
REDIS_HOST=$rhost
REDIS_PORT=$rport
REDIS_DB=$rdb
EOF
  echo "Wrote server/.env (DB_TYPE=$db_type, all keys populated)."
}

write_client_config() {
  local target="$ROOT_DIR/client/config.js"
  local existing_url=""
  [[ -f "$target" ]] && existing_url="$(sed -n 's/.*API_SERVER_URL:[[:space:]]*"\([^"]*\)".*/\1/p' "$target" | head -n1)"
  local def_url="http://127.0.0.1:${RESOLVED_SERVER_PORT}${RESOLVED_SERVER_CTX}"
  local url; url="$(pick "$API_URL" "API server URL" "$existing_url" "$def_url")"
  backup_if_exists "$target"
  cat > "$target" <<EOF
const config = {
    API_SERVER_URL: "$url"
};

export default config;
EOF
  echo "Wrote client/config.js (API_SERVER_URL=$url)."
}

install_systemd_service() {
  # Root is verified by the caller before we get here.
  local template="$1" target_name="$2"
  local target="/etc/systemd/system/${target_name}"
  local rendered
  rendered="$(sed \
    -e "s#__TIMEWEAVER_ROOT__#${ROOT_DIR}#g" \
    -e "s#__TIMEWEAVER_USER__#${SERVICE_USER}#g" \
    -e "s#__TIMEWEAVER_SERVER_HOST__#${RESOLVED_SERVER_HOST}#g" \
    -e "s#__TIMEWEAVER_SERVER_PORT__#${RESOLVED_SERVER_PORT}#g" \
    "$template")"
  printf '%s\n' "$rendered" > "$target"
  systemctl daemon-reload
  systemctl enable "$target_name"
  echo "Installed and enabled $target_name"
}

# G1: in an interactive install, surface the service question instead of hiding
# it behind a flag nobody discovers. A flag (--install-services) or CI keep the
# unattended behaviour untouched; we only ask when nothing decided it already.
maybe_prompt_services() {
  # Only the server has a unit; nothing to register for a client-only install.
  if [[ "$DO_SERVER" != "1" ]]; then return; fi
  if [[ "$INSTALL_SERVICES" == "1" ]]; then return; fi
  if [[ "$NONINTERACTIVE" == "1" ]]; then return; fi
  local ans
  ans="$(ask "Register TimeWeaver to start on boot as a systemd service? (y/N)" "N")"
  [[ "$ans" =~ ^([Yy]|[Yy][Ee][Ss])$ ]] && INSTALL_SERVICES=1
}

require_command "$PYTHON_BIN"
if [[ "$DO_CLIENT" == "1" ]]; then require_command npm; fi

# Resolve cross-cutting values up front.
resolve_shared_db
RESOLVED_SERVER_HOST="$(pick "$SERVER_HOST" "Server bind host" "" "0.0.0.0")"
RESOLVED_SERVER_PORT="$(pick "$SERVER_PORT" "Server bind port" "" "8000")"
RESOLVED_SERVER_CTX="${CONTEXT:-$(env_get CONTEXT "$ENV_FILE")}"
RESOLVED_SERVER_CTX="${RESOLVED_SERVER_CTX:-/time_weaver}"
maybe_prompt_services

if [[ "$DO_SERVER" == "1" ]]; then
  mkdir -p "$ROOT_DIR/server/log"
  setup_python_project "$ROOT_DIR/server"
  write_server_env
fi

if [[ "$DO_CLIENT" == "1" ]]; then
  pushd "$ROOT_DIR/client" >/dev/null
  npm ci
  write_client_config
  npm run build
  popd >/dev/null
fi

if [[ "$DO_SERVER" == "1" ]]; then
  cat > "$ROOT_DIR/run-server.sh" <<EOF
#!/usr/bin/env bash
cd "\$(cd "\$(dirname "\${BASH_SOURCE[0]}")" && pwd)/server"
exec .venv/bin/python -m uvicorn app:app --host $RESOLVED_SERVER_HOST --port $RESOLVED_SERVER_PORT --workers 1
EOF
  finalize_run_script "$ROOT_DIR/run-server.sh"
fi

SERVICES_INSTALLED=0
if [[ "$INSTALL_SERVICES" == "1" ]]; then
  if [[ "$(id -u)" -ne 0 ]]; then
    # G4: don't abort a finished install just because we can't write unit files.
    # Everything else is already done; tell the user exactly how to add the
    # services in a second, root-only pass (config is reused, not rebuilt).
    echo "" >&2
    echo "  [!] Registering systemd services requires root, but this install is not running as root." >&2
    echo "      Everything else is done. To add the services, re-run as root - this only writes the unit files:" >&2
    echo "        sudo ./install.sh --component $COMPONENT --install-services --service-user \"$SERVICE_USER\" --non-interactive" >&2
  else
    SERVICE_GROUP="$(id -gn "$SERVICE_USER")"
    CHOWN_TARGETS=()
    [[ "$DO_SERVER" == "1" ]] && CHOWN_TARGETS+=("$ROOT_DIR/server")
    [[ "$DO_CLIENT" == "1" ]] && CHOWN_TARGETS+=("$ROOT_DIR/client")
    if [[ ${#CHOWN_TARGETS[@]} -gt 0 ]]; then
      chown -R "$SERVICE_USER:$SERVICE_GROUP" "${CHOWN_TARGETS[@]}"
    fi
    if [[ "$DO_SERVER" == "1" ]]; then
      install_systemd_service "$ROOT_DIR/scripts/systemd/timeweaver-server.service" "timeweaver-server.service"
      SERVICES_INSTALLED=1
    fi
  fi
fi

cat <<EOF

TimeWeaver Linux setup complete (component: $COMPONENT).
Config was written automatically - no files to copy or edit.
EOF

# G3: branch the closing "how to start" guidance on what actually happened. If
# services were registered, advertise systemctl (not a foreground command that
# makes it look like manual running is all there is).
if [[ "$SERVICES_INSTALLED" == "1" ]]; then
  SVC_NAMES=()
  [[ "$DO_SERVER" == "1" ]] && SVC_NAMES+=("timeweaver-server")
  echo "Services were registered and enabled (they start automatically on boot)."
  echo "Start them now without rebooting:"
  echo "  sudo systemctl start ${SVC_NAMES[*]}"
  echo "Check status / logs:"
  echo "  systemctl status ${SVC_NAMES[*]}"
  echo "  journalctl -u ${SVC_NAMES[0]:-timeweaver-server} -f"
  [[ "$DO_CLIENT" == "1" ]] && echo "Client: serve client/dist, or 'npm run serve' for development"
else
  echo "Start:"
  [[ "$DO_SERVER" == "1" ]] && echo "  - ./run-server.sh   (listens on $RESOLVED_SERVER_HOST:$RESOLVED_SERVER_PORT)"
  [[ "$DO_CLIENT" == "1" ]] && echo "  - serve client/dist, or 'npm run serve' for development"
fi

exit 0

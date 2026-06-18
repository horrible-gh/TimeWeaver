#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PYTHON_BIN="${PYTHON_BIN:-python3}"
INSTALL_SERVICES="${INSTALL_SERVICES:-0}"
SERVICE_USER="${SERVICE_USER:-$USER}"

usage() {
  cat <<'USAGE'
Usage: scripts/setup-linux.sh [--install-services] [--service-user USER]

Sets up TimeWeaver on Linux:
  - creates Python virtual environments for server and agent
  - installs Python and Node dependencies
  - creates local config files from samples when missing
  - builds the Vue UI
  - optionally installs systemd services for the server and agent

Environment:
  PYTHON_BIN          Python executable to use (default: python3)
  INSTALL_SERVICES   Set to 1 to install systemd services
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --install-services)
      INSTALL_SERVICES=1
      shift
      ;;
    --service-user)
      SERVICE_USER="${2:?Missing service user}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      usage
      exit 2
      ;;
  esac
done

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Required command not found: $1" >&2
    exit 1
  fi
}

copy_if_missing() {
  local source="$1"
  local target="$2"
  if [[ ! -f "$target" ]]; then
    cp "$source" "$target"
    echo "Created $target"
  else
    echo "Kept existing $target"
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

install_systemd_service() {
  local template="$1"
  local target_name="$2"
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
require_command npm

mkdir -p "$ROOT_DIR/server/log" "$ROOT_DIR/agent/log"

setup_python_project "$ROOT_DIR/server"
copy_if_missing "$ROOT_DIR/server/.env.sample" "$ROOT_DIR/server/.env"

setup_python_project "$ROOT_DIR/agent"
copy_if_missing "$ROOT_DIR/agent/conf/server.sample.json" "$ROOT_DIR/agent/conf/server.json"
copy_if_missing "$ROOT_DIR/agent/conf/time_weaver.sample.json" "$ROOT_DIR/agent/conf/time_weaver.json"

pushd "$ROOT_DIR/client" >/dev/null
npm ci
copy_if_missing "$ROOT_DIR/client/config.sample.js" "$ROOT_DIR/client/config.js"
npm run build
popd >/dev/null

if [[ "$INSTALL_SERVICES" == "1" ]]; then
  if [[ "$(id -u)" -eq 0 ]]; then
    SERVICE_GROUP="$(id -gn "$SERVICE_USER")"
    chown -R "$SERVICE_USER:$SERVICE_GROUP" \
      "$ROOT_DIR/server" \
      "$ROOT_DIR/agent" \
      "$ROOT_DIR/client"
  fi

  install_systemd_service "$ROOT_DIR/scripts/systemd/timeweaver-server.service" "timeweaver-server.service"
  install_systemd_service "$ROOT_DIR/scripts/systemd/timeweaver-agent.service" "timeweaver-agent.service"
  echo "Start services with: sudo systemctl start timeweaver-server timeweaver-agent"
fi

cat <<EOF
TimeWeaver Linux setup complete.

Next:
  1. Edit server/.env.
  2. Edit agent/conf/server.json and agent/conf/time_weaver.json.
  3. Start the server:
     cd "$ROOT_DIR/server" && . .venv/bin/activate && uvicorn app:app --host 0.0.0.0 --port 8000 --workers 1
  4. Serve client/dist with your web server, or run npm run serve for development.
EOF

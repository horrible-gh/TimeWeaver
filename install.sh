#!/usr/bin/env bash
#
# TimeWeaver legacy Python server / standalone client installer (Linux).
# For the Go agent, see https://github.com/horrible-gh/TimeWeaver-Agent.
#
# Thin pass-through wrapper around scripts/setup-linux.sh so the project can be
# installed directly from the repository root. Every argument is forwarded
# unchanged.
#
# Run with no arguments for an interactive install (it asks which component to
# install and writes ready-to-run config for you - nothing to copy or edit).
#
# Examples:
#   ./install.sh                                    # interactive
#   ./install.sh --component server                 # legacy Python server only
#   ./install.sh --component server --non-interactive   # unattended defaults
#   sudo ./install.sh --install-services --service-user "$USER"
#
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec "$ROOT_DIR/scripts/setup-linux.sh" "$@"

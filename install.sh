#!/usr/bin/env bash
#
# TimeWeaver root installer (Linux).
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
#   ./install.sh --component agent                  # agent only
#   ./install.sh --component server --non-interactive   # unattended defaults
#   sudo ./install.sh --install-services --service-user "$USER"
#
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec "$ROOT_DIR/scripts/setup-linux.sh" "$@"

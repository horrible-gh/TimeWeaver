#!/usr/bin/env bash
# Build and run the unified Go server in the foreground.
set -euo pipefail
root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
env_file="server/.env"
pull=false
build=true
force=false
setup=false
while (($#)); do
  case "$1" in
    --env)
      if (($# < 2)); then printf '%s\n' '--env requires a filename' >&2; exit 2; fi
      env_file="$2"; shift 2 ;;
    --pull) pull=true; shift ;;
    --no-build) build=false; shift ;;
    --force-build) force=true; shift ;;
    --setup) setup=true; shift ;;
    -h|--help)
      printf '%s\n' 'Usage: ./run-go.sh [--env FILE] [--pull] [--setup] [--no-build | --force-build]'
      printf '%s\n' 'The default rebuilds the UI and server. --no-build reuses dist/timeweaver explicitly.'
      printf '%s\n' 'Routine database upgrades are automatic. --setup is for initial setup or explicit repair.'
      exit 0 ;;
    *) printf 'Unknown option: %s\n' "$1" >&2; exit 2 ;;
  esac
done
if $force && ! $build; then
  printf '%s\n' '--no-build and --force-build cannot be combined' >&2
  exit 2
fi
cd "$root"
if $pull; then
  printf '%s\n' '[run] git pull --ff-only...'
  git pull --ff-only
fi
if $build; then
  printf '%s\n' '[run] building the embedded UI and Go server...'
  bash scripts/build-go.sh
fi
binary="$root/dist/timeweaver"
if [[ ! -x "$binary" ]]; then
  printf '%s\n' 'dist/timeweaver is missing or not executable; run without --no-build.' >&2
  exit 1
fi
"$binary" version
if $setup; then
  "$binary" setup --env "$env_file" --allow-write
fi
# Serve applies approved incremental upgrades before checking readiness.
printf '%s\n' '[run] starting the server (automatic database upgrades and readiness checks)...'
exec "$binary" serve --env "$env_file"

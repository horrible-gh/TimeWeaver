#!/usr/bin/env bash
set -euo pipefail
root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
npm ci --prefix client
TW_EMBEDDED_BUILD=1 npm run build --prefix client
go run ./cmd/timeweaver assets-check
TW_REQUIRE_EMBEDDED_UI=1 go test ./...
mkdir -p dist
suffix=""
if [[ "$(go env GOOS)" == "windows" ]]; then suffix=".exe"; fi
go build -trimpath -o "dist/timeweaver${suffix}" ./cmd/timeweaver
printf 'Built dist/timeweaver%s (migration CLI; not a production API server yet).\n' "$suffix"

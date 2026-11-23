#!/usr/bin/env bash
set -euo pipefail
root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
version="${TIMEWEAVER_BUILD_VERSION:-dev}"
commit="${TIMEWEAVER_BUILD_COMMIT:-}"
if [[ -z "$commit" ]]; then
  if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    commit="$(git rev-parse --short=12 HEAD)"
    if [[ -n "$(git status --porcelain --untracked-files=normal)" ]]; then commit+="-dirty"; fi
  else
    commit="archive"
  fi
fi
if [[ ! "$version" =~ ^[A-Za-z0-9._+-]+$ || ! "$commit" =~ ^[A-Za-z0-9._+-]+$ ]]; then
  printf 'TIMEWEAVER_BUILD_VERSION and TIMEWEAVER_BUILD_COMMIT must contain only letters, digits, dots, underscores, plus signs or hyphens.\n' >&2
  exit 2
fi
npm ci --prefix client
TW_EMBEDDED_BUILD=1 npm run build --prefix client
go run ./cmd/timeweaver assets-check
TW_REQUIRE_EMBEDDED_UI=1 go test ./...
mkdir -p dist
suffix=""
if [[ "$(go env GOOS)" == "windows" ]]; then suffix=".exe"; fi
temporary_dir="$(mktemp -d "$root/dist/timeweaver-build.XXXXXX")"
temporary="$temporary_dir/timeweaver${suffix}"
trap 'rm -f -- "$temporary"; rmdir -- "$temporary_dir"' EXIT
go build -trimpath -ldflags "-X main.version=$version -X main.commit=$commit" -o "$temporary" ./cmd/timeweaver
chmod +x "$temporary"
"$temporary" assets-check
mv -f -- "$temporary" "dist/timeweaver${suffix}"
printf 'Built dist/timeweaver%s (%s, %s), including the Vue UI.\n' "$suffix" "$version" "$commit"
printf 'Run ./dist/timeweaver%s serve --env server/.env after database setup.\n' "$suffix"

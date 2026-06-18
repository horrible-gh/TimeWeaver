#!/usr/bin/env sh
set -eu

cat > .env <<EOF
ALLOWED_ORIGIN=${ALLOWED_ORIGIN:-*}
SECRET_KEY=${SECRET_KEY:-change-me-for-production}
ACCESS_TOKEN_EXPIRE_MINUTES=${ACCESS_TOKEN_EXPIRE_MINUTES:-30}
CONTEXT=${CONTEXT:-/time_weaver}
DB_TYPE=${DB_TYPE:-mysql}
DB_HOST=${DB_HOST:-mysql}
DB_PORT=${DB_PORT:-3306}
DB_USER=${DB_USER:-timeweaver}
DB_PASSWORD=${DB_PASSWORD:-timeweaver}
DB_DATABASE=${DB_DATABASE:-timeweaver}
DB_SCHEMA=${DB_SCHEMA:-timeweaver}
DB_LOG=${DB_LOG:-true}
DB_PATH=${DB_PATH:-/data/timeweaver.sqlite3}
REDIS_HOST=${REDIS_HOST:-redis}
REDIS_PORT=${REDIS_PORT:-6379}
REDIS_DB=${REDIS_DB:-0}
EOF

exec "$@"

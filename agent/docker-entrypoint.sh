#!/usr/bin/env sh
set -eu

mkdir -p conf log

cat > conf/server.json <<EOF
{
  "log": {
    "base": {
      "name": "TimeWeaverAgent",
      "level": "${LOG_LEVEL:-debug}"
    },
    "console": {
      "level": "${LOG_LEVEL:-debug}",
      "format": "%(asctime)s[%(levelname)s]%(message)s"
    },
    "file_timed": {
      "level": "${LOG_LEVEL:-debug}",
      "format": "%(asctime)s[%(levelname)s]%(message)s",
      "file_name": "log/server.log",
      "when": "midnight",
      "interval": 1,
      "backup_count": 30
    }
  }
}
EOF

cat > conf/time_weaver.json <<EOF
{
  "device": "${DEVICE_NAME:-test}",
  "api": {
    "base_url": "${TIMEWEAVER_SERVER_URL:-http://server:8000${CONTEXT:-/time_weaver}}",
    "credential_path": "conf/agent_credential.json",
    "enrollment_token_env": "TIMEWEAVER_ENROLLMENT_TOKEN"
  },
  "reschedule": {
    "year": "${RESCHEDULE_YEAR:-*}",
    "month": "${RESCHEDULE_MONTH:-*}",
    "day": "${RESCHEDULE_DAY:-*}",
    "hour": "${RESCHEDULE_HOUR:-*}",
    "minute": "${RESCHEDULE_MINUTE:-*/5}",
    "second": "${RESCHEDULE_SECOND:-0}"
  }
}
EOF

exec "$@"
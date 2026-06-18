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
  },
  "databases": {
    "time_weaver": {
      "database": {
        "type": "mysql",
        "mysql": {
          "host": "${DB_HOST:-mysql}",
          "port": ${DB_PORT:-3306},
          "user": "${DB_USER:-timeweaver}",
          "password": "${DB_PASSWORD:-timeweaver}",
          "database": "${DB_DATABASE:-timeweaver}",
          "schema": "${DB_SCHEMA:-timeweaver}",
          "log": ${DB_LOG:-true},
          "sqloder": "res/time_weaver/sql/sqloader"
        },
        "migration": {
          "auto_migration": true,
          "migration_path": "res/time_weaver/sql/migration"
        },
        "service": {
          "log": true,
          "sqloder": "res/time_weaver/sql/sqloader"
        }
      }
    }
  }
}
EOF

cat > conf/time_weaver.json <<EOF
{
  "device": "${DEVICE_NAME:-test}",
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

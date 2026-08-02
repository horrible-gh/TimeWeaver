import io
import json
import os
import re
import sys
from contextlib import redirect_stdout
from enum import Enum

import LogAssist.log as logger
from pydantic import model_validator
from pydantic_settings import BaseSettings, SettingsConfigDict
from schema_guard import ensure_critical_schema
from sqloader.init import database_init


class DBType(str, Enum):
    MYSQL = "mysql"
    SQLITE = "sqlite"
    SQLITE3 = "sqlite3"
    LOCAL = "local"


_DEFAULT_BLOCKING_WORKERS = max(4, min(32, 4 * (os.cpu_count() or 1)))
_EXAMPLE_SECRET = "change-me-for-production"
_MIGRATION_FAILURE = re.compile(
    r"Database Migration Failed\.Failed to apply migration "
    r"(?P<migration_file>[^:\r\n]+):\s*(?P<db_error>.+)",
    re.DOTALL,
)

# timeweaver_server_011.sql aborts on purpose by selecting one of these
# nonexistent columns so the raw MySQL 1054 error carries an operator hint.
# Expand this JSON diagnostic with the full guidance so an unmanned or
# unfamiliar operator does not mistake an intentional fail-closed guard for a
# regression of the schedule_detail soft-delete fix (see
# timeweaver.server.0004 TR0005 / T0004).
_MIGRATION_ABORT_GUIDANCE = {
    "tw011_invalid_detail_run_scripts_diagnose_execution_log_orphans": (
        "timeweaver_server_011.sql aborted on purpose: execution_log still has "
        "rows whose detail_id does not map to any schedule_detail row. This is "
        "expected for execution history created before schedule_detail "
        "soft-delete was introduced -- that fix only stops *new* orphans from "
        "being created, it does not repair old ones. Run "
        "scripts/diagnose_execution_log_orphans.sql against this database, "
        "review each row it lists from execution_log_quarantine, and resolve "
        "every one by choosing (1) remap detail_id to another task, (2) restore "
        "the tombstoned schedule_detail row, or (3) approve deleting the "
        "quarantined row. The migration keeps failing on every startup until "
        "all quarantined rows are resolved; this is intentional fail-closed "
        "behavior, not a bug."
    ),
    "tw_migration_011_abort_duplicate_attempt_repair_failed": (
        "timeweaver_server_011.sql aborted on purpose: automatic repair could "
        "not make (execution_grp_id, detail_id, attempt) unique in "
        "execution_log. Inspect execution_log_attempt_repair_log and the "
        "remaining duplicate rows manually -- the migration will not guess "
        "which duplicate row is authoritative."
    ),
}


class _ForwardingCapture(io.StringIO):
    """Mirror sqloader stdout while retaining its migration exception text."""

    def __init__(self, forward):
        super().__init__()
        self.forward = forward

    def write(self, value):
        self.forward.write(value)
        return super().write(value)

    def flush(self):
        self.forward.flush()
        return super().flush()


def _database_init_with_diagnostics(config):
    """Log sqloader's swallowed migration exception, then preserve fail-closed exit."""

    captured = _ForwardingCapture(sys.stdout)
    try:
        with redirect_stdout(captured):
            return database_init(config)
    except SystemExit as exc:
        output = captured.getvalue()
        match = _MIGRATION_FAILURE.search(output)
        if match:
            migration_file = match.group("migration_file").strip()
            db_error = match.group("db_error").strip()
        else:
            migration_file = None
            lines = [line.strip() for line in output.splitlines() if line.strip()]
            db_error = lines[-1] if lines else f"sqloader exited with code {exc.code}"

        db_type = config.get("type")
        db_config = config.get(db_type, {}) if db_type else {}
        diagnostic = {
            "event": "database_migration_failed",
            "database": db_config.get("database") or db_config.get("db_name"),
            "migration_file": migration_file,
            "db_error": db_error,
            "exit_code": exc.code,
        }
        guidance = next(
            (
                text
                for marker, text in _MIGRATION_ABORT_GUIDANCE.items()
                if marker in db_error
            ),
            None,
        )
        if guidance:
            diagnostic["guidance"] = guidance
        logger.error(
            "[database-migration-failure] "
            + json.dumps(diagnostic, ensure_ascii=False, sort_keys=True)
        )
        raise


class Settings(BaseSettings):
    ALLOWED_ORIGIN: str
    SECRET_KEY: str
    AGENT_SECRET_KEY: str = ""
    ACCESS_TOKEN_EXPIRE_MINUTES: int = 30
    CONTEXT: str
    DB_TYPE: DBType
    DB_HOST: str = ""
    DB_PORT: int = 0
    DB_USER: str = ""
    DB_PASSWORD: str = ""
    DB_DATABASE: str = ""
    DB_SCHEMA: str = ""
    DB_LOG: bool = True
    DB_PATH: str = ""
    REDIS_HOST: str = "localhost"
    REDIS_PORT: int = 6379
    REDIS_DB: int = 0
    TRUSTED_PROXIES: str = ""
    RATE_LIMIT_CAPACITY: int = 60
    RATE_LIMIT_REFILL: float = 1.0
    BLOCKING_DB_WORKER_COUNT: int = _DEFAULT_BLOCKING_WORKERS
    BLOCKING_DB_QUEUE_CAPACITY: int = 2 * _DEFAULT_BLOCKING_WORKERS
    DB_HEALTH_TIMEOUT: float = 1.0
    HEALTH_RETRY_AFTER: int = 5
    CORS_ALLOW_CREDENTIALS: bool = True

    @model_validator(mode="after")
    def reject_unsafe_startup(self):
        for field_name in ("SECRET_KEY", "AGENT_SECRET_KEY"):
            value = getattr(self, field_name)
            if value and (
                value == _EXAMPLE_SECRET or len(value.encode("utf-8")) < 32
            ):
                raise ValueError(f"{field_name} must be a non-example key of at least 32 bytes")
        origins = {item.strip() for item in self.ALLOWED_ORIGIN.split(",")}
        if self.CORS_ALLOW_CREDENTIALS and "*" in origins:
            raise ValueError("credentialed CORS cannot use a wildcard origin")
        if self.RATE_LIMIT_REFILL <= 0:
            raise ValueError("RATE_LIMIT_REFILL must be greater than zero")
        if self.RATE_LIMIT_CAPACITY <= 0:
            raise ValueError("RATE_LIMIT_CAPACITY must be greater than zero")
        if self.BLOCKING_DB_WORKER_COUNT <= 0:
            raise ValueError("BLOCKING_DB_WORKER_COUNT must be greater than zero")
        if self.BLOCKING_DB_QUEUE_CAPACITY < 0:
            raise ValueError("BLOCKING_DB_QUEUE_CAPACITY cannot be negative")
        if self.DB_HEALTH_TIMEOUT <= 0 or self.HEALTH_RETRY_AFTER <= 0:
            raise ValueError("health timeout and retry settings must be positive")
        return self

    model_config = SettingsConfigDict(
        env_file=".env",
        hide_input_in_errors=True,
    )


settings = Settings()


class DatabaseSetting:
    _instance = None

    def __new__(cls):
        if cls._instance is None:
            cls._instance = super(DatabaseSetting, cls).__new__(cls)
            cls._instance._init_db()
        return cls._instance

    def _init_db(self):
        self.db_instance = None
        self.sqloader = None
        self.migrator = None
        self.config = {}

        if settings.DB_TYPE.value == DBType.MYSQL:
            self.config = {
                "type": settings.DB_TYPE.value,
                f"{settings.DB_TYPE.value}": {
                    "host": settings.DB_HOST,
                    "port": settings.DB_PORT,
                    "user": settings.DB_USER,
                    "password": settings.DB_PASSWORD,
                    "database": settings.DB_DATABASE,
                    "schema": settings.DB_SCHEMA,
                    "log": settings.DB_LOG,
                },
                "service": {"log": True, "sqloder": "res/sql/sqloader/mysql"},
                "migration": {
                    "auto_migration": True,
                    "migration_path": "res/sql/migration/mysql",
                },
            }
        elif settings.DB_TYPE.value in (DBType.SQLITE, DBType.SQLITE3, DBType.LOCAL):
            self.config = {
                "type": settings.DB_TYPE.value,
                f"{settings.DB_TYPE.value}": {"db_name": settings.DB_PATH},
                "service": {"log": True, "sqloder": "res/sql/sqloader/sqlite"},
                "migration": {
                    "auto_migration": True,
                    "migration_path": "res/sql/migration/sqlite",
                },
            }
        self.instance_init()

    def instance_init(self):
        self.db_instance, self.sqloader, self.migrator = (
            _database_init_with_diagnostics(self.config)
        )
        if settings.DB_TYPE.value == DBType.MYSQL:
            ensure_critical_schema(self.db_instance)

    def get_db_instance(self):
        return self.db_instance

    def get_sqloader_instance(self):
        return self.sqloader


db = DatabaseSetting()


def get_db_instance():
    return db.get_db_instance()


def get_sqloader_instance():
    return db.get_sqloader_instance()
import os
from enum import Enum

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
        self.db_instance, self.sqloader, self.migrator = database_init(self.config)
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
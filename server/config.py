from pydantic_settings import BaseSettings
from enum import Enum
from sqloader.init import database_init

# 🔹 Define DB_TYPE explicitly with an enum
class DBType(str, Enum):
    MYSQL = "mysql"
    SQLITE = "sqlite"
    SQLITE3 = "sqlite3"
    LOCAL = "local"

# 🔹 Settings class using Pydantic
class Settings(BaseSettings):
    ALLOWED_ORIGIN: str
    SECRET_KEY: str
    ACCESS_TOKEN_EXPIRE_MINUTES: int = 30
    CONTEXT: str
    DB_TYPE: DBType  # Use enum
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

    class Config:
        env_file = ".env"

settings = Settings()

# 🔹 Database settings class using the singleton pattern
class DatabaseSetting:
    _instance = None

    def __new__(cls):
        if cls._instance is None:
            cls._instance = super(DatabaseSetting, cls).__new__(cls)
            cls._instance._init_db()
        return cls._instance

    def _init_db(self):
        """Initialize database"""
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
                "service": {
                    "log": True,
                    "sqloder": "res/sql/sqloader/mysql"
                },
                "migration": {
                    "auto_migration": True,
                    "migration_path": "res/sql/migration/mysql"
                },
            }
        elif settings.DB_TYPE.value in (DBType.SQLITE, DBType.SQLITE3, DBType.LOCAL):
            self.config = {
                "type": settings.DB_TYPE.value,
                f"{settings.DB_TYPE.value}": {
                    "db_name": settings.DB_PATH
                },
                "service": {
                    "log": True,
                    "sqloder": "res/sql/sqloader/sqlite"
                },
                "migration": {
                    "auto_migration": True,
                    "migration_path": "res/sql/migration/sqlite"
                },
            }

        self.instance_init()


    def instance_init(self):
        """Initialize database instance"""
        self.db_instance, self.sqloader, self.migrator = database_init(self.config)

    def get_db_instance(self):
        return self.db_instance

    def get_sqloader_instance(self):
        return self.sqloader

# 🔹 Create singleton object
db = DatabaseSetting()

# 🔹 Function used for FastAPI dependency injection
def get_db_instance():
    return db.get_db_instance()

def get_sqloader_instance():
    return db.get_sqloader_instance()

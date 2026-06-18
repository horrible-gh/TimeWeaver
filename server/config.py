from pydantic_settings import BaseSettings
from enum import Enum
from sqloader.init import database_init

# 🔹 Enum을 사용하여 DB_TYPE을 명확하게 정의
class DBType(str, Enum):
    MYSQL = "mysql"
    SQLITE = "sqlite"
    SQLITE3 = "sqlite3"
    LOCAL = "local"

# 🔹 설정 클래스 (Pydantic 활용)
class Settings(BaseSettings):
    ALLOWED_ORIGIN: str
    SECRET_KEY: str
    ACCESS_TOKEN_EXPIRE_MINUTES: int = 30
    CONTEXT: str
    DB_TYPE: DBType  # Enum 적용
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

# 🔹 DB 설정 클래스 (싱글톤 패턴 적용)
class DatabaseSetting:
    _instance = None

    def __new__(cls):
        if cls._instance is None:
            cls._instance = super(DatabaseSetting, cls).__new__(cls)
            cls._instance._init_db()
        return cls._instance

    def _init_db(self):
        """DB 초기화"""
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
        """DB 인스턴스 초기화"""
        self.db_instance, self.sqloader, self.migrator = database_init(self.config)

    def get_db_instance(self):
        return self.db_instance

    def get_sqloader_instance(self):
        return self.sqloader

# 🔹 싱글톤 객체 생성
db = DatabaseSetting()

# 🔹 FastAPI에서 의존성 주입으로 사용할 함수
def get_db_instance():
    return db.get_db_instance()

def get_sqloader_instance():
    return db.get_sqloader_instance()

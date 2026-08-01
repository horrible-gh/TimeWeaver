"""Live-MySQL regression tests for server-owned schema bootstrap.

Set TIMEWEAVER_TEST_MYSQL_HOST (plus optional PORT/USER/PASSWORD) to run. The
account must be allowed to create and drop temporary databases.
"""
import os
import re
import uuid
from pathlib import Path

import pymysql
import pytest
from passlib.context import CryptContext
from sqloader.migrator import DatabaseMigrator, _split_sql_statements
from sqloader.mysql import MySqlWrapper

from conftest import MIGRATION_DIR
from repositories.agent_runtime import AgentRuntimeRepository

MYSQL_HOST = os.getenv("TIMEWEAVER_TEST_MYSQL_HOST")
pytestmark = pytest.mark.skipif(
    not MYSQL_HOST,
    reason="set TIMEWEAVER_TEST_MYSQL_HOST to run live MySQL bootstrap tests",
)

CORE_TABLES = (
    "devices",
    "schedule_group",
    "schedule_detail",
    "task_detail",
    "manual_execution",
    "execution_log",
)
AGENT_TABLES = ("agent_enrollment_token", "agent_device_credential")
CORE_FILES = tuple(MIGRATION_DIR / f"core_bootstrap_{number:03d}_{table}.sql" for number, table in enumerate(CORE_TABLES, 1))
AGENT_FILES = (
    MIGRATION_DIR / "timeweaver_server_005.sql",
    MIGRATION_DIR / "timeweaver_server_006.sql",
)
RUNTIME_FILES = (
    MIGRATION_DIR / "timeweaver_server_007.sql",
    MIGRATION_DIR / "timeweaver_server_008.sql",
)
EXPECTED_PREFIX = tuple(path.name for path in CORE_FILES)
PASSWORD_CONTEXT = CryptContext(schemes=["pbkdf2_sha256"], deprecated="auto")


def connect(database=None):
    return pymysql.connect(
        host=MYSQL_HOST,
        port=int(os.getenv("TIMEWEAVER_TEST_MYSQL_PORT", "3306")),
        user=os.getenv("TIMEWEAVER_TEST_MYSQL_USER", "root"),
        password=os.getenv("TIMEWEAVER_TEST_MYSQL_PASSWORD", ""),
        database=database,
        charset="utf8mb4",
        autocommit=True,
        cursorclass=pymysql.cursors.DictCursor,
    )


@pytest.fixture
def empty_database():
    database = f"timeweaver_bootstrap_{uuid.uuid4().hex}"
    admin = connect()
    with admin.cursor() as cursor:
        cursor.execute(f"CREATE DATABASE `{database}` CHARACTER SET utf8mb4")
    try:
        yield database
    finally:
        with admin.cursor() as cursor:
            cursor.execute(f"DROP DATABASE IF EXISTS `{database}`")
        admin.close()


def run_server_migrator(database):
    wrapper = MySqlWrapper(
        host=MYSQL_HOST,
        port=int(os.getenv("TIMEWEAVER_TEST_MYSQL_PORT", "3306")),
        user=os.getenv("TIMEWEAVER_TEST_MYSQL_USER", "root"),
        password=os.getenv("TIMEWEAVER_TEST_MYSQL_PASSWORD", ""),
        db=database,
    )
    try:
        DatabaseMigrator(wrapper, MIGRATION_DIR, auto_run=True)
    finally:
        wrapper.close()


def apply_file(connection, path):
    sql = Path(path).read_text(encoding="utf-8")
    with connection.cursor() as cursor:
        for statement in _split_sql_statements(sql):
            cursor.execute(statement)


def table_definitions(connection):
    result = {}
    with connection.cursor() as cursor:
        for table in CORE_TABLES:
            cursor.execute(f"SHOW CREATE TABLE `{table}`")
            row = cursor.fetchone()
            create_sql = row["Create Table"]
            result[table] = re.sub(r"AUTO_INCREMENT=\d+\s*", "", create_sql)
    return result


def table_rows(connection):
    result = {}
    with connection.cursor() as cursor:
        for table in CORE_TABLES:
            cursor.execute(f"SELECT COUNT(*) AS count FROM `{table}`")
            result[table] = cursor.fetchone()["count"]
    return result


def seed_existing_core_data(connection):
    statements = (
        "INSERT INTO devices (device_id, device_name) VALUES (7, 'existing-device')",
        "INSERT INTO schedule_group (schedule_id, name, target_device) VALUES (12, 'existing-schedule', 7)",
        "INSERT INTO schedule_detail (detail_id, schedule_name, schedule_id) VALUES (42, 'existing-detail', 12)",
        "INSERT INTO task_detail (detail_id, command) VALUES (42, 'echo existing')",
        "INSERT INTO manual_execution (manual_id, detail_id, status) VALUES (5, UNHEX('00112233445566778899AABBCCDDEEFF'), 'wait')",
        "INSERT INTO execution_log (execution_grp_id, schedule_id, detail_id, start_time, result_code) VALUES (UNHEX('FFEEDDCCBBAA99887766554433221100'), 12, 42, NOW(), 0)",
    )
    with connection.cursor() as cursor:
        for statement in statements:
            cursor.execute(statement)


def authenticate_initial_admin(connection, username="timeweaver", password="timeweaver"):
    with connection.cursor() as cursor:
        cursor.execute(
            "SELECT group_id, user_id, name, password, role FROM users WHERE user_id = %s",
            (username,),
        )
        user = cursor.fetchone()
    if not user or not PASSWORD_CONTEXT.verify(password, user["password"]):
        return None
    return user


def test_empty_database_bootstrap_creates_core_schema_and_admin_login(empty_database):
    migration_files = sorted(MIGRATION_DIR.glob("*.sql"))
    assert tuple(path.name for path in migration_files[:6]) == EXPECTED_PREFIX

    run_server_migrator(empty_database)

    connection = connect(empty_database)
    try:
        with connection.cursor() as cursor:
            cursor.execute("SELECT filename FROM migrations ORDER BY filename")
            assert [row["filename"] for row in cursor.fetchall()] == [path.name for path in migration_files]

        with connection.cursor() as cursor:
            cursor.execute("SHOW TABLES")
            tables = {next(iter(row.values())) for row in cursor.fetchall()}
        assert set(CORE_TABLES + AGENT_TABLES) <= tables

        with connection.cursor() as cursor:
            cursor.execute(
                "SHOW INDEX FROM schedule_group "
                "WHERE Key_name = 'idx_schedule_group_001'"
            )
            index = cursor.fetchone()
            assert index is not None
            assert index["Column_name"] == "target_device"

            cursor.execute(
                """
                SELECT column_name, data_type, character_maximum_length
                  FROM information_schema.columns
                 WHERE table_schema = DATABASE()
                   AND table_name = 'devices'
                   AND column_name IN ('last_heartbeat_at', 'applied_revision')
                """
            )
            columns = {row["column_name"]: row for row in cursor.fetchall()}
            assert columns["last_heartbeat_at"]["data_type"] == "datetime"
            assert columns["applied_revision"]["data_type"] == "varchar"
            assert columns["applied_revision"]["character_maximum_length"] == 80

        user = authenticate_initial_admin(connection)
        assert user is not None
        assert user["user_id"] == "timeweaver"
        assert user["role"] == "admin"
    finally:
        connection.close()


def test_agent_runtime_repository_uses_device_scope_and_consistent_snapshot(empty_database):
    run_server_migrator(empty_database)
    connection = connect(empty_database)
    first_detail = "8F0D65C5B6A44BB0A2C5F23672FC9B76"
    other_detail = "1A749B4BBAD44FF3A3C449EB3DCDE0B4"
    try:
        with connection.cursor() as cursor:
            statements = (
                "INSERT INTO devices (device_id, device_name) VALUES (7, 'device-7')",
                "INSERT INTO devices (device_id, device_name) VALUES (8, 'device-8')",
                "INSERT INTO schedule_group "
                "(schedule_id, name, target_device, status) "
                "VALUES (12, 'owned', 7, 'active')",
                "INSERT INTO schedule_group "
                "(schedule_id, name, target_device, status) "
                "VALUES (13, 'other', 8, 'active')",
            )
            for statement in statements:
                cursor.execute(statement)
            for detail_id, schedule_id, name in (
                (first_detail, 12, "owned-task"),
                (other_detail, 13, "other-task"),
            ):
                cursor.execute(
                    """
                    INSERT INTO schedule_detail
                        (detail_id, schedule_name, schedule_id, sequence, status)
                    VALUES (UNHEX(%s), %s, %s, 10, 'active')
                    """,
                    (detail_id, name, schedule_id),
                )
                cursor.execute(
                    """
                    INSERT INTO task_detail
                        (detail_id, task_type, command, error_on_missing_source)
                    VALUES (UNHEX(%s), 'command', %s, TRUE)
                    """,
                    (detail_id, f"echo {name}"),
                )
            cursor.execute(
                """
                INSERT INTO manual_execution
                    (manual_id, detail_id, status, schedule_datetime)
                VALUES (41, UNHEX(%s), 'wait', '2026-08-01 06:00:00')
                """,
                (first_detail,),
            )

        wrapper = MySqlWrapper(
            host=MYSQL_HOST,
            port=int(os.getenv("TIMEWEAVER_TEST_MYSQL_PORT", "3306")),
            user=os.getenv("TIMEWEAVER_TEST_MYSQL_USER", "root"),
            password=os.getenv("TIMEWEAVER_TEST_MYSQL_PASSWORD", ""),
            db=empty_database,
        )
        try:
            repository = AgentRuntimeRepository(wrapper)
            heartbeat = repository.record_heartbeat(7, "agent-live", "sha256:" + "a" * 64)
            assert heartbeat.db_now is not None

            rows = repository.load_snapshot(7)
            assert rows.device["device_id"] == 7
            assert [row["schedule_id"] for row in rows.groups] == [12]
            assert len(rows.details) == 1
            assert rows.details[0]["schedule_id"] == 12
            assert [row["manual_id"] for row in rows.manuals] == [41]
        finally:
            wrapper.close()

        with connection.cursor() as cursor:
            cursor.execute(
                """
                SELECT version, applied_revision, last_heartbeat_at
                  FROM devices
                 WHERE device_id = 7
                """
            )
            device = cursor.fetchone()
            assert device["version"] == "agent-live"
            assert device["applied_revision"] == "sha256:" + "a" * 64
            assert device["last_heartbeat_at"] is not None
    finally:
        connection.close()


def test_agent_identity_migrations_are_noop_when_tables_already_exist(empty_database):
    connection = connect(empty_database)
    try:
        for path in CORE_FILES:
            apply_file(connection, path)
        for path in AGENT_FILES:
            apply_file(connection, path)

        with connection.cursor() as cursor:
            cursor.execute(
                """
                INSERT INTO agent_enrollment_token
                    (enrollment_id, token_hash, group_id, expires_at)
                VALUES (UNHEX(%s), UNHEX(SHA2(%s, 256)), 0, DATE_ADD(NOW(), INTERVAL 1 DAY))
                """,
                ("00112233445566778899AABBCCDDEEFF", "enrollment-secret"),
            )
            cursor.execute(
                """
                INSERT INTO agent_device_credential
                    (device_id, token_hash, expires_at)
                VALUES (7, UNHEX(SHA2(%s, 256)), DATE_ADD(NOW(), INTERVAL 90 DAY))
                """,
                ("refresh-secret",),
            )
            before = {}
            for table in AGENT_TABLES:
                cursor.execute(f"SHOW CREATE TABLE `{table}`")
                before[table] = cursor.fetchone()["Create Table"]
                cursor.execute(f"SELECT COUNT(*) AS count FROM `{table}`")
                before[f"{table}:rows"] = cursor.fetchone()["count"]

        for path in AGENT_FILES:
            apply_file(connection, path)

        with connection.cursor() as cursor:
            for table in AGENT_TABLES:
                cursor.execute(f"SHOW CREATE TABLE `{table}`")
                assert cursor.fetchone()["Create Table"] == before[table]
                cursor.execute(f"SELECT COUNT(*) AS count FROM `{table}`")
                assert cursor.fetchone()["count"] == before[f"{table}:rows"]
    finally:
        connection.close()


def test_agent_runtime_migrations_are_noop_when_schema_already_exists(empty_database):
    connection = connect(empty_database)
    try:
        for path in CORE_FILES:
            apply_file(connection, path)
        for path in RUNTIME_FILES:
            apply_file(connection, path)

        with connection.cursor() as cursor:
            cursor.execute(
                "INSERT INTO devices (device_id, device_name, applied_revision) "
                "VALUES (7, 'runtime-device', %s)",
                ("sha256:" + "a" * 64,),
            )
            cursor.execute("SHOW CREATE TABLE schedule_group")
            schedule_before = cursor.fetchone()["Create Table"]
            cursor.execute("SHOW CREATE TABLE devices")
            devices_before = cursor.fetchone()["Create Table"]

        for path in RUNTIME_FILES:
            apply_file(connection, path)

        with connection.cursor() as cursor:
            cursor.execute("SHOW CREATE TABLE schedule_group")
            assert cursor.fetchone()["Create Table"] == schedule_before
            cursor.execute("SHOW CREATE TABLE devices")
            assert cursor.fetchone()["Create Table"] == devices_before
            cursor.execute(
                "SELECT applied_revision FROM devices WHERE device_id = 7"
            )
            assert cursor.fetchone()["applied_revision"] == "sha256:" + "a" * 64
    finally:
        connection.close()


def test_core_bootstrap_is_noop_when_tables_already_exist(empty_database):
    connection = connect(empty_database)
    try:
        for path in CORE_FILES:
            apply_file(connection, path)

        seed_existing_core_data(connection)
        before_schema = table_definitions(connection)
        before_rows = table_rows(connection)

        for path in CORE_FILES:
            apply_file(connection, path)

        assert table_definitions(connection) == before_schema
        assert table_rows(connection) == before_rows
    finally:
        connection.close()
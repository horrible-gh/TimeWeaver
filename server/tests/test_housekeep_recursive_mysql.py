"""Live migration checks, using a dedicated temporary test database only."""
import os
from pathlib import Path

import pytest

from test_mysql_bootstrap import empty_database, connect, apply_file

pytestmark = pytest.mark.skipif(
    not os.getenv("TIMEWEAVER_TEST_MYSQL_HOST"), reason="live MySQL not configured"
)


def test_recursive_migration_preserves_defaults_and_replays_safely(empty_database):
    migration = Path(__file__).resolve().parents[1] / "res/sql/migration/mysql/timeweaver_server_015.sql"
    connection = connect(empty_database)
    try:
        with connection.cursor() as cursor:
            cursor.execute("CREATE TABLE task_detail (detail_id INT PRIMARY KEY, house_keep_days INT)")
            cursor.execute("INSERT INTO task_detail VALUES (1,30)")
            cursor.execute("CREATE TABLE devices(device_id INT PRIMARY KEY)")
            cursor.execute("INSERT INTO devices VALUES(1)")
        apply_file(connection, migration)
        with connection.cursor() as cursor:
            cursor.execute("SELECT house_keep_days,house_keep_recursive FROM task_detail WHERE detail_id=1")
            assert cursor.fetchone() == {"house_keep_days": 30, "house_keep_recursive": 0}
            cursor.execute("UPDATE task_detail SET house_keep_recursive=1 WHERE detail_id=1")
            cursor.execute("SELECT agent_capabilities FROM devices WHERE device_id=1")
            assert cursor.fetchone()["agent_capabilities"] == ""
        apply_file(connection, migration)
        with connection.cursor() as cursor:
            cursor.execute("SELECT house_keep_recursive FROM task_detail WHERE detail_id=1")
            assert cursor.fetchone()["house_keep_recursive"] == 1
    finally:
        connection.close()

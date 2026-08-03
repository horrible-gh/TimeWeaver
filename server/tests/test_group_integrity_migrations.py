import sqlite3
from pathlib import Path

import pytest


MIGRATION_ROOT = (
    Path(__file__).resolve().parents[1] / "res" / "sql" / "migration"
)


def test_sqlite_group_integrity_and_group_scoped_device_names():
    connection = sqlite3.connect(":memory:")
    try:
        for name in (
            "groups_001.sql",
            "groups_002.sql",
            "users_001.sql",
            "users_002.sql",
            "users_003_group_integrity.sql",
        ):
            connection.executescript(
                (MIGRATION_ROOT / "sqlite" / name).read_text(encoding="utf-8")
            )

        assert connection.execute(
            "SELECT group_id, group_name FROM groups WHERE group_id = 0"
        ).fetchone() == (0, "Unknown")
        assert connection.execute(
            "SELECT device_id, device_name FROM devices WHERE device_id = -1"
        ).fetchone() == (-1, "%")

        for table in ("devices", "users", "agent_enrollment_token", "schedule_group"):
            targets = {
                row[2]
                for row in connection.execute(
                    f"PRAGMA foreign_key_list({table})"
                ).fetchall()
            }
            assert "groups" in targets

        unique_indexes = connection.execute("PRAGMA index_list(devices)").fetchall()
        scoped = [
            row[1]
            for row in unique_indexes
            if row[2] and [
                column[2]
                for column in connection.execute(
                    f"PRAGMA index_info({row[1]})"
                ).fetchall()
            ] == ["group_id", "device_name"]
        ]
        assert scoped

        connection.execute(
            "INSERT INTO groups(group_id, group_name) VALUES (1, 'Operators')"
        )
        connection.execute(
            "INSERT INTO devices(group_id, device_name) VALUES (0, 'shared-name')"
        )
        connection.execute(
            "INSERT INTO devices(group_id, device_name) VALUES (1, 'shared-name')"
        )
        with pytest.raises(sqlite3.IntegrityError):
            connection.execute(
                "INSERT INTO devices(group_id, device_name) VALUES (1, 'shared-name')"
            )
        with pytest.raises(sqlite3.IntegrityError):
            connection.execute(
                "INSERT INTO devices(group_id, device_name) VALUES (999, 'orphan')"
            )
    finally:
        connection.close()


def test_mysql_integrity_migration_covers_all_group_owned_tables():
    groups_sql = (
        MIGRATION_ROOT / "mysql" / "groups_002.sql"
    ).read_text(encoding="utf-8")
    integrity_sql = (
        MIGRATION_ROOT / "mysql" / "users_004_group_integrity.sql"
    ).read_text(encoding="utf-8")

    assert "NO_AUTO_VALUE_ON_ZERO" in groups_sql
    assert "VALUES (0, 'Unknown')" in groups_sql
    assert "UNIQUE (group_id, device_name)" in integrity_sql
    for constraint in (
        "fk_devices_group",
        "fk_users_group",
        "fk_agent_enrollment_token_group",
        "fk_schedule_group_group",
    ):
        assert constraint in integrity_sql
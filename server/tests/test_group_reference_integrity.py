"""Regression tests for NR0007 (timeweaver.server.0007.0007-NR): rows whose
group_id outlives its groups.group_id row, and the fixes T0008 approved --
group-existence checks on the dashboard write paths, a reassign-then-delete
remove_group, and orphan reassignment in both group-integrity migrations.
"""
import asyncio
import sqlite3
from pathlib import Path

import pytest
from fastapi import HTTPException

from schemas.devices import DeviceInsertRequest
from schemas.schedules import ScheduleInsertRequest


MIGRATION_ROOT = Path(__file__).resolve().parents[1] / "res" / "sql" / "migration"


def test_insert_device_rejects_nonexistent_group(make_router_module):
    module, db = make_router_module("devices")
    db.missing_group_ids = {999}

    with pytest.raises(HTTPException) as raised:
        asyncio.run(module.insert_device(DeviceInsertRequest(
            group_id=999, device_name="orphan-device", status="active", creator="tester",
        )))

    assert raised.value.status_code == 422
    assert raised.value.detail["code"] == "group_not_found"
    assert db.execute_query_calls == []


def test_insert_device_allows_existing_group(make_router_module):
    module, db = make_router_module("devices")

    asyncio.run(module.insert_device(DeviceInsertRequest(
        group_id=5, device_name="edge-5", status="active", creator="tester",
    )))

    assert [params for _, params in db.execute_query_calls] == [
        (5, "edge-5", "active", "tester"),
    ]


def test_insert_schedule_rejects_nonexistent_group(make_router_module):
    module, db = make_router_module("schedule")
    db.missing_group_ids = {999}

    with pytest.raises(HTTPException) as raised:
        asyncio.run(module.insert_schedule(ScheduleInsertRequest(
            group_id=999, name="orphan-schedule", status="active", creator="tester",
        )))

    assert raised.value.status_code == 422
    assert raised.value.detail["code"] == "group_not_found"
    assert db.execute_query_calls == []


def test_insert_schedule_allows_existing_group(make_router_module):
    module, db = make_router_module("schedule")

    asyncio.run(module.insert_schedule(ScheduleInsertRequest(
        group_id=5, name="nightly", status="active", creator="tester",
    )))

    assert len(db.execute_query_calls) == 1
    assert db.execute_query_calls[0][1][0] == 5


def test_remove_group_reassigns_references_before_deleting(make_router_module):
    module, db = make_router_module("groups")

    asyncio.run(module.remove_group(7))

    assert len(db.transactions) == 1
    pending = db.transactions[0].pending
    statements = [query for query, _ in pending]
    assert all(params == (7,) for _, params in pending)
    assert statements[-1].startswith("DELETE FROM groups")
    assert any(q.startswith("UPDATE devices") for q in statements)
    assert any(q.startswith("UPDATE users") for q in statements)
    assert any(q.startswith("UPDATE agent_enrollment_token") for q in statements)
    assert any(q.startswith("UPDATE schedule_group") for q in statements)


def test_remove_group_refuses_to_delete_reserved_group_zero(make_router_module):
    module, db = make_router_module("groups")

    with pytest.raises(HTTPException) as raised:
        asyncio.run(module.remove_group(0))

    assert raised.value.status_code == 400
    assert raised.value.detail["code"] == "group_reserved"
    assert db.transactions == []


def test_mysql_integrity_migration_reassigns_orphans_before_each_fk():
    """Static companion to test_mysql_integrity_migration_covers_all_group_owned_tables:
    each ADD CONSTRAINT ... FOREIGN KEY must be preceded by the UPDATE that
    reassigns rows orphaned by insert_device/insert_schedule/remove_group
    (NR0007) to group 0, or the FK add fails with 1452 exactly as reported."""
    integrity_sql = (
        MIGRATION_ROOT / "mysql" / "users_004_group_integrity.sql"
    ).read_text(encoding="utf-8")

    for table, fk in (
        ("devices d", "fk_devices_group"),
        ("users u", "fk_users_group"),
        ("agent_enrollment_token t", "fk_agent_enrollment_token_group"),
        ("schedule_group s", "fk_schedule_group_group"),
    ):
        update_pos = integrity_sql.find(f"UPDATE {table}")
        fk_pos = integrity_sql.find(fk)
        assert update_pos != -1, f"missing orphan reassignment for {table}"
        assert fk_pos != -1, f"missing {fk}"
        assert update_pos < fk_pos, f"{table} reassignment must precede {fk}"


def test_sqlite_migration_reassigns_preexisting_orphans_instead_of_copying_them():
    """NR0007 section 3: PRAGMA foreign_keys=OFF let the pre-fix table rebuild
    copy orphan group_id values across without raising, unlike MySQL's 1452.
    This is the "before this migration runs" scenario the existing
    test_sqlite_group_integrity_and_group_scoped_device_names test didn't
    cover -- that test only inserts rows after the FK already exists."""
    connection = sqlite3.connect(":memory:")
    try:
        for name in ("groups_001.sql", "groups_002.sql", "users_001.sql", "users_002.sql"):
            connection.executescript(
                (MIGRATION_ROOT / "sqlite" / name).read_text(encoding="utf-8")
            )

        # Reproduce the pre-existing agent-owned devices/schedule_group schema
        # plus a legacy agent_enrollment_token table -- the same installations
        # users_003_group_integrity.sql's own CREATE TABLE IF NOT EXISTS is
        # written for. Column lists match those CREATE TABLE IF NOT EXISTS
        # blocks exactly: the migration's INSERT ... SELECT names every
        # column, so a trimmed-down table here would fail with "no such
        # column" instead of exercising the orphan-reassignment path.
        connection.executescript(
            """
            CREATE TABLE devices (
                group_id INTEGER NOT NULL DEFAULT 0,
                device_id INTEGER PRIMARY KEY AUTOINCREMENT,
                device_name TEXT NOT NULL UNIQUE,
                status TEXT CHECK (status IN ('active', 'inactive')) DEFAULT 'active',
                version TEXT DEFAULT NULL,
                creator TEXT DEFAULT NULL,
                created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
                modifier TEXT DEFAULT NULL,
                modified_at DATETIME DEFAULT NULL,
                last_login_at DATETIME DEFAULT CURRENT_TIMESTAMP,
                last_heartbeat_at DATETIME DEFAULT NULL,
                applied_revision TEXT DEFAULT NULL
            );
            CREATE TABLE schedule_group (
                group_id INTEGER NOT NULL DEFAULT 0,
                schedule_id INTEGER PRIMARY KEY AUTOINCREMENT,
                name TEXT NOT NULL,
                year TEXT DEFAULT '*',
                month TEXT DEFAULT '*',
                day_of_week TEXT DEFAULT '*',
                day TEXT DEFAULT '*',
                hour TEXT DEFAULT '*',
                minute TEXT DEFAULT '*',
                second TEXT DEFAULT '*',
                is_error_stop INTEGER DEFAULT 1,
                status TEXT DEFAULT 'active',
                target_device INTEGER DEFAULT 0,
                creator TEXT DEFAULT NULL,
                created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
                modifier TEXT DEFAULT NULL,
                modified_at DATETIME DEFAULT NULL
            );
            CREATE TABLE agent_enrollment_token (
                enrollment_id BLOB NOT NULL PRIMARY KEY,
                token_hash BLOB NOT NULL UNIQUE,
                device_name TEXT DEFAULT NULL,
                group_id INTEGER NOT NULL DEFAULT 0,
                created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
                expires_at DATETIME NOT NULL,
                used_at DATETIME DEFAULT NULL,
                used_by_device_id INTEGER DEFAULT NULL,
                revoked_at DATETIME DEFAULT NULL
            );
            """
        )
        # Orphan group_id values: exactly what insert_device/insert_schedule
        # allowed pre-fix, and what remove_group left behind pre-fix.
        connection.execute(
            "INSERT INTO devices(group_id, device_name) VALUES (999, 'orphan-device')"
        )
        connection.execute(
            "INSERT INTO schedule_group(group_id, name) VALUES (999, 'orphan-schedule')"
        )
        connection.execute(
            "INSERT INTO agent_enrollment_token"
            "(enrollment_id, token_hash, group_id, expires_at)"
            " VALUES (x'00', x'00', 999, '2030-01-01')"
        )
        connection.execute(
            "INSERT INTO users(group_id, user_id, name, password)"
            " VALUES (999, 'orphan-user', 'Orphan', 'x')"
        )
        connection.commit()

        connection.executescript(
            (MIGRATION_ROOT / "sqlite" / "users_003_group_integrity.sql").read_text(
                encoding="utf-8"
            )
        )

        assert connection.execute(
            "SELECT group_id FROM devices WHERE device_name = 'orphan-device'"
        ).fetchone() == (0,)
        assert connection.execute(
            "SELECT group_id FROM schedule_group WHERE name = 'orphan-schedule'"
        ).fetchone() == (0,)
        assert connection.execute(
            "SELECT group_id FROM agent_enrollment_token"
        ).fetchone() == (0,)
        assert connection.execute(
            "SELECT group_id FROM users WHERE user_id = 'orphan-user'"
        ).fetchone() == (0,)
    finally:
        connection.close()

def test_mysql_integrity_migration_recreates_group_zero_before_reassigning():
    """Regression for the rejection this TR fixes: groups_002.sql is recorded
    in the migrations table once applied and never runs again, so if an old,
    unguarded remove_group (pre-T0008) already deleted group 0 on some
    database, group 0 stays missing forever and every orphan reassignment
    below points at a group_id that itself does not exist -- the exact same
    1452 on fk_devices_group this migration exists to prevent, just against
    group 0 instead of the original orphaned value. This migration must
    recreate group 0 idempotently instead of assuming groups_002.sql's
    effects are still present."""
    integrity_sql = (
        MIGRATION_ROOT / "mysql" / "users_004_group_integrity.sql"
    ).read_text(encoding="utf-8")

    insert_pos = integrity_sql.find("INSERT IGNORE INTO groups(group_id, group_name) VALUES (0")
    first_update_pos = integrity_sql.find("UPDATE devices d")
    assert insert_pos != -1, "missing idempotent group 0 recreation"
    assert first_update_pos != -1
    assert insert_pos < first_update_pos, (
        "group 0 must be recreated before the first orphan reassignment"
    )


def test_sqlite_migration_recreates_group_zero_before_reassigning():
    """SQLite counterpart of the MySQL regression above: PRAGMA
    foreign_keys=OFF would let a rebuild silently copy rows pointing at a
    still-missing group 0 instead of raising (see NR0007 section 3), so this
    migration must not depend on groups_002.sql's effects still being
    present either."""
    integrity_sql = (
        MIGRATION_ROOT / "sqlite" / "users_003_group_integrity.sql"
    ).read_text(encoding="utf-8")

    insert_pos = integrity_sql.find("INSERT OR IGNORE INTO groups(group_id, group_name) VALUES (0")
    begin_pos = integrity_sql.find("BEGIN TRANSACTION")
    first_devices_update_pos = integrity_sql.find("UPDATE devices\n")
    assert insert_pos != -1, "missing idempotent group 0 recreation"
    assert begin_pos != -1
    assert insert_pos > begin_pos, "group 0 recreation must be inside the rebuild transaction"
    assert insert_pos < first_devices_update_pos, (
        "group 0 must be recreated before the first orphan reassignment"
    )


def test_sqlite_migration_reassigns_orphans_even_when_group_zero_was_deleted():
    """Live-executes the exact scenario the rejection's root cause implies:
    an old, unguarded remove_group already deleted group 0 on this database
    (groups_002.sql itself is recorded as applied and will not re-run to
    recreate it), and orphaned group_id values already exist in all four
    group-owned tables. Without recreating group 0 first, the reassignment
    below would just point every orphan at a group_id that still does not
    exist."""
    connection = sqlite3.connect(":memory:")
    try:
        # groups_001.sql only -- deliberately skip groups_002.sql to model a
        # database where group 0 was deleted after being created; the
        # migrations table would still record groups_002.sql as applied, so
        # it never runs again to restore it.
        for name in ("groups_001.sql", "users_001.sql", "users_002.sql"):
            connection.executescript(
                (MIGRATION_ROOT / "sqlite" / name).read_text(encoding="utf-8")
            )

        connection.executescript(
            """
            CREATE TABLE devices (
                group_id INTEGER NOT NULL DEFAULT 0,
                device_id INTEGER PRIMARY KEY AUTOINCREMENT,
                device_name TEXT NOT NULL UNIQUE,
                status TEXT CHECK (status IN ('active', 'inactive')) DEFAULT 'active',
                version TEXT DEFAULT NULL,
                creator TEXT DEFAULT NULL,
                created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
                modifier TEXT DEFAULT NULL,
                modified_at DATETIME DEFAULT NULL,
                last_login_at DATETIME DEFAULT CURRENT_TIMESTAMP,
                last_heartbeat_at DATETIME DEFAULT NULL,
                applied_revision TEXT DEFAULT NULL
            );
            CREATE TABLE schedule_group (
                group_id INTEGER NOT NULL DEFAULT 0,
                schedule_id INTEGER PRIMARY KEY AUTOINCREMENT,
                name TEXT NOT NULL,
                year TEXT DEFAULT '*',
                month TEXT DEFAULT '*',
                day_of_week TEXT DEFAULT '*',
                day TEXT DEFAULT '*',
                hour TEXT DEFAULT '*',
                minute TEXT DEFAULT '*',
                second TEXT DEFAULT '*',
                is_error_stop INTEGER DEFAULT 1,
                status TEXT DEFAULT 'active',
                target_device INTEGER DEFAULT 0,
                creator TEXT DEFAULT NULL,
                created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
                modifier TEXT DEFAULT NULL,
                modified_at DATETIME DEFAULT NULL
            );
            CREATE TABLE agent_enrollment_token (
                enrollment_id BLOB NOT NULL PRIMARY KEY,
                token_hash BLOB NOT NULL UNIQUE,
                device_name TEXT DEFAULT NULL,
                group_id INTEGER NOT NULL DEFAULT 0,
                created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
                expires_at DATETIME NOT NULL,
                used_at DATETIME DEFAULT NULL,
                used_by_device_id INTEGER DEFAULT NULL,
                revoked_at DATETIME DEFAULT NULL
            );
            """
        )
        connection.execute(
            "INSERT INTO devices(group_id, device_name) VALUES (999, 'orphan-device')"
        )
        connection.execute(
            "INSERT INTO schedule_group(group_id, name) VALUES (999, 'orphan-schedule')"
        )
        connection.execute(
            "INSERT INTO agent_enrollment_token"
            "(enrollment_id, token_hash, group_id, expires_at)"
            " VALUES (x'00', x'00', 999, '2030-01-01')"
        )
        connection.execute(
            "INSERT INTO users(group_id, user_id, name, password)"
            " VALUES (999, 'orphan-user', 'Orphan', 'x')"
        )
        connection.commit()

        connection.executescript(
            (MIGRATION_ROOT / "sqlite" / "users_003_group_integrity.sql").read_text(
                encoding="utf-8"
            )
        )

        assert connection.execute(
            "SELECT group_id, group_name FROM groups WHERE group_id = 0"
        ).fetchone() == (0, "Unknown")
        assert connection.execute(
            "SELECT group_id FROM devices WHERE device_name = 'orphan-device'"
        ).fetchone() == (0,)
        assert connection.execute(
            "SELECT group_id FROM schedule_group WHERE name = 'orphan-schedule'"
        ).fetchone() == (0,)
        assert connection.execute(
            "SELECT group_id FROM agent_enrollment_token"
        ).fetchone() == (0,)
        assert connection.execute(
            "SELECT group_id FROM users WHERE user_id = 'orphan-user'"
        ).fetchone() == (0,)
        assert connection.execute("PRAGMA foreign_key_check").fetchall() == []
    finally:
        connection.close()

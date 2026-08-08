import sqlite3
from datetime import datetime, timedelta, timezone
from pathlib import Path
from uuid import uuid4


SERVER_ROOT = Path(__file__).resolve().parents[1]
MIGRATION = SERVER_ROOT / "res" / "sql" / "migration" / "sqlite" / "timeweaver_server_014.sql"
TASKS_SQL = SERVER_ROOT / "res" / "sql" / "sqloader" / "sqlite" / "dashboard" / "charts" / "tasks.sql"


def _build_connection():
    connection = sqlite3.connect(":memory:")
    connection.row_factory = sqlite3.Row
    connection.executescript(
        """
        CREATE TABLE devices (
            device_id INTEGER PRIMARY KEY,
            last_login_at TEXT NOT NULL,
            last_heartbeat_at TEXT,
            status TEXT NOT NULL
        );
        CREATE TABLE schedule_group (
            schedule_id INTEGER PRIMARY KEY,
            target_device INTEGER NOT NULL,
            status TEXT NOT NULL
        );
        CREATE TABLE schedule_detail (
            schedule_id INTEGER NOT NULL,
            detail_id BLOB NOT NULL,
            status TEXT NOT NULL
        );
        CREATE TABLE execution_log (
            detail_id BLOB NOT NULL,
            result_code INTEGER,
            start_time TEXT,
            end_time TEXT
        );
        """
    )
    migration = MIGRATION.read_text(encoding="utf-8")
    connection.executescript(migration)
    connection.executescript(migration)
    return connection


def test_sqlite_running_migration_replays_and_task_chart_counts_all_states():
    connection = _build_connection()
    query = TASKS_SQL.read_text(encoding="utf-8")

    now = datetime.now(timezone.utc).replace(tzinfo=None, microsecond=0)
    details = [uuid4().bytes for _ in range(5)]
    connection.execute(
        "INSERT INTO devices VALUES (7, ?, ?, 'active')",
        (now, now),
    )
    connection.execute("INSERT INTO schedule_group VALUES (12, 7, 'active')")
    connection.executemany(
        "INSERT INTO schedule_detail VALUES (12, ?, 'active')",
        [(detail,) for detail in details],
    )
    # The running task also has successful history, proving running has priority.
    connection.executemany(
        "INSERT INTO execution_log VALUES (?, ?, ?, ?)",
        [
            (details[0], 0, now - timedelta(minutes=10), now - timedelta(minutes=9)),
            (details[1], 0, now - timedelta(minutes=8), now - timedelta(minutes=7)),
            (details[2], 5, now - timedelta(minutes=6), now - timedelta(minutes=5)),
        ],
    )
    connection.executemany(
        "INSERT INTO execution_running VALUES (12, ?, ?, 1, ?)",
        [
            (details[0], uuid4().bytes, now),
            # A crashed agent's old marker must age out to pending.
            (details[4], uuid4().bytes, now - timedelta(hours=2)),
        ],
    )

    counts = dict(connection.execute(query).fetchone())
    assert counts == {
        "pending_count": 2,
        "in_progress_count": 1,
        "completed_count": 1,
        "error_count": 1,
    }
    columns = {
        row[1]: row
        for row in connection.execute("PRAGMA table_info(execution_running)")
    }
    assert columns["schedule_id"][5] == 1
    assert columns["detail_id"][5] == 2


def test_sqlite_task_chart_counts_device_with_stale_login_but_recent_heartbeat():
    # An agent logs in once and then keeps its session alive with heartbeats only,
    # so last_login_at can be days old on a perfectly healthy device.
    connection = _build_connection()
    query = TASKS_SQL.read_text(encoding="utf-8")

    now = datetime.now(timezone.utc).replace(tzinfo=None, microsecond=0)
    stale_login = now - timedelta(days=3)
    detail = uuid4().bytes
    connection.execute(
        "INSERT INTO devices VALUES (7, ?, ?, 'active')",
        (stale_login, now),
    )
    connection.execute("INSERT INTO schedule_group VALUES (12, 7, 'active')")
    connection.execute(
        "INSERT INTO schedule_detail VALUES (12, ?, 'active')",
        (detail,),
    )

    counts = dict(connection.execute(query).fetchone())
    assert counts == {
        "pending_count": 1,
        "in_progress_count": 0,
        "completed_count": 0,
        "error_count": 0,
    }


def test_sqlite_task_chart_ignores_device_stale_on_both_login_and_heartbeat():
    connection = _build_connection()
    query = TASKS_SQL.read_text(encoding="utf-8")

    now = datetime.now(timezone.utc).replace(tzinfo=None, microsecond=0)
    stale = now - timedelta(days=3)
    detail = uuid4().bytes
    connection.execute(
        "INSERT INTO devices VALUES (7, ?, ?, 'active')",
        (stale, stale),
    )
    connection.execute("INSERT INTO schedule_group VALUES (12, 7, 'active')")
    connection.execute(
        "INSERT INTO schedule_detail VALUES (12, ?, 'active')",
        (detail,),
    )

    counts = dict(connection.execute(query).fetchone())
    assert counts == {
        "pending_count": 0,
        "in_progress_count": 0,
        "completed_count": 0,
        "error_count": 0,
    }


def test_sqlite_task_chart_counts_each_detail_once_despite_execution_log_fanout():
    # A frequently-run schedule can leave many execution_log rows for the same
    # detail_id inside the 24h window; only the most recent one should count.
    connection = _build_connection()
    query = TASKS_SQL.read_text(encoding="utf-8")

    now = datetime.now(timezone.utc).replace(tzinfo=None, microsecond=0)
    detail = uuid4().bytes
    connection.execute(
        "INSERT INTO devices VALUES (7, ?, ?, 'active')",
        (now, now),
    )
    connection.execute("INSERT INTO schedule_group VALUES (12, 7, 'active')")
    connection.execute(
        "INSERT INTO schedule_detail VALUES (12, ?, 'active')",
        (detail,),
    )
    connection.executemany(
        "INSERT INTO execution_log VALUES (?, ?, ?, ?)",
        [
            (detail, 0, now - timedelta(hours=3), now - timedelta(hours=3)),
            (detail, 0, now - timedelta(hours=2), now - timedelta(hours=2)),
            # Most recent run failed; that must be the only one counted.
            (detail, 5, now - timedelta(hours=1), now - timedelta(hours=1)),
        ],
    )

    counts = dict(connection.execute(query).fetchone())
    assert counts == {
        "pending_count": 0,
        "in_progress_count": 0,
        "completed_count": 0,
        "error_count": 1,
    }

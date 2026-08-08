import sqlite3
from datetime import datetime, timedelta, timezone
from pathlib import Path
from uuid import uuid4


SERVER_ROOT = Path(__file__).resolve().parents[1]
MIGRATION = SERVER_ROOT / "res" / "sql" / "migration" / "sqlite" / "timeweaver_server_014.sql"
TASKS_SQL = SERVER_ROOT / "res" / "sql" / "sqloader" / "sqlite" / "dashboard" / "charts" / "tasks.sql"


def test_sqlite_running_migration_replays_and_task_chart_counts_all_states():
    connection = sqlite3.connect(":memory:")
    connection.row_factory = sqlite3.Row
    migration = MIGRATION.read_text(encoding="utf-8")
    query = TASKS_SQL.read_text(encoding="utf-8")

    connection.executescript(
        """
        CREATE TABLE devices (
            device_id INTEGER PRIMARY KEY,
            last_login_at TEXT NOT NULL,
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
    connection.executescript(migration)
    connection.executescript(migration)

    now = datetime.now(timezone.utc).replace(tzinfo=None, microsecond=0)
    details = [uuid4().bytes for _ in range(5)]
    connection.execute(
        "INSERT INTO devices VALUES (7, ?, 'active')",
        (now,),
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
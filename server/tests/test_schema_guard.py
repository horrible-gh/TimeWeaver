"""Unit tests for the startup schema guard (B0001 follow-up, NR0009).

The guard exists because timeweaver_server_004.sql only runs when the
process boots against a database that has not recorded the filename yet.
These tests pin down: detection of the narrowed enum, the repair being a
pure widening, and the guard staying quiet when the schema is healthy.
"""
from contextlib import contextmanager

import pytest

from schema_guard import CRITICAL_ENUMS, ensure_critical_schema, parse_enum_members


class GuardDb:
    """fetch_all serves canned COLUMN_TYPE strings; execute records repairs."""

    def __init__(
        self,
        column_types,
        fail_execute=False,
        fail_fetch=False,
        hidden_group={"group_id": 0, "group_name": "Unknown"},
    ):
        self.column_types = column_types
        self.fail_execute = fail_execute
        self.fail_fetch = fail_fetch
        self.hidden_group = hidden_group
        self.executed = []

    def fetch_all(self, query, params=None):
        if self.fail_fetch:
            raise RuntimeError("simulated inspect failure")
        if "FROM groups" in query:
            return [self.hidden_group] if self.hidden_group else []
        column_type = self.column_types.get(tuple(params))
        if column_type is None:
            return []
        return [{"column_type": column_type}]

    def execute(self, query, params=None, commit=False):
        if self.fail_execute:
            raise RuntimeError("simulated ALTER failure")
        self.executed.append((query, commit))
        return {"rowcount": 0}

    @contextmanager
    def begin_transaction(self):
        db = self

        class Txn:
            def fetch_one(self, _query):
                return {"sql_mode": "STRICT_TRANS_TABLES"}

            def execute(self, query, params=None):
                if db.fail_execute:
                    raise RuntimeError("simulated hidden-group repair failure")
                db.executed.append((query, True))
                return 1

        yield Txn()


HEALTHY = {
    ("task_detail", "archive_type"): "enum('null','zip')",
    ("schedule_detail", "status"): "enum('active','inactive','error','manual')",
}

NARROWED = {
    ("task_detail", "archive_type"): "enum('zip')",
    ("schedule_detail", "status"): "enum('active','inactive','error')",
}


def test_parse_enum_members_basic():
    assert parse_enum_members("enum('null','zip')") == {"null", "zip"}
    assert parse_enum_members("ENUM('a', 'b', 'c')") == {"a", "b", "c"}


def test_parse_enum_members_bytes_and_escapes():
    assert parse_enum_members(b"enum('zip')") == {"zip"}
    assert parse_enum_members("enum('it''s')") == {"it's"}


def test_parse_enum_members_non_enum_returns_none():
    assert parse_enum_members("varchar(50)") is None
    assert parse_enum_members(None) is None
    assert parse_enum_members("") is None


def test_healthy_schema_triggers_no_repair():
    db = GuardDb(HEALTHY)
    assert ensure_critical_schema(db) == []
    assert db.executed == []


def test_narrowed_enums_are_repaired_with_the_004_statements():
    db = GuardDb(NARROWED)
    repaired = ensure_critical_schema(db)
    assert repaired == ["task_detail.archive_type", "schedule_detail.status"]
    statements = [q for q, _ in db.executed]
    assert any("enum('null','zip')" in q for q in statements)
    assert any("'manual'" in q for q in statements)
    # Every repair must be a widening MODIFY, never a DROP/DELETE/TRUNCATE.
    for q in statements:
        assert q.upper().startswith("ALTER TABLE ")
        assert "MODIFY COLUMN" in q
        for forbidden in ("DROP", "DELETE", "TRUNCATE"):
            assert forbidden not in q.upper()
    assert all(commit for _, commit in db.executed)


def test_missing_table_is_left_to_the_migrator():
    db = GuardDb({})
    assert ensure_critical_schema(db) == []
    assert db.executed == []


def test_missing_hidden_group_is_restored_with_zero_preserving_session_mode():
    db = GuardDb(HEALTHY, hidden_group=None)
    assert ensure_critical_schema(db) == ["groups.0"]
    statements = [query for query, _ in db.executed]
    assert any("NO_AUTO_VALUE_ON_ZERO" in query for query in statements)
    assert any("VALUES (0, 'Unknown')" in query for query in statements)


def test_failed_repair_is_reported_not_raised():
    db = GuardDb(NARROWED, fail_execute=True)
    assert ensure_critical_schema(db) == []


def test_failed_inspection_is_reported_not_raised():
    db = GuardDb(HEALTHY, fail_fetch=True)
    assert ensure_critical_schema(db) == []


def test_repair_statements_match_migration_004():
    """The guard must stay in lockstep with timeweaver_server_004.sql."""
    from pathlib import Path

    migration = (
        Path(__file__).resolve().parents[1]
        / "res" / "sql" / "migration" / "mysql" / "timeweaver_server_004.sql"
    ).read_text(encoding="utf-8")
    migration_statements = [
        " ".join(stmt.split())
        for stmt in migration.split(";")
        if stmt.strip() and not all(
            line.strip().startswith("--")
            for line in stmt.strip().splitlines()
            if line.strip()
        )
    ]
    for _, _, _, repair_sql in CRITICAL_ENUMS:
        normalized = " ".join(repair_sql.split())
        assert any(normalized in stmt or stmt.endswith(normalized)
                   for stmt in migration_statements), repair_sql

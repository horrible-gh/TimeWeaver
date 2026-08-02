import json
from pathlib import Path

from conftest import MIGRATION_DIR, PROJECT_ROOT, SERVER_ROOT


SQL_ROOT = SERVER_ROOT / "res" / "sql" / "sqloader" / "mysql"


def read(path):
    return Path(path).read_text(encoding="utf-8")


def test_soft_delete_migration_is_guarded_and_replayable():
    sql = read(MIGRATION_DIR / "timeweaver_server_013.sql")

    assert "information_schema.columns" in sql
    assert "column_name = 'deleted_at'" in sql
    assert "ALTER TABLE schedule_detail ADD COLUMN deleted_at DATETIME NULL" in sql
    assert "'SELECT 1'" in sql
    assert "DROP TABLE" not in sql.upper()
    assert "DELETE FROM" not in sql.upper()


def test_active_mysql_queries_exclude_schedule_detail_tombstones():
    paths = (
        SQL_ROOT / "tasks" / "get_tasks.sql",
        SQL_ROOT / "dashboard" / "lastest_schedules.sql",
        SQL_ROOT / "dashboard" / "charts" / "tasks.sql",
        SQL_ROOT / "manual_execution" / "insert_manual_execution.sql",
    )

    for path in paths:
        assert "deleted_at IS NULL" in read(path), path

    sqloader = json.loads(read(SQL_ROOT / "time_weaver.json"))
    assert "deleted_at=NOW()" in sqloader["tasks"]["remove_schedule_detail"]
    assert "DELETE FROM schedule_detail" not in sqloader["tasks"]["remove_schedule_detail"]
    assert "deleted_at IS NULL" in sqloader["tasks"]["update_schedule_detail"]
    assert "deleted_at IS NULL" in sqloader["manual_execution"]["get_manual_execution"]


def test_agent_active_reads_exclude_tombstones_but_result_history_does_not():
    runtime = read(SERVER_ROOT / "repositories" / "agent_runtime.py")
    execution = read(SERVER_ROOT / "repositories" / "agent_execution.py")

    assert runtime.count("AND sd.deleted_at IS NULL") == 2
    assert execution.count("deleted_at IS NULL") == 2
    accept_result = execution[execution.index("    def accept_result"):execution.index("    @staticmethod\n    def _find_result")]
    assert "deleted_at IS NULL" not in accept_result


def test_execution_history_keeps_soft_deleted_schedule_metadata():
    history = read(SQL_ROOT / "dashboard" / "execution_logs.sql")

    assert "JOIN schedule_detail sd" in history
    assert "LEFT JOIN task_detail td" in history
    assert "deleted_at IS NULL" not in history


def test_migration_011_remains_fail_closed_and_null_safe():
    """Only rows with no UUID identity left to restore may abort the startup."""
    sql = read(MIGRATION_DIR / "timeweaver_server_011.sql")

    assert "tw011_invalid_detail_run_scripts_diagnose_execution_log_orphans" in sql
    assert "@tw_unrepairable_detail_count > 0" in sql
    assert "duplicated.detail_id <=> e.detail_id" in sql
    assert "OR d.detail_id IS NULL" in sql
    assert "uq_execution_log_idempotency" in sql

    recount = sql[sql.index("SET @tw_unrepairable_detail_count"):]
    recount = recount[: recount.index("SET @tw_invalid_detail_abort_sql")]
    assert "OCTET_LENGTH(e.detail_id) <> 16" in recount
    assert "OR d.detail_id IS NULL" in recount


def test_migration_011_restores_orphan_details_as_tombstones():
    """The pre-soft-delete orphans repair themselves instead of blocking boot."""
    sql = read(MIGRATION_DIR / "timeweaver_server_011.sql")

    assert "CREATE TEMPORARY TABLE tw_schedule_detail_restore" in sql
    assert "schedule_detail_restore_log" in sql
    assert "ALTER TABLE schedule_detail ADD COLUMN deleted_at DATETIME NULL" in sql

    restore = sql[sql.index("'INSERT IGNORE INTO schedule_detail ("):]
    restore = restore[: restore.index("PREPARE tw_restore_insert_stmt")]
    # A restored row must be a tombstone, or a removed task would come back.
    assert "deleted_at" in restore
    assert "''inactive''" in restore

    # Only a real 16-byte UUID can be restored; the rest still fail closed.
    selection = sql[sql.index("CREATE TEMPORARY TABLE tw_schedule_detail_restore"):]
    selection = selection[: selection.index("PREPARE tw_restore_temp_stmt")]
    assert "OCTET_LENGTH(e.detail_id) = 16" in selection
    assert "e.detail_id IS NOT NULL" in selection


def test_migration_011_never_rewrites_execution_history():
    sql = read(MIGRATION_DIR / "timeweaver_server_011.sql")

    assert "DELETE FROM" not in sql.upper()
    assert "TRUNCATE" not in sql.upper()
    assert "SET e.detail_id" not in sql


def test_orphan_diagnostic_is_read_only():
    sql = read(PROJECT_ROOT / "scripts" / "diagnose_execution_log_orphans.sql")
    executable = "\n".join(
        line for line in sql.splitlines()
        if not line.lstrip().startswith("--")
    ).upper()

    assert "START TRANSACTION READ ONLY" in executable
    assert "EXECUTION_LOG_QUARANTINE" in executable
    assert "UPDATE " not in executable
    assert "DELETE " not in executable
    assert "INSERT " not in executable
    assert "REPLACE " not in executable
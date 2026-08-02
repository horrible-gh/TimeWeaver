"""Startup migration failure diagnostics remain fail-closed."""
import importlib
import json
import sys

import LogAssist.log as logger
import pytest
import sqloader.init as sqloader_init


def test_migration_failure_is_structured_and_system_exit_is_preserved(monkeypatch):
    original_config = sys.modules.pop("config", None)
    logged_errors = []

    def failing_database_init(_config):
        print(
            "Database Migration Failed.Failed to apply migration "
            "timeweaver_server_011.sql: "
            "(1062, \"Duplicate entry 'legacy-1' for key "
            "'uq_execution_log_idempotency'\")"
        )
        raise SystemExit(1)

    monkeypatch.setattr(sqloader_init, "database_init", failing_database_init)
    monkeypatch.setattr(logger, "error", logged_errors.append)
    for name, value in {
        "ALLOWED_ORIGIN": "http://localhost",
        "SECRET_KEY": "s" * 32,
        "AGENT_SECRET_KEY": "a" * 32,
        "CONTEXT": "/timeweaver",
        "DB_TYPE": "mysql",
        "DB_HOST": "database.test",
        "DB_PORT": "3306",
        "DB_USER": "timeweaver",
        "DB_PASSWORD": "not-logged",
        "DB_DATABASE": "timeweaver_test",
    }.items():
        monkeypatch.setenv(name, value)

    try:
        with pytest.raises(SystemExit) as error:
            importlib.import_module("config")
        assert error.value.code == 1
    finally:
        sys.modules.pop("config", None)
        if original_config is not None:
            sys.modules["config"] = original_config

    assert len(logged_errors) == 1
    prefix = "[database-migration-failure] "
    assert logged_errors[0].startswith(prefix)
    diagnostic = json.loads(logged_errors[0][len(prefix):])
    assert diagnostic == {
        "database": "timeweaver_test",
        "db_error": (
            "(1062, \"Duplicate entry 'legacy-1' for key "
            "'uq_execution_log_idempotency'\")"
        ),
        "event": "database_migration_failed",
        "exit_code": 1,
        "migration_file": "timeweaver_server_011.sql",
    }
    assert "not-logged" not in logged_errors[0]
    assert "guidance" not in diagnostic


def test_migration_011_invalid_detail_abort_carries_operator_guidance(monkeypatch):
    """011's intentional poison-column abort must not read as an unexplained bug."""
    original_config = sys.modules.pop("config", None)
    logged_errors = []

    def failing_database_init(_config):
        print(
            "Database Migration Failed.Failed to apply migration "
            "timeweaver_server_011.sql: "
            "(1054, \"Unknown column "
            "'tw011_invalid_detail_run_scripts_diagnose_execution_log_orphans' "
            "in 'SELECT'\")"
        )
        raise SystemExit(1)

    monkeypatch.setattr(sqloader_init, "database_init", failing_database_init)
    monkeypatch.setattr(logger, "error", logged_errors.append)
    for name, value in {
        "ALLOWED_ORIGIN": "http://localhost",
        "SECRET_KEY": "s" * 32,
        "AGENT_SECRET_KEY": "a" * 32,
        "CONTEXT": "/timeweaver",
        "DB_TYPE": "mysql",
        "DB_HOST": "database.test",
        "DB_PORT": "3306",
        "DB_USER": "timeweaver",
        "DB_PASSWORD": "not-logged",
        "DB_DATABASE": "timeweaver_test",
    }.items():
        monkeypatch.setenv(name, value)

    try:
        with pytest.raises(SystemExit) as error:
            importlib.import_module("config")
        assert error.value.code == 1
    finally:
        sys.modules.pop("config", None)
        if original_config is not None:
            sys.modules["config"] = original_config

    assert len(logged_errors) == 1
    prefix = "[database-migration-failure] "
    assert logged_errors[0].startswith(prefix)
    diagnostic = json.loads(logged_errors[0][len(prefix):])
    assert diagnostic["migration_file"] == "timeweaver_server_011.sql"
    assert "tw011_invalid_detail_run_scripts_diagnose_execution_log_orphans" in diagnostic["db_error"]
    assert "scripts/diagnose_execution_log_orphans.sql" in diagnostic["guidance"]
    assert "not a bug" in diagnostic["guidance"]

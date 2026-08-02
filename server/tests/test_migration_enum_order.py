"""Migration-ordering regression tests for B0001 (see NR0003 / TR0005).

The migrator replays files in plain ``sorted(glob)`` order, so filenames decide
global execution order across every table. task_detail_001.sql widened
archive_type to enum('null','zip'), but timeweaver_server_001.sql sorts *later*
and re-created the table from an older definition with enum('zip') - silently
undoing the widening and breaking "add task".

These tests replay the same sort the migrator uses and assert the final enum
state, so any future file that re-narrows a column fails here instead of in
production.
"""
import re

import pytest

from conftest import MIGRATION_DIR, PROJECT_ROOT

CLEANUP_SCRIPT = PROJECT_ROOT / "scripts" / "cleanup_orphan_schedule_detail.sql"

# Mirrors sqloader/migrator.py: sorted(glob(...)).
MIGRATION_FILES = sorted(MIGRATION_DIR.glob("*.sql"))

ENUM_MEMBERS = re.compile(r"enum\s*\(([^)]*)\)", re.IGNORECASE)
ALTER_MODIFY = re.compile(
    r"ALTER\s+TABLE\s+`?(\w+)`?\s+MODIFY\s+(?:COLUMN\s+)?`?(\w+)`?\s+(enum\s*\([^)]*\))",
    re.IGNORECASE,
)
CREATE_TABLE = re.compile(
    r"CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?`?(\w+)`?\s*\((.*?)\)\s*ENGINE",
    re.IGNORECASE | re.DOTALL,
)
COLUMN_ENUM = re.compile(r"`?(\w+)`?\s+(enum\s*\([^)]*\))", re.IGNORECASE)


def members(enum_sql):
    body = ENUM_MEMBERS.search(enum_sql).group(1)
    return {m.strip().strip("'\"") for m in body.split(",") if m.strip()}


def replay_enum_state():
    """Return {(table, column): members} after replaying files in sort order."""
    state = {}
    history = {}
    for path in MIGRATION_FILES:
        sql = path.read_text(encoding="utf-8")

        for table, body in CREATE_TABLE.findall(sql):
            for column, enum_sql in COLUMN_ENUM.findall(body):
                state[(table, column)] = members(enum_sql)
                history.setdefault((table, column), []).append((path.name, members(enum_sql)))

        for table, column, enum_sql in ALTER_MODIFY.findall(sql):
            state[(table, column)] = members(enum_sql)
            history.setdefault((table, column), []).append((path.name, members(enum_sql)))

    return state, history


FINAL_STATE, HISTORY = replay_enum_state()


class TestSortOrderRootCause:
    def test_reverting_migration_still_sorts_before_the_fix(self):
        names = [p.name for p in MIGRATION_FILES]
        assert names.index("timeweaver_server_004.sql") > names.index("timeweaver_server_001.sql")

    def test_the_revert_is_still_present(self):
        """If 001 is ever edited to stop narrowing these columns, 004 becomes
        redundant and this test should be revisited deliberately."""
        sql = (MIGRATION_DIR / "timeweaver_server_001.sql").read_text(encoding="utf-8")
        assert "enum('zip')" in sql.replace(" ", "")
        assert "enum('active','inactive','error')" in sql.replace(" ", "")


class TestFinalEnumState:
    def test_archive_type_accepts_null_and_zip(self):
        assert FINAL_STATE[("task_detail", "archive_type")] == {"null", "zip"}

    def test_schedule_detail_status_accepts_manual(self):
        assert "manual" in FINAL_STATE[("schedule_detail", "status")]

    def test_schedule_detail_status_full_set(self):
        assert FINAL_STATE[("schedule_detail", "status")] == {
            "active",
            "inactive",
            "error",
            "manual",
        }

    @pytest.mark.parametrize("key", sorted(HISTORY, key=str))
    def test_no_column_ends_narrower_than_an_earlier_migration(self, key):
        """The B0001 class of bug: an earlier file widens an enum and a
        later-sorted file quietly drops members back off."""
        union = set().union(*(m for _, m in HISTORY[key]))
        missing = union - FINAL_STATE[key]
        assert not missing, (
            f"{key[0]}.{key[1]} loses {sorted(missing)} after replaying migrations "
            f"in filename order; history={[(n, sorted(m)) for n, m in HISTORY[key]]}"
        )


class TestMigratorParsingHazards:
    """sqloader/migrator.py splits files on ';', so a semicolon inside a
    comment leaks its tail into the next statement. TR0005 section 6 hit
    exactly this and it silently skipped a CREATE while still running a DELETE."""

    @pytest.mark.parametrize("path", MIGRATION_FILES, ids=lambda p: p.name)
    def test_no_semicolon_inside_a_comment(self, path):
        offenders = [
            (i, line.strip())
            for i, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1)
            if line.strip().startswith("--") and ";" in line
        ]
        assert not offenders, f"semicolon inside comment in {path.name}: {offenders}"


class TestCleanupScriptIsNotAutoRun:
    """It deletes user-entered rows; the migrator would run it unattended at
    startup. TR0005 section 6 pulled it out of the migration directory for
    exactly that reason."""

    def test_script_exists(self):
        assert CLEANUP_SCRIPT.is_file()

    def test_script_is_outside_the_migration_directory(self):
        assert not (MIGRATION_DIR / CLEANUP_SCRIPT.name).exists()
        assert CLEANUP_SCRIPT.parent.name == "scripts"

    def test_backs_up_before_deleting(self):
        sql = CLEANUP_SCRIPT.read_text(encoding="utf-8")
        backup_at = sql.upper().find("CREATE TABLE IF NOT EXISTS SCHEDULE_DETAIL_ORPHAN_BAK")
        delete_at = sql.upper().find("DELETE SD FROM SCHEDULE_DETAIL")
        assert backup_at != -1 and delete_at != -1
        assert backup_at < delete_at, "backup must precede the delete"

    def test_deletes_only_orphans(self):
        sql = CLEANUP_SCRIPT.read_text(encoding="utf-8")
        delete_stmt = sql[sql.upper().find("DELETE SD FROM SCHEDULE_DETAIL") :]
        assert "td.detail_id IS NULL" in delete_stmt
        assert "sd.deleted_at IS NULL" in delete_stmt
        assert "NOT EXISTS" in delete_stmt
        assert "execution_log" in delete_stmt

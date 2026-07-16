"""Startup guard for the enum columns that task saves depend on (B0001).

``timeweaver_server_004.sql`` restores the enum members that an older
CREATE TABLE replayed over, but a migration only runs when the server
process boots against that database. If the process is never restarted,
runs from a checkout that lacks the file, or the ``migrations`` table
already lists the filename without the ALTER being in effect, the column
stays narrow and every task save fails with
``(1265, "Data truncated for column 'archive_type' at row 1")``.

This guard re-checks the live schema on every boot and re-applies the
widening when members are missing. The repair is non-destructive: adding
members to an enum never rewrites or deletes row data, and re-running it
after the migration converges on the same definition.
"""
import LogAssist.log as logger

# (table, column, required members, repair statement). Repair statements
# must stay in sync with timeweaver_server_004.sql and must only widen.
CRITICAL_ENUMS = [
    (
        "task_detail",
        "archive_type",
        {"null", "zip"},
        "ALTER TABLE task_detail MODIFY COLUMN archive_type "
        "enum('null','zip') DEFAULT NULL",
    ),
    (
        "schedule_detail",
        "status",
        {"active", "inactive", "error", "manual"},
        "ALTER TABLE schedule_detail MODIFY COLUMN status "
        "enum('active','inactive','error','manual') DEFAULT 'active'",
    ),
]


def parse_enum_members(column_type):
    """Parse ``enum('a','b')`` into ``{'a', 'b'}``; None for non-enum types."""
    if isinstance(column_type, bytes):
        column_type = column_type.decode("utf-8", errors="replace")
    if not isinstance(column_type, str):
        return None
    column_type = column_type.strip()
    if not column_type.lower().startswith("enum(") or not column_type.endswith(")"):
        return None
    members = set()
    for part in column_type[len("enum("):-1].split(","):
        part = part.strip()
        if len(part) >= 2 and part[0] == "'" and part[-1] == "'":
            members.add(part[1:-1].replace("''", "'"))
    return members


def ensure_critical_schema(db_instance):
    """Verify and repair CRITICAL_ENUMS; returns repaired 'table.column' names."""
    repaired = []
    for table, column, required, repair_sql in CRITICAL_ENUMS:
        try:
            rows = db_instance.fetch_all(
                "SELECT COLUMN_TYPE AS column_type FROM information_schema.COLUMNS"
                " WHERE TABLE_SCHEMA = DATABASE()"
                " AND TABLE_NAME = %s AND COLUMN_NAME = %s",
                (table, column),
            )
        except Exception as exc:
            logger.error(f"[schema-guard] could not inspect {table}.{column}: {exc}")
            continue
        if not rows:
            # The migrator owns table creation; a missing table is its problem.
            continue
        members = parse_enum_members(rows[0].get("column_type"))
        if members is None or required.issubset(members):
            continue
        missing = sorted(required - members)
        logger.error(
            f"[schema-guard] {table}.{column} is missing enum members {missing}"
            " (B0001 symptom: task saves fail with error 1265)."
            " Re-applying the widening from timeweaver_server_004.sql"
        )
        try:
            db_instance.execute(repair_sql, None, commit=True)
        except Exception as exc:
            logger.error(
                f"[schema-guard] FAILED to repair {table}.{column}: {exc}."
                f" Run this against the server database manually: {repair_sql}"
            )
            continue
        repaired.append(f"{table}.{column}")
        logger.info(f"[schema-guard] repaired {table}.{column}")
    return repaired

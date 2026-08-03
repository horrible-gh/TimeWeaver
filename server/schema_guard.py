"""Startup guards for schema invariants that dashboard and task saves depend on."""

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
    """Parse an enum definition into its members; return None for non-enums."""
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


def ensure_hidden_group(db_instance):
    """Ensure MariaDB contains the canonical hidden group (0, Unknown)."""
    try:
        rows = db_instance.fetch_all(
            "SELECT group_id, group_name FROM groups WHERE group_id = %s",
            (0,),
        )
    except Exception as exc:
        logger.error(f"[schema-guard] could not inspect hidden group 0: {exc}")
        return []

    if rows and rows[0].get("group_name") == "Unknown":
        return []

    try:
        with db_instance.begin_transaction() as txn:
            mode_row = txn.fetch_one("SELECT @@SESSION.sql_mode AS sql_mode")
            previous_mode = (mode_row or {}).get("sql_mode", "")
            txn.execute(
                "SET SESSION sql_mode = CONCAT_WS(',', "
                "NULLIF(@@SESSION.sql_mode, ''), 'NO_AUTO_VALUE_ON_ZERO')"
            )
            txn.execute(
                "INSERT INTO groups(group_id, group_name) VALUES (0, 'Unknown') "
                "ON DUPLICATE KEY UPDATE group_name = VALUES(group_name)"
            )
            txn.execute("SET SESSION sql_mode = %s", (previous_mode,))
    except Exception as exc:
        logger.error(
            "[schema-guard] FAILED to restore hidden group 0: "
            f"{exc}. Run groups_002.sql against the server database manually."
        )
        return []

    logger.info("[schema-guard] restored hidden group groups.0")
    return ["groups.0"]


def ensure_critical_schema(db_instance):
    """Verify and repair critical enums and the canonical hidden group."""
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
            continue
        members = parse_enum_members(rows[0].get("column_type"))
        if members is None or required.issubset(members):
            continue
        missing = sorted(required - members)
        logger.error(
            f"[schema-guard] {table}.{column} is missing enum members {missing}. "
            "Re-applying the widening from timeweaver_server_004.sql"
        )
        try:
            db_instance.execute(repair_sql, None, commit=True)
        except Exception as exc:
            logger.error(
                f"[schema-guard] FAILED to repair {table}.{column}: {exc}. "
                f"Run this against the server database manually: {repair_sql}"
            )
            continue
        repaired.append(f"{table}.{column}")
        logger.info(f"[schema-guard] repaired {table}.{column}")
    repaired.extend(ensure_hidden_group(db_instance))
    return repaired
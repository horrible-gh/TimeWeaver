"""Canonical task-field normalization shared by dashboard writes and snapshots."""

TASK_VALUE_FIELDS = (
    "command",
    "archive_type",
    "source_path",
    "destination_path",
    "date_format",
    "target_date_format",
    "destination_date_format",
    "house_keep_days",
)

FORBIDDEN_TASK_FIELDS = {
    "command": {"archive_type", "source_path", "destination_path", "house_keep_days"},
    "copy": {"command", "archive_type", "house_keep_days"},
    "archive": {"command", "house_keep_days"},
    "housekeep": {"command", "archive_type", "source_path"},
}


def normalize_task_row(row: dict) -> dict:
    """Return a copy with empty sentinels and task-inapplicable fields cleared."""

    normalized = dict(row)
    for field in TASK_VALUE_FIELDS:
        value = normalized.get(field)
        if isinstance(value, str) and value.strip().lower() in {"", "null"}:
            normalized[field] = None

    for field in FORBIDDEN_TASK_FIELDS.get(normalized.get("task_type"), set()):
        normalized[field] = None
    # Only housekeep tasks can opt in. Old rows/clients stay non-recursive.
    value = normalized.get("house_keep_recursive", False)
    if isinstance(value, str):
        value = value.strip().lower()
    normalized["house_keep_recursive"] = (
        normalized.get("task_type") == "housekeep"
        and value in (True, 1, "1", "true")
    )
    return normalized

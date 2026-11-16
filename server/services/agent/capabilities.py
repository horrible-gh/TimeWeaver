"""Optional agent features that must never silently degrade."""

HOUSEKEEP_RECURSIVE = "housekeep_recursive_v1"


def supports_recursive_housekeep(value: str | None) -> bool:
    return isinstance(value, str) and HOUSEKEEP_RECURSIVE in {
        item.strip() for item in value.split(",")
    }


def needs_recursive_housekeep(snapshot: dict) -> bool:
    return any(
        detail["task"].get("task_type") == "housekeep"
        and detail["task"].get("house_keep_recursive") is True
        for schedule in snapshot.get("schedules", [])
        for detail in schedule.get("details", [])
    )

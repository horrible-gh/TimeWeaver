"""Regression tests for B0001 - "cannot add task" (see NR0003 / TR0005).

Covers the router-side half of the fix:
  * insert_task / update_task write both rows in one transaction (no orphans)
  * to_bool stops bool("0") from turning every "No" into a "Yes"
  * target_date_format / destination_date_format survive the pydantic schema
"""
import asyncio

import pytest

from schemas.tasks import TaskInsertRequest, TaskUpdateRequest

INSERT_SCHEDULE_DETAIL = "INSERT INTO schedule_detail"
INSERT_TASK = "INSERT INTO task_detail"
UPDATE_SCHEDULE_DETAIL = "UPDATE schedule_detail"
UPDATE_TASK = "UPDATE task_detail"
REMOVE_TASK = "DELETE FROM task_detail"
SOFT_DELETE_SCHEDULE_DETAIL = "UPDATE schedule_detail SET deleted_at=NOW()"


def valid_insert_payload(**overrides):
    payload = {
        "schedule_id": 1,
        "task_name": "nightly-archive",
        "year": "*",
        "month": "*",
        "day_of_week": "*",
        "day": "*",
        "hour": "3",
        "minute": "0",
        "second": "0",
        "status": "active",
        "command": "ls",
        "task_type": "command",
        "archive_type": "null",
        "source_path": "/var/log",
        "destination_path": "/backup",
        "date_format": "%Y%m%d",
        "target_date_format": "%Y%m%d",
        "destination_date_format": "%Y%m",
        "house_keep_days": 30,
        "creator": "tester",
    }
    payload.update(overrides)
    return payload


def valid_update_payload(**overrides):
    payload = valid_insert_payload()
    payload.pop("creator", None)
    payload["detail_id"] = "3f2b1c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d"
    payload["modifier"] = "tester"
    payload.update(overrides)
    return payload


def statements(calls):
    return [q for q, _ in calls]


def find(calls, needle):
    for query, params in calls:
        if needle in query:
            return params
    return None


class TestToBool:
    """The add-task form posts "0"/"1" as strings; bool("0") is True."""

    @pytest.mark.parametrize(
        "value,expected",
        [
            ("0", False),
            ("1", True),
            ("false", False),
            ("true", True),
            ("no", False),
            ("yes", True),
            ("", False),
            (0, False),
            (1, True),
            (False, False),
            (True, True),
            ("FALSE", False),
            ("  0  ", False),
        ],
    )
    def test_coerces_form_values(self, make_tasks_module, value, expected):
        tasks_module, _ = make_tasks_module()
        assert tasks_module.to_bool(value) is expected

    def test_none_falls_back_to_default(self, make_tasks_module):
        tasks_module, _ = make_tasks_module()
        assert tasks_module.to_bool(None) is True
        assert tasks_module.to_bool(None, default=False) is False

    def test_plain_bool_would_have_been_wrong(self, make_tasks_module):
        """Pins the exact bug: bool("0") is True, to_bool("0") is False."""
        tasks_module, _ = make_tasks_module()
        assert bool("0") is True
        assert tasks_module.to_bool("0") is False


class TestSchemaCarriesDateFormats:
    """pydantic drops undeclared fields, so the router read None every time
    and update_task overwrote both columns with NULL on every save."""

    def test_insert_request_keeps_date_formats(self):
        row = TaskInsertRequest(**valid_insert_payload()).model_dump()
        assert row["target_date_format"] == "%Y%m%d"
        assert row["destination_date_format"] == "%Y%m"

    def test_update_request_keeps_date_formats(self):
        row = TaskUpdateRequest(**valid_update_payload()).model_dump()
        assert row["target_date_format"] == "%Y%m%d"
        assert row["destination_date_format"] == "%Y%m"

    def test_archive_type_null_string_survives(self):
        row = TaskInsertRequest(**valid_insert_payload(archive_type="null")).model_dump()
        assert row["archive_type"] == "null"


class TestTaskContractNormalization:
    @pytest.mark.parametrize(
        "payload,expected",
        [
            (
                {
                    "task_type": "command", "command": "echo ok",
                    "archive_type": "null", "source_path": "",
                    "destination_path": "/ignored", "house_keep_days": 10,
                },
                {
                    "command": "echo ok", "archive_type": None,
                    "source_path": None, "destination_path": None,
                    "house_keep_days": None,
                },
            ),
            (
                {
                    "task_type": "copy", "command": "null",
                    "archive_type": "zip", "source_path": "/source",
                    "destination_path": "/destination", "house_keep_days": 5,
                },
                {
                    "command": None, "archive_type": None,
                    "source_path": "/source", "destination_path": "/destination",
                    "house_keep_days": None,
                },
            ),
            (
                {
                    "task_type": "archive", "command": "ignored",
                    "archive_type": "zip", "source_path": "/source",
                    "destination_path": "/destination", "house_keep_days": 5,
                },
                {
                    "command": None, "archive_type": "zip",
                    "source_path": "/source", "destination_path": "/destination",
                    "house_keep_days": None,
                },
            ),
            (
                {
                    "task_type": "housekeep", "command": "",
                    "archive_type": "null", "source_path": "/ignored",
                    "destination_path": "/destination", "house_keep_days": 30,
                },
                {
                    "command": None, "archive_type": None,
                    "source_path": None, "destination_path": "/destination",
                    "house_keep_days": 30,
                },
            ),
        ],
    )
    def test_normalizes_all_task_types(self, make_tasks_module, payload, expected):
        tasks_module, _ = make_tasks_module()
        normalized = tasks_module._normalize_task_row(payload)
        for field, value in expected.items():
            assert normalized[field] == value

    @pytest.mark.parametrize("archive_type", [None, "", "null", "tar"])
    def test_archive_requires_zip(self, make_tasks_module, archive_type):
        tasks_module, _ = make_tasks_module()
        with pytest.raises(tasks_module.HTTPException) as exc:
            tasks_module._normalize_task_row(
                {"task_type": "archive", "archive_type": archive_type}
            )
        assert exc.value.status_code == 422
        assert exc.value.detail["code"] == "invalid_archive_type"


class TestInsertTaskTransaction:
    def test_both_rows_commit_together(self, make_tasks_module):
        tasks_module, db = make_tasks_module()
        task = TaskInsertRequest(**valid_insert_payload())

        asyncio.run(tasks_module.insert_task(task))

        assert len(db.transactions) == 1, "both inserts must share one transaction"
        committed = statements(db.committed)
        assert any(INSERT_SCHEDULE_DETAIL in q for q in committed)
        assert any(INSERT_TASK in q for q in committed)

    def test_does_not_use_autocommitting_execute_query(self, make_tasks_module):
        """execute_query opens its own connection and commits on its own - that
        is precisely the path that stranded schedule_detail rows."""
        tasks_module, db = make_tasks_module()

        asyncio.run(tasks_module.insert_task(TaskInsertRequest(**valid_insert_payload())))

        assert db.execute_query_calls == []

    def test_failed_task_insert_leaves_no_orphan(self, make_tasks_module):
        """B0001 itself: task_detail insert blows up on archive_type, and the
        already-committed schedule_detail row shows up as an empty task."""
        tasks_module, db = make_tasks_module(fail_on=INSERT_TASK)
        task = TaskInsertRequest(**valid_insert_payload())

        with pytest.raises(RuntimeError):
            asyncio.run(tasks_module.insert_task(task))

        assert db.committed == [], "nothing may commit when task_detail fails"
        assert any(INSERT_SCHEDULE_DETAIL in q for q in statements(db.rolled_back))

    def test_schedule_detail_and_task_share_one_detail_id(self, make_tasks_module):
        tasks_module, db = make_tasks_module()

        asyncio.run(tasks_module.insert_task(TaskInsertRequest(**valid_insert_payload())))

        schedule_params = find(db.committed, INSERT_SCHEDULE_DETAIL)
        task_params = find(db.committed, INSERT_TASK)
        assert schedule_params[0] == task_params[0]

    def test_command_sentinel_and_forbidden_fields_are_stored_as_null(
        self, make_tasks_module
    ):
        tasks_module, db = make_tasks_module()

        asyncio.run(
            tasks_module.insert_task(TaskInsertRequest(**valid_insert_payload()))
        )

        task_params = find(db.committed, INSERT_TASK)
        assert task_params[3] is None
        assert task_params[4] is None
        assert task_params[6] is None
        assert task_params[10] is None

    def test_error_on_missing_source_no_is_stored_false(self, make_tasks_module):
        tasks_module, db = make_tasks_module()
        task = TaskInsertRequest(**valid_insert_payload(error_on_missing_source="0"))

        asyncio.run(tasks_module.insert_task(task))

        task_params = find(db.committed, INSERT_TASK)
        assert task_params[5] is False

    def test_date_formats_reach_the_insert(self, make_tasks_module):
        tasks_module, db = make_tasks_module()

        asyncio.run(tasks_module.insert_task(TaskInsertRequest(**valid_insert_payload())))

        task_params = find(db.committed, INSERT_TASK)
        assert task_params[8] == "%Y%m%d"
        assert task_params[9] == "%Y%m"

    def test_placeholder_count_matches_bound_params(self, make_tasks_module):
        """Guards the column list against the tuple the router builds."""
        tasks_module, db = make_tasks_module()

        asyncio.run(tasks_module.insert_task(TaskInsertRequest(**valid_insert_payload())))

        for query, params in db.committed:
            assert query.count("%s") == len(params), query


class TestUpdateTaskTransaction:
    def test_both_updates_commit_together(self, make_tasks_module):
        tasks_module, db = make_tasks_module()

        asyncio.run(tasks_module.update_tasks(TaskUpdateRequest(**valid_update_payload())))

        assert len(db.transactions) == 1
        committed = statements(db.committed)
        assert any(UPDATE_SCHEDULE_DETAIL in q for q in committed)
        assert any(UPDATE_TASK in q for q in committed)
        assert db.execute_query_calls == []

    def test_failed_task_update_rolls_back_schedule_detail(self, make_tasks_module):
        tasks_module, db = make_tasks_module(fail_on=UPDATE_TASK)

        with pytest.raises(RuntimeError):
            asyncio.run(tasks_module.update_tasks(TaskUpdateRequest(**valid_update_payload())))

        assert db.committed == []

    def test_date_formats_are_not_nulled_out(self, make_tasks_module):
        """Before the schema fix these two arrived as None and every save wiped
        the stored values."""
        tasks_module, db = make_tasks_module()

        asyncio.run(tasks_module.update_tasks(TaskUpdateRequest(**valid_update_payload())))

        task_params = find(db.committed, UPDATE_TASK)
        assert task_params[7] == "%Y%m%d"
        assert task_params[8] == "%Y%m"


class TestRemoveTaskTransaction:
    def test_task_delete_and_detail_tombstone_commit_together(self, make_tasks_module):
        tasks_module, db = make_tasks_module()
        detail_id = valid_update_payload()["detail_id"]

        asyncio.run(tasks_module.remove_Task(detail_id))

        assert len(db.transactions) == 1
        committed = statements(db.committed)
        assert any(REMOVE_TASK in query for query in committed)
        assert any(SOFT_DELETE_SCHEDULE_DETAIL in query for query in committed)
        assert db.execute_query_calls == []

    def test_soft_delete_preserves_execution_history_mapping(self, make_tasks_module):
        tasks_module, db = make_tasks_module()
        detail_id = valid_update_payload()["detail_id"]

        asyncio.run(tasks_module.remove_Task(detail_id))

        detail_query = next(
            query for query in statements(db.committed)
            if SOFT_DELETE_SCHEDULE_DETAIL in query
        )
        assert detail_query.startswith("UPDATE schedule_detail")
        assert "deleted_at=NOW()" in detail_query
        assert "DELETE FROM schedule_detail" not in detail_query

    def test_failed_tombstone_rolls_back_task_delete(self, make_tasks_module):
        tasks_module, db = make_tasks_module(fail_on=SOFT_DELETE_SCHEDULE_DETAIL)
        detail_id = valid_update_payload()["detail_id"]

        with pytest.raises(RuntimeError):
            asyncio.run(tasks_module.remove_Task(detail_id))

        assert db.committed == []
        rolled_back = statements(db.rolled_back)
        assert any(REMOVE_TASK in query for query in rolled_back)
        assert any(SOFT_DELETE_SCHEDULE_DETAIL in query for query in rolled_back)

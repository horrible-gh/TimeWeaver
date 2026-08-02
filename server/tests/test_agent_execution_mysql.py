"""Live-MySQL coverage for T4 agent execution ownership."""
import os
import uuid
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timedelta
import pymysql
import pytest
from sqloader.mysql import MySqlWrapper

from conftest import MIGRATION_DIR
from repositories.agent_execution import (
    AgentExecutionRepository,
    ExecutionRepositoryError,
)
from test_mysql_bootstrap import (
    apply_file,
    connect,
    empty_database,
    run_server_migrator,
)


MYSQL_HOST = os.getenv("TIMEWEAVER_TEST_MYSQL_HOST")
pytestmark = pytest.mark.skipif(
    not MYSQL_HOST,
    reason="set TIMEWEAVER_TEST_MYSQL_HOST to run live MySQL execution tests",
)
EXECUTION_FILES = tuple(
    MIGRATION_DIR / f"timeweaver_server_{number:03d}.sql"
    for number in range(9, 13)
)
DETAIL_ID = uuid.UUID("8f0d65c5-b6a4-4bb0-a2c5-f23672fc9b76")


def wrapper(database):
    return MySqlWrapper(
        host=MYSQL_HOST,
        port=int(os.getenv("TIMEWEAVER_TEST_MYSQL_PORT", "3306")),
        user=os.getenv("TIMEWEAVER_TEST_MYSQL_USER", "root"),
        password=os.getenv("TIMEWEAVER_TEST_MYSQL_PASSWORD", ""),
        db=database,
        max_parallel_queries=20,
    )


def test_execution_schema_is_created_and_replay_is_noop(empty_database):
    run_server_migrator(empty_database)
    connection = connect(empty_database)
    try:
        with connection.cursor() as cursor:
            cursor.execute(
                """
                SELECT table_name, column_name, data_type, character_maximum_length
                  FROM information_schema.columns
                 WHERE table_schema = DATABASE()
                   AND (
                       (table_name = 'manual_execution' AND column_name IN ('claim_token', 'claim_expires_at'))
                       OR (table_name = 'execution_log' AND column_name IN ('attempt', 'manual_id'))
                   )
                """
            )
            columns = {(row["table_name"], row["column_name"]): row for row in cursor.fetchall()}
            assert columns[("manual_execution", "claim_token")]["character_maximum_length"] == 64
            assert columns[("manual_execution", "claim_expires_at")]["data_type"] == "datetime"
            assert columns[("execution_log", "attempt")]["data_type"] == "int"
            assert columns[("execution_log", "manual_id")]["data_type"] == "int"

            cursor.execute(
                "SHOW INDEX FROM execution_log WHERE Key_name = 'uq_execution_log_idempotency'"
            )
            assert [row["Column_name"] for row in cursor.fetchall()] == [
                "execution_grp_id",
                "detail_id",
                "attempt",
            ]
            cursor.execute("SHOW CREATE TABLE agent_event")
            before_event = cursor.fetchone()["Create Table"]
            cursor.execute("SHOW CREATE TABLE manual_execution")
            before_manual = cursor.fetchone()["Create Table"]
            cursor.execute("SHOW CREATE TABLE execution_log")
            before_log = cursor.fetchone()["Create Table"]

        for path in EXECUTION_FILES:
            apply_file(connection, path)

        with connection.cursor() as cursor:
            cursor.execute("SHOW CREATE TABLE agent_event")
            assert cursor.fetchone()["Create Table"] == before_event
            cursor.execute("SHOW CREATE TABLE manual_execution")
            assert cursor.fetchone()["Create Table"] == before_manual
            cursor.execute("SHOW CREATE TABLE execution_log")
            assert cursor.fetchone()["Create Table"] == before_log
    finally:
        connection.close()


def test_duplicate_preflight_repair_and_unique_retry(empty_database):
    run_server_migrator(empty_database)
    connection = connect(empty_database)
    duplicate_group_id = uuid.uuid4().hex
    untouched_group_id = uuid.uuid4().hex
    detail_id = DETAIL_ID.hex
    try:
        with connection.cursor() as cursor:
            cursor.execute("ALTER TABLE execution_log DROP INDEX uq_execution_log_idempotency")
            cursor.execute(
                """
                INSERT INTO schedule_detail
                    (detail_id, schedule_name, schedule_id)
                VALUES (UNHEX(%s), 'migration-repair-detail', 12)
                """,
                (detail_id,),
            )
            for result_code in (0, 1):
                cursor.execute(
                    """
                    INSERT INTO execution_log
                        (execution_grp_id, schedule_id, detail_id, attempt,
                         start_time, result_code)
                    VALUES (UNHEX(%s), 12, UNHEX(%s), 1, UTC_TIMESTAMP(), %s)
                    """,
                    (duplicate_group_id, detail_id, result_code),
                )
            cursor.execute(
                """
                INSERT INTO execution_log
                    (execution_grp_id, schedule_id, detail_id, attempt,
                     start_time, result_code)
                VALUES (UNHEX(%s), 12, UNHEX(%s), 7, UTC_TIMESTAMP(), 0)
                """,
                (untouched_group_id, detail_id),
            )

        apply_file(connection, EXECUTION_FILES[2])

        with connection.cursor() as cursor:
            cursor.execute(
                """
                SELECT attempt
                  FROM execution_log
                 WHERE execution_grp_id = UNHEX(%s)
                 ORDER BY execution_id
                """,
                (duplicate_group_id,),
            )
            assert [row["attempt"] for row in cursor.fetchall()] == [1, 2]
            cursor.execute(
                """
                SELECT attempt
                  FROM execution_log
                 WHERE execution_grp_id = UNHEX(%s)
                """,
                (untouched_group_id,),
            )
            assert cursor.fetchone()["attempt"] == 7
            cursor.execute(
                """
                SELECT old_attempt, new_attempt
                  FROM execution_log_attempt_repair_log
                 WHERE execution_grp_id = UNHEX(%s)
                 ORDER BY source_execution_id
                """,
                (duplicate_group_id,),
            )
            assert [
                (row["old_attempt"], row["new_attempt"])
                for row in cursor.fetchall()
            ] == [(1, 1), (1, 2)]
            cursor.execute(
                "SELECT COUNT(*) AS c FROM execution_log_attempt_repair_log"
            )
            repair_log_count = cursor.fetchone()["c"]
            assert repair_log_count == 2
            cursor.execute("SHOW CREATE TABLE execution_log")
            schema_before_replay = cursor.fetchone()["Create Table"]

        apply_file(connection, EXECUTION_FILES[2])

        with connection.cursor() as cursor:
            cursor.execute("SHOW CREATE TABLE execution_log")
            assert cursor.fetchone()["Create Table"] == schema_before_replay
            cursor.execute(
                "SELECT COUNT(*) AS c FROM execution_log_attempt_repair_log"
            )
            assert cursor.fetchone()["c"] == repair_log_count
            cursor.execute(
                """
                SELECT attempt
                  FROM execution_log
                 WHERE execution_grp_id = UNHEX(%s)
                 ORDER BY execution_id
                """,
                (duplicate_group_id,),
            )
            assert [row["attempt"] for row in cursor.fetchall()] == [1, 2]
    finally:
        connection.close()


def test_unmapped_detail_is_quarantined_and_migration_fails(empty_database):
    run_server_migrator(empty_database)
    connection = connect(empty_database)
    group_id = uuid.uuid4().hex
    sentinel_hex = "2D31" + ("00" * 14)
    try:
        with connection.cursor() as cursor:
            cursor.execute("ALTER TABLE execution_log DROP INDEX uq_execution_log_idempotency")
            cursor.execute(
                """
                INSERT INTO execution_log
                    (execution_grp_id, schedule_id, detail_id, attempt,
                     start_time, result_code)
                VALUES (
                    UNHEX(%s), 12, CAST('-1' AS BINARY(16)), 1,
                    UTC_TIMESTAMP(), 0
                )
                """,
                (group_id,),
            )
            execution_id = cursor.lastrowid

        with pytest.raises(pymysql.MySQLError) as error:
            apply_file(connection, EXECUTION_FILES[2])
        assert (
            "tw_migration_011_abort_invalid_detail_id_manual_repair_required"
            in str(error.value)
        )

        with connection.cursor() as cursor:
            cursor.execute(
                """
                SELECT HEX(detail_id) AS detail_hex
                  FROM execution_log
                 WHERE execution_id = %s
                """,
                (execution_id,),
            )
            assert cursor.fetchone()["detail_hex"] == sentinel_hex
            cursor.execute(
                """
                SELECT detail_id_hex, source_detail_data_type, quarantine_reason
                  FROM execution_log_quarantine
                 WHERE source_execution_id = %s
                """,
                (execution_id,),
            )
            quarantined = cursor.fetchone()
            assert quarantined["detail_id_hex"] == sentinel_hex
            assert quarantined["source_detail_data_type"] == "binary"
            assert "schedule_detail UUID mapping" in quarantined["quarantine_reason"]
            cursor.execute(
                """
                SELECT COUNT(*) AS c
                  FROM information_schema.statistics
                 WHERE table_schema = DATABASE()
                   AND table_name = 'execution_log'
                   AND index_name = 'uq_execution_log_idempotency'
                """
            )
            assert cursor.fetchone()["c"] == 0
    finally:
        connection.close()


def test_live_claim_sweep_and_result_concurrency(empty_database):
    run_server_migrator(empty_database)
    connection = connect(empty_database)
    try:
        with connection.cursor() as cursor:
            cursor.execute("INSERT INTO devices (device_id, device_name) VALUES (7, 'device-7')")
            cursor.execute(
                "INSERT INTO schedule_group (schedule_id, name, target_device) VALUES (12, 'owned', 7)"
            )
            cursor.execute(
                """
                INSERT INTO schedule_detail
                    (detail_id, schedule_name, schedule_id, is_error_stop, status)
                VALUES (UNHEX(%s), 'owned-detail', 12, TRUE, 'active')
                """,
                (DETAIL_ID.hex,),
            )
            cursor.execute(
                """
                INSERT INTO manual_execution
                    (manual_id, detail_id, status, schedule_datetime)
                VALUES (41, UNHEX(%s), 'wait', UTC_TIMESTAMP())
                """,
                (DETAIL_ID.hex,),
            )

        db_wrapper = wrapper(empty_database)
        repository = AgentExecutionRepository(db_wrapper)
        try:
            def claim(index):
                try:
                    return repository.claim_manual_run(7, 41, f"{index:064x}")
                except ExecutionRepositoryError as exc:
                    return exc.code

            with ThreadPoolExecutor(max_workers=20) as pool:
                claims = list(pool.map(claim, range(1, 21)))
            assert sum(not isinstance(item, str) for item in claims) == 1
            assert claims.count("already_claimed") == 19

            with connection.cursor() as cursor:
                cursor.execute(
                    "UPDATE manual_execution SET claim_expires_at = UTC_TIMESTAMP() - INTERVAL 1 SECOND WHERE manual_id = 41"
                )
            with ThreadPoolExecutor(max_workers=2) as pool:
                swept = list(pool.map(lambda _: repository.sweep_expired_claims(), range(2)))
            assert sorted(swept) == [0, 1]

            execution_group = uuid.uuid4()
            payload = {
                "execution_grp_id": execution_group.bytes,
                "schedule_id": 12,
                "detail_id": DETAIL_ID.bytes,
                "attempt": 1,
                "manual_id": None,
                "claim_token": None,
                "started_at": datetime(2026, 8, 1, 2, 0, 1),
                "finished_at": datetime(2026, 8, 1, 2, 0, 9),
                "result_code": 0,
                "result_message": None,
                "environment_info": '{"host":"device-7"}',
            }
            with ThreadPoolExecutor(max_workers=20) as pool:
                results = list(pool.map(lambda _: repository.accept_result(7, payload), range(20)))
            assert sum(not item.duplicate for item in results) == 1
            assert sum(item.duplicate for item in results) == 19
            assert len({item.execution_id for item in results}) == 1
            with connection.cursor() as cursor:
                cursor.execute(
                    """
                    SELECT COUNT(*) AS c
                      FROM execution_log
                     WHERE execution_grp_id = UNHEX(%s)
                       AND detail_id = UNHEX(%s)
                       AND attempt = 1
                    """,
                    (execution_group.hex, DETAIL_ID.hex),
                )
                assert cursor.fetchone()["c"] == 1
        finally:
            db_wrapper.close()
    finally:
        connection.close()
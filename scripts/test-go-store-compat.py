#!/usr/bin/env python3
"""Disposable MySQL comparison against sqloader 0.2.18; never uses DB_DATABASE.

Requires explicit TIMEWEAVER_TEST_MYSQL_ALLOW_CREATE=1. Both engines use the
same server and separate randomly named databases. Python uses the frozen
migration files; Go also applies the reviewed additive native migration.
No running TimeWeaver installation, credentials or production DB is accessed.
"""
import argparse
import json
import os
from pathlib import Path
import re
import secrets
import subprocess
import tempfile

import pymysql
from sqloader.migrator import DatabaseMigrator
from sqloader.mysql import MySqlWrapper

ROOT = Path(__file__).resolve().parents[1]
MIGRATIONS = ROOT / "server/res/sql/migration/mysql"


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("binary", type=Path)
    args = parser.parse_args()
    if os.environ.get("TIMEWEAVER_TEST_MYSQL_ALLOW_CREATE") != "1":
        raise SystemExit("This test requires explicit permission to create disposable databases")
    host = os.environ["TIMEWEAVER_TEST_MYSQL_HOST"]
    password = os.environ["TIMEWEAVER_TEST_MYSQL_PASSWORD"]
    connection_options = dict(host=host, port=3306, user="root", password=password,
                              charset="utf8mb4", autocommit=True,
                              cursorclass=pymysql.cursors.DictCursor)
    admin = pymysql.connect(**connection_options)
    with admin.cursor() as cursor:
        cursor.execute("SELECT VERSION() AS version")
        mariadb = "MariaDB" in cursor.fetchone()["version"]
    names = ["tw_compat_" + secrets.token_hex(10) for _ in range(2)]
    databases = []
    try:
        for name in names:
            with admin.cursor() as cursor:
                cursor.execute(f"CREATE DATABASE `{name}` CHARACTER SET utf8mb4")
            databases.append(name)
        with tempfile.TemporaryDirectory(prefix="tw-store-test-") as temp:
            def go(database, action="up"):
                envfile = Path(temp) / (database + ".env")
                envfile.write_text("# isolated CI test\n", encoding="utf-8")
                env = {**os.environ, "SECRET_KEY": "ci-only-" + secrets.token_hex(32),
                       "AGENT_SECRET_KEY": "", "CONTEXT": "/timeweaver_svr",
                       "DB_TYPE": "mysql", "DB_HOST": host, "DB_PORT": "3306",
                       "DB_USER": "root", "DB_PASSWORD": password, "DB_DATABASE": database}
                command = [str(args.binary.resolve()), "migrate", action, "--env", str(envfile)]
                if action == "up":
                    command.append("--allow-write")
                result = subprocess.run(command, env=env, capture_output=True, text=True,
                                        timeout=120, check=False)
                if result.returncode:
                    raise AssertionError("Go migration failed: " + result.stderr)
                return json.loads(result.stdout)

            def python_migrate(database):
                wrapper = MySqlWrapper(host=host, port=3306, user="root", password=password,
                                       db=database)
                try:
                    DatabaseMigrator(wrapper, MIGRATIONS, auto_run=True)
                finally:
                    wrapper.close()

            def snapshot(database, schema=False):
                with pymysql.connect(database=database, **connection_options) as connection:
                    with connection.cursor() as cursor:
                        cursor.execute("SHOW TABLES")
                        tables = sorted(next(iter(row.values())) for row in cursor.fetchall())
                        result = {}
                        for table in tables:
                            if table == "tw_migration_attempt":
                                continue
                            if schema:
                                cursor.execute(f"SHOW CREATE TABLE `{table}`")
                                result[table] = re.sub(r"AUTO_INCREMENT=\d+\s*", "",
                                                      cursor.fetchone()["Create Table"])
                            else:
                                cursor.execute(f"SELECT * FROM `{table}`")
                                result[table] = sorted(cursor.fetchall(), key=repr)
                        return result

            status = go(names[1], "status")
            assert not status["history_exists"] and status["pending"] == 32
            assert any(m["name"] == "timeweaver_server_016.sql" and m["state"] == "pending"
                       for m in status["migrations"])
            assert snapshot(names[1]) == {}, "read-only preflight changed empty database"
            if mariadb:
                python_migrate(names[0])
            else:
                # Do not silently 'fix' the reference or claim MySQL parity with
                # a Python bootstrap that does not work there. Keep that failure
                # as an explicit regression, separate from Go compatibility.
                try:
                    python_migrate(names[0])
                except Exception as exc:
                    message = str(exc)
                    assert "groups_001.sql" in message and "1064" in message, message
                    print("PASS: untouched Python bootstrap reproduces MySQL 8.4 groups syntax failure")
                else:
                    raise AssertionError("MySQL baseline changed; review the reserved-name workaround")
            assert go(names[1])["pending"] == 0
            if mariadb:
                left, right = snapshot(names[0], True), snapshot(names[1], True)
                # Native Go migrations add one explicit default-off column.
                # Compare every remaining byte of the frozen table definitions.
                ddl = right["task_detail"]
                ddl, count = re.subn(r"(?m)^  `house_keep_delete_empty_dirs` tinyint\(1\) NOT NULL DEFAULT (?:0|'0'),\n", "", ddl)
                assert count == 1, "Go housekeeping column missing or not default-off"
                right["task_detail"] = ddl
                mismatches = sorted(key for key in left.keys() | right.keys()
                                    if left.get(key) != right.get(key))
                assert not mismatches, "schema differs beyond the approved addition: " + ", ".join(mismatches)
                print("PASS: MariaDB frozen table definitions match apart from the default-off housekeeping column")
            else:
                tables = snapshot(names[1], True)
                required = {"groups", "users", "devices", "schedule_group", "schedule_detail",
                            "task_detail", "manual_execution", "execution_log",
                            "agent_enrollment_token", "agent_device_credential"}
                assert required <= tables.keys(), "Go bootstrap is missing required tables"
                assert "fk_users_group" in tables["users"]
                assert "fk_schedule_group_group" in tables["schedule_group"]
                print("PASS: MySQL Go bootstrap completes with quoted legacy groups identifier")
            preserved_database = names[0] if mariadb else names[1]

            # Seed representative data, including binary credential digests. Go
            # must not rerun the admin seed or rotate/replace any identity rows.
            with pymysql.connect(database=preserved_database, **connection_options) as connection:
                with connection.cursor() as cursor:
                    cursor.execute("UPDATE users SET name='custom-admin', password='preserve-this-hash' WHERE user_id='timeweaver'")
                    cursor.execute("INSERT INTO devices(device_id,device_name) VALUES(7,'retained-agent')")
                    cursor.execute("INSERT INTO schedule_group(schedule_id,name,target_device) VALUES(12,'retained-schedule',7)")
                    cursor.execute("INSERT INTO schedule_detail(detail_id,schedule_name,schedule_id) VALUES(UNHEX(%s),'retained-task',12)", ("11" * 16,))
                    cursor.execute("INSERT INTO task_detail(detail_id,command) VALUES(UNHEX(%s),'echo retained')", ("11" * 16,))
                    cursor.execute("INSERT INTO execution_log(execution_grp_id,schedule_id,detail_id,start_time,result_code) VALUES(UNHEX(%s),12,UNHEX(%s),UTC_TIMESTAMP(),0)", ("22" * 16, "11" * 16))
                    cursor.execute("INSERT INTO agent_enrollment_token(enrollment_id,token_hash,device_name,group_id,expires_at) VALUES(UNHEX(%s),UNHEX(%s),'retained-agent',0,UTC_TIMESTAMP()+INTERVAL 1 DAY)", ("33" * 16, "44" * 32))
                    cursor.execute("INSERT INTO agent_device_credential(device_id,token_hash,issued_at,expires_at) VALUES(7,UNHEX(%s),UTC_TIMESTAMP(),UTC_TIMESTAMP()+INTERVAL 90 DAY)", ("55" * 32,))
            before = snapshot(preserved_database)
            pending = go(preserved_database, "status")
            assert pending["pending"] == (1 if mariadb else 0)
            assert not pending["blocked"]
            assert go(preserved_database)["pending"] == 0
            after = snapshot(preserved_database)
            if mariadb:
                # The actual legacy DB lacked 016. Its only allowed changes
                # are the newly recorded migration and the default-off option.
                applied = [row for row in after["migrations"]
                           if row["filename"] == "timeweaver_server_016.sql"]
                assert len(applied) == 1
                after["migrations"].remove(applied[0])
                for row in after["task_detail"]:
                    assert row.pop("house_keep_delete_empty_dirs") == 0
                after["task_detail"].sort(key=repr)
                assert snapshot(names[0], True) == snapshot(names[1], True), "upgraded and fresh Go schemas differ"
            assert after == before, "Go changed pre-existing data or migration timestamps"
            print("PASS: additive upgrade preserves data, historical rows, hash and binary token digest")
            before = snapshot(names[1])
            python_migrate(names[1])
            assert snapshot(names[1]) == before, "Python replayed Go migrations"
            print("PASS: Python recognizes all Go-applied filenames without replay")
            assert go(names[1], "status")["pending"] == 0
    finally:
        for name in databases:
            with admin.cursor() as cursor:
                cursor.execute(f"DROP DATABASE `{name}`")
        admin.close()


if __name__ == "__main__":
    main()

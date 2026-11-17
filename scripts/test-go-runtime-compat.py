#!/usr/bin/env python3
"""Compare real Go HTTP snapshots with the frozen Python runtime on a CI-only DB.

Never imports the application's config.py or reads its .env/DB_DATABASE. The
optional validator binary uses the separately pinned Go agent's actual decoder.
"""
import copy
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import subprocess
import sys
import types
from urllib.error import HTTPError
from urllib.parse import urlparse
from urllib.request import HTTPRedirectHandler, Request, build_opener

import pymysql
from sqloader.mysql import MySqlWrapper


def main():
    if os.environ.get("TIMEWEAVER_TEST_MYSQL_ALLOW_CREATE") != "1":
        raise RuntimeError("Disposable database permission is required")
    base, database = sys.argv[1:]
    target = urlparse(base)
    if target.hostname not in ("127.0.0.1", "::1") or target.scheme != "http":
        raise RuntimeError("Test HTTP server must be loopback")
    if not re.fullmatch(r"tw_identity_[a-f0-9]{24}", database):
        raise RuntimeError("Refusing non-test database")
    host = os.environ["TIMEWEAVER_TEST_MYSQL_HOST"]
    if host not in ("127.0.0.1", "::1"):
        raise RuntimeError("Test database must be loopback")
    password = os.environ["TIMEWEAVER_TEST_MYSQL_PASSWORD"]
    config = types.ModuleType("config")
    config.settings = types.SimpleNamespace(SECRET_KEY=os.environ["TW_INTEROP_SECRET"], AGENT_SECRET_KEY="")
    config.db = types.SimpleNamespace(db_instance=None)
    sys.modules["config"] = config
    sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "server"))
    from repositories.agent_identity import AgentIdentityRepository
    from repositories.agent_runtime import AgentRuntimeRepository
    from schemas.agent_runtime import HeartbeatRequest, SnapshotEnvelope
    from services.agent.identity_service import AgentIdentityService
    from services.agent.runtime_service import AgentRuntimeService, canonical_snapshot_bytes

    wrapper = MySqlWrapper(host=host, port=3306, user="root", password=password, db=database)
    identity = AgentIdentityService(AgentIdentityRepository(wrapper))
    runtime = AgentRuntimeService(AgentRuntimeRepository(wrapper))
    db = pymysql.connect(host=host, port=3306, user="root", password=password,
                         database=database, charset="utf8mb4", autocommit=True)

    class NoRedirect(HTTPRedirectHandler):
        def redirect_request(self, *args):
            return None

    opener = build_opener(NoRedirect())
    prefix = "/timeweaver_svr/api/agent/v1"
    validator = os.environ.get("TW_AGENT_CONTRACT_VALIDATOR")
    if validator and not Path(validator).is_file():
        raise RuntimeError("Pinned Go-agent contract validator is missing")

    def request(path, body=None, token=None, headers=None, expected=200):
        values = {"Content-Type": "application/json", **(headers or {})}
        if token:
            values["Authorization"] = "Bearer " + token
        req = Request(base + prefix + path, headers=values,
                      data=None if body is None else json.dumps(body).encode())
        try:
            response = opener.open(req, timeout=10)
        except HTTPError as exc:
            response = exc
        with response:
            assert response.status == expected, (path, response.status, expected)
            assert not response.headers.get("Location"), "Unexpected redirect"
            raw = response.read()
            if expected == 304:
                assert not raw, "304 contains a body"
                return None, response.headers
            return json.loads(raw), response.headers

    def sql(query, params=()):
        with db.cursor() as cursor:
            cursor.execute(query, params)
            return cursor.fetchall()

    try:
        enrolled = []
        for name in ("runtime-한글-😀", "other-device"):
            token = "enr_" + secrets.token_hex(32)
            sql("""INSERT INTO agent_enrollment_token
                (enrollment_id,token_hash,device_name,group_id,created_at,expires_at)
                VALUES (%s,%s,%s,0,UTC_TIMESTAMP(),UTC_TIMESTAMP()+INTERVAL 1 DAY)""",
                (secrets.token_bytes(16), hashlib.sha256(token.encode()).digest(), name))
            data, _ = request("/enroll", {"enrollment_token": token, "device_name": name, "agent_version": "runtime-v1"})
            enrolled.append(data["data"])
        current, other = enrolled
        principal = identity.verify_access(current["access_token"])

        def compare(capability="housekeep_recursive_v1"):
            actual, headers = request("/snapshot", token=current["access_token"],
                                      headers={"X-TW-Agent-Capabilities": capability})
            reference = runtime.snapshot(principal)
            expected = SnapshotEnvelope(server_time=reference.generated_at, data=reference.data).model_dump(mode="json")
            # Read times may cross a second; all business fields and the entire
            # revision/ETag remain compared, with no other exclusions.
            assert actual["schema_version"] == expected["schema_version"] == "1"
            assert actual["server_time"] == actual["data"]["generated_at"]
            got_data = {k: v for k, v in actual["data"].items() if k != "generated_at"}
            want_data = {k: v for k, v in expected["data"].items() if k != "generated_at"}
            assert got_data == want_data, "Go/Python snapshot data differs"
            assert headers["ETag"] == reference.etag
            digest = hashlib.sha256(canonical_snapshot_bytes(reference.data)).hexdigest()
            assert actual["data"]["revision"] == "sha256:" + digest
            if validator:
                result = subprocess.run([validator], input=json.dumps({
                    "envelope": actual, "etag": headers["ETag"],
                    "identity": {"device_id": current["device_id"], "device_name": current["device_name"]},
                }), text=True, capture_output=True, timeout=10)
                assert result.returncode == 0, "Actual Go-agent decoder rejected snapshot: " + result.stderr
            return actual, headers["ETag"]

        empty, empty_etag = compare()
        assert empty["data"]["schedules"] == [] and empty["data"]["manual_runs"] == []
        for sid, name, device, status in (
            (12, "group <>& 한글 日本語 😀 \u2028 \u2029 \\u2028", current["device_id"], "active"),
            (2, "empty-group", current["device_id"], "active"),
            (99, "other-group", other["device_id"], "active"),
            (98, "inactive-group", current["device_id"], "inactive"),
        ):
            sql("INSERT INTO schedule_group(schedule_id,name,target_device,status,year,day_of_week) VALUES(%s,%s,%s,%s,NULL,'mon-fri')", (sid, name, device, status))

        def detail(n, kind, sequence, schedule=12, status="active", deleted=None):
            uid = n.to_bytes(16, "big")
            sql("""INSERT INTO schedule_detail(detail_id,schedule_id,schedule_name,sequence,status,deleted_at,retry_count,year,is_error_stop)
                VALUES(%s,%s,%s,%s,%s,%s,NULL,NULL,0)""", (uid, schedule, "task-" + str(n), sequence, status, deleted))
            sql("""INSERT INTO task_detail(detail_id,task_type,command,archive_type,source_path,destination_path,
                date_format,target_date_format,destination_date_format,house_keep_days,house_keep_recursive,error_on_missing_source)
                VALUES(%s,%s,%s,'zip','/tmp/source','/tmp/target',' null ','','%%Y',7,1,1)""",
                (uid, kind, "echo 한글 日本語 😀 <>& \" \\ \\u2028 \u2028 \u2029\n\t"))
            return uid

        # Deliberately insert out of order. Two details share a sequence, and
        # all task types contain irrelevant legacy values requiring normalization.
        detail(3, "housekeep", 20)
        detail(2, "copy", 10)
        first = detail(1, "command", 10)
        detail(4, "archive", 30)
        detail(5, "command", 40, status="inactive")
        detail(6, "command", 50, deleted="2020-01-01 00:00:00")
        detail(7, "command", 1, schedule=99)
        detail(8, "command", 1, schedule=98)
        for mid, state, expiry, date, uid in (
            (4, "processing", None, "2001-01-01 00:00:00", first),
            (3, "processing", "2099-01-01 00:00:00", "2001-01-01 00:00:00", first),
            (2, "processing", "2000-01-01 00:00:00", "2001-01-01 00:00:00", first),
            (1, "wait", None, "2002-01-01 00:00:00", first),
            (5, "done", None, "2001-01-01 00:00:00", first),
            (6, "wait", None, "2001-01-01 00:00:00", (7).to_bytes(16, "big")),
            (7, "wait", None, "2001-01-01 00:00:00", (6).to_bytes(16, "big")),
        ):
            sql("INSERT INTO manual_execution(manual_id,detail_id,status,claim_expires_at,schedule_datetime) VALUES(%s,%s,%s,%s,%s)", (mid, uid, state, expiry, date))
        actual, etag = compare()
        assert etag != empty_etag
        assert [v["schedule_id"] for v in actual["data"]["schedules"]] == [2, 12]
        details = actual["data"]["schedules"][1]["details"]
        assert [v["exec_sequence"] for v in details] == [1, 1, 2, 3]
        assert [v["manual_id"] for v in actual["data"]["manual_runs"]] == [2, 3, 4, 1]
        assert [v["claimable"] for v in actual["data"]["manual_runs"]] == [True, False, False, True]
        for cap in (None, "housekeep_recursive_v10"):
            data, _ = request("/snapshot", token=current["access_token"],
                              headers={"If-None-Match": etag, "X-TW-Agent-Capabilities": cap or ""}, expected=409)
            assert data["detail"]["code"] == "agent_upgrade_required"
        request("/snapshot/", token=current["access_token"], headers={"If-None-Match": etag, "X-TW-Agent-Capabilities": "housekeep_recursive_v1"}, expected=304)
        # Heartbeat metadata must not change the content hash. Agent version must.
        for state in ("BOOTSTRAP", "HEALTHY", "DEGRADED", "HALTED"):
            body = {"agent_version": "runtime-v1", "state": state, "applied_revision": actual["data"]["revision"]}
            data, _ = request("/heartbeat", body, current["access_token"], {"X-TW-Agent-Capabilities": "housekeep_recursive_v1"})
            assert data["data"]["device_id"] == current["device_id"]
            reference = runtime.heartbeat(principal, HeartbeatRequest(**body), "housekeep_recursive_v1")
            assert reference["device_id"] == data["data"]["device_id"] and reference["device_status"] == data["data"]["device_status"]
        assert compare()[1] == etag
        sql("UPDATE devices SET version='runtime-v2' WHERE device_id=%s", (current["device_id"],))
        assert compare()[1] != etag
        # Numeric nullable fields and Unicode remain identical after changes.
        sql("UPDATE task_detail SET command='echo changed' WHERE detail_id=%s", (first,))
        changed, changed_etag = compare()
        assert changed_etag != etag
        sql("UPDATE manual_execution SET claim_expires_at='2000-01-01 00:00:00' WHERE manual_id=3")
        assert compare()[1] != changed_etag
        # Deliberate safety correction: the pinned agent dispatches every
        # claimable row without examining schedule_datetime. The frozen Python
        # marks a future reservation claimable. Go must differ only in that
        # field and its derived revision/ETag; the entire other payload remains
        # compared. Due and immediate fixtures above/below keep exact parity.
        sql("UPDATE manual_execution SET schedule_datetime='2099-01-01 00:00:00' WHERE manual_id=1")
        future, future_headers = request("/snapshot", token=current["access_token"],
                                         headers={"X-TW-Agent-Capabilities": "housekeep_recursive_v1"})
        reference = runtime.snapshot(principal)
        corrected = copy.deepcopy(reference.data)
        reserved = [m for m in corrected["manual_runs"] if m["manual_id"] == 1]
        assert len(reserved) == 1 and reserved[0]["status"] == "wait" and not reserved[0]["is_immediate"]
        assert reserved[0]["claimable"] is True, "Frozen reference behavior unexpectedly changed"
        reserved[0]["claimable"] = False
        future_digest = hashlib.sha256(canonical_snapshot_bytes(corrected)).hexdigest()
        corrected["revision"] = "sha256:" + future_digest
        expected = SnapshotEnvelope(server_time=reference.generated_at, data=corrected).model_dump(mode="json")
        assert future["schema_version"] == expected["schema_version"] == "1"
        assert future["server_time"] == future["data"]["generated_at"]
        assert {k: v for k, v in future["data"].items() if k != "generated_at"} == {
            k: v for k, v in expected["data"].items() if k != "generated_at"
        }, "Future reservation changed unrelated snapshot fields"
        assert future_headers["ETag"] == 'W/"' + future_digest[:16] + '"'
        if validator:
            decoded = subprocess.run([validator], input=json.dumps({
                "envelope": future, "etag": future_headers["ETag"],
                "identity": {"device_id": current["device_id"], "device_name": current["device_name"]},
            }), text=True, capture_output=True, timeout=10)
            assert decoded.returncode == 0, "Pinned agent rejected due-gated snapshot: " + decoded.stderr
        # Immediate=true still dispatches despite a stale/future stored date,
        # preserving the existing meaning and full reference equality.
        sql("UPDATE manual_execution SET is_immediate=1 WHERE manual_id=1")
        immediate, _ = compare()
        assert next(m for m in immediate["data"]["manual_runs"] if m["manual_id"] == 1)["claimable"] is True
        # Removing the task half of a visible detail fails the WHOLE snapshot.
        sql("DELETE FROM task_detail WHERE detail_id=%s", (first,))
        data, _ = request("/snapshot", token=current["access_token"], expected=500)
        assert data["detail"]["code"] == "server_error"
        sql("UPDATE devices SET status='inactive' WHERE device_id=%s", (current["device_id"],))
        data, _ = request("/snapshot", token=current["access_token"], headers={"If-None-Match": changed_etag}, expected=403)
        assert data["detail"]["code"] == "device_revoked"
        print("PASS: frozen Python/Go heartbeat, full due/immediate snapshot data, Unicode canonical hash, ETag, device scope, task normalization, dense ranks and lease visibility")
        print("PASS: future-reservation safety difference limited to claimable and derived revision/ETag; all other snapshot fields compared")
        if validator:
            print("PASS: actual pinned Go-agent DecodeSnapshot accepted all successful snapshot fixtures")
    finally:
        db.close()
        wrapper.close()


if __name__ == "__main__":
    main()

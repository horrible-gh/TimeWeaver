#!/usr/bin/env python3
"""Cross-deliver execution requests through frozen Python and real Go HTTP.

Only the explicitly authorized disposable loopback DB created by the Go test is
used. No application's config.py/.env, real agent command or production DB is read.
"""
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import sys
import types
import uuid
from urllib.error import HTTPError
from urllib.parse import urlparse
from urllib.request import HTTPRedirectHandler, Request, build_opener

import pymysql
from sqloader.mysql import MySqlWrapper


def main():
    if os.environ.get("TIMEWEAVER_TEST_MYSQL_ALLOW_CREATE") != "1":
        raise RuntimeError("Explicit disposable database permission required")
    base, database = sys.argv[1:]
    target = urlparse(base)
    if target.hostname not in ("127.0.0.1", "::1") or target.scheme != "http":
        raise RuntimeError("HTTP server must be loopback")
    if not re.fullmatch(r"tw_identity_[a-f0-9]{24}", database):
        raise RuntimeError("Refusing non-test database")
    host = os.environ["TIMEWEAVER_TEST_MYSQL_HOST"]
    if host not in ("127.0.0.1", "::1"):
        raise RuntimeError("Database must be loopback")
    password = os.environ["TIMEWEAVER_TEST_MYSQL_PASSWORD"]
    config = types.ModuleType("config")
    config.settings = types.SimpleNamespace(SECRET_KEY=os.environ["TW_INTEROP_SECRET"], AGENT_SECRET_KEY="")
    config.db = types.SimpleNamespace(db_instance=None)
    sys.modules["config"] = config
    sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "server"))
    from repositories.agent_identity import AgentIdentityRepository
    from repositories.agent_execution import AgentExecutionRepository
    from schemas.agent_execution import ExecutionResultRequest, ExecutionStartRequest, AgentEventRequest, canonical_environment
    from services.agent.identity_service import AgentIdentityService
    from services.agent.execution_service import AgentExecutionService, AgentExecutionError

    wrapper = MySqlWrapper(host=host, port=3306, user="root", password=password, db=database)
    identity = AgentIdentityService(AgentIdentityRepository(wrapper))
    execution = AgentExecutionService(AgentExecutionRepository(wrapper))
    db = pymysql.connect(host=host, port=3306, user="root", password=password,
                         database=database, charset="utf8mb4", autocommit=True)

    class NoRedirect(HTTPRedirectHandler):
        def redirect_request(self, *args):
            return None

    opener = build_opener(NoRedirect())
    prefix = "/timeweaver_svr/api/agent/v1"

    def request(path, body=None, token=None, headers=None, expected=200):
        values = {"Content-Type": "application/json", **(headers or {})}
        if token:
            values["Authorization"] = "Bearer " + token
        req = Request(base + prefix + path, headers=values, method="POST",
                      data=None if body is None else json.dumps(body, ensure_ascii=False).encode())
        try:
            response = opener.open(req, timeout=15)
        except HTTPError as exc:
            response = exc
        with response:
            raw = json.loads(response.read())
            assert response.status == expected, (path, response.status, expected, raw)
            assert not response.headers.get("Location"), "Unexpected redirect"
            if expected == 200:
                assert raw["schema_version"] == "1" and raw["server_time"].endswith("Z")
            return raw

    def sql(query, params=()):
        with db.cursor() as cursor:
            cursor.execute(query, params)
            return cursor.fetchall()

    try:
        enrolled = []
        for name in ("execution-한글", "other-device"):
            token = "enr_" + secrets.token_hex(32)
            sql("""INSERT INTO agent_enrollment_token
                (enrollment_id,token_hash,device_name,group_id,created_at,expires_at)
                VALUES (%s,%s,%s,0,UTC_TIMESTAMP(),UTC_TIMESTAMP()+INTERVAL 1 DAY)""",
                (secrets.token_bytes(16), hashlib.sha256(token.encode()).digest(), name))
            enrolled.append(request("/enroll", {"enrollment_token": token, "device_name": name})["data"])
        current, other = enrolled
        principal = identity.verify_access(current["access_token"])
        detail = uuid.UUID("11111111-1111-1111-1111-111111111111")
        sql("INSERT INTO schedule_group(schedule_id,name,target_device,is_error_stop) VALUES(12,'execution',%s,TRUE)", (principal.device_id,))
        sql("INSERT INTO schedule_detail(detail_id,schedule_name,schedule_id,is_error_stop) VALUES(%s,'detail',12,TRUE)", (detail.bytes,))
        sql("INSERT INTO manual_execution(manual_id,detail_id,status,schedule_datetime) VALUES(41,%s,'wait',UTC_TIMESTAMP()),(42,%s,'wait',UTC_TIMESTAMP())", (detail.bytes, detail.bytes))
        environment = {"host": "한글 日本語 😀 \u2028\u2029", "literal": "\\u2028", "float": 1.0,
                       "values": [1e-5, 1e15, 1e16, -0.0, 5e-324], "integer": 10**70,
                       "nested": {"b": None, "a": [True, False]}}

        def body(**overrides):
            result = dict(execution_grp_id=str(uuid.uuid4()), schedule_id=12, detail_id=str(detail), attempt=1,
                          started_at="2026-08-01T02:00:01.123456Z", finished_at="2026-08-01T02:00:09.987654Z",
                          result_code=0, result_message="완료", environment_info=environment)
            result.update(overrides)
            return result

        def key(value):
            return f"{value['execution_grp_id']}:{value['detail_id']}:{value['attempt']}"

        def go_result(value, expected=200, token=None):
            result = request(f"/executions/{value['execution_grp_id']}/results/", value,
                             token or current["access_token"], {"Idempotency-Key": key(value)}, expected)
            return result["data"] if expected == 200 else result["detail"]

        def py_result(value):
            return execution.accept_result(principal, uuid.UUID(value["execution_grp_id"]), key(value), ExecutionResultRequest(**value))

        def py_error(value, code):
            try:
                py_result(value)
            except AgentExecutionError as exc:
                assert exc.code == code, (exc.code, code)
            else:
                raise AssertionError("Python accepted an invalid result")

        def start(value, python):
            part = {k: value[k] for k in ("execution_grp_id", "schedule_id", "detail_id", "attempt", "started_at")}
            if python:
                execution.start_execution(principal, uuid.UUID(part["execution_grp_id"]), ExecutionStartRequest(**part))
            else:
                assert request(f"/executions/{part['execution_grp_id']}/start/", part, current["access_token"])["data"] == {"accepted": True}
            assert sql("SELECT COUNT(*) FROM execution_running")[0][0] == 1

        # Each direction crosses process boundaries without copying or resetting
        # credential/result rows. This directly exercises rollback-era outboxes.
        for python_first in (False, True):
            for target in ("none", "schedule_group", "schedule_detail"):
                sql("UPDATE schedule_group SET status='active',is_error_stop=%s WHERE schedule_id=12", (target == "schedule_group",))
                sql("UPDATE schedule_detail SET status='active',is_error_stop=%s WHERE detail_id=%s", (target != "none", detail.bytes))
                value = body(result_code=0 if target == "none" else 2)
                start(value, python=not python_first)
                first = py_result(value) if python_first else go_result(value)
                duplicate = go_result(value) if python_first else py_result(value)
                assert first["duplicate"] is False and duplicate["duplicate"] is True
                assert first["execution_id"] == duplicate["execution_id"]
                assert duplicate["applied_transitions"] == []
                expected = [] if target == "none" else [{"target": target, "id": 12 if target == "schedule_group" else str(detail), "status": "error"}]
                assert first["applied_transitions"] == expected
                assert sql("SELECT COUNT(*) FROM execution_running")[0][0] == 0
                assert sql("SELECT environment_info FROM execution_log WHERE execution_id=%s", (first["execution_id"],))[0][0] == canonical_environment(environment)
                changed = {**value, "result_message": "different"}
                assert go_result(changed, 422)["code"] == "invalid_request"
                py_error(changed, "invalid_request")
                assert go_result(value, 404, other["access_token"])["code"] == "not_found"

        # Python claim -> Go completion and Go claim -> Python completion, then
        # duplicate replay after the lease has already been cleared by completion.
        sql("UPDATE schedule_group SET status='active',is_error_stop=TRUE WHERE schedule_id=12")
        for manual_id, python_claim, code in ((41, True, 1), (42, False, 0)):
            claim = execution.claim_manual_run(principal, manual_id) if python_claim else request(f"/manual-runs/{manual_id}/claim/", token=current["access_token"])["data"]
            value = body(manual_id=manual_id, claim_token=claim["claim_token"], result_code=code)
            first = go_result(value) if python_claim else py_result(value)
            second = py_result(value) if python_claim else go_result(value)
            assert first["duplicate"] is False and second["duplicate"] is True
            assert first["execution_id"] == second["execution_id"]
            assert first["applied_transitions"] == [{"target": "manual_execution", "id": manual_id, "status": "failed" if code else "done"}]
            assert sql("SELECT claim_token,claim_expires_at FROM manual_execution WHERE manual_id=%s", (manual_id,))[0] == (None, None)
            assert sql("SELECT status FROM schedule_group WHERE schedule_id=12")[0][0] == "active"

        # Expired lease cannot authorize a new result. Reclaim must rotate it.
        old_token = "a" * 64
        sql("UPDATE manual_execution SET status='processing',claim_token=%s,claim_expires_at=UTC_TIMESTAMP()-INTERVAL 1 SECOND WHERE manual_id=41", (old_token,))
        value = body(manual_id=41, claim_token=old_token)
        assert go_result(value, 410)["code"] == "claim_expired"
        py_error(value, "claim_expired")
        claim = request("/manual-runs/41/claim", token=current["access_token"])["data"]
        assert claim["claim_token"] != old_token
        assert go_result(value, 410)["code"] == "claim_expired"
        assert request("/manual-runs/41/claim", token=current["access_token"], expected=409)["detail"]["code"] == "already_claimed"
        assert request("/manual-runs/41/claim", token=other["access_token"], expected=404)["detail"]["code"] == "not_found"

        # Events store the same normalized UTC and canonical environment text.
        event = dict(event_type="sync_error", severity="error", occurred_at="2026-08-01T02:00:00.123456Z", message="에러", environment_info=environment)
        execution.accept_event(principal, AgentEventRequest(**event))
        assert request("/events/", event, current["access_token"])["data"] == {"accepted": True}
        event_rows = sql("SELECT device_id,event_type,severity,occurred_at,message,environment_info FROM agent_event ORDER BY event_id")
        assert len(event_rows) == 2 and event_rows[0] == event_rows[1]
        assert request("/events", {**event, "message": "가"*6000}, current["access_token"], expected=422)["detail"]["code"] == "invalid_request"

        sql("UPDATE schedule_detail SET deleted_at=UTC_TIMESTAMP() WHERE detail_id=%s", (detail.bytes,))
        delayed = body()
        assert go_result(delayed)["duplicate"] is False
        assert py_result(delayed)["duplicate"] is True
        assert request("/manual-runs/41/claim", token=current["access_token"], expected=404)["detail"]["code"] == "not_found"
        print("PASS: Python/Go bidirectional claims, starts, duplicate results, canonical environment, transitions, device scope, expired claims, tombstones and events")
    finally:
        db.close()
        wrapper.close()


if __name__ == "__main__":
    main()

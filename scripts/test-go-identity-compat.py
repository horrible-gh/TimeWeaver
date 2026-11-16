#!/usr/bin/env python3
"""Exercise frozen Python identity code against the Go test server and one CI DB.

Invoked only by TestPythonIdentityInterop. Never reads DB_DATABASE/server/.env;
all credentials and tokens are synthetic, and raw token values are never logged.
"""
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import sys
import types
from urllib.error import HTTPError
from urllib.parse import urlparse
from urllib.request import Request, urlopen

import jwt
import pymysql
from sqloader.mysql import MySqlWrapper


def main():
    if os.environ.get("TIMEWEAVER_TEST_MYSQL_ALLOW_CREATE") != "1":
        raise RuntimeError("Disposable database permission is required")
    base, database = sys.argv[1:]
    target = urlparse(base)
    if target.hostname not in ("127.0.0.1", "::1") or target.scheme != "http":
        raise RuntimeError("Test server must be loopback HTTP")
    if not re.fullmatch(r"tw_identity_[a-f0-9]{24}", database):
        raise RuntimeError("Refusing a database outside the disposable namespace")
    host = os.environ["TIMEWEAVER_TEST_MYSQL_HOST"]
    if host not in ("127.0.0.1", "::1"):
        raise RuntimeError("Test database must be loopback")
    password = os.environ["TIMEWEAVER_TEST_MYSQL_PASSWORD"]
    settings = types.SimpleNamespace(
        SECRET_KEY=os.environ["TW_INTEROP_SECRET"],
        AGENT_SECRET_KEY=os.environ["TW_INTEROP_AGENT_SECRET"],
    )
    # Import the exact legacy repository/service without booting config.py,
    # running migrations, or opening the real application's configured DB.
    config = types.ModuleType("config")
    config.settings = settings
    config.db = types.SimpleNamespace(db_instance=None)
    sys.modules["config"] = config
    sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "server"))
    from repositories.agent_identity import AgentIdentityRepository
    from services.agent.identity_service import AgentIdentityService, IdentityError

    wrapper = MySqlWrapper(host=host, port=3306, user="root", password=password, db=database)
    legacy = AgentIdentityService(AgentIdentityRepository(wrapper))
    connection = pymysql.connect(host=host, port=3306, user="root", password=password,
                                 database=database, charset="utf8mb4", autocommit=True)

    def request(path, body=None, token=None, expected=200):
        headers = {"Content-Type": "application/json"}
        if token:
            headers["Authorization"] = "Bearer " + token
        req = Request(base + path, headers=headers,
                      data=None if body is None else json.dumps(body).encode())
        try:
            response = urlopen(req, timeout=10)
        except HTTPError as exc:
            response = exc
        with response:
            if response.status != expected:
                raise AssertionError(f"Unexpected status on {path}: {response.status}, expected {expected}")
            assert not response.headers.get("Location"), "Agent endpoint redirected"
            assert response.headers.get("Cache-Control") == "no-store"
            return json.loads(response.read())

    prefix = "/timeweaver_svr/api/agent/v1"
    try:
        name = "interop-한글-" + secrets.token_hex(6)
        token = "enr_" + secrets.token_hex(32)
        with connection.cursor() as cursor:
            cursor.execute("""INSERT INTO agent_enrollment_token
                (enrollment_id,token_hash,device_name,group_id,created_at,expires_at)
                VALUES (%s,%s,%s,0,UTC_TIMESTAMP(),UTC_TIMESTAMP()+INTERVAL 1 DAY)""",
                (secrets.token_bytes(16), hashlib.sha256(token.encode()).digest(), name))
        response = request(prefix + "/enroll", {
            "enrollment_token": token, "device_name": " " + name + " ", "agent_version": "interop-1",
        })
        assert response["schema_version"] == "1"
        current = response["data"]
        device_id = current["device_id"]
        assert current["device_name"] == name and current["token_type"] == "bearer"
        assert set(current) == {"device_id", "device_name", "token_type", "access_token",
                                "refresh_token", "access_token_expires_at", "refresh_token_expires_at"}
        assert legacy.verify_access(current["access_token"]).device_id == device_id
        assert request("/verify", token=current["access_token"])["device_id"] == device_id
        assert request(prefix + "/enroll/", {"enrollment_token": token, "device_name": name},
                       expected=403)["detail"]["code"] == "enrollment_token_invalid"
        old = current
        # Actual legacy SQL transaction and Python signer create the new pair.
        current = legacy.rotate(old["refresh_token"])
        assert request("/verify", token=current["access_token"])["device_id"] == device_id
        assert request("/verify", token=old["access_token"], expected=403)["detail"]["code"] == "device_revoked"
        assert request(prefix + "/token", {"refresh_token": old["refresh_token"]},
                       expected=401)["detail"]["code"] == "invalid_token"
        for path in ("/token", "/token/", "/token/refresh", "/token/refresh/"):
            old = current
            current = request(prefix + path, {"refresh_token": old["refresh_token"]})["data"]
            assert "device_id" not in current and "device_name" not in current
            assert legacy.verify_access(current["access_token"]).device_id == device_id
            try:
                legacy.verify_access(old["access_token"])
            except IdentityError as error:
                assert error.status_code == 403 and error.code == "device_revoked"
            else:
                raise AssertionError("Python accepted a Go-revoked credential")
        assert request("/verify", token=current["access_token"])["device_id"] == device_id
        human = jwt.encode({"sub": "timeweaver", "aud": "timeweaver-dashboard", "typ": "user"},
                           settings.SECRET_KEY, algorithm="HS256")
        assert request("/verify", token=human, expected=401)["detail"]["code"] == "invalid_token"
        with connection.cursor() as cursor:
            cursor.execute("UPDATE devices SET status='inactive' WHERE device_id=%s", (device_id,))
        assert request("/verify", token=current["access_token"], expected=403)["detail"]["code"] == "device_revoked"
        assert request(prefix + "/token", {"refresh_token": current["refresh_token"]},
                       expected=403)["detail"]["code"] == "device_inactive"
        print("PASS: Python/Go signing, refresh rotation, revocation, Unicode identity and redirect-free aliases")
    finally:
        connection.close()
        wrapper.close()


if __name__ == "__main__":
    main()

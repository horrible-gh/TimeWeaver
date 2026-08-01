import uuid
from dataclasses import dataclass
from datetime import datetime, timedelta


class IdentityRepositoryError(Exception):
    def __init__(self, code: str):
        super().__init__(code)
        self.code = code


@dataclass(frozen=True)
class EnrollmentIssue:
    enrollment_id: uuid.UUID
    expires_at: datetime


@dataclass(frozen=True)
class CredentialIssue:
    credential_id: int
    device_id: int
    device_name: str
    access_expires_at: datetime
    refresh_expires_at: datetime


class AgentIdentityRepository:
    """The sole SQL boundary for enrollment and device credentials."""

    def __init__(self, db_instance):
        self.db = db_instance

    def get_user_principal(self, user_id: str):
        return self.db.fetch_one(
            "SELECT user_id, group_id, role FROM users WHERE user_id = %s",
            (user_id,),
        )

    def issue_enrollment(
        self,
        enrollment_id: uuid.UUID,
        digest: bytes,
        device_name: str | None,
        group_id: int,
        ttl_hours: int,
    ) -> EnrollmentIssue:
        with self.db.begin_transaction() as txn:
            now = txn.fetch_one("SELECT UTC_TIMESTAMP() AS db_now")["db_now"]
            group = txn.fetch_one(
                "SELECT group_id, status FROM groups WHERE group_id = %s FOR UPDATE",
                (group_id,),
            )
            if not group or group["status"] != "active":
                raise IdentityRepositoryError("group_inactive")
            expires_at = now + timedelta(hours=ttl_hours)
            txn.execute(
                """
                INSERT INTO agent_enrollment_token
                    (enrollment_id, token_hash, device_name, group_id, expires_at)
                VALUES (%s, %s, %s, %s, %s)
                """,
                (enrollment_id.bytes, digest, device_name, group_id, expires_at),
            )
        return EnrollmentIssue(enrollment_id, expires_at)

    def list_enrollments(self, group_id: int | None = None):
        where = ""
        params = None
        if group_id is not None:
            where = "WHERE group_id = %s"
            params = (group_id,)
        return self.db.fetch_all(
            f"""
            SELECT enrollment_id, device_name, group_id, created_at, expires_at,
                   used_at, used_by_device_id, revoked_at, UTC_TIMESTAMP() AS db_now
              FROM agent_enrollment_token
              {where}
             ORDER BY created_at DESC
            """,
            params,
        )

    def revoke_enrollment(self, enrollment_id: uuid.UUID):
        with self.db.begin_transaction() as txn:
            now = txn.fetch_one("SELECT UTC_TIMESTAMP() AS db_now")["db_now"]
            row = txn.fetch_one(
                """
                SELECT used_at, revoked_at
                  FROM agent_enrollment_token
                 WHERE enrollment_id = %s
                 FOR UPDATE
                """,
                (enrollment_id.bytes,),
            )
            if not row:
                raise IdentityRepositoryError("not_found")
            if row["used_at"] is not None:
                raise IdentityRepositoryError("already_used")
            revoked_at = row["revoked_at"] or now
            if row["revoked_at"] is None:
                txn.execute(
                    "UPDATE agent_enrollment_token SET revoked_at = %s WHERE enrollment_id = %s",
                    (revoked_at, enrollment_id.bytes),
                )
        return revoked_at

    def enroll(
        self,
        enrollment_digest: bytes,
        requested_device_name: str,
        agent_version: str | None,
        refresh_digest: bytes,
        access_ttl_minutes: int,
        refresh_ttl_days: int,
    ) -> CredentialIssue:
        with self.db.begin_transaction() as txn:
            now = txn.fetch_one("SELECT UTC_TIMESTAMP() AS db_now")["db_now"]
            token_row = txn.fetch_one(
                """
                SELECT enrollment_id, device_name, group_id, used_at, expires_at, revoked_at
                  FROM agent_enrollment_token
                 WHERE token_hash = %s
                 FOR UPDATE
                """,
                (enrollment_digest,),
            )
            if (
                not token_row
                or token_row["used_at"] is not None
                or token_row["revoked_at"] is not None
                or token_row["expires_at"] <= now
                or (
                    token_row["device_name"] is not None
                    and token_row["device_name"] != requested_device_name
                )
            ):
                raise IdentityRepositoryError("enrollment_token_invalid")

            group = txn.fetch_one(
                "SELECT status FROM groups WHERE group_id = %s FOR UPDATE",
                (token_row["group_id"],),
            )
            if not group or group["status"] != "active":
                raise IdentityRepositoryError("enrollment_token_invalid")

            device = txn.fetch_one(
                """
                SELECT device_id, device_name, group_id, status
                  FROM devices
                 WHERE device_name = %s
                 FOR UPDATE
                """,
                (requested_device_name,),
            )
            if device:
                if device["status"] != "active":
                    raise IdentityRepositoryError("device_inactive")
                if device["group_id"] != token_row["group_id"]:
                    raise IdentityRepositoryError("enrollment_token_invalid")
                txn.execute(
                    """
                    UPDATE devices
                       SET version = %s, last_login_at = %s
                     WHERE device_id = %s
                    """,
                    (agent_version, now, device["device_id"]),
                )
                device_id = device["device_id"]
            else:
                txn.execute(
                    """
                    INSERT INTO devices
                        (group_id, device_name, status, version, last_login_at)
                    VALUES (%s, %s, 'active', %s, %s)
                    """,
                    (token_row["group_id"], requested_device_name, agent_version, now),
                )
                device_id = txn.fetch_one("SELECT LAST_INSERT_ID() AS device_id")["device_id"]

            txn.execute(
                """
                UPDATE agent_device_credential
                   SET revoked_at = %s
                 WHERE device_id = %s AND revoked_at IS NULL
                """,
                (now, device_id),
            )
            refresh_expires_at = now + timedelta(days=refresh_ttl_days)
            txn.execute(
                """
                INSERT INTO agent_device_credential
                    (device_id, token_hash, issued_at, expires_at)
                VALUES (%s, %s, %s, %s)
                """,
                (device_id, refresh_digest, now, refresh_expires_at),
            )
            credential_id = txn.fetch_one("SELECT LAST_INSERT_ID() AS credential_id")["credential_id"]
            affected = txn.execute(
                """
                UPDATE agent_enrollment_token
                   SET used_at = %s, used_by_device_id = %s
                 WHERE enrollment_id = %s
                   AND used_at IS NULL
                   AND revoked_at IS NULL
                   AND expires_at > %s
                """,
                (now, device_id, token_row["enrollment_id"], now),
            )
            if affected != 1:
                raise IdentityRepositoryError("enrollment_token_invalid")

        return CredentialIssue(
            credential_id=credential_id,
            device_id=device_id,
            device_name=requested_device_name,
            access_expires_at=now + timedelta(minutes=access_ttl_minutes),
            refresh_expires_at=refresh_expires_at,
        )

    def rotate(
        self,
        refresh_digest: bytes,
        new_refresh_digest: bytes,
        access_ttl_minutes: int,
        refresh_ttl_days: int,
    ) -> CredentialIssue:
        with self.db.begin_transaction() as txn:
            now = txn.fetch_one("SELECT UTC_TIMESTAMP() AS db_now")["db_now"]
            current = txn.fetch_one(
                """
                SELECT credential_id, device_id, expires_at, revoked_at
                  FROM agent_device_credential
                 WHERE token_hash = %s
                 FOR UPDATE
                """,
                (refresh_digest,),
            )
            if (
                not current
                or current["revoked_at"] is not None
                or current["expires_at"] <= now
            ):
                raise IdentityRepositoryError("invalid_token")
            device = txn.fetch_one(
                """
                SELECT device_id, device_name, status
                  FROM devices
                 WHERE device_id = %s
                 FOR UPDATE
                """,
                (current["device_id"],),
            )
            if not device or device["status"] != "active":
                raise IdentityRepositoryError("device_inactive")

            txn.execute(
                """
                UPDATE agent_device_credential
                   SET revoked_at = %s
                 WHERE device_id = %s AND revoked_at IS NULL
                """,
                (now, current["device_id"]),
            )
            refresh_expires_at = now + timedelta(days=refresh_ttl_days)
            txn.execute(
                """
                INSERT INTO agent_device_credential
                    (device_id, token_hash, issued_at, expires_at)
                VALUES (%s, %s, %s, %s)
                """,
                (current["device_id"], new_refresh_digest, now, refresh_expires_at),
            )
            credential_id = txn.fetch_one("SELECT LAST_INSERT_ID() AS credential_id")["credential_id"]

        return CredentialIssue(
            credential_id=credential_id,
            device_id=current["device_id"],
            device_name=device["device_name"],
            access_expires_at=now + timedelta(minutes=access_ttl_minutes),
            refresh_expires_at=refresh_expires_at,
        )

    def get_access_identity(self, credential_id: int, device_id: int):
        return self.db.fetch_one(
            """
            SELECT c.credential_id, c.device_id, c.expires_at, c.revoked_at,
                   d.device_name, d.status, UTC_TIMESTAMP() AS db_now
              FROM agent_device_credential c
              JOIN devices d ON d.device_id = c.device_id
             WHERE c.credential_id = %s AND c.device_id = %s
            """,
            (credential_id, device_id),
        )

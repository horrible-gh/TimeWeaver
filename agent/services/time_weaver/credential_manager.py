"""Single-flight access refresh and fail-closed credential persistence."""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
import getpass
import json
import os
from pathlib import Path
import stat
import subprocess
import tempfile
import threading
from typing import Callable, Protocol

from .api_client import ApiClientError
from .models import AccessCredential, parse_datetime


ACCESS_REFRESH_LEAD = 60
CREDENTIAL_FILE_MODE = 0o600
CREDENTIAL_REPLACE_ATTEMPTS = 1


class RefreshClient(Protocol):
    def refresh_access_token(self, refresh_token: str) -> AccessCredential: ...


@dataclass(frozen=True, slots=True)
class StoredCredential:
    device_id: int
    device_name: str
    refresh_token: str
    refresh_token_expires_at: datetime
    schema_version: str = "1"

    @classmethod
    def from_json(cls, value: object) -> "StoredCredential":
        if not isinstance(value, dict):
            raise ValueError("credential file must contain an object")
        device_id = value.get("device_id")
        device_name = value.get("device_name")
        refresh_token = value.get("refresh_token")
        schema_version = value.get("schema_version")
        if isinstance(device_id, bool) or not isinstance(device_id, int) or device_id < 1:
            raise ValueError("invalid credential device_id")
        if not isinstance(device_name, str) or not device_name:
            raise ValueError("invalid credential device_name")
        if not isinstance(refresh_token, str) or not refresh_token:
            raise ValueError("invalid credential refresh_token")
        if schema_version != "1":
            raise ValueError("invalid credential schema_version")
        return cls(
            device_id=device_id,
            device_name=device_name,
            refresh_token=refresh_token,
            refresh_token_expires_at=parse_datetime(
                value.get("refresh_token_expires_at"), "refresh_token_expires_at"
            ),
            schema_version=schema_version,
        )

    def to_json(self) -> dict[str, object]:
        return {
            "schema_version": self.schema_version,
            "device_id": self.device_id,
            "device_name": self.device_name,
            "refresh_token": self.refresh_token,
            "refresh_token_expires_at": self.refresh_token_expires_at.astimezone(timezone.utc).isoformat().replace("+00:00", "Z"),
        }


@dataclass(frozen=True, slots=True)
class CredentialOutcome:
    access_token: str | None = None
    access_token_expires_at: datetime | None = None
    reason: str | None = None

    @property
    def ok(self) -> bool:
        return self.access_token is not None and self.reason is None


class CredentialStore:
    """Credential file store with permission validation and atomic replacement."""

    def __init__(
        self,
        path: str | os.PathLike[str],
        *,
        permission_setter: Callable[[Path], None] | None = None,
        replace: Callable[[str, str], None] | None = None,
    ) -> None:
        self.path = Path(path)
        self._permission_setter = permission_setter or self._secure_owner_only
        self._replace = replace or self._atomic_replace
        self.replace_count = 0

    def read(self) -> StoredCredential | None:
        if not self.path.exists():
            return None
        self._verify_owner_only(self.path)
        with self.path.open("r", encoding="utf-8") as handle:
            return StoredCredential.from_json(json.load(handle))

    def write(self, credential: StoredCredential) -> None:
        self.path.parent.mkdir(parents=True, exist_ok=True)
        descriptor, temporary_name = tempfile.mkstemp(
            prefix=f".{self.path.name}.", dir=str(self.path.parent)
        )
        temporary = Path(temporary_name)
        try:
            self._permission_setter(temporary)
            with os.fdopen(descriptor, "w", encoding="utf-8", newline="\n") as handle:
                descriptor = -1
                json.dump(credential.to_json(), handle, ensure_ascii=False, sort_keys=True)
                handle.write("\n")
                handle.flush()
                os.fsync(handle.fileno())
            self._verify_owner_only(temporary)
            self._replace(str(temporary), str(self.path))
            self.replace_count += 1
            self._sync_directory()
            self._verify_owner_only(self.path)
        finally:
            if descriptor >= 0:
                os.close(descriptor)
            try:
                temporary.unlink()
            except FileNotFoundError:
                pass

    def remove(self) -> None:
        try:
            self.path.unlink()
        except FileNotFoundError:
            pass

    @staticmethod
    def _atomic_replace(source: str, target: str) -> None:
        if os.name != "nt":
            os.replace(source, target)
            return
        import ctypes
        movefile_replace_existing = 0x1
        movefile_write_through = 0x8
        move_file = ctypes.windll.kernel32.MoveFileExW
        move_file.argtypes = [ctypes.c_wchar_p, ctypes.c_wchar_p, ctypes.c_uint32]
        move_file.restype = ctypes.c_int
        if not move_file(source, target, movefile_replace_existing | movefile_write_through):
            raise ctypes.WinError()

    def _secure_owner_only(self, path: Path) -> None:
        if os.name != "nt":
            os.chmod(path, CREDENTIAL_FILE_MODE)
            return
        principal = getpass.getuser()
        completed = subprocess.run(
            ["icacls", str(path), "/inheritance:r", "/grant:r", f"{principal}:(R,W)"],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            check=False,
        )
        if completed.returncode != 0:
            raise PermissionError("owner-only credential ACL could not be applied")

    def _verify_owner_only(self, path: Path) -> None:
        if os.name != "nt":
            if stat.S_IMODE(path.stat().st_mode) != CREDENTIAL_FILE_MODE:
                raise PermissionError("credential file mode is not owner-only")
            return
        completed = subprocess.run(
            ["icacls", str(path), "/verify"],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            check=False,
        )
        if completed.returncode != 0:
            raise PermissionError("credential ACL could not be verified")

    def _sync_directory(self) -> None:
        if os.name == "nt":
            return
        flags = os.O_RDONLY | getattr(os, "O_DIRECTORY", 0)
        descriptor = os.open(str(self.path.parent), flags)
        try:
            os.fsync(descriptor)
        finally:
            os.close(descriptor)


class CredentialManager:
    """Owns the refresh file, memory access token and credential_lock."""

    def __init__(
        self,
        client: RefreshClient,
        credential_path: str | os.PathLike[str],
        *,
        store: CredentialStore | None = None,
        now: Callable[[], datetime] | None = None,
    ) -> None:
        self._client = client
        self._store = store or CredentialStore(credential_path)
        try:
            self.credential_existed_at_start = self._store.read() is not None
        except (OSError, ValueError):
            self.credential_existed_at_start = False
        self._now = now or (lambda: datetime.now(timezone.utc))
        self.credential_lock = threading.Lock()
        self._access_token: str | None = None
        self._access_expires_at: datetime | None = None

    def ensure_access_token(
        self, min_validity: float | timedelta = ACCESS_REFRESH_LEAD
    ) -> CredentialOutcome:
        minimum = min_validity if isinstance(min_validity, timedelta) else timedelta(seconds=float(min_validity))
        current = self._usable_memory_token(minimum)
        if current is not None:
            return current

        with self.credential_lock:
            current = self._usable_memory_token(minimum)
            if current is not None:
                return current
            try:
                stored = self._store.read()
            except (OSError, ValueError):
                try:
                    self._store.remove()
                except OSError:
                    pass
                self._clear_memory()
                return CredentialOutcome(reason="needs_enrollment")
            if stored is None or stored.refresh_token_expires_at <= self._now():
                self._store.remove()
                self._clear_memory()
                return CredentialOutcome(reason="needs_enrollment")

            try:
                refreshed = self._client.refresh_access_token(stored.refresh_token)
            except ApiClientError as exc:
                return self._handle_refresh_failure(exc)

            rotated = StoredCredential(
                device_id=refreshed.device_id or stored.device_id,
                device_name=refreshed.device_name or stored.device_name,
                refresh_token=refreshed.refresh_token,
                refresh_token_expires_at=refreshed.refresh_token_expires_at,
            )
            try:
                self._store.write(rotated)
            except (OSError, ValueError):
                self._clear_memory()
                return CredentialOutcome(reason="credential_persist_failed")

            self._access_token = refreshed.access_token
            self._access_expires_at = refreshed.access_token_expires_at
            return CredentialOutcome(self._access_token, self._access_expires_at)

    def install_enrollment(self, credential: AccessCredential) -> CredentialOutcome:
        if credential.device_id is None or credential.device_name is None:
            return CredentialOutcome(reason="credential_persist_failed")
        stored = StoredCredential(
            device_id=credential.device_id,
            device_name=credential.device_name,
            refresh_token=credential.refresh_token,
            refresh_token_expires_at=credential.refresh_token_expires_at,
        )
        with self.credential_lock:
            try:
                self._store.write(stored)
            except (OSError, ValueError):
                self._clear_memory()
                return CredentialOutcome(reason="credential_persist_failed")
            self._access_token = credential.access_token
            self._access_expires_at = credential.access_token_expires_at
            return CredentialOutcome(self._access_token, self._access_expires_at)

    def identity(self) -> tuple[int, str] | None:
        try:
            stored = self._store.read()
        except (OSError, ValueError):
            return None
        return None if stored is None else (stored.device_id, stored.device_name)

    def _usable_memory_token(self, minimum: timedelta) -> CredentialOutcome | None:
        if (
            self._access_token is not None
            and self._access_expires_at is not None
            and self._access_expires_at - self._now() >= minimum
        ):
            return CredentialOutcome(self._access_token, self._access_expires_at)
        return None

    def _handle_refresh_failure(self, exc: ApiClientError) -> CredentialOutcome:
        if exc.code in {"invalid_token", "token_expired"}:
            self._store.remove()
            self._clear_memory()
            return CredentialOutcome(reason="needs_enrollment")
        if exc.code in {"device_inactive", "device_revoked"}:
            self._clear_memory()
            return CredentialOutcome(reason="device_inactive")
        self._clear_memory()
        return CredentialOutcome(reason="transient")

    def _clear_memory(self) -> None:
        self._access_token = None
        self._access_expires_at = None
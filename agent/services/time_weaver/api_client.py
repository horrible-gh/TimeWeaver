"""Synchronous, DB-independent HTTP boundary for the TimeWeaver agent."""

from __future__ import annotations

from dataclasses import dataclass
import time
from typing import Any, Callable, Mapping, Protocol
from urllib.parse import quote, urlsplit

import requests

from .models import AccessCredential, ModelValidationError


class ApiClientError(RuntimeError):
    """Base API error with a protocol error code and no secret-bearing payload."""

    def __init__(
        self,
        message: str,
        *,
        code: str,
        retry_after: float | None = None,
        details: Mapping[str, Any] | None = None,
    ) -> None:
        super().__init__(message)
        self.code = code
        self.retry_after = retry_after
        self.details = dict(details or {})


class CommunicationError(ApiClientError):
    pass


class AuthenticationError(ApiClientError):
    pass


class DeviceInactiveError(AuthenticationError):
    pass


class EnrollmentTokenInvalidError(AuthenticationError):
    pass


class ClientRejectedError(ApiClientError):
    pass


class TransientServerError(ApiClientError):
    pass


class RateLimitError(TransientServerError):
    pass


class MalformedResponseError(ApiClientError):
    def __init__(self, message: str) -> None:
        super().__init__(message, code="malformed_response")


class SchemaMismatchError(ClientRejectedError):
    pass


@dataclass(frozen=True, slots=True)
class NotModified:
    etag: str | None


@dataclass(frozen=True, slots=True)
class SnapshotResponse:
    envelope: Mapping[str, Any]
    etag: str


class Response(Protocol):
    status_code: int
    headers: Mapping[str, str]

    def json(self) -> Any: ...


class Transport(Protocol):
    def request(self, method: str, url: str, **kwargs: Any) -> Response: ...


@dataclass(frozen=True, slots=True)
class EndpointPaths:
    enroll: str = "/api/agent/v1/enroll"
    token: str = "/api/agent/v1/token"
    heartbeat: str = "/api/agent/v1/heartbeat"
    snapshot: str = "/api/agent/v1/snapshot"
    claim_manual_execution: str = "/api/agent/v1/manual-runs/{manual_id}/claim"
    execution_results: str = "/api/agent/v1/executions/{execution_grp_id}/results"
    events: str = "/api/agent/v1/events"

    @property
    def register(self) -> str:
        return self.enroll

    @property
    def schedule_snapshot(self) -> str:
        return self.snapshot


class AgentApiClient:
    """Protocol-aligned synchronous client; it owns no scheduler or database state."""

    def __init__(
        self,
        base_url: str,
        credential: str | None = None,
        *,
        agent_version: str = "unknown",
        transport: Transport | None = None,
        endpoints: EndpointPaths | None = None,
        schema_version: str = "1",
        connect_timeout: float = 5.0,
        read_timeout: float = 30.0,
        retries: int = 2,
        backoff: float = 1.0,
        sleeper: Callable[[float], None] = time.sleep,
    ) -> None:
        if not base_url or not base_url.strip():
            raise ValueError("base_url is required")
        parts = urlsplit(base_url.strip())
        if parts.scheme not in ("http", "https"):
            raise ValueError("base_url scheme must be http or https")
        if not parts.netloc:
            raise ValueError("base_url must include a host")
        if parts.query or parts.fragment:
            raise ValueError("base_url must not contain a query or fragment")
        if connect_timeout <= 0 or read_timeout <= 0:
            raise ValueError("timeouts must be positive")
        if retries < 0 or backoff < 0:
            raise ValueError("retries and backoff must be non-negative")
        self._base_url = base_url.strip().rstrip("/")
        self._credential = credential
        self._agent_version = agent_version
        self._transport = transport or requests.Session()
        self._endpoints = endpoints or EndpointPaths()
        self._schema_version = str(schema_version)
        self._timeout = (connect_timeout, read_timeout)
        self._retries = retries
        self._backoff = backoff
        self._sleep = sleeper

    @property
    def endpoints(self) -> EndpointPaths:
        return self._endpoints

    def endpoint_url(self, path: str) -> str:
        """Exact URL _request will hit for *path* (same join rule, no drift)."""
        return f"{self._base_url}/{path.lstrip('/')}"

    def set_access_token(self, token: str | None) -> None:
        self._credential = token

    def enroll(self, enrollment_token: str, device_name: str) -> AccessCredential:
        if not enrollment_token or not device_name:
            raise ValueError("enrollment_token and device_name are required")
        data = self._request(
            "POST",
            self._endpoints.enroll,
            json={
                "enrollment_token": enrollment_token,
                "device_name": device_name,
                "agent_version": self._agent_version,
            },
            include_contract_headers=True,
            use_default_credential=False,
        )
        return self._credential_from_data(data)

    def register_or_handshake(self, device_name: str) -> AccessCredential:
        if not self._credential:
            raise ValueError("an enrollment credential is required")
        return self.enroll(self._credential, device_name)

    def refresh_access_token(self, refresh_token: str) -> AccessCredential:
        if not refresh_token:
            raise ValueError("refresh_token is required")
        data = self._request(
            "POST",
            self._endpoints.token,
            json={"refresh_token": refresh_token},
            use_default_credential=False,
        )
        return self._credential_from_data(data)

    def send_heartbeat(
        self,
        agent_version: str,
        state: str,
        applied_revision: str | None,
        *,
        access_token: str | None = None,
    ) -> Mapping[str, Any]:
        data = self._request(
            "POST",
            self._endpoints.heartbeat,
            json={
                "agent_version": agent_version,
                "applied_revision": applied_revision,
                "state": state,
            },
            access_token=access_token,
            include_contract_headers=True,
        )
        if not isinstance(data, Mapping):
            raise MalformedResponseError("heartbeat data must be an object")
        return data

    def get_schedule_snapshot(
        self,
        *,
        etag: str | None = None,
        access_token: str | None = None,
    ) -> SnapshotResponse | NotModified:
        extra_headers = {"If-None-Match": etag} if etag else None
        result = self._request(
            "GET",
            self._endpoints.snapshot,
            access_token=access_token,
            include_contract_headers=True,
            extra_headers=extra_headers,
            allow_not_modified=True,
            return_envelope=True,
        )
        if isinstance(result, NotModified):
            return result
        envelope, headers = result
        response_etag = headers.get("ETag") or headers.get("etag")
        if not isinstance(response_etag, str) or not response_etag:
            raise MalformedResponseError("snapshot response is missing ETag")
        return SnapshotResponse(envelope=envelope, etag=response_etag)

    def claim_manual_execution(
        self, manual_id: int, *, access_token: str | None = None
    ) -> Mapping[str, Any]:
        path = self._endpoints.claim_manual_execution.format(
            manual_id=quote(str(manual_id), safe="")
        )
        data = self._request(
            "POST", path, access_token=access_token, include_contract_headers=True
        )
        if not isinstance(data, Mapping):
            raise MalformedResponseError("claim data must be an object")
        return data

    def report_execution_results(
        self,
        execution_grp_id: str,
        results: Mapping[str, Any],
        *,
        idempotency_key: str,
        access_token: str | None = None,
    ) -> Mapping[str, Any]:
        if not idempotency_key:
            raise ValueError("idempotency_key is required")
        path = self._endpoints.execution_results.format(
            execution_grp_id=quote(execution_grp_id, safe="")
        )
        data = self._request(
            "POST",
            path,
            json=dict(results),
            access_token=access_token,
            include_contract_headers=True,
            extra_headers={"Idempotency-Key": idempotency_key},
        )
        if not isinstance(data, Mapping):
            raise MalformedResponseError("execution result data must be an object")
        return data

    def report_execution_event(
        self,
        event: Mapping[str, Any],
        *,
        idempotency_key: str | None = None,
        access_token: str | None = None,
    ) -> Mapping[str, Any]:
        headers = {"Idempotency-Key": idempotency_key} if idempotency_key else None
        data = self._request(
            "POST",
            self._endpoints.events,
            json=dict(event),
            access_token=access_token,
            include_contract_headers=True,
            extra_headers=headers,
        )
        if not isinstance(data, Mapping):
            raise MalformedResponseError("event data must be an object")
        return data

    def _credential_from_data(self, data: Any) -> AccessCredential:
        try:
            return AccessCredential.from_dict(data)
        except ModelValidationError as exc:
            raise MalformedResponseError(str(exc)) from exc

    def _request(
        self,
        method: str,
        path: str,
        *,
        access_token: str | None = None,
        include_contract_headers: bool = False,
        extra_headers: Mapping[str, str] | None = None,
        allow_not_modified: bool = False,
        return_envelope: bool = False,
        use_default_credential: bool = True,
        **kwargs: Any,
    ) -> Any:
        headers = {"Accept": "application/json"}
        token = access_token if access_token is not None else (
            self._credential if use_default_credential else None
        )
        if token:
            headers["Authorization"] = f"Bearer {token}"
        if include_contract_headers:
            headers["X-TW-Agent-Version"] = self._agent_version
            headers["X-TW-Schema-Version"] = self._schema_version
        if extra_headers:
            headers.update(extra_headers)
        url = self.endpoint_url(path)

        for attempt in range(self._retries + 1):
            try:
                response = self._transport.request(
                    method, url, headers=headers, timeout=self._timeout, **kwargs
                )
            except (TimeoutError, ConnectionError, requests.Timeout, requests.ConnectionError) as exc:
                if attempt < self._retries:
                    self._sleep(self._backoff * (2**attempt))
                    continue
                raise CommunicationError(
                    "server communication failed after retries", code="unavailable"
                ) from exc

            if response.status_code == 304 and allow_not_modified:
                return NotModified(response.headers.get("ETag") or response.headers.get("etag"))

            if 200 <= response.status_code <= 299:
                envelope = self._decode_envelope(response)
                if return_envelope:
                    return envelope, response.headers
                return envelope["data"]

            code, retry_after, details = self._decode_error(response)
            retryable = code in {"rate_limited", "server_error", "unavailable"}
            if retryable and attempt < self._retries:
                delay = retry_after if retry_after is not None else self._backoff * (2**attempt)
                self._sleep(delay)
                continue
            self._raise_protocol_error(code, retry_after, details)

        raise CommunicationError("server communication failed", code="unavailable")

    def _decode_envelope(self, response: Response) -> Mapping[str, Any]:
        try:
            payload = response.json()
        except (TypeError, ValueError) as exc:
            raise MalformedResponseError("response body is not valid JSON") from exc
        if not isinstance(payload, Mapping):
            raise MalformedResponseError("response envelope must be an object")
        received_schema = payload.get("schema_version")
        if str(received_schema) != self._schema_version:
            raise SchemaMismatchError(
                "response schema is not supported", code="schema_mismatch"
            )
        if "data" not in payload:
            raise MalformedResponseError("response envelope is missing data")
        if not isinstance(payload.get("server_time"), str):
            raise MalformedResponseError("response envelope is missing server_time")
        return payload

    def _decode_error(
        self, response: Response
    ) -> tuple[str, float | None, Mapping[str, Any]]:
        fallback = self._fallback_error_code(response.status_code)
        retry_after = self._retry_after(response.headers)
        try:
            payload = response.json()
        except (TypeError, ValueError):
            return fallback, retry_after, {}
        if not isinstance(payload, Mapping):
            return fallback, retry_after, {}
        error = payload.get("error")
        if not isinstance(error, Mapping):
            error = payload.get("detail")
        if not isinstance(error, Mapping) or not isinstance(error.get("code"), str):
            return fallback, retry_after, {}
        body_retry = error.get("retry_after")
        if retry_after is None and isinstance(body_retry, (int, float)) and not isinstance(body_retry, bool):
            retry_after = max(float(body_retry), 0.0)
        return error["code"], retry_after, error

    def _raise_protocol_error(
        self,
        code: str,
        retry_after: float | None,
        details: Mapping[str, Any],
    ) -> None:
        if code in {"invalid_token", "token_expired"}:
            raise AuthenticationError("agent token rejected", code=code)
        if code in {"device_inactive", "device_revoked"}:
            raise DeviceInactiveError("device is not active", code=code)
        if code == "enrollment_token_invalid":
            if details.get("reason") == "device_name_mismatch":
                expected = details.get("expected_device_name", "")
                actual = details.get("actual_device_name", "")
                raise EnrollmentTokenInvalidError(
                    f"enrollment device name mismatch "
                    f"(token: {expected} / agent: {actual})",
                    code=code,
                    details=details,
                )
            raise EnrollmentTokenInvalidError(
                "enrollment token rejected", code=code, details=details
            )
        if code == "schema_mismatch":
            raise SchemaMismatchError("contract schema mismatch", code=code)
        if code == "rate_limited":
            raise RateLimitError("server rate limited request", code=code, retry_after=retry_after)
        if code in {"server_error", "unavailable"}:
            raise TransientServerError("server is temporarily unavailable", code=code, retry_after=retry_after)
        raise ClientRejectedError("server rejected request", code=code, retry_after=retry_after)

    @staticmethod
    def _fallback_error_code(status: int) -> str:
        if status == 429:
            return "rate_limited"
        if status >= 500:
            return "server_error"
        if status == 401:
            return "invalid_token"
        if status == 403:
            return "device_inactive"
        if status in {400, 422}:
            return "invalid_request"
        return "invalid_request"

    @staticmethod
    def _retry_after(headers: Mapping[str, str]) -> float | None:
        value = headers.get("Retry-After") or headers.get("retry-after")
        if value is None:
            return None
        try:
            parsed = float(value)
        except (TypeError, ValueError):
            return None
        return parsed if parsed >= 0 else None
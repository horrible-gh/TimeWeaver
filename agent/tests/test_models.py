from datetime import timezone
from uuid import UUID

import pytest

from agent.services.time_weaver.models import (
    AccessCredential,
    CronFields,
    Device,
    ModelValidationError,
    ScheduleSnapshot,
    parse_bool,
    parse_datetime,
)


@pytest.mark.parametrize(("value", "expected"), [
    (True, True), (False, False), (1, True), (0, False),
    ("true", True), ("FALSE", False), ("1", True), ("0", False),
])
def test_boolean_variants_are_explicit(value, expected):
    assert parse_bool(value, "flag") is expected


@pytest.mark.parametrize("value", [2, -1, "yes", "", None, [], {}])
def test_invalid_boolean_variants_are_rejected(value):
    with pytest.raises(ModelValidationError):
        parse_bool(value, "flag")


def test_device_uses_protocol_identity_fields_and_ignores_unknown_fields():
    device = Device.from_dict({
        "device_id": 7, "device_name": "batch-01", "status": "active",
        "known_agent_version": "1.2.3", "future_field": True,
    })
    assert device.device_id == 7
    assert device.name == "batch-01"
    assert device.agent_version == "1.2.3"


@pytest.mark.parametrize("device_id", [0, -1, True, "7"])
def test_device_id_must_be_positive_integer(device_id):
    with pytest.raises(ModelValidationError):
        Device.from_dict({"device_id": device_id, "device_name": "a", "status": "active"})


def test_access_credential_parses_utc_dates():
    value = AccessCredential.from_dict({
        "device_id": 7,
        "device_name": "batch-01",
        "access_token": "access",
        "access_token_expires_at": "2026-08-01T05:15:00Z",
        "refresh_token": "refresh",
        "refresh_token_expires_at": "2026-10-30T05:00:00Z",
    })
    assert value.device_id == 7
    assert value.access_token_expires_at.tzinfo == timezone.utc


def test_iso_datetime_requires_timezone():
    assert parse_datetime("2026-08-01T05:00:00Z", "when").tzinfo == timezone.utc
    with pytest.raises(ModelValidationError, match="timezone"):
        parse_datetime("2026-08-01T05:00:00", "when")


def test_snapshot_model_exposes_protocol_metadata():
    digest = "a" * 64
    snapshot = ScheduleSnapshot.from_envelope({
        "schema_version": "1",
        "server_time": "2026-08-01T05:00:00Z",
        "data": {
            "revision": f"sha256:{digest}",
            "generated_at": "2026-08-01T05:00:00Z",
            "device": {"device_id": 7, "device_name": "batch-01", "status": "active"},
            "schedules": [{
                "schedule_id": 12,
                "name": "nightly",
                "cron": {"hour": 2, "minute": "*/5"},
                "is_error_stop": True,
                "details": [{
                    "detail_id": "47f3784c-bbb9-4363-b9c5-a8672450a29d",
                    "schedule_name": "copy-logs",
                    "cron": {"second": "*/10"},
                    "is_error_stop": True,
                    "sequence": 3,
                    "exec_sequence": 1,
                    "retry_count": 0,
                    "task": {
                        "task_type": "copy", "command": None, "archive_type": None,
                        "source_path": "/in", "error_on_missing_source": False,
                        "destination_path": "/out", "date_format": "%Y%m%d",
                        "target_date_format": None, "destination_date_format": None,
                        "house_keep_days": None,
                    },
                }],
            }],
            "manual_runs": [{
                "manual_id": 41, "schedule_id": 12,
                "detail_id": "47f3784c-bbb9-4363-b9c5-a8672450a29d",
                "status": "wait", "is_immediate": False,
                "schedule_datetime": "2026-08-01T06:00:00Z", "claimable": True,
            }],
        },
    }, etag='W/"aaaaaaaaaaaaaaaa"')
    assert snapshot.revision == f"sha256:{digest}"
    assert snapshot.etag == 'W/"aaaaaaaaaaaaaaaa"'
    assert snapshot.schedules[0].schedule_id == 12
    assert snapshot.schedules[0].details[0].detail_id == UUID("47f3784c-bbb9-4363-b9c5-a8672450a29d")


def test_invalid_cron_shape_and_uuid_are_rejected():
    with pytest.raises(ModelValidationError):
        CronFields.from_dict({"minute": ["*/5"]})
    with pytest.raises(ModelValidationError, match="lowercase canonical"):
        from agent.services.time_weaver.models import parse_uuid
        parse_uuid("47F3784C-BBB9-4363-B9C5-A8672450A29D", "detail_id", require_lowercase=True)
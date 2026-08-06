from copy import deepcopy

import pytest

from agent.services.time_weaver.sync_coordinator import (
    LocalIdentity,
    ReconciliationApplyError,
    ReconciliationRestoreError,
    RunningContext,
    SnapshotValidationError,
    SyncCoordinator,
    build_reconcile_plan,
    validate_snapshot,
)


IDENTITY = LocalIdentity(7, "batch-01")


def detail(identifier="8f0d65c5-b6a4-4bb0-a2c5-f23672fc9b76", *, sequence=10, exec_sequence=1):
    return {
        "detail_id": identifier,
        "schedule_name": "copy-logs",
        "cron": {"year": "*", "month": "*", "day_of_week": "*", "day": "*", "hour": "*", "minute": "*", "second": "0"},
        "is_error_stop": True,
        "sequence": sequence,
        "exec_sequence": exec_sequence,
        "retry_count": 0,
        "task": {
            "task_type": "copy",
            "command": None,
            "archive_type": None,
            "source_path": "/data/log",
            "error_on_missing_source": True,
            "destination_path": "/backup/log",
            "date_format": "%Y%m%d",
            "target_date_format": None,
            "destination_date_format": None,
            "house_keep_days": None,
        },
    }


def schedule(identifier=12, *, hour="2", detail_id=None):
    return {
        "schedule_id": identifier,
        "name": f"schedule-{identifier}",
        "cron": {"year": "*", "month": "*", "day_of_week": "*", "day": "*", "hour": hour, "minute": "0", "second": "0"},
        "is_error_stop": True,
        "details": [detail(detail_id or f"00000000-0000-0000-0000-{identifier:012x}")],
    }


def response(*, seed="a", schedules=None, manuals=None, server_time="2026-08-01T05:10:00Z"):
    digest = seed * 64
    schedules = [schedule()] if schedules is None else schedules
    if manuals is None:
        target = schedules[0]
        target_detail = target["details"][0]["detail_id"]
        manuals = [{
            "manual_id": 41,
            "schedule_id": target["schedule_id"],
            "detail_id": target_detail,
            "status": "wait",
            "is_immediate": False,
            "schedule_datetime": "2026-08-01T06:00:00Z",
            "claimable": True,
        }]
    return {
        "etag": f'W/"{digest[:16]}"',
        "schema_version": "1",
        "server_time": server_time,
        "data": {
            "revision": f"sha256:{digest}",
            "generated_at": server_time,
            "device": {
                "device_id": 7,
                "device_name": "batch-01",
                "status": "active",
                "known_agent_version": "v1",
            },
            "schedules": schedules,
            "manual_runs": manuals,
        },
    }


def mutate_schema(value): value["schema_version"] = "2"
def mutate_revision(value): value["data"]["revision"] = "bad"
def mutate_etag(value): value["etag"] = 'W/"wrongwrongwrongwr"'
def mutate_non_utc(value): value["server_time"] = "2026-08-01T14:10:00+09:00"
def mutate_identity(value): value["data"]["device"]["device_name"] = "other"
def mutate_inactive(value): value["data"]["device"]["status"] = "inactive"
def mutate_duplicate_schedule(value): value["data"]["schedules"].append(deepcopy(value["data"]["schedules"][0]))
def mutate_negative_id(value): value["data"]["schedules"][0]["schedule_id"] = -1
def mutate_duplicate_detail(value): value["data"]["schedules"][0]["details"].append(deepcopy(value["data"]["schedules"][0]["details"][0]))
def mutate_missing_reference(value): value["data"]["manual_runs"][0]["schedule_id"] = 999
def mutate_bad_cron(value): value["data"]["schedules"][0]["cron"]["minute"] = "bad cron"
def mutate_exec_order(value):
    first = value["data"]["schedules"][0]["details"][0]
    first["sequence"] = 1
    first["exec_sequence"] = 2
    value["data"]["schedules"][0]["details"].append(
        detail("9f0d65c5-b6a4-4bb0-a2c5-f23672fc9b77", sequence=2, exec_sequence=1)
    )
def mutate_task_fields(value): value["data"]["schedules"][0]["details"][0]["task"]["command"] = "forbidden"
def mutate_manual_time(value): value["data"]["manual_runs"][0]["schedule_datetime"] = "2026-08-01T15:00:00+09:00"
def mutate_missing_field(value): value["data"]['schedules'][0]['cron'].pop('year')


@pytest.mark.parametrize("mutator", [
    mutate_schema, mutate_revision, mutate_etag, mutate_non_utc, mutate_identity,
    mutate_inactive, mutate_duplicate_schedule, mutate_negative_id,
    mutate_duplicate_detail, mutate_missing_reference, mutate_bad_cron,
    mutate_exec_order, mutate_task_fields, mutate_manual_time, mutate_missing_field,
])
def test_each_snapshot_contract_violation_rejects_the_whole_snapshot(mutator):
    candidate = response()
    mutator(candidate)
    with pytest.raises(SnapshotValidationError):
        validate_snapshot(candidate, IDENTITY)


def test_exec_sequence_scope_is_per_schedule_not_global():
    # Regression for timeweaver.agent.0011.0003-NR: exec_sequence is only
    # guaranteed nondecreasing within a schedule_id (server dense_rank is per
    # schedule_id), so overlapping `sequence` values across different
    # schedules must not be rejected by a global sort/check.
    schedule_a = schedule(11, detail_id="00000000-0000-0000-0000-000000000011")
    schedule_a["details"][0]["sequence"] = 40
    schedule_a["details"][0]["exec_sequence"] = 4
    schedule_b = schedule(13, detail_id="00000000-0000-0000-0000-000000000013")
    schedule_b["details"][0]["sequence"] = 40
    schedule_b["details"][0]["exec_sequence"] = 3
    snapshot = validate_snapshot(
        response(schedules=[schedule_a, schedule_b], manuals=[]), IDENTITY
    )
    assert [item.schedule_id for item in snapshot.schedules] == [11, 13]


def test_valid_snapshot_is_immutable_and_normalized():
    snapshot = validate_snapshot(response(), IDENTITY)
    assert snapshot.device.device_id == 7
    assert snapshot.schedules[0].schedule_id == 12
    with pytest.raises(AttributeError):
        snapshot.revision = "changed"


def test_plan_has_remove_update_add_order_and_regular_stop_marker():
    old = validate_snapshot(response(seed="a", schedules=[schedule(1), schedule(2)], manuals=[]), IDENTITY)
    new = validate_snapshot(response(seed="b", schedules=[schedule(1, hour="3"), schedule(3)], manuals=[]), IDENTITY)
    contexts = [
        RunningContext("regular-1", "regular", schedule_id=1),
        RunningContext("manual-1", "manual", schedule_id=2),
    ]
    plan = build_reconcile_plan(old, new, contexts)
    assert plan.removals == ("schedule:2",)
    assert [item.key for item in plan.updates] == ["schedule:1"]
    assert [item.key for item in plan.additions] == ["schedule:3"]
    assert plan.ordered_actions == (
        ("remove", "schedule:2"),
        ("update", "schedule:1"),
        ("add", "schedule:3"),
    )
    assert plan.stop_after_current_sequence == ("regular-1",)


def test_fingerprint_excludes_revision_generated_at_and_server_time():
    old = validate_snapshot(response(seed="a", schedules=[schedule(1)], manuals=[], server_time="2026-08-01T05:00:00Z"), IDENTITY)
    new = validate_snapshot(response(seed="b", schedules=[schedule(1)], manuals=[], server_time="2026-08-01T06:00:00Z"), IDENTITY)
    plan = build_reconcile_plan(old, new)
    assert plan.removals == ()
    assert plan.updates == ()
    assert plan.additions == ()


class FakeAdapter:
    def __init__(self, jobs=None, *, fail_action=None, fail_forever=False):
        self.jobs = dict(jobs or {})
        self.fail_action = fail_action
        self.fail_forever = fail_forever
        self.failed = False
        self.calls = []

    def _check(self, action, key):
        self.calls.append((action, key))
        if self.fail_action == action and (self.fail_forever or not self.failed):
            self.failed = True
            raise RuntimeError(f"injected {action} failure")

    def add_job(self, key, spec):
        self._check("add", key)
        self.jobs[key] = spec

    def update_job(self, key, spec):
        self._check("update", key)
        if key not in self.jobs:
            raise KeyError(key)
        self.jobs[key] = spec

    def remove_job(self, key):
        self._check("remove", key)
        self.jobs.pop(key, None)

    def list_jobs(self):
        return dict(self.jobs)


def reconciliation_fixture():
    old = validate_snapshot(response(seed="a", schedules=[schedule(1), schedule(2)], manuals=[]), IDENTITY)
    new = validate_snapshot(response(seed="b", schedules=[schedule(1, hour="3"), schedule(3)], manuals=[]), IDENTITY)
    old_specs = {spec.key: spec for spec in build_reconcile_plan(None, old).additions}
    plan = build_reconcile_plan(old, new)
    return old, new, old_specs, plan


def test_apply_uses_deterministic_mutation_order_and_installs_new_snapshot():
    old, new, old_specs, plan = reconciliation_fixture()
    adapter = FakeAdapter(old_specs)
    coordinator = SyncCoordinator(old)
    assert coordinator.apply_reconcile_plan(old.revision, new, plan, adapter) is new
    assert adapter.calls[:3] == [
        ("remove", "schedule:2"),
        ("update", "schedule:1"),
        ("add", "schedule:3"),
    ]
    assert set(adapter.jobs) == {"schedule:1", "schedule:3"}
    assert coordinator.current_snapshot is new


@pytest.mark.parametrize("failure_action", ["remove", "update", "add"])
def test_each_mutation_failure_restores_previous_job_specs(failure_action):
    old, new, old_specs, plan = reconciliation_fixture()
    adapter = FakeAdapter(old_specs, fail_action=failure_action)
    coordinator = SyncCoordinator(old)
    with pytest.raises(ReconciliationApplyError):
        coordinator.apply_reconcile_plan(old.revision, new, plan, adapter)
    assert adapter.jobs == old_specs
    assert coordinator.current_snapshot is old


def test_restore_failure_removes_every_managed_job_and_signals_fail_closed():
    old, new, old_specs, plan = reconciliation_fixture()
    adapter = FakeAdapter(old_specs, fail_action="add", fail_forever=True)
    coordinator = SyncCoordinator(old)
    with pytest.raises(ReconciliationRestoreError):
        coordinator.apply_reconcile_plan(old.revision, new, plan, adapter)
    assert adapter.jobs == {}
    assert coordinator.current_snapshot is old
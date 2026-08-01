from datetime import datetime, timezone
from uuid import UUID

import pytest
from apscheduler.schedulers.background import BackgroundScheduler
from apscheduler.triggers.cron import CronTrigger
from apscheduler.triggers.date import DateTrigger

from agent.services.time_weaver.models import (
    CronFields,
    ManualExecution,
    ScheduleDetail,
    ScheduleGroup,
)
from agent.services.time_weaver.scheduler_adapter import ApSchedulerAdapter
from agent.services.time_weaver.sync_coordinator import JobSpec


DETAIL_ID = UUID("00000000-0000-0000-0000-000000000012")
NOW = datetime(2026, 8, 1, tzinfo=timezone.utc)


def group(hour="2"):
    detail = ScheduleDetail(
        detail_id=DETAIL_ID,
        schedule_name="copy",
        cron=CronFields(second="0"),
        is_error_stop=True,
        sequence=1,
        exec_sequence=1,
        retry_count=0,
        task_type="copy",
        source_path="/source",
        destination_path="/destination",
    )
    return ScheduleGroup(
        schedule_id=12,
        name="nightly",
        cron=CronFields(hour=hour, minute="0", second="0"),
        is_error_stop=True,
        details=(detail,),
    )


def schedule_spec(hour="2", fingerprint="schedule-fingerprint"):
    return JobSpec("schedule:12", "schedule", fingerprint, group(hour))


def manual_spec(fingerprint="manual-fingerprint"):
    value = ManualExecution(
        manual_id=41,
        schedule_id=12,
        detail_id=DETAIL_ID,
        status="wait",
        is_immediate=True,
        schedule_datetime=NOW,
        claimable=True,
    )
    return JobSpec("manual:41", "manual", fingerprint, value)


@pytest.fixture
def scheduler():
    value = BackgroundScheduler()
    value.start(paused=True)
    yield value
    value.shutdown(wait=False)


def test_schedule_add_update_remove_uses_namespaced_cron_job(scheduler):
    calls = []
    adapter = ApSchedulerAdapter(scheduler, calls.append)
    original = schedule_spec()

    adapter.add_job(original.key, original)
    job = scheduler.get_job("schedule:12")
    assert job is not None
    assert isinstance(job.trigger, CronTrigger)
    assert job.args == (12,)
    assert adapter.list_jobs() == {"schedule:12": original}

    updated = schedule_spec("3", "updated-fingerprint")
    adapter.update_job(updated.key, updated)
    assert scheduler.get_job("schedule:12") is not None
    assert adapter.list_jobs() == {"schedule:12": updated}

    adapter.remove_job("schedule:12")
    adapter.remove_job("schedule:12")
    assert scheduler.get_job("schedule:12") is None
    assert adapter.list_jobs() == {}


def test_manual_job_creates_separate_one_shot_and_retains_bookkeeping(scheduler):
    calls = []
    adapter = ApSchedulerAdapter(
        scheduler, lambda schedule_id: None, calls.append,
        manual_dispatch_delay=1, now=lambda: NOW,
    )
    spec = manual_spec()

    adapter.add_job(spec.key, spec)
    assert adapter.list_jobs() == {"manual:41": spec}
    assert scheduler.get_job("manual:41") is None
    job = scheduler.get_job("manual_dispatch:41")
    assert job is not None
    assert isinstance(job.trigger, DateTrigger)
    assert job.args == (41,)

    scheduler.remove_job("manual_dispatch:41")
    job.func(*job.args)
    assert calls == [41]

    adapter.mark_manual_attempted(41)
    adapter.schedule_pending_manuals()
    assert scheduler.get_job("manual_dispatch:41") is None
    adapter.release_manual(41)
    adapter.schedule_pending_manuals()
    assert scheduler.get_job("manual_dispatch:41") is not None

    adapter.remove_job(spec.key)
    assert scheduler.get_job("manual_dispatch:41") is None
    assert adapter.list_jobs() == {}


def test_same_manual_fingerprint_does_not_redispatch_while_claim_is_held(scheduler):
    adapter = ApSchedulerAdapter(
        scheduler, lambda schedule_id: None, lambda manual_id: None,
        now=lambda: NOW,
    )
    spec = manual_spec()
    adapter.add_job(spec.key, spec)
    adapter.mark_manual_attempted(41)
    scheduler.remove_job("manual_dispatch:41")
    adapter.schedule_pending_manuals()
    assert scheduler.get_job("manual_dispatch:41") is None

    adapter.update_job(spec.key, manual_spec("changed"))
    assert scheduler.get_job("manual_dispatch:41") is None


def test_list_jobs_returns_an_independent_exact_snapshot(scheduler):
    adapter = ApSchedulerAdapter(
        scheduler, lambda schedule_id: None, lambda manual_id: None
    )
    schedule = schedule_spec()
    manual = manual_spec()
    adapter.add_job(schedule.key, schedule)
    adapter.add_job(manual.key, manual)

    listed = adapter.list_jobs()
    assert listed == {schedule.key: schedule, manual.key: manual}
    listed.clear()
    assert adapter.list_jobs() == {schedule.key: schedule, manual.key: manual}
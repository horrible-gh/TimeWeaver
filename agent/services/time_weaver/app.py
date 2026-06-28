from util import jsonutil, string_util as su
from sqloader import DatabasePrototype, SQLoader
import LogAssist.log as Logger
from apscheduler.schedulers.background import BackgroundScheduler
from apscheduler.triggers.cron import CronTrigger
from threading import RLock
import datetime
from datetime import timedelta
import os
import getpass
import socket
from configure import time_weaver_config as twconfig, version
import sys
import services.time_weaver.task as task
import uuid

service_name = "time_weaver"
device_name = ""
db_instance: DatabasePrototype = None
sqloader: SQLoader = None

# Task list
task_list = {}
running_tasks = {}

# APScheduler
scheduler = BackgroundScheduler()
scheduler.start()

# Sequence definitions
group_execution_status = {}
# Track running and remaining tasks
task_completion_status = {}

# Reentrant lock
execution_lock = RLock()


def set_instances(_db_instance: DatabasePrototype, _sqloader: SQLoader):
    global db_instance, sqloader, device_name

    db_instance = _db_instance
    sqloader = _sqloader
    device_name = twconfig['device']
    device = db_instance.fetch_one(sqloader.load_sql(service_name, "get_device"), [device_name])

    if device is None:
        # Zero-touch onboarding: register this device automatically on first run.
        # The devices schema defaults new rows to 'active', so a fresh install
        # runs without any manual DB seeding. A device that an operator has
        # explicitly set to 'inactive' is left untouched (handled below).
        Logger.info(f"Device {device_name} not found; registering it automatically.")
        db_instance.execute_query(sqloader.load_sql(service_name, "insert_device"), [device_name])
        device = db_instance.fetch_one(sqloader.load_sql(service_name, "get_device"), [device_name])

    Logger.debug(f"device={device}")

    device_error = False
    msg = ""

    if device is None:
        msg = f"Device {device_name} could not be registered."
        device_error = True
    elif device['status'] != 'active':
        msg = f"Device {device_name} is inactive"
        device_error = True

    if device_error:
        error_handle(msg)
        sys.exit(1)

def task_rescheduler():
    scheduler.add_job(
        func=task_initializer
        , trigger='cron'
        , id="header"
        , replace_existing=True
        , year=f"{twconfig['reschedule']['year']}"
        , month=f"{twconfig['reschedule']['month']}"
        , day=f"{twconfig['reschedule']['day']}"
        , hour=f"{twconfig['reschedule']['hour']}"
        , minute=f"{twconfig['reschedule']['minute']}"
        , second=f"{twconfig['reschedule']['second']}"
    )


def task_initializer():
    global task_list

    with execution_lock:
        try:
            # 1. Fetch the latest schedule data from the database
            task_datas = db_instance.fetch_all(
                sqloader.load_sql(service_name, "get_tasks_all"), [device_name]
            )

            # 2. Build a new schedule dictionary from database data
            new_tasks = {}
            for task_data in task_datas:
                schedule_id = task_data['schedule_id']

                if schedule_id not in new_tasks:
                    new_tasks[schedule_id] = {
                        "name": task_data['name'],
                        "sg_year": task_data['sg_year'],
                        "sg_month": task_data['sg_month'],
                        "sg_day_of_week": task_data['sg_day_of_week'],
                        "sg_day": task_data['sg_day'],
                        "sg_hour": task_data['sg_hour'],
                        "sg_minute": task_data['sg_minute'],
                        "sg_second": task_data['sg_second'],
                        "sg_is_error_stop": task_data['sg_is_error_stop'],
                        "is_manual": task_data['is_manual'],
                        "is_immediate": task_data['is_immediate'],
                        "schedule_datetime": task_data['schedule_datetime'],
                        "details": {}
                    }

                detail_id = task_data['detail_id']
                new_tasks[schedule_id]['details'][detail_id] = {
                    "detail_id": task_data['detail_id'],
                    "is_manual": task_data['is_manual'],
                    "manual_id": task_data['manual_id'],
                    "me_status": task_data['me_status'],
                    "sd_year": task_data['sd_year'],
                    "sd_month": task_data['sd_month'],
                    "sd_day_of_week": task_data['sd_day_of_week'],
                    "sd_day": task_data['sd_day'],
                    "sd_hour": task_data['sd_hour'],
                    "sd_minute": task_data['sd_minute'],
                    "sd_second": task_data['sd_second'],
                    "sd_is_error_stop": task_data['sd_is_error_stop'],
                    "sequence": task_data['sequence'],
                    "retry_count": task_data['retry_count'],
                    "new_sequence": task_data['new_sequence'],

                    "task_type": task_data['task_type'],
                    "command": task_data['command'],
                    "archive_type": task_data['archive_type'],
                    "source_path": task_data['source_path'],
                    "error_on_missing_source": task_data['error_on_missing_source'],
                    "destination_path": task_data['destination_path'],
                    "date_format": task_data['date_format'],
                    "target_date_format": task_data['target_date_format'],
                    "destination_date_format": task_data['destination_date_format'],
                    "house_keep_days": task_data['house_keep_days'],
                }

            Logger.debug(f"Fetched new_tasks from DB: {new_tasks}")

            # 3. Compare current task_list with new_tasks
            existing_schedule_ids = set(task_list.keys())
            new_schedule_ids = set(new_tasks.keys())

            # 3.1. Tasks to add (schedule_id exists only in new_tasks)
            schedules_to_add = new_schedule_ids - existing_schedule_ids
            for schedule_id in schedules_to_add:
                task_list[schedule_id] = {
                    "name": new_tasks[schedule_id]['name'],
                    "sg_year": new_tasks[schedule_id]['sg_year'],
                    "sg_month": new_tasks[schedule_id]['sg_month'],
                    "sg_day_of_week": new_tasks[schedule_id]['sg_day_of_week'],
                    "sg_day": new_tasks[schedule_id]['sg_day'],
                    "sg_hour": new_tasks[schedule_id]['sg_hour'],
                    "sg_minute": new_tasks[schedule_id]['sg_minute'],
                    "sg_second": new_tasks[schedule_id]['sg_second'],
                    "sg_is_error_stop": new_tasks[schedule_id]['sg_is_error_stop'],
                    "sg_is_manual": new_tasks[schedule_id]['is_manual'],
                    "sg_is_immediate": new_tasks[schedule_id]['is_immediate'],
                    "sg_schedule_datetime": new_tasks[schedule_id]['schedule_datetime'],

                    "details": []
                }

                # Add task details
                for detail in new_tasks[schedule_id]['details'].values():
                    task_list[schedule_id]['details'].append(detail)

                Logger.debug(f"Added new schedule: {schedule_id}")

                # Register a new job in APScheduler
                try:
                    if su.is_false(new_tasks[schedule_id]['is_manual']):
                        scheduler.add_job(
                            func=start_group_execution,
                            trigger=CronTrigger(
                                year=new_tasks[schedule_id]['sg_year'],
                                month=new_tasks[schedule_id]['sg_month'],
                                day_of_week=new_tasks[schedule_id]['sg_day_of_week'],
                                day=new_tasks[schedule_id]['sg_day'],
                                hour=new_tasks[schedule_id]['sg_hour'],
                                minute=new_tasks[schedule_id]['sg_minute'],
                                second=new_tasks[schedule_id]['sg_second']
                            ),
                            args=[schedule_id],
                            id=str(schedule_id),
                            replace_existing=True,
                            misfire_grace_time=60  # Adjust as needed
                        )
                    else:
                        if su.is_true(new_tasks[schedule_id]['is_immediate']):
                            run_time = datetime.datetime.now() + timedelta(seconds=1)  # Run one second later
                        else:
                            run_time = new_tasks[schedule_id]['schedule_datetime']
                            if isinstance(run_time, str):
                                # MySQL datetime format: 'YYYY-MM-DD HH:MM:SS'
                                run_time = datetime.strptime(run_time, '%Y/%m/%d %H:%M:%S')
                        scheduler.add_job(
                            func=start_group_execution,
                            id=str(schedule_id),
                            args=[schedule_id],
                            trigger='date',
                            run_date=run_time,
                            replace_existing=True
                        )

                    Logger.info(f"Added new scheduler job: {schedule_id}")
                    Logger.debug(f'Group={schedule_id} / {task_list[schedule_id]["sg_year"]} / {task_list[schedule_id]["sg_month"]} / {task_list[schedule_id]["sg_day_of_week"]}  / {task_list[schedule_id]["sg_day"]} / {task_list[schedule_id]["sg_hour"]} / {task_list[schedule_id]["sg_minute"]} / {task_list[schedule_id]["sg_second"]}')
                except Exception as e:
                    error_handle(f"Error adding scheduler job {schedule_id}: {e}")

            # 3.2. Tasks to update (schedule_id exists in both dictionaries)
            schedules_to_update = new_schedule_ids & existing_schedule_ids
            for schedule_id in schedules_to_update:
                existing_task = task_list[schedule_id]
                new_task = new_tasks[schedule_id]

                if su.is_true(new_task['is_manual']):
                    # Immediate execution does not need updates
                    if su.is_true(new_task['is_immediate']):
                        continue

                    # For scheduled execution
                    run_time = new_task['schedule_datetime']
                    if isinstance(run_time, str):
                        run_time = datetime.datetime.strptime(run_time, '%Y/%m/%d %H:%M:%S')

                    # No update needed if the time has already passed
                    if run_time <= datetime.datetime.now():
                        continue

                    # Check schedule_datetime changes
                    existing_schedule_datetime = existing_task.get('sg_schedule_datetime')
                    if isinstance(existing_schedule_datetime, str):
                        existing_schedule_datetime = datetime.datetime.strptime(existing_schedule_datetime, '%Y/%m/%d %H:%M:%S')

                    # Reschedule if schedule_datetime changed
                    if existing_schedule_datetime != run_time:
                        task_list[schedule_id]['sg_schedule_datetime'] = new_task['schedule_datetime']
                        Logger.debug(f"Schedule {schedule_id} schedule_datetime changed from '{existing_schedule_datetime}' to '{run_time}'")

                        # Re-register APScheduler job
                        job_id = str(schedule_id)
                        try:
                            scheduler.remove_job(job_id)
                            Logger.info(f"Removed existing manual scheduler job: {job_id}")
                        except Exception as e:
                            Logger.debug(f"No existing job {job_id} to remove: {e}")

                        scheduler.add_job(
                            func=start_group_execution,
                            id=job_id,
                            args=[schedule_id],
                            trigger='date',
                            run_date=run_time,
                            replace_existing=True
                        )
                        Logger.info(f"Rescheduled manual job: {job_id} to {run_time}")

                    # For manual execution, update only details and continue
                    # (Skip the automatic schedule logic below)

                else:
                    # Handle automatic schedule
                    # Compare group-level fields
                    compare_fields = ['name', 'sg_year', 'sg_month', 'sg_day_of_week', 'sg_day', 'sg_hour', 'sg_minute', 'sg_second', 'sg_is_error_stop']
                    needs_update = False

                    for field in compare_fields:
                        if str(existing_task.get(field, '')) != str(new_task.get(field, '')):
                            needs_update = True
                            Logger.debug(f"Schedule {schedule_id} field '{field}' changed from '{existing_task.get(field, '')}' to '{new_task.get(field, '')}'")
                            break

                    if needs_update:
                        # Update group-level fields
                        for field in compare_fields:
                            task_list[schedule_id][field] = new_task[field]
                        Logger.debug(f"Updated schedule fields: {schedule_id}")

                        # Update APScheduler job
                        job_id = str(schedule_id)
                        try:
                            scheduler.remove_job(job_id)
                            Logger.info(f"Removed existing scheduler job: {job_id}")
                        except Exception as e:
                            Logger.debug(f"No existing job {job_id} to remove: {e}")

                        scheduler.add_job(
                            func=start_group_execution,
                            trigger=CronTrigger(
                                year=new_task['sg_year'],
                                month=new_task['sg_month'],
                                day_of_week=new_task['sg_day_of_week'],
                                day=new_task['sg_day'],
                                hour=new_task['sg_hour'],
                                minute=new_task['sg_minute'],
                                second=new_task['sg_second']
                            ),
                            args=[schedule_id],
                            id=job_id,
                            replace_existing=True,
                            misfire_grace_time=60
                        )
                        Logger.info(f"Added new scheduler job: {job_id}")

                # Compare and update task details (common to automatic and manual schedules)
                existing_details = {detail['detail_id']: detail for detail in existing_task['details']}
                new_details = new_task['details']

                # 3.2.1. Task details to add
                details_to_add = set(new_details.keys()) - set(existing_details.keys())
                for detail_id in details_to_add:
                    task_list[schedule_id]['details'].append(new_details[detail_id])
                    Logger.debug(f"Added new detail: {detail_id} to schedule: {schedule_id}")

                # 3.2.2. Task details to update
                details_to_update = set(new_details.keys()) & set(existing_details.keys())
                for detail_id in details_to_update:
                    existing_detail = existing_details[detail_id]
                    new_detail = new_details[detail_id]

                    detail_fields = [
                        'sd_year', 'sd_month', 'sd_day_of_week', 'sd_day', 'sd_hour',
                        'sd_minute', 'sd_second', 'sd_is_error_stop',
                        'sequence', 'retry_count', 'new_sequence','task_type', 'command', "archive_type",
                        "source_path", "error_on_missing_source", "destination_path", "date_format", "target_date_format",  "destination_date_format",
                        "house_keep_days"]
                    detail_needs_update = False

                    for field in detail_fields:
                        if str(existing_detail.get(field, '')) != str(new_detail.get(field, '')):
                            detail_needs_update = True
                            Logger.debug(f"Detail {detail_id} field '{field}' changed from '{existing_detail.get(field, '')}' to '{new_detail.get(field, '')}'")
                            break

                    if detail_needs_update:
                        # Update task detail fields
                        for field in detail_fields:
                            existing_detail[field] = new_detail[field]
                        Logger.info(f"Updated detail: {detail_id} in schedule: {schedule_id}")

                # 3.2.3. Task details to delete
                details_to_remove = set(existing_details.keys()) - set(new_details.keys())
                if details_to_remove:
                    task_list[schedule_id]['details'] = [
                        detail for detail in task_list[schedule_id]['details']
                        if detail['detail_id'] not in details_to_remove
                    ]
                    for detail_id in details_to_remove:
                        Logger.info(f"Removed detail: {detail_id} from schedule: {schedule_id}")


            # 3.3. Tasks to delete (schedule_id exists only in task_list)
            schedules_to_remove = existing_schedule_ids - new_schedule_ids
            for schedule_id in schedules_to_remove:
                del task_list[schedule_id]
                Logger.debug(f"Removed schedule: {schedule_id}")

                # Remove job from APScheduler
                try:
                    scheduler.remove_job(str(schedule_id))
                    Logger.info(f"Removed scheduler job: {schedule_id}")
                except Exception as e:
                    error_handle(f"Error removing scheduler job {schedule_id}: {e}")

            db_instance.execute_query(sqloader.load_sql("time_weaver", "update_device"), [version['version'], twconfig['device']])
            Logger.debug(f"Final task_list after initialization: {task_list}")

        except Exception as e:
            Logger.error(f"Process Error: {e}")

def start_group_execution(group_id):
    """
    Start group execution.
    - Set the task list by sequence in group_execution_status[group_id]
    - Call the first sequence to run, usually 1
    """

    global group_execution_status
    global task_completion_status

    with execution_lock:
        group_execution_status[group_id] = {}
        task_completion_status[group_id] = {}

        for t in task_list[group_id]['details']:
            seq = t['new_sequence']
            group_execution_status[group_id].setdefault(seq, []).append(t)


    Logger.debug(f"group_execution_status={group_execution_status}")
    Logger.debug(f"[start_group_execution] Starting Group={group_id}")

    # Assume execution starts from sequence 1
    execute_next_task(group_id, sequence=1)


def execute_next_task(group_id, sequence):
    """
    Register tasks for a specific sequence in the scheduler.
    """

    global group_execution_status
    global task_completion_status

    if sequence == 1:
        group_execution_status[group_id]['uuid'] = uuid.uuid4()
        Logger.debug(f"Allocation of UUID={group_execution_status[group_id]['uuid']}")


    is_error_stop_group = task_list[group_id]["sg_is_error_stop"]
    tasks = group_execution_status[group_id].get(sequence, [])
    Logger.debug(
        f"[execute_next_task] Found tasks for Seq={sequence} in Group={group_id}: {tasks}")

    if not tasks:
        Logger.debug(
            f"[execute_next_task] No tasks for Seq={sequence}. Nothing to schedule.")
        return

    with execution_lock:
        # When starting the next sequence, record that sequence task list
        task_completion_status[group_id][sequence] = [
            t['detail_id'] for t in tasks]

    Logger.debug(
        f"[execute_next_task] Scheduling Seq={sequence} for Group={group_id}, Tasks={[t['detail_id'] for t in tasks]}")

    for t in tasks:
        if su.is_false(t['is_manual']) or t['me_status'] == 'wait':
            run_time = datetime.datetime.now() + timedelta(seconds=1)  # Run one second later
            job_id = f"{group_id}_{t['detail_id']}_{sequence}"
            is_error_stop_detail = t['sd_is_error_stop']
            is_manual = t['is_manual']
            manual_id = None
            if su.is_true(is_manual):
                manual_id = t['manual_id']
            scheduler.add_job(
                func=execute_task,
                id=job_id,
                args=[t['detail_id'], group_id, sequence, t, is_error_stop_group, is_error_stop_detail, manual_id],
                trigger='date',
                run_date=run_time,
                replace_existing=True
            )

            Logger.debug(
                f"[execute_next_task] Detail={t['detail_id']} scheduled at {run_time}")
        else :
            Logger.debug(f"[execute_next_task] Detail={t['detail_id']} status is {t['me_status']}")
    Logger.debug(
        f"[execute_next_task] Current Scheduler jobs: {scheduler.get_jobs()}")


def execute_task(detail_id, group_id, sequence, task_data, is_error_stop_group, is_error_stop_detail, manual_id):
    """
    Execute an actual task.
    After completing sequence/task handling, schedule the next sequence if needed.
    """

    global group_execution_status
    global task_completion_status
    global running_tasks

    running_tasks[group_id] = True
    next_task_run = True

    Logger.debug(f"[execute_task] Task={task_data}")
    Logger.debug(f"[execute_task] Start: Group={group_id}, Detail={detail_id}, Seq={sequence}")

    # Update status for manual execution
    if manual_id:
        db_instance.execute_query(sqloader.load_sql(service_name, "update_manual_execution_status"), ['processing', manual_id])
        Logger.debug(f"[execute_task] Manual ID={manual_id}")

    # Execute task with exception handling
    start_time = datetime.datetime.now()
    result = -1  # Default to failure
    msg = None

    try:
        result, msg = task.task_run(task_data)
    except Exception as e:
        # Handle exceptions
        import traceback
        msg = f"Unexpected error during task execution:\n{traceback.format_exc()}"
        result = -1
        Logger.error(f"[execute_task] Exception occurred: {msg}")

    end_time = datetime.datetime.now()

    exec_uuid = group_execution_status[group_id]['uuid']
    log_group_id = group_id
    if manual_id:
        log_group_id = group_id.split('_')[1]

    try:
        param = [exec_uuid, log_group_id, detail_id, start_time, end_time, result, msg, get_environment_info(device_name)]
    except:
        Logger.warn("get_environment_info failed")
        param = [exec_uuid, log_group_id, detail_id, start_time, end_time, result, msg, ""]
    Logger.debug(f"param={param}")
    db_instance.execute_query(sqloader.load_sql(service_name, "insert_execute_log"), param)


    Logger.debug(f"[execute_task] result={result}, manual_id={manual_id}")

    if result != 0:
        Logger.debug(f"[execute_task] Entered error handling block")
        if manual_id:
            # Update status for manual execution
            Logger.debug(f"[execute_task] Updating manual_id={manual_id} to failed")
            db_instance.execute_query(sqloader.load_sql(service_name, "update_manual_execution_status"), ['failed', manual_id])
            Logger.debug(f"[DEBUG] Successfully updated manual_id={manual_id} to failed")
        if su.is_true(is_error_stop_group):
            # Update status for manual execution
            if manual_id:

                db_instance.execute_query(sqloader.load_sql(service_name, "update_manual_execution_group_status"), ['failed', group_id])
            else:
                db_instance.execute_query(sqloader.load_sql(service_name, "update_schedule_group_status"), ['error', group_id])
            next_task_run = False
            scheduler.remove_job(f"{group_id}")
        if su.is_true(is_error_stop_detail):
            db_instance.execute_query(sqloader.load_sql(service_name, "update_schedule_detail_status"), ['error', group_id, detail_id])
            exclude_task(group_id, detail_id)
        Logger.error(msg)
    else:
        # Success path; replaced elif with else
        if manual_id:
            db_instance.execute_query(sqloader.load_sql(service_name, "update_manual_execution_status"), ['done', manual_id])

    if next_task_run:
        with execution_lock:
            # 1) Mark the current task as completed
            task_completion_status[group_id][sequence].remove(detail_id)
            Logger.info(
                f"[execute_task] Done Task={detail_id}. Remaining in Seq {sequence}: {task_completion_status[group_id][sequence]}")

            # Check whether the sequence is complete
            sequence_finished = False
            if not task_completion_status[group_id][sequence]:
                Logger.debug(
                    f"[execute_task] Seq {sequence} in Group={group_id} is complete.")
                del task_completion_status[group_id][sequence]  # Delete sequence
                sequence_finished = True

            # 2) If the sequence is fully complete → Check and register the next sequence
            next_sequence = sequence + 1
            if sequence_finished and next_sequence in group_execution_status[group_id]:
                Logger.debug(
                    f"[execute_task] Preparing next sequence={next_sequence} for Group={group_id}")
                # RLock prevents deadlock even if execute_next_task() is called again here
                execute_next_task(group_id, next_sequence)

            # 3) Finish when all sequences in the current group are gone
            if not task_completion_status[group_id]:
                Logger.debug(
                    f"[execute_task] All tasks in Group={log_group_id} are complete.")
                running_tasks[group_id] = False
            #    scheduler.shutdown(wait=False)
            #    sys.exit(0)
    else:
        running_tasks[group_id] = False


def get_environment_info(device_name):
    return jsonutil.json_to_string({
        "host": socket.gethostname(),
        "ip": socket.gethostbyname(socket.gethostname()),
        "os": os.name,
        "user": getpass.getuser(),
        "device_name": device_name
    })

def exclude_task(group_id, exclude_detail_id):
    global task_list
    task_list[group_id]["details"] = [detail for detail in task_list[group_id]["details"] if detail["detail_id"] != exclude_detail_id]
    Logger.warn(f"[exclude_task] Group={group_id}, Detail={exclude_detail_id} Task was excluded")

def error_handle(msg):
    start_time = datetime.datetime.now()
    end_time = datetime.datetime.now()

    param = [-1, -1, -1, start_time, end_time, -1, msg, get_environment_info(device_name)]
    Logger.debug(f"Error param={param}")
    db_instance.execute_query(sqloader.load_sql(service_name, "insert_execute_log"), param)

# Unused function
def cleanup_stale_tasks(self, timeout_minutes=60):
    """
    Mark tasks stuck in processing beyond a threshold as failed
    """
    sql = """
        UPDATE manual_execution
        SET status = 'failed',
            error_message = 'Task timed out',
            completed_at = NOW()
        WHERE status = 'processing'
        AND started_at < DATE_SUB(NOW(), INTERVAL %s MINUTE)
    """
    with self.db_connection.cursor() as cursor:
        cursor.execute(sql, (timeout_minutes,))
        affected = cursor.rowcount
        self.db_connection.commit()
        Logger.info(f"Cleaned up {affected} stale tasks")

# Unused function
def retry_failed_task(self, task_id, max_retries=3):
    """
    Retry failed tasks
    """
    # Check the current retry count
    with self.db_connection.cursor() as cursor:
        cursor.execute(
            "SELECT retry_count FROM manual_execution WHERE id = %s",
            (task_id,)
        )
        result = cursor.fetchone()

        if result and result[0] < max_retries:
            # Retry is available
            cursor.execute("""
                UPDATE manual_execution
                SET status = 'pending',
                    retry_count = retry_count + 1
                WHERE id = %s
            """, (task_id,))
            self.db_connection.commit()
            return True
    return False

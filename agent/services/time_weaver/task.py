import traceback
import subprocess
import shutil
import os
import time
from datetime import datetime
import LogAssist.log as logger
import util.string_util as strutil

BASIC_DATE_FORMAT = '%Y%m%d'
CHECKED_SOURCE = 1

def execute_process(command):
    result_code = 0
    err_msg = None

    try:
        logger.debug(f"Executing command: {command}")

        # Set environment variables for UTF-8 encoding on Windows
        env = os.environ.copy()
        env['PYTHONIOENCODING'] = 'utf-8'

        result = subprocess.run(
            command,
            capture_output=True,
            text=True,
            check=True,
            shell=True,
            env=env,
            encoding='utf-8',
            errors='replace'
        )
        logger.debug(f"result={result.stdout}")
    except subprocess.CalledProcessError as e:
        err_msg = e.stderr if e.stderr else str(e)
        logger.error(f"Error executing command: {command}")
        logger.error(err_msg)
        result_code = -1
    except Exception as e:
        err_msg = str(e)
        logger.error(f"Unexpected error executing command: {command}")
        logger.error(err_msg)
        result_code = -1
    return result_code, err_msg


def house_keep(task_path, days):
    # Calculate the timestamp for the given number of days before now
    cutoff = time.time() - (days * 86400)

    # Get file list from the task path
    files = os.listdir(task_path)

    # For each file
    for file in files:
        file_path = os.path.join(task_path, file)
        # Get the file last-modified timestamp
        file_time = os.path.getmtime(file_path)

        # Delete the file if its last-modified time is older than the cutoff
        if file_time < cutoff:
            logger.info(f"Deleting {task_path}/{file}...")
            os.remove(file_path)


def task_run(task: dict):
    """
    Execute a task and return the result.

    Returns:
        tuple: (result_code, error_message)
        - result_code: 0(success), -1(failure)
        - error_message: Error message when an error occurs, otherwise None
    """
    result = 0
    msg = None

    house_keep_days = 0
    house_keep_path = ""
    date_format, target_date_format, destination_date_format, source_path, destination_path = check_paths(task)

    try:
        task_type = task.get("task_type", "")
        logger.debug(f"task_type={task_type}")

        if task_type == "archive":
            # Handle file archive task
            archive_type = task.get("archive_type", None)
            if archive_type in ('zip',):
                check_result = check_source_path(task, source_path, destination_path)
                if check_result == CHECKED_SOURCE:
                    # Process the task using that information
                    import zipfile
                    with zipfile.ZipFile(f"{destination_path}.{archive_type}", 'w', zipfile.ZIP_DEFLATED) as zipf:
                        for root, dirs, files in os.walk(source_path):
                            for file in files:
                                filepath = os.path.join(root, file)
                                arcname = os.path.relpath(filepath, source_path)
                                try:
                                    zipf.write(filepath, arcname)
                                except PermissionError as e:
                                    logger.warn(f"Skipping locked file: {filepath}")
                                    continue
                    logger.debug(f"File Archive Successful")
                elif check_result == -1:
                    raise ValueError(f"Source path error: {source_path}")
                else:
                    # check_result == 0: source file missing, skip
                    logger.info(f"Skipping archive task - source not found")
                    return 0, None
            else:
                raise ValueError(f"Unsupported archive_type: {archive_type}")


        elif task_type == "command":
            # Handle task using a command
            command = task.get("command", "").format(
                date=datetime.now().strftime(date_format)
            )

            # Process the task with the command
            result, msg = execute_process(command)
            if result != 0:
                # Return immediately on error instead of continuing, such as for house_keep
                raise Exception(f"Command execution failed: {command}\n{msg}")

        elif task_type == "copy":
            check_result = check_source_path(task, source_path, destination_path)
            if check_result == CHECKED_SOURCE:
                # Create the destination directory if missing
                dest_dir = os.path.dirname(destination_path)
                if dest_dir and not os.path.exists(dest_dir):
                    os.makedirs(dest_dir, exist_ok=True)

                shutil.copyfile(source_path, destination_path)
                logger.debug(f"File Copy Successful")
            elif check_result == -1:
                raise ValueError(f"Source path error: {source_path}")
            else:
                # check_result == 0: source file missing, skip
                logger.info(f"Skipping copy task - source not found")
                return 0, None

        house_keep_path = os.path.dirname(destination_path)

        if task_type == "housekeep":
            house_keep_path = destination_path
            house_keep_days = strutil.none_check(task.get("house_keep_days", 30), 30)
        else:
            house_keep_days = strutil.none_check(task.get("house_keep_days", 0), 0)

        if house_keep_days > 0 and house_keep_path != "":
            # Housekeeping
            time.sleep(1)  # Wait one second
            house_keep(house_keep_path, house_keep_days)

    except Exception as e:
        msg = traceback.format_exc()
        logger.error(f"Task failed with error:\n{msg}")
        result = -1

    return result, msg


def check_paths(task: dict):
    if task is None:
        raise ValueError("task data is None, make sure the correct data is delivered.")

    date_format = strutil.check_date_format(task.get("date_format", BASIC_DATE_FORMAT), BASIC_DATE_FORMAT)
    logger.debug(f"date_format={date_format}")

    target_date_format = strutil.check_date_format(task.get("target_date_format", BASIC_DATE_FORMAT), date_format)
    logger.debug(f"target_date_format={target_date_format}")

    destination_date_format = strutil.check_date_format(task.get("destination_date_format", BASIC_DATE_FORMAT), date_format)
    logger.debug(f"destination_date_format={destination_date_format}")

    # Default source_path and destination_path to an empty string when they are None
    source_path = task.get("source_path") or ""
    source_path = source_path.format(
        date=datetime.now().strftime(target_date_format)
    )
    logger.debug(f"After source_path={source_path}")

    destination_path = task.get("destination_path") or ""
    destination_path = destination_path.format(
        date=datetime.now().strftime(destination_date_format)
    )
    logger.debug(f"destination_path={destination_path}")

    return date_format, target_date_format, destination_date_format, source_path, destination_path


def check_source_path(task: dict, source_path, destination_path):
    """
    Check whether the source path exists.

    Returns:
        int: CHECKED_SOURCE(1): source exists
             -1: error when error_on_missing_source=True and source is missing
             0: skip when error_on_missing_source=False and source is missing
    """
    error_on_missing_source = task.get("error_on_missing_source", None)
    logger.debug(f"error_on_missing_source={error_on_missing_source}")
    exist_source = os.path.exists(source_path)
    logger.debug(f"exist_source={exist_source}")

    if exist_source:
        return CHECKED_SOURCE
    elif error_on_missing_source:
        logger.error(f"Source file or directory does not exist: {source_path}")
        return -1
    else:
        logger.info(f"Skip because file or directory is missing: {source_path}")
        return 0

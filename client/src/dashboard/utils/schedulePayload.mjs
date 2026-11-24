// Explicit write contracts: never send display metadata or audit identities.
export const cronDefaults = () => ({ year: '*', month: '*', day_of_week: '*', day: '*', hour: '*', minute: '*', second: '*' });
export const formBool = value => value === true || value === 1 || value === '1' || value === 'true';
const integer = value => value === '' || value == null ? null : Number(value);
const optional = value => value == null || String(value).trim() === '' ? null : String(value);
function cron(form) { return Object.fromEntries(Object.keys(cronDefaults()).map(k => [k, form[k] == null || form[k] === '' ? '*' : String(form[k])])); }
export function schedulePayload(form, edit) {
  const result = { ...cron(form), group_id: integer(form.group_id), name: form.name || '', target_device: integer(form.target_device), status: form.status, is_error_stop: formBool(form.is_error_stop) };
  if (edit) result.schedule_id = integer(form.schedule_id);
  return result;
}
export const isFileTask = type => type === 'copy' || type === 'archive';

// Switching type is explicit: never reuse a file destination as a cleanup
// directory, or silently enable deletion with an old retention/recursive value.
export function resetTaskType(type) {
  return {
    task_type: type, command: '', archive_type: type === 'archive' ? 'zip' : null,
    source_path: '', destination_path: '', error_on_missing_source: '1',
    house_keep_days: '', house_keep_recursive: false,
    date_format: null, target_date_format: null, destination_date_format: null,
  };
}

// Keep the same field contract as the agent. Use explicit nulls for unused
// fields (also on edits); preserve existing applicable date formats.
export function taskTypePayload(form) {
  const type = form.task_type;
  const file = isFileTask(type), housekeep = type === 'housekeep';
  return {
    command: type === 'command' ? optional(form.command) : null,
    archive_type: type === 'archive' ? optional(form.archive_type) : null,
    source_path: file ? optional(form.source_path) : null,
    destination_path: file || housekeep ? optional(form.destination_path) : null,
    error_on_missing_source: file && formBool(form.error_on_missing_source),
    house_keep_days: housekeep ? integer(form.house_keep_days) : null,
    house_keep_recursive: housekeep && formBool(form.house_keep_recursive),
    date_format: optional(form.date_format),
    target_date_format: file ? optional(form.target_date_format) : null,
    destination_date_format: file || housekeep ? optional(form.destination_date_format) : null,
  };
}
export function taskPayload(form, edit) {
  const result = { ...cron(form), schedule_id: integer(form.schedule_id), task_name: form.task_name || '', status: form.status, task_type: form.task_type,
    is_error_stop: formBool(form.is_error_stop), sequence: integer(form.sequence) ?? 0, retry_count: integer(form.retry_count) ?? 0,
    ...taskTypePayload(form) };
  if (edit) result.detail_id = form.detail_id;
  return result;
}
export function scheduleError(error) {
  const code = error?.response?.data?.detail?.code;
  return ['invalid_cron','invalid_task','invalid_archive_type','schedule_in_use','task_in_use','task_incomplete','resource_changed','device_group_mismatch','device_not_found','device_inactive','group_inactive','agent_upgrade_required','forbidden','invalid_request'].includes(code) ? `schedule_error_${code}` : 'access_failed';
}

// A list filter represents a schedule ID, not a possibly duplicated name.
// Retain the original ID type for the form's native/legacy select binding.
export function selectedTaskScheduleId(filter, available) {
  if (filter == null || String(filter) === '') return '';
  const match = available.find(s => String(s.schedule_id) === String(filter));
  return match?.schedule_id ?? '';
}

// Include schedules without tasks, while retaining rows during legacy/read
// failures. Distinguish identical names by group and ID.
export function taskScheduleOptions(schedules, tasks) {
  const byId = new Map();
  for (const row of tasks) {
    if (row.schedule_id == null || String(row.schedule_id) === '') continue;
    const id = String(row.schedule_id);
    byId.set(id, {id, name: row.schedule_group_name || id, group: row.group_name || ''});
  }
  for (const row of schedules) {
    if (row.schedule_id == null || String(row.schedule_id) === '') continue;
    const id = String(row.schedule_id);
    byId.set(id, {id, name: row.name || byId.get(id)?.name || id, group: row.group_name || ''});
  }
  const counts = new Map();
  for (const row of byId.values()) counts.set(row.name, (counts.get(row.name) || 0) + 1);
  return [...byId.values()].map(row => {
    const duplicate = counts.get(row.name) > 1;
    const label = duplicate ? (row.group ? row.group + ' / ' : '') + row.name + ' (#' + row.id + ')' : row.name;
    return {...row, duplicate, label};
  });
}

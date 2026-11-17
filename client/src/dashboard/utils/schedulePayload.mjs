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
export function taskPayload(form, edit) {
  const result = { ...cron(form), schedule_id: integer(form.schedule_id), task_name: form.task_name || '', status: form.status, task_type: form.task_type,
    is_error_stop: formBool(form.is_error_stop), error_on_missing_source: formBool(form.error_on_missing_source),
    sequence: integer(form.sequence) ?? 0, retry_count: integer(form.retry_count) ?? 0,
    house_keep_days: integer(form.house_keep_days), house_keep_recursive: form.task_type === 'housekeep' && formBool(form.house_keep_recursive) };
  for (const key of ['command','archive_type','source_path','destination_path','date_format','target_date_format','destination_date_format']) result[key] = optional(form[key]);
  if (edit) result.detail_id = form.detail_id;
  return result;
}
export function scheduleError(error) {
  const code = error?.response?.data?.detail?.code;
  return ['invalid_cron','invalid_task','invalid_archive_type','schedule_in_use','task_in_use','task_incomplete','resource_changed','device_group_mismatch','device_not_found','device_inactive','group_inactive','agent_upgrade_required','forbidden','invalid_request'].includes(code) ? `schedule_error_${code}` : 'access_failed';
}

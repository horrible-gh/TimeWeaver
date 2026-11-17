import { formBool } from './schedulePayload.mjs';

function invalid(code) { const error = new Error(code); error.code = code; throw error; }
function positiveID(value) {
  const id = value === '' || value == null ? NaN : Number(value);
  if (!Number.isSafeInteger(id) || id < 1 || id > 2147483647) invalid('invalid_request');
  return id;
}

// datetime-local is wall time in the browser's timezone. Never send it without
// an offset, and reject calendar/DST normalization instead of silently moving it.
export function localTimeToUTC(value) {
  const parts = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})(?::(\d{2}))?$/.exec(value || '');
  if (!parts) invalid('invalid_datetime');
  const [year, month, day, hour, minute, second] = parts.slice(1).map(part => Number(part || 0));
  if (year < 1000 || year > 9999) invalid('invalid_datetime');
  const date = new Date(year, month - 1, day, hour, minute, second);
  if (Number.isNaN(date.getTime()) || date.getFullYear() !== year || date.getMonth() !== month - 1 || date.getDate() !== day || date.getHours() !== hour || date.getMinutes() !== minute || date.getSeconds() !== second) invalid('invalid_datetime');
  return date.toISOString();
}

export function localTimeValue(value = new Date()) {
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) return '';
  const pad = n => String(n).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
}

function timing(form) {
  const result = { is_immediate: formBool(form.is_immediate), status: 'wait' };
  if (!result.is_immediate) {
    result.schedule_datetime = localTimeToUTC(form.schedule_datetime);
    if (Date.parse(result.schedule_datetime) <= Date.now()) invalid('invalid_datetime');
  }
  return result;
}

export function manualCreatePayload(form, target) {
  const result = timing(form);
  if (target === 'schedule') result.schedule_id = positiveID(form.schedule_id);
  else if (target === 'task' && typeof form.detail_id === 'string' && form.detail_id) result.detail_id = form.detail_id;
  else invalid('invalid_request');
  return result;
}

export function manualUpdatePayload(form) { return { ...timing(form), manual_id: positiveID(form.manual_id) }; }
export function manualCancelPayload(form) { return { manual_id: positiveID(form.manual_id) }; }

export function historyQuery(start, end) {
  const query = {};
  if (start) query.start_date = localTimeToUTC(start);
  if (end) query.end_date = localTimeToUTC(end);
  if (query.start_date && query.end_date) {
    const span = Date.parse(query.end_date) - Date.parse(query.start_date);
    if (span < 0 || span > 366 * 86400000) invalid('invalid_date_range');
  }
  return query;
}

export function manualError(error) {
  const code = error?.response?.data?.detail?.code || error?.code;
  return ['invalid_datetime', 'invalid_date_range', 'history_too_large', 'manual_not_pending', 'resource_changed', 'no_runnable_tasks', 'schedule_inactive', 'task_inactive', 'task_incomplete', 'too_many_tasks', 'device_inactive', 'group_inactive', 'agent_upgrade_required', 'forbidden', 'invalid_request'].includes(code) ? `manual_error_${code}` : 'access_failed';
}

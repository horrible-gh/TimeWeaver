import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { spawnSync } from 'node:child_process';
import { manualCreatePayload, manualUpdatePayload, manualCancelPayload, historyQuery, localTimeToUTC, manualError } from '../src/dashboard/utils/manualPayload.mjs';

test('queued writes exclude forged actors, display fields and target switching metadata', () => {
 const form = { manual_id: '18', schedule_id: '7', detail_id: 'task-id', is_immediate: '1', schedule_datetime: 'stale invalid date', status: 'processing', creator: 'forged', modifier: 'forged', can_cancel: true, name: 'display' };
 assert.deepEqual(manualCreatePayload(form, 'schedule'), { is_immediate: true, status: 'wait', schedule_id: 7 });
 assert.deepEqual(manualCreatePayload(form, 'task'), { is_immediate: true, status: 'wait', detail_id: 'task-id' });
 assert.deepEqual(manualUpdatePayload(form), { is_immediate: true, status: 'wait', manual_id: 18 });
 assert.deepEqual(manualCancelPayload(form), { manual_id: 18 });
 for (const id of ['', 0, -1, 1.5, 2147483648, 'oops']) assert.throws(() => manualCancelPayload({ manual_id: id }), { code: 'invalid_request' });
});

test('browser local reservations preserve wall time and seconds in UTC requests', () => {
 const moduleURL = new URL('../src/dashboard/utils/manualPayload.mjs', import.meta.url).href;
 const source = `import { manualCreatePayload, localTimeValue, localTimeToUTC } from ${JSON.stringify(moduleURL)};
 const reservation = manualCreatePayload({detail_id:'task',is_immediate:'0',schedule_datetime:'2099-06-02T09:10:11'},'task');
 console.log(JSON.stringify({reservation,local:localTimeValue(reservation.schedule_datetime),utc:localTimeToUTC('2099-06-02T09:10')}));`;
 const result = spawnSync(process.execPath, ['--input-type=module', '-e', source], { env: { ...process.env, TZ: 'Asia/Tokyo' }, encoding: 'utf8' });
 assert.equal(result.status, 0, result.stderr);
 const data = JSON.parse(result.stdout);
 assert.equal(data.reservation.is_immediate, false);
 assert.equal(data.reservation.schedule_datetime, '2099-06-02T00:10:11.000Z');
 assert.equal(data.local, '2099-06-02T09:10:11');
 assert.equal(data.utc, '2099-06-02T00:10:00.000Z');
});

test('invalid dates, past reservations and daylight-saving gaps do not silently move a run', () => {
 for (const value of ['', '2027-02-30T10:00', '2027-00-01T10:00', '2027-01-01T24:00', '2027-01-01T10:60', '2027-01-01T10:00Z']) assert.throws(() => localTimeToUTC(value), { code: 'invalid_datetime' });
 assert.throws(() => manualCreatePayload({ schedule_id: 1, is_immediate: false, schedule_datetime: '2000-01-01T00:00' }, 'schedule'), { code: 'invalid_datetime' });
 const moduleURL = new URL('../src/dashboard/utils/manualPayload.mjs', import.meta.url).href;
 const result = spawnSync(process.execPath, ['--input-type=module', '-e', `import { localTimeToUTC } from ${JSON.stringify(moduleURL)}; try { localTimeToUTC('2027-03-14T02:30'); process.exit(1); } catch(e) { console.log(e.code); }`], { env: { ...process.env, TZ: 'America/New_York' }, encoding: 'utf8' });
 assert.equal(result.status, 0, result.stderr);
 assert.equal(result.stdout.trim(), 'invalid_datetime');
});

test('history bounds support old dates, reject reversal and excessive ranges', () => {
 const query = historyQuery('2025-01-01T00:00:01', '2025-01-02T00:00:02');
 assert.match(query.start_date, /Z$/); assert.match(query.end_date, /Z$/);
 assert.equal(Date.parse(query.end_date) - Date.parse(query.start_date), 86401000);
 assert.deepEqual(historyQuery('', ''), {});
 assert.throws(() => historyQuery('2025-01-02T00:00', '2025-01-01T00:00'), { code: 'invalid_date_range' });
 assert.throws(() => historyQuery('2025-01-01T00:00', '2027-01-01T00:00'), { code: 'invalid_date_range' });
});

test('manual and history errors are translated without exposing arbitrary server text', async () => {
 const source = await readFile(new URL('../src/dashboard/i18n/manualAccess.js', import.meta.url), 'utf8');
 const { default: messages } = await import('data:text/javascript;base64,' + Buffer.from(source).toString('base64'));
 assert.deepEqual(Object.keys(messages.ko), Object.keys(messages.en));
 assert.deepEqual(Object.keys(messages.ja), Object.keys(messages.en));
 for (const key of Object.keys(messages.en).filter(key => key.startsWith('manual_error_'))) {
   const code = key.slice('manual_error_'.length);
   assert.equal(manualError({ response: { data: { detail: { code } } } }), key);
 }
 assert.equal(manualError({ response: { data: { detail: { code: 'SQL password=secret', message: 'private' } } } }), 'access_failed');
});

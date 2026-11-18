import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { dashboardCounts, latestSchedules, dashboardSection, loadDashboardSection } from '../src/dashboard/utils/dashboardData.mjs';

test('driver strings and JSON numbers aggregate as counts, not concatenated text', () => {
 const devices = dashboardCounts('devices', { active_count: '12', error_count: 2, inactive_count: '3' });
 assert.deepEqual(devices, { active: 12, error: 2, inactive: 3 });
 assert.equal(Object.values(devices).reduce((sum, value) => sum + value, 0), 17);
 assert.deepEqual(dashboardCounts('tasks', { pending_count: '0', in_progress_count: 1, completed_count: '8', error_count: 0 }), { in_progress: 1, pending: 0, completed: 8, error: 0 });
 for (const value of [null, undefined, '', ' ', true, -1, 1.5, 'NaN', 'Infinity', 9007199254740992]) {
   assert.throws(() => dashboardCounts('schedules', { active_count: value, error_count: 0, inactive_count: 0 }), /invalid_dashboard_data/);
 }
 for (const value of [null, [], {}, { active_count: 0, error_count: 0 }]) assert.throws(() => dashboardCounts('devices', value), /invalid_dashboard_data/);
});

test('loading deduplicates requests and only records an update after a successful response', async () => {
 const state = dashboardSection();
 let requests = 0;
 let resolve;
 const response = new Promise(done => { resolve = done; });
 const fetch = () => { requests++; return response; };
 const normalize = data => dashboardCounts('devices', data);
 const loading = loadDashboardSection(state, fetch, normalize, () => Date.parse('2026-11-18T03:04:05Z'));
 assert.equal(state.loading, true); assert.equal(state.data, null); assert.equal(state.updatedAt, null);
 assert.equal(await loadDashboardSection(state, fetch, normalize), false);
 assert.equal(requests, 1);
 resolve({ active_count: '2', error_count: 0, inactive_count: 0 });
 assert.equal(await loading, true);
 assert.deepEqual(state, { data: { active: 2, error: 0, inactive: 0 }, loading: false, error: false, updatedAt: '2026-11-18T03:04:05.000Z' });
});

test('a failed refresh clears stale counts/time and recovery can show a genuine zero', async () => {
 const state = dashboardSection();
 const normalize = data => dashboardCounts('schedules', data);
 await loadDashboardSection(state, async () => ({ active_count: 6, error_count: 1, inactive_count: 0 }), normalize);
 assert.equal(state.data.active, 6);
 assert.equal(await loadDashboardSection(state, async () => { throw new Error('private SQL failure'); }, normalize), false);
 assert.deepEqual(state, { data: null, loading: false, error: true, updatedAt: null });
 assert.equal(await loadDashboardSection(state, async () => ({ active_count: 0, error_count: 0, inactive_count: 0 }), normalize, () => Date.parse('2026-11-18T03:06:00Z')), true);
 assert.deepEqual(state.data, { active: 0, error: 0, inactive: 0 });
 assert.equal(state.error, false); assert.equal(state.updatedAt, '2026-11-18T03:06:00.000Z');
 // A malformed success body is also unavailable, never an all-zero chart.
 assert.equal(await loadDashboardSection(state, async () => ({}), normalize), false);
 assert.equal(state.data, null); assert.equal(state.updatedAt, null);
});

test('independent sections retain successful results when another endpoint fails', async () => {
 const first = dashboardSection(), second = dashboardSection();
 await Promise.all([
   loadDashboardSection(first, async () => ({ active_count: 3, error_count: 0, inactive_count: 0 }), value => dashboardCounts('devices', value)),
   loadDashboardSection(second, async () => { throw new Error('offline'); }, latestSchedules),
 ]);
 assert.equal(first.data.active, 3); assert.equal(first.error, false);
 assert.equal(second.data, null); assert.equal(second.error, true);
});

test('recent-list validation distinguishes an empty result from malformed data', () => {
 assert.deepEqual(latestSchedules([]), []);
 const rows = latestSchedules([{ schedule_id: '7', task_count: '2', group_start_time: '2026-11-18T01:00:00Z', group_end_time: null, custom_status: 'warning', error_summary: '<script>literal</script>' }]);
 assert.equal(rows[0].schedule_id, 7); assert.equal(rows[0].task_count, 2); assert.equal(rows[0].group_end_time, null);
 assert.equal(rows[0].error_summary, '<script>literal</script>'); // Vue text interpolation remains responsible for escaping.
 for (const value of [null, {}, [null], [{ schedule_id: 7 }], [{ schedule_id: 0, task_count: 1 }]]) assert.throws(() => latestSchedules(value), /invalid_dashboard_data/);
});

test('dashboard state labels and corrected metric descriptions cover all supported locales', async () => {
 const source = await readFile(new URL('../src/dashboard/i18n/chartsAccess.js', import.meta.url), 'utf8');
 const { default: messages } = await import('data:text/javascript;base64,' + Buffer.from(source).toString('base64'));
 assert.deepEqual(Object.keys(messages.ko), Object.keys(messages.en));
 assert.deepEqual(Object.keys(messages.ja), Object.keys(messages.en));
 for (const locale of Object.values(messages)) for (const text of Object.values(locale)) assert.equal(typeof text, 'string');
 assert.ok(!messages.en.metric_errors_label.includes('24h'));
 assert.ok(!messages.en.last_schedules_desc.includes('24'));
 assert.equal(messages.en.metric_devices_meta, 'Active {n}');
});

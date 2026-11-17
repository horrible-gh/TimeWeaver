import assert from 'node:assert/strict';
import test from 'node:test';
import { readFile } from 'node:fs/promises';
import { canEditGroup, devicesForGroup } from '../src/dashboard/utils/devicePermissions.mjs';
test('device editing hints require the group editor role, not just a group id', () => {
  const user = { role: 'user', group_id: 7, groups: [{ group_id: 7, role: 'editor' }, { group_id: 8, role: 'operator' }] };
  assert.equal(canEditGroup(user, 7), true);
  for (const group of [0, 8, 9, null, '', -1]) assert.equal(canEditGroup(user, group), false);
  assert.equal(canEditGroup({ role: 'admin' }, 0), true);
  assert.equal(canEditGroup({ ...user, groups: [] }, 7), false);
  assert.equal(canEditGroup({ ...user, must_change_password: true, role: 'admin' }, 7), false);
  assert.equal(canEditGroup({ ...user, groups: [{ group_id: 0, role: 'editor' }] }, 99), true);
});
test('reissue choices are active devices in the selected group, even for duplicate names', () => {
  const rows = [{ device_id: 1, group_id: 7, device_name: 'same', status: 'active' },
    { device_id: 2, group_id: 8, device_name: 'same', status: 'active' },
    { device_id: 3, group_id: 7, device_name: 'disabled', status: 'inactive' },
    { device_id: 4, device_name: 'unknown', status: 'active' }];
  assert.deepEqual(devicesForGroup(rows, 7).map(d => d.device_id), [1]);
  assert.deepEqual(devicesForGroup(rows, '8').map(d => d.device_id), [2]);
  assert.deepEqual(devicesForGroup(rows, null), []);
  assert.equal(rows.length, 4);
});
test('device access messages have matching locale keys', async () => {
  const source = await readFile(new URL('../src/dashboard/i18n/devicesAccess.js', import.meta.url), 'utf8');
  const { default: messages } = await import(`data:text/javascript;base64,${Buffer.from(source).toString('base64')}`);
  for (const language of ['ko', 'ja']) assert.deepEqual(Object.keys(messages[language]).sort(), Object.keys(messages.en).sort());
});

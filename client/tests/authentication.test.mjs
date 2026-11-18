import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { availableLoginMethods, loginPayload, userPayload } from '../authentication.mjs';
import { sessionMessages } from '../session.mjs';

test('OS login is offered only when explicitly advertised; local discovery fallback remains available', () => {
 for (const response of [undefined, null, {}, { methods: 'os' }, { methods: ['OS', 'ldap'] }, { methods: [true] }]) assert.deepEqual(availableLoginMethods(response), ['local']);
 assert.deepEqual(availableLoginMethods({ methods: ['local'] }), ['local']);
 assert.deepEqual(availableLoginMethods({ methods: ['os', 'local', 'os', 'unknown'] }), ['local', 'os']);
});

test('login sends the chosen method explicitly without changing standalone Python credentials', () => {
 assert.deepEqual(loginPayload('alice', 'password'), { username: 'alice', password: 'password', method: 'local' });
 assert.deepEqual(loginPayload('DOMAIN\\alice', 'password', 'os'), { username: 'DOMAIN\\alice', password: 'password', method: 'os' });
 assert.deepEqual(loginPayload('alice', 'password', 'os', false), { username: 'alice', password: 'password' });
 assert.throws(() => loginPayload('alice', 'password', 'OS'), /invalid_auth_method/);
 assert.throws(() => loginPayload('alice', 'password', 'fallback'), /invalid_auth_method/);
});

test('OS creation submits only an account name and application permissions, never native claims or passwords', () => {
 const form = { user_id: 'stale-local-id', name: 'Alice', email: 'alice@example.test', role: 'user', status: 'active', version: 0, auth_method: 'os', os_name: ' DOMAIN\\alice ', password: 'never transmit', subject: 'windows:sid:forged', native_id: '0', must_change_password: true };
 const groups = [{ group_id: 7, role: 'viewer' }];
 assert.deepEqual(userPayload(form, groups, true), { name: 'Alice', email: 'alice@example.test', role: 'user', status: 'active', groups, version: 0, auth_method: 'os', os_name: 'DOMAIN\\alice' });
 const edit = userPayload({ ...form, user_id: 'os_generated_id', version: 2 }, groups, false);
 assert.equal(edit.user_id, 'os_generated_id'); assert.equal(edit.version, 2);
 for (const key of ['password', 'subject', 'native_id', 'must_change_password']) assert.ok(!(key in edit));
 assert.equal(userPayload({ ...form, os_name: '' }, groups, false).os_name, '');
});

test('local creation retains its password contract and ordinary edits cannot accidentally reset a password', () => {
 const form = { user_id: 'local-user', name: 'User', email: '', role: 'user', status: 'active', version: 0, password: 'temporary-password', os_name: 'stale-os-name' };
 const create = userPayload(form, [], true);
 assert.equal(create.auth_method, 'local'); assert.equal(create.user_id, 'local-user'); assert.equal(create.password, 'temporary-password'); assert.ok(!('os_name' in create));
 const edit = userPayload({ ...form, version: 3 }, [], false);
 assert.equal(edit.version, 3); assert.ok(!('password' in edit)); assert.ok(!('os_name' in edit));
 assert.throws(() => userPayload({ ...form, auth_method: 'unknown' }, [], true), /invalid_auth_method/);
});

test('OS login and administration labels and errors exist in every supported locale', async () => {
 for (const locale of ['ko', 'ja']) assert.deepEqual(Object.keys(sessionMessages[locale]), Object.keys(sessionMessages.en));
 const source = await readFile(new URL('../src/dashboard/i18n/access.js', import.meta.url), 'utf8');
 const { default: messages } = await import('data:text/javascript;base64,' + Buffer.from(source).toString('base64'));
 for (const locale of ['ko', 'ja']) assert.deepEqual(Object.keys(messages[locale]), Object.keys(messages.en));
 for (const locale of Object.values(messages)) {
   for (const code of ['os_login_unavailable', 'os_account_not_found', 'os_identity_in_use', 'auth_method_immutable', 'os_password_managed', 'last_local_admin']) assert.ok(locale[`access_error_${code}`]);
 }
});

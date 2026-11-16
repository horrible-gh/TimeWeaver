import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createCookieSession } from '../session.mjs';
function storage() { const m = new Map([['access_token', 'legacy-token']]); return { setItem: (k,v) => m.set(k,v), getItem: k => m.get(k), removeItem: k => m.delete(k), values: m }; }
const reply = { user: { user_id: 'reader', groups: [{ group_id: 7, role: 'viewer' }] }, csrf_token: 'a'.repeat(64) };
test('CSRF stays in memory and a reload restores it via auth/me', async () => {
  const s = storage(); let calls = 0;
  const session = createCookieSession(async () => { calls++; return reply; }, s);
  assert.equal(s.getItem('access_token'), undefined);
  assert.deepEqual(await session.headers(), { 'X-CSRF-Token': reply.csrf_token });
  await session.headers(); assert.equal(calls, 1);
  assert.equal(s.getItem('user'), JSON.stringify(reply.user));
  assert.ok(!JSON.stringify([...s.values]).includes(reply.csrf_token));
  const reloaded = createCookieSession(async () => { calls++; return reply; }, s);
  await reloaded.headers(); assert.equal(calls, 2);
});
test('parallel mutations share bootstrap, failures are retriable', async () => {
  let calls = 0;
  const s = createCookieSession(async () => { calls++; if (calls === 1) throw new Error('offline'); return reply; }, storage());
  const first = await Promise.allSettled([s.headers(), s.headers()]);
  assert.equal(calls, 1); assert.ok(first.every(x => x.status === 'rejected'));
  assert.deepEqual(await s.headers(), { 'X-CSRF-Token': reply.csrf_token });
  assert.equal(calls, 2);
});
test('an old in-flight me response cannot restore a signed-out session', async () => {
  let resolve; const store = storage();
  const s = createCookieSession(() => new Promise(r => { resolve = r; }), store);
  const pending = s.read(); s.clear(); resolve(reply);
  await assert.rejects(pending, /Session changed/);
  assert.equal(store.getItem('user'), undefined);
});
test('JWT-shaped or malformed responses never become cookie sessions', () => {
  const s = createCookieSession(async () => reply, storage());
  assert.throws(() => s.accept({ user: reply.user, access_token: 'jwt' }));
  assert.throws(() => s.accept({ user: {}, csrf_token: reply.csrf_token }));
});

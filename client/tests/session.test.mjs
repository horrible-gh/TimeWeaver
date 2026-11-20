import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createCookieSession, isUnauthenticated, signalSessionChange, watchCookieSession } from '../session.mjs';
function storage() { const m = new Map([['access_token', 'legacy-token']]); return { setItem: (k,v) => m.set(k,v), getItem: k => m.get(k), removeItem: k => m.delete(k), values: m }; }
const reply = { user: { user_id: 'reader', groups: [{ group_id: 7, role: 'viewer' }] }, csrf_token: 'a'.repeat(64), session: { remembered: false, expires_at: 1800000000, refresh_after: 0, refresh_after_seconds: 0 } };
const remembered = (seconds = 43200) => ({ ...reply, session: { remembered: true, expires_at: 1800000000, refresh_after: 1799990000, refresh_after_seconds: seconds } });
const failure = (status, code) => ({ response: { status, data: { detail: { code } } } });
const deferred = () => { let resolve, reject; const promise = new Promise((res, rej) => { resolve = res; reject = rej; }); return { promise, resolve, reject }; };
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
  for (const session of [undefined, {}, { ...reply.session, remembered: 'true' }, { ...reply.session, refresh_after_seconds: -1 }, { ...reply.session, refresh_after: 1 }, { ...remembered().session, expires_at: NaN }]) {
    assert.throws(() => s.accept({ ...reply, session }), /Invalid session response/);
  }
});

test('remembered bootstrap shares one read and renewal before parallel protected requests', async () => {
  const refresh = deferred(), calls = [];
  const s = createCookieSession(async (url, options) => {
    calls.push({ url, options });
    if (url === '/auth/me') return remembered(0);
    if (url === '/auth/refresh') return refresh.promise;
    return { ok: true };
  }, storage(), { now: () => 0 });
  const read = s.read(), mutation = s.request('/dashboard/groups', { method: 'post', data: { name: 'One' } }), headers = s.headers();
  await Promise.resolve(); await Promise.resolve();
  assert.deepEqual(calls.map(call => call.url), ['/auth/me', '/auth/refresh']);
  assert.equal(calls[1].options.headers['X-CSRF-Token'], reply.csrf_token);
  refresh.resolve(remembered());
  await Promise.all([read, mutation, headers]);
  assert.deepEqual(calls.map(call => call.url), ['/auth/me', '/auth/refresh', '/dashboard/groups']);
  assert.equal(calls[2].options.headers['X-CSRF-Token'], reply.csrf_token);
});

test('renewal uses server-relative monotonic delay and never treats a local timestamp as expiry', async () => {
  let clock = 100, refreshes = 0;
  const s = createCookieSession(async url => {
    if (url === '/auth/refresh') { refreshes++; return remembered(10); }
    return { ok: true };
  }, storage(), { now: () => clock });
  // The absolute dates are deliberately long past. They are not authorization
  // decisions and must not defeat the server's relative renewal schedule.
  s.accept({ ...remembered(10), session: { ...remembered(10).session, expires_at: 1, refresh_after: 1 } });
  clock = 10099; await s.headers(); assert.equal(refreshes, 0);
  clock = 10100; await s.headers(); assert.equal(refreshes, 1);
  clock = 10101; await s.request('/dashboard/groups', { method: 'get' }); assert.equal(refreshes, 1);
});

test('ordinary sessions do not renew or poll between protected actions', async () => {
  let clock = 0; const calls = [];
  const s = createCookieSession(async url => { calls.push(url); return url === '/auth/me' ? reply : { ok: true }; }, storage(), { now: () => clock });
  await s.headers(); clock = 31 * 86400000;
  await s.headers(); await s.request('/dashboard/groups', { method: 'get' });
  assert.deepEqual(calls, ['/auth/me', '/dashboard/groups']);
});

test('a lost renewal response preserves state and allows explicit predecessor recovery', async () => {
  const store = storage(), calls = []; let attempts = 0, ended = 0;
  const s = createCookieSession(async url => {
    calls.push(url);
    if (url === '/auth/me') return remembered(0);
    if (url === '/auth/refresh') {
      if (++attempts === 1) throw failure(503, 'unavailable');
      return remembered();
    }
    return { ok: true };
  }, store, { now: () => 0, onUnauthenticated: () => { ended++; } });
  await assert.rejects(s.request('/dashboard/groups', { method: 'post', data: { name: 'One' } }));
  assert.equal(store.getItem('user'), JSON.stringify(reply.user)); assert.equal(ended, 0);
  assert.deepEqual(calls, ['/auth/me', '/auth/refresh']);
  // The server can return the same successor for the predecessor cookie after
  // a lost response. The application action itself has not been sent yet.
  await s.request('/dashboard/groups', { method: 'post', data: { name: 'One' } });
  assert.deepEqual(calls, ['/auth/me', '/auth/refresh', '/auth/refresh', '/dashboard/groups']);
});

test('only definitive unauthenticated ends a session; no dispatched mutation is replayed', async () => {
  for (const error of [new Error('offline'), failure(503, 'unavailable'), failure(403, 'forbidden'), failure(401, 'invalid_credentials'), failure(401, 'unauthenticated')]) {
    const store = storage(); let requests = 0, ended = 0;
    const s = createCookieSession(async () => { requests++; throw error; }, store, { onUnauthenticated: () => { ended++; } });
    s.accept(reply);
    await assert.rejects(s.request('/dashboard/groups', { method: 'post', data: { name: 'One' } }), actual => actual === error);
    assert.equal(requests, 1);
    assert.equal(ended, isUnauthenticated(error) ? 1 : 0);
    assert.equal(store.getItem('user'), isUnauthenticated(error) ? undefined : JSON.stringify(reply.user));
  }
});

test('bootstrap unauthorized reaches every waiter once without dispatching mutations', async () => {
  const error = failure(401, 'unauthenticated'); let calls = 0, ended = 0;
  const s = createCookieSession(async () => { calls++; throw error; }, storage(), { onUnauthenticated: () => { ended++; } });
  const results = await Promise.allSettled([s.request('/logout', { method: 'post' }), s.headers(), s.read()]);
  assert.equal(calls, 1); assert.equal(ended, 1);
  assert.ok(results.every(result => result.status === 'rejected' && result.reason === error));
});

test('late renewal, bootstrap and mutation responses cannot restore or revoke a newer session', async () => {
  for (const kind of ['refresh', 'read', 'mutation']) {
    for (const rejects of [false, true]) {
      const wait = deferred(), store = storage(); let ended = 0;
      const s = createCookieSession(() => wait.promise, store, { now: () => 0, onUnauthenticated: () => { ended++; } });
      s.accept(kind === 'refresh' ? remembered(0) : reply);
      const pending = kind === 'refresh' ? s.ready() : kind === 'read' ? s.read() : s.request('/dashboard/groups', { method: 'post' });
      await Promise.resolve();
      const next = { ...reply, user: { user_id: 'new-user' } };
      const epoch = s.clear(); s.accept(next, epoch);
      if (rejects) wait.reject(failure(401, 'unauthenticated'));
      else wait.resolve(reply);
      await assert.rejects(pending, { code: 'session_changed' });
      assert.equal(ended, 0); assert.equal(store.getItem('user'), JSON.stringify(next.user));
    }
  }
});

test('a pending sign-in response cannot be accepted after an explicit logout', () => {
  const store = storage(), s = createCookieSession(async () => reply, store);
  const loginEpoch = s.clear(); s.clear();
  assert.throws(() => s.accept(reply, loginEpoch), { code: 'session_changed' });
  assert.equal(store.getItem('user'), undefined);
});

test('clearing a ready session still cancels headers before a mutation can use them', async () => {
  const s = createCookieSession(async () => reply, storage());
  s.accept(reply);
  const headers = s.headers(); s.clear();
  await assert.rejects(headers, { code: 'session_changed' });
});

function eventTarget() {
  const listeners = new Map();
  return {
    addEventListener(type, callback) { if (!listeners.has(type)) listeners.set(type, new Set()); listeners.get(type).add(callback); },
    removeEventListener(type, callback) { listeners.get(type)?.delete(callback); },
    emit(type, event = {}) { for (const callback of listeners.get(type) || []) callback(event); },
  };
}

test('wake checks coalesce while visible and never schedule background activity', async () => {
  const window = eventTarget(), document = { ...eventTarget(), visibilityState: 'hidden' }, wait = deferred();
  let resumes = 0, enabled = true;
  const stop = watchCookieSession({ session: { clear() {} }, storage: storage(), key: 'session:test', window, document,
    onResume: () => { resumes++; return wait.promise; }, onLogout() {}, onLogin() {}, shouldResume: () => enabled });
  window.emit('focus'); window.emit('online'); document.emit('visibilitychange');
  await Promise.resolve(); assert.equal(resumes, 0);
  document.visibilityState = 'visible';
  window.emit('pageshow', { persisted: false }); await Promise.resolve(); assert.equal(resumes, 0);
  window.emit('pageshow', { persisted: true }); window.emit('focus'); window.emit('online'); document.emit('visibilitychange');
  await Promise.resolve(); assert.equal(resumes, 1);
  wait.resolve(); await new Promise(resolve => setImmediate(resolve));
  enabled = false; window.emit('online'); await Promise.resolve(); assert.equal(resumes, 1);
  enabled = true; window.emit('online'); await Promise.resolve(); assert.equal(resumes, 2);
  await new Promise(resolve => setImmediate(resolve)); stop();
  window.emit('focus'); document.emit('visibilitychange'); await Promise.resolve(); assert.equal(resumes, 2);
});

test('cross-tab changes are context-scoped, secret-free and invalidate pending work', async () => {
  const store = storage(), window = eventTarget(), document = eventTarget(), wait = deferred();
  const s = createCookieSession(() => wait.promise, store);
  let loggedOut = 0, loggedIn = 0;
  watchCookieSession({ session: s, storage: store, key: 'session:one', window, document,
    onResume() {}, onLogout: () => { loggedOut++; }, onLogin: () => { loggedIn++; } });
  const pending = s.read();
  signalSessionChange(store, 'session:one', 'logout');
  const value = store.getItem('session:one');
  assert.deepEqual(Object.keys(JSON.parse(value)), ['type', 'nonce']);
  assert.ok(!value.includes(reply.csrf_token));
  for (const event of [{ key: 'session:two', newValue: value }, { key: 'session:one', newValue: '{bad' }, { key: 'session:one', newValue: value, storageArea: storage() }]) window.emit('storage', event);
  assert.equal(loggedOut, 0);
  window.emit('storage', { key: 'session:one', newValue: value, storageArea: store });
  wait.resolve(reply); await assert.rejects(pending, { code: 'session_changed' });
  assert.equal(loggedOut, 1); assert.equal(store.getItem('user'), undefined);
  signalSessionChange(store, 'session:one', 'login');
  window.emit('storage', { key: 'session:one', newValue: store.getItem('session:one') });
  assert.equal(loggedIn, 1);
});

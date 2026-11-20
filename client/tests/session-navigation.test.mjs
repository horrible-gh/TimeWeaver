import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import vm from 'node:vm';
import { isUnauthenticated, isSessionChanged } from '../session.mjs';

const source = (await readFile(new URL('../src/dashboard/router/index.js', import.meta.url), 'utf8'))
  .replace(/^import .*$/gm, '').replace('export default router;', '');

// Exercise the real route guard and recovery watcher with deterministic session
// results. Vue and browser rendering are covered by the browser integration job.
function navigation() {
  const replacements = [], sessionUnavailable = { value: false }, sessionUser = { value: null };
  let guard, watcher, error = null;
  const router = {
    beforeEach(callback) { guard = callback; },
    replace(path) { replacements.push(path); return Promise.resolve(); },
  };
  const user = { user_id: 'reader', role: 'user', must_change_password: false };
  const sandbox = {
    createRouter: () => router, createWebHistory() {}, watch: (_, callback) => { watcher = callback; },
    sessionMode: true, sessionUnavailable, sessionUser, isUnauthenticated, isSessionChanged,
    ensureSession: async () => {
      if (error) { sessionUnavailable.value = true; throw error; }
      sessionUser.value = user; sessionUnavailable.value = false;
      return { user };
    },
    logout: async () => {}, window: { location: { href: '' } },
    localStorage: { getItem: () => null },
  };
  for (const name of ['DashboardForm', 'ScheduleHistoryForm', 'GroupsOverviewForm', 'DevicesOverviewForm', 'SchedulesOverviewForm', 'TasksOverviewForm', 'ManualExecutionOverviewForm', 'UsersOverview']) sandbox[name] = {};
  vm.runInNewContext(source, sandbox);
  return {
    replacements,
    async go(path, failure = null) {
      error = failure;
      const result = await guard({ path: path.split(/[?#]/)[0], fullPath: path, meta: {} });
      watcher(sessionUnavailable.value);
      return result;
    },
    wake(failure = null) {
      sessionUser.value = failure ? null : user;
      sessionUnavailable.value = Boolean(failure);
      watcher(sessionUnavailable.value);
    },
  };
}

test('verified wake recovery resumes the exact destination aborted during startup', async () => {
  const n = navigation();
  assert.equal(await n.go('/groups?search=one#details', new Error('offline')), false);
  assert.deepEqual(n.replacements, []);
  n.wake(); n.wake();
  assert.deepEqual(n.replacements, ['/groups?search=one#details']);
});

test('a new user navigation cancels an older failed destination', async () => {
  const n = navigation();
  await n.go('/groups', new Error('offline'));
  assert.equal(await n.go('/devices'), true);
  assert.deepEqual(n.replacements, []);
});

test('ordinary wake recovery does not reload or navigate away from a mounted form', async () => {
  const n = navigation();
  assert.equal(await n.go('/schedules'), true);
  n.wake(new Error('offline')); n.wake();
  assert.deepEqual(n.replacements, []);
});

test('revoked or superseded sessions do not schedule an abandoned route for recovery', async () => {
  for (const error of [{ response: { status: 401, data: { detail: { code: 'unauthenticated' } } } }, { code: 'session_changed' }]) {
    const n = navigation();
    assert.equal(await n.go('/groups', error), false);
    n.wake();
    assert.deepEqual(n.replacements, []);
  }
});

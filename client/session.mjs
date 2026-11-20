export const isUnauthenticated = error => error?.response?.status === 401 &&
  error.response.data?.detail?.code === 'unauthenticated';

export const isSessionChanged = error => error?.code === 'session_changed';

// Cookie credentials are HttpOnly. Only CSRF, renewal timing and display data
// enter JavaScript. The server alone decides whether a session is still valid.
export function createCookieSession(request, storage, options = {}) {
  const now = options.now || (() => performance.now());
  let csrf = '', current = null, pending = null, generation = 0, refreshAt = Infinity;
  storage.removeItem('access_token');
  function clear() {
    generation += 1;
    csrf = ''; current = null; pending = null; refreshAt = Infinity;
    storage.removeItem('access_token'); storage.removeItem('user');
    return generation;
  }
  function unchanged(epoch) {
    if (epoch !== generation) {
      const error = new Error('Session changed');
      error.code = 'session_changed';
      throw error;
    }
  }
  function accept(data, epoch = generation) {
    unchanged(epoch);
    const metadata = data?.session;
    if (!data?.user?.user_id || !/^[0-9a-f]{64}$/.test(data.csrf_token || '') ||
        typeof metadata?.remembered !== 'boolean' ||
        !Number.isSafeInteger(metadata.expires_at) || metadata.expires_at <= 0 ||
        !Number.isSafeInteger(metadata.refresh_after) || metadata.refresh_after < 0 ||
        !Number.isSafeInteger(metadata.refresh_after_seconds) || metadata.refresh_after_seconds < 0 ||
        (!metadata.remembered && (metadata.refresh_after !== 0 || metadata.refresh_after_seconds !== 0))) {
      throw new Error('Invalid session response');
    }
    csrf = data.csrf_token;
    current = data;
    refreshAt = metadata.remembered ? now() + metadata.refresh_after_seconds * 1000 : Infinity;
    storage.removeItem('access_token');
    storage.setItem('user', JSON.stringify(data.user));
    return data;
  }

  async function checked(operation, epoch) {
    try {
      const result = await operation();
      unchanged(epoch);
      return result;
    } catch (error) {
      // An old response may finish after logout or a new sign-in. It must not
      // invalidate the newer session or repopulate its display cache.
      unchanged(epoch);
      if (isUnauthenticated(error)) {
        clear();
        options.onUnauthenticated?.();
      }
      throw error;
    }
  }

  function singleFlight(operation) {
    if (!pending) {
      const epoch = generation;
      const attempt = checked(() => operation(epoch), epoch);
      pending = attempt;
      attempt.finally(() => { if (pending === attempt) pending = null; }).catch(() => {});
    }
    return pending;
  }

  async function refresh(epoch) {
    const data = await request('/auth/refresh', {
      method: 'post', headers: { 'X-CSRF-Token': csrf }, timeout: 15000,
    });
    return accept(data, epoch);
  }

  function read() {
    return singleFlight(async epoch => {
      const data = accept(await request('/auth/me', { method: 'get', timeout: 15000 }), epoch);
      return now() >= refreshAt ? refresh(epoch) : data;
    });
  }

  function ready() {
    if (pending) return pending;
    if (!current) return read();
    if (now() >= refreshAt) return singleFlight(refresh);
    return Promise.resolve(current);
  }

  async function headers() {
    const epoch = generation;
    await ready();
    unchanged(epoch);
    return { 'X-CSRF-Token': csrf };
  }

  // Readiness happens before a protected request. Once dispatched, a mutation
  // is never replayed: a timeout or lost response may already have committed it.
  async function authorized(url, requestOptions = {}) {
    const epoch = generation;
    await ready();
    unchanged(epoch);
    const requestHeaders = { ...requestOptions.headers };
    if (!['get', 'head'].includes((requestOptions.method || 'get').toLowerCase())) {
      requestHeaders['X-CSRF-Token'] = csrf;
    }
    return checked(() => request(url, { ...requestOptions, headers: requestHeaders }), epoch);
  }

  return { accept, clear, read, ready, headers, request: authorized };
}

// This signal contains no session credential. It only makes another tab forget
// stale state after an explicit sign-in, sign-out or password change.
export function signalSessionChange(storage, key, type) {
  try {
    storage.setItem(key, JSON.stringify({ type, nonce: `${Date.now()}-${Math.random()}` }));
  } catch { /* A full display cache must not make a completed logout fail. */ }
}

export function watchCookieSession({ session, storage, key, window, document, onResume, onLogout, onLogin, shouldResume = () => true }) {
  let waking = null;
  const wake = () => {
    if (document.visibilityState === 'hidden' || !shouldResume() || waking) return;
    const attempt = Promise.resolve().then(onResume);
    waking = attempt;
    attempt.catch(() => {}).finally(() => { if (waking === attempt) waking = null; });
  };
  const changed = event => {
    if (event.key !== key || !event.newValue || (event.storageArea && event.storageArea !== storage)) return;
    let type;
    try { ({ type } = JSON.parse(event.newValue)); } catch { return; }
    if (!['login', 'logout'].includes(type)) return;
    session.clear();
    if (type === 'logout') onLogout();
    else onLogin();
  };
  const restored = event => { if (event.persisted) wake(); };
  window.addEventListener('storage', changed);
  window.addEventListener('focus', wake);
  window.addEventListener('pageshow', restored);
  window.addEventListener('online', wake);
  document.addEventListener('visibilitychange', wake);
  return () => {
    window.removeEventListener('storage', changed);
    window.removeEventListener('focus', wake);
    window.removeEventListener('pageshow', restored);
    window.removeEventListener('online', wake);
    document.removeEventListener('visibilitychange', wake);
  };
}

export const sessionMessages = {
  en: { method: 'Sign-in method', local: 'Local account', os: 'OS account', osAccount: 'OS account name', osUnavailable: 'OS login is unavailable on this server.', remember: 'Keep me signed in', retry: 'Retry', change: 'Change password', current: 'Current password', next: 'New password (12+ characters)', confirm: 'Confirm password', mismatch: 'Passwords do not match.', required: 'Change your initial password to continue.', changed: 'Password changed. Sign in again.', unavailable: 'Unable to connect. Try again.', limited: 'Too many attempts. Try again in five minutes.', logout: 'Sign out failed. Try again.', policy: 'Use a different password with at least 12 characters.' },
  ko: { method: '로그인 방식', local: '자체 계정', os: 'OS 계정', osAccount: 'OS 계정 이름', osUnavailable: '이 서버에서 OS 로그인을 사용할 수 없습니다.', remember: '로그인 유지', retry: '다시 시도', change: '비밀번호 변경', current: '현재 비밀번호', next: '새 비밀번호 (12자 이상)', confirm: '비밀번호 확인', mismatch: '비밀번호가 일치하지 않습니다.', required: '초기 비밀번호를 변경해 주세요.', changed: '변경했습니다. 다시 로그인해 주세요.', unavailable: '연결할 수 없습니다. 다시 시도해 주세요.', limited: '시도가 많습니다. 5분 후 다시 시도해 주세요.', logout: '로그아웃하지 못했습니다. 다시 시도해 주세요.', policy: '기존과 다른 12자 이상의 비밀번호를 입력해 주세요.' },
  ja: { method: 'ログイン方法', local: 'ローカルアカウント', os: 'OSアカウント', osAccount: 'OSアカウント名', osUnavailable: 'このサーバーではOSログインを利用できません。', remember: 'ログイン状態を保持', retry: '再試行', change: 'パスワード変更', current: '現在のパスワード', next: '新しいパスワード（12文字以上）', confirm: 'パスワードの確認', mismatch: 'パスワードが一致しません。', required: '初期パスワードを変更してください。', changed: '変更しました。再度ログインしてください。', unavailable: '接続できません。もう一度お試しください。', limited: '試行回数が多すぎます。5分後にお試しください。', logout: 'ログアウトできませんでした。もう一度お試しください。', policy: '現在と異なる12文字以上のパスワードを入力してください。' },
};

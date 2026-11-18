// Cookie credentials are HttpOnly. Only CSRF and display data enter JavaScript.
export function createCookieSession(request, storage) {
  let csrf = '', pending = null, generation = 0;
  storage.removeItem('access_token');
  function clear() {
    generation += 1;
    csrf = ''; pending = null;
    storage.removeItem('access_token'); storage.removeItem('user');
  }
  function accept(data) {
    if (!data?.user?.user_id || !/^[0-9a-f]{64}$/.test(data.csrf_token || '')) {
      throw new Error('Invalid session response');
    }
    csrf = data.csrf_token;
    storage.removeItem('access_token');
    storage.setItem('user', JSON.stringify(data.user));
    return data;
  }
  async function read() {
    if (!pending) {
      const epoch = generation;
      const current = request('/auth/me', { method: 'get' }).then(data => {
        if (epoch !== generation) throw new Error('Session changed');
        return accept(data);
      });
      pending = current;
      current.finally(() => { if (pending === current) pending = null; }).catch(() => {});
    }
    return pending;
  }
  async function headers() {
    if (!csrf) await read();
    return { 'X-CSRF-Token': csrf };
  }
  return { accept, clear, read, headers };
}

export const sessionMessages = {
  en: { method: 'Sign-in method', local: 'Local account', os: 'OS account', osAccount: 'OS account name', osUnavailable: 'OS login is unavailable on this server.', change: 'Change password', current: 'Current password', next: 'New password (12+ characters)', confirm: 'Confirm password', mismatch: 'Passwords do not match.', required: 'Change your initial password to continue.', changed: 'Password changed. Sign in again.', unavailable: 'Unable to connect. Try again.', limited: 'Too many attempts. Try again in five minutes.', logout: 'Sign out failed. Try again.', policy: 'Use a different password with at least 12 characters.' },
  ko: { method: '로그인 방식', local: '자체 계정', os: 'OS 계정', osAccount: 'OS 계정 이름', osUnavailable: '이 서버에서 OS 로그인을 사용할 수 없습니다.', change: '비밀번호 변경', current: '현재 비밀번호', next: '새 비밀번호 (12자 이상)', confirm: '비밀번호 확인', mismatch: '비밀번호가 일치하지 않습니다.', required: '초기 비밀번호를 변경해 주세요.', changed: '변경했습니다. 다시 로그인해 주세요.', unavailable: '연결할 수 없습니다. 다시 시도해 주세요.', limited: '시도가 많습니다. 5분 후 다시 시도해 주세요.', logout: '로그아웃하지 못했습니다. 다시 시도해 주세요.', policy: '기존과 다른 12자 이상의 비밀번호를 입력해 주세요.' },
  ja: { method: 'ログイン方法', local: 'ローカルアカウント', os: 'OSアカウント', osAccount: 'OSアカウント名', osUnavailable: 'このサーバーではOSログインを利用できません。', change: 'パスワード変更', current: '現在のパスワード', next: '新しいパスワード（12文字以上）', confirm: 'パスワードの確認', mismatch: 'パスワードが一致しません。', required: '初期パスワードを変更してください。', changed: '変更しました。再度ログインしてください。', unavailable: '接続できません。もう一度お試しください。', limited: '試行回数が多すぎます。5分後にお試しください。', logout: 'ログアウトできませんでした。もう一度お試しください。', policy: '現在と異なる12文字以上のパスワードを入力してください。' },
};

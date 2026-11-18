<template>
  <div class="auth">
    <div class="auth-card panel">
      <div class="auth-brand">
        <span class="brand-mark"><img src="@/assets/img/dashboard/logo-timeweaver.png" alt="TimeWeaver" /></span>
        <strong>TimeWeaver</strong><span>{{ changing ? text.change : $t('title') }}</span>
      </div>
      <form class="form-grid" @submit.prevent="submit">
        <div class="field full" v-if="sessionMode && !changing && loginMethods.includes('os')">
          <label for="auth-method">{{ text.method }}</label>
          <select id="auth-method" v-model="method" :disabled="isLoading" @change="changeMethod">
            <option value="local">{{ text.local }}</option>
            <option value="os">{{ text.os }}</option>
          </select>
        </div>
        <div class="field full" v-if="!changing">
          <label for="auth-id">{{ method === 'os' ? text.osAccount : 'ID' }}</label>
          <input id="auth-id" name="username" v-model="id" :disabled="isLoading" autocomplete="username" :placeholder="method === 'os' ? text.osAccount : 'ID'" :maxlength="method === 'os' ? 256 : 50" required />
        </div>
        <div class="field full">
          <label for="auth-password">{{ changing ? text.current : $t('password') }}</label>
          <input id="auth-password" name="password" type="password" v-model="password" :disabled="isLoading" autocomplete="current-password" required />
        </div>
        <template v-if="changing">
          <div class="field full">
            <label for="auth-new-password">{{ text.next }}</label>
            <input id="auth-new-password" type="password" v-model="nextPassword" autocomplete="new-password" required />
          </div>
          <div class="field full">
            <label for="auth-confirm">{{ text.confirm }}</label>
            <input id="auth-confirm" type="password" v-model="confirmation" autocomplete="new-password" required />
          </div>
        </template>
        <div class="field full" v-if="notice"><small role="status">{{ notice }}</small></div>
        <div class="field full" v-if="error"><small class="error" role="alert">{{ error }}</small></div>
        <div class="field full auth-actions">
          <button type="submit" class="btn primary" :disabled="isLoading">
            <i v-if="isLoading" class="ph ph-circle-notch auth-spinner"></i>
            <span v-else>{{ changing ? text.change : $t('login') }}</span>
          </button>
          <button v-if="!sessionMode" type="button" class="btn" @click="register">{{ $t('join') }}</button>
        </div>
      </form>
      <div class="auth-foot" v-if="!sessionMode">
        <button type="button" class="text-btn" @click="forgot">{{ $t('forgot') }}</button>
      </div>
    </div>
  </div>
</template>
<script>
import { login, ensureSession, changePassword, sessionMode, getLoginMethods } from "@api";
import { sessionMessages } from "../../../session.mjs";
export default {
  name: 'LoginForm',
  data: () => ({ id: '', password: '', nextPassword: '', confirmation: '', changing: false, isLoading: false, error: '', notice: '', method: 'local', loginMethods: ['local'], sessionMode }),
  computed: { text() { return sessionMessages[this.$i18n.locale] || sessionMessages.en; } },
  async mounted() {
    const methodsRequest = sessionMode ? getLoginMethods().then(methods => { this.loginMethods = methods; }).catch(() => { this.loginMethods = ['local']; }) : Promise.resolve();
    if (sessionMode && new URLSearchParams(window.location.search).get('password') === 'required') {
      try {
        const data = await ensureSession();
        this.id = data.user.user_id;
        this.changing = data.user.must_change_password;
        if (this.changing) this.notice = this.text.required;
      } catch { this.error = this.text.unavailable; }
    }
    await methodsRequest;
  },
  methods: {
    changeMethod() { this.id = ''; this.password = ''; this.error = ''; this.notice = ''; },
    async submit() {
      if (this.isLoading) return;
      this.error = '';
      if (this.changing && this.nextPassword !== this.confirmation) { this.error = this.text.mismatch; return; }
      this.isLoading = true;
      try {
        if (this.changing) {
          await changePassword(this.password, this.nextPassword);
          this.changing = false; this.notice = this.text.changed;
          this.password = ''; this.nextPassword = ''; this.confirmation = '';
          window.history.replaceState(null, '', '/login');
        } else {
          const data = await login(this.id, this.password, this.method);
          this.password = '';
          if (sessionMode && data.user.must_change_password) {
            this.changing = true; this.notice = this.text.required;
          } else { window.location.href = '/dashboard'; }
        }
      } catch (error) {
        const code = error.response?.data?.detail?.code;
        if (code === 'os_login_unavailable') this.error = this.text.osUnavailable;
        else if (code === 'login_limited') this.error = this.text.limited;
        else if (code === 'password_policy' || code === 'password_unchanged') this.error = this.text.policy;
        else if (!error.response || error.response.status >= 500) this.error = this.text.unavailable;
        else this.error = this.$t('login_failed');
      } finally { if (this.method === 'os') this.password = ''; this.isLoading = false; }
    },
    register() { alert('Go to the registration page'); },
    forgot() { alert('Show password recovery page'); },
  },
};
</script>

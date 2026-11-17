<template>
  <div class="page-head"><h1>{{ t('access_users') }}</h1></div>
  <section class="panel" data-testid="users-page">
    <div class="panel-head">
      <input v-model="search" type="search" :aria-label="t('access_login')" :placeholder="t('access_login')" />
      <div class="toolbar">
        <button class="btn" :disabled="loading" @click="load">{{ t('btn_refresh') }}</button>
        <button v-if="allowed" class="btn primary" :disabled="loading" @click="open('create')">{{ t('access_add') }}</button>
      </div>
    </div>
    <p v-if="pageError" class="security-callout error" role="alert">{{ pageError }}</p>
    <div v-if="loading" class="empty-state">{{ t('msg_loading') }}</div>
    <div v-else-if="allowed" class="table-scroll">
      <table>
        <thead><tr><th>{{ t('access_login') }}</th><th>{{ t('access_name') }}</th><th>{{ t('access_role') }}</th><th>{{ t('access_status') }}</th><th>{{ t('access_groups') }}</th><th>{{ t('list_label_actions') }}</th></tr></thead>
        <tbody>
          <tr v-for="user in filtered" :key="user.user_id" :data-user="user.user_id">
            <td>{{ user.user_id }}</td><td>{{ user.name }}</td><td>{{ t('access_' + user.role) }}</td>
            <td><span class="badge" :class="user.status === 'active' ? 'online' : 'offline'">{{ t('access_' + user.status) }}</span><small v-if="user.must_change_password">{{ t('access_required') }}</small></td>
            <td>{{ memberships(user) }}</td>
            <td><div class="row-actions">
              <button class="btn" @click="open('edit', user)">{{ t('access_edit') }}</button>
              <button v-if="user.user_id !== currentID" class="btn" @click="open('password', user)">{{ t('access_reset') }}</button>
            </div></td>
          </tr>
        </tbody>
      </table>
      <div v-if="filtered.length === 0" class="empty-state">{{ t('access_empty') }}</div>
    </div>
  </section>
  <ModalComponent :isOpen="opened" :title="mode === 'password' ? t('access_reset') : t('access_users')" :confirmText="t('access_save')" :busy="busy" @close="close" @confirm="save">
    <div class="form-grid">
      <template v-if="mode !== 'password'">
        <div class="field"><label for="user-id">{{ t('access_login') }}</label><input id="user-id" v-model="form.user_id" :disabled="mode !== 'create' || busy" maxlength="50" autocomplete="off" /></div>
        <div class="field"><label for="user-name">{{ t('access_name') }}</label><input id="user-name" v-model="form.name" :disabled="busy" maxlength="255" /></div>
        <div class="field full"><label for="user-email">{{ t('access_email') }}</label><input id="user-email" v-model="form.email" :disabled="busy" type="email" maxlength="100" /></div>
        <div class="field"><label for="user-role">{{ t('access_role') }}</label><select id="user-role" v-model="form.role" :disabled="busy"><option value="user">{{ t('access_user') }}</option><option value="admin">{{ t('access_admin') }}</option></select></div>
        <div class="field"><label for="user-status">{{ t('access_status') }}</label><select id="user-status" v-model="form.status" :disabled="busy"><option value="active">{{ t('access_active') }}</option><option value="disabled">{{ t('access_disabled') }}</option></select></div>
        <div class="field full" v-if="form.role === 'user'">
          <label>{{ t('access_groups') }}</label>
          <div class="grant-list">
            <div v-for="group in choices" :key="group.group_id" class="grant-row">
              <label :for="'grant-' + group.group_id">{{ group.group_name }}</label>
              <select :id="'grant-' + group.group_id" v-model="grants[group.group_id]" :disabled="busy">
                <option value="">{{ t('access_none') }}</option><option value="viewer">{{ t('access_viewer') }}</option><option value="operator">{{ t('access_operator') }}</option><option value="editor">{{ t('access_editor') }}</option>
              </select>
            </div>
          </div>
        </div>
      </template>
      <div v-if="mode !== 'edit'" class="field full"><label for="user-password">{{ t('access_password') }}</label><input id="user-password" v-model="form.password" :disabled="busy" type="password" autocomplete="new-password" :placeholder="t('access_password_hint')" maxlength="1024" /></div>
    </div>
    <p v-if="formError" class="security-callout error" role="alert">{{ formError }}</p>
  </ModalComponent>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue';
import { useI18n } from 'vue-i18n';
import { ensureSession, getRequest, postRequest, putRequest } from '@api';
import ModalComponent from '../../misc/ModalComponent.vue';
const { t, te } = useI18n();
const users = ref([]), groups = ref([]), search = ref(''), currentID = ref('');
const loading = ref(false), allowed = ref(false), opened = ref(false), busy = ref(false), mode = ref('create');
const pageError = ref(''), formError = ref(''), grants = ref({});
const form = reactive({ user_id: '', name: '', email: '', role: 'user', status: 'active', version: 0, password: '' });
const choices = computed(() => [{ group_id: 0, group_name: t('access_all') }, ...groups.value]);
const filtered = computed(() => users.value.filter(u => `${u.user_id} ${u.name}`.toLocaleLowerCase().includes(search.value.toLocaleLowerCase())));
function message(error) { const key = 'access_error_' + error.response?.data?.detail?.code; return te(key) ? t(key) : t('access_failed'); }
function memberships(user) {
  if (user.role === 'admin') return t('access_all');
  return user.groups.map(m => `${choices.value.find(g => g.group_id === m.group_id)?.group_name ?? m.group_id}: ${t('access_' + m.role)}`).join(', ') || t('access_none');
}
async function load() {
  if (loading.value) return;
  loading.value = true; pageError.value = '';
  try {
    const me = await ensureSession(); currentID.value = me.user.user_id;
    allowed.value = me.user.role === 'admin';
    if (!allowed.value) { pageError.value = t('access_forbidden'); return; }
    const [list, available] = await Promise.all([getRequest('/dashboard/users'), getRequest('/dashboard/groups/get_groups')]);
    users.value = list; groups.value = available;
  } catch (error) { pageError.value = message(error); }
  finally { loading.value = false; }
}
function open(next, user = null) {
  mode.value = next; formError.value = ''; grants.value = {};
  for (const g of choices.value) grants.value[g.group_id] = '';
  Object.assign(form, { user_id: '', name: '', email: '', role: 'user', status: 'active', version: 0, password: '' });
  if (user) {
    Object.assign(form, { user_id: user.user_id, name: user.name, email: user.email, role: user.role, status: user.status, version: user.version });
    for (const m of user.groups) grants.value[m.group_id] = m.role;
  }
  opened.value = true;
}
function close() { if (busy.value) return; opened.value = false; form.password = ''; }
async function save() {
  if (busy.value) return;
  busy.value = true; formError.value = '';
  try {
    let result;
    if (mode.value === 'password') result = await postRequest('/dashboard/users/password', { user_id: form.user_id, version: form.version, password: form.password }, 'json');
    else {
      const selected = form.role === 'admin' ? [] : Object.entries(grants.value).filter(([, role]) => role).map(([id, role]) => ({ group_id: Number(id), role }));
      const body = { user_id: form.user_id, name: form.name, email: form.email, role: form.role, status: form.status, groups: selected, version: form.version };
      if (mode.value === 'create') { body.password = form.password; result = await postRequest('/dashboard/users', body, 'json'); }
      else result = await putRequest('/dashboard/users', body, 'json');
    }
    opened.value = false; form.password = '';
    if (result.reauthenticate) { window.location.href = '/login'; return; }
    await load();
  } catch (error) { formError.value = message(error); }
  finally { busy.value = false; }
}
onMounted(load);
</script>

<style scoped>
.grant-list { max-height: 240px; overflow-y: auto; }
.grant-row { display: grid; grid-template-columns: minmax(0, 1fr) 130px; gap: 12px; align-items: center; padding: 5px 0; }
.grant-row label { overflow-wrap: anywhere; }
small { display: block; margin-top: 4px; }
.row-actions { flex-wrap: wrap; }
</style>

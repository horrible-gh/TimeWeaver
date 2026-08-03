<template>
  <section class="history-board">
    <div class="history-heading">
      <div>
        <h2>{{ $t('enroll_history_title') }}</h2>
        <p>{{ $t('enroll_history_desc') }}</p>
      </div>
      <div class="history-actions">
        <select v-model="statusFilter">
          <option value="all">{{ $t('select_box_all') }}</option>
          <option value="unused">{{ $t('enroll_status_unused') }}</option>
          <option value="used">{{ $t('enroll_status_used') }}</option>
          <option value="expired">{{ $t('enroll_status_expired') }}</option>
          <option value="revoked">{{ $t('enroll_status_revoked') }}</option>
        </select>
        <button class="ghost" @click="$emit('refresh')">{{ $t('btn_refresh') }}</button>
      </div>
    </div>
    <p v-if="notice" class="notice">{{ $t(notice.key, notice.params) }}</p>
    <div v-if="loading" class="empty">{{ $t('msg_loading') }}</div>
    <div v-else-if="error" class="empty">
      <p>{{ $t('msg_list_load_failed') }}</p>
      <button class="ghost" @click="$emit('refresh')">{{ $t('btn_retry') }}</button>
    </div>
    <div v-else-if="items.length === 0" class="empty">{{ $t('enroll_history_empty') }}</div>
    <div v-else-if="filtered.length === 0" class="empty">{{ $t('enroll_history_no_filtered') }}</div>
    <div v-else class="table-scroll">
      <table class="history-table">
        <thead>
          <tr>
            <th class="id">{{ $t('enroll_label_enrollment_id') }}</th>
            <th class="device">{{ $t('list_label_device') }}</th>
            <th class="group">{{ $t('list_label_group') }}</th>
            <th class="created">{{ $t('enroll_label_created_at') }}</th>
            <th class="expires">{{ $t('enroll_label_expires_at') }}</th>
            <th class="status">{{ $t('list_label_status') }}</th>
            <th class="used">{{ $t('enroll_label_used_device') }}</th>
            <th class="actions">{{ $t('list_label_actions') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in paged" :key="item.enrollment_id">
            <td :title="String(item.enrollment_id)">{{ shortId(item.enrollment_id) }}</td>
            <td>{{ item.device_name || '—' }}</td>
            <td>{{ groupLabel(item.group_id) }}</td>
            <td>{{ formatDate(item.created_at) }}</td>
            <td>{{ formatDate(item.expires_at) }}</td>
            <td><span class="token-state" :class="'token-' + item.status">{{ $t('enroll_status_' + item.status) }}</span></td>
            <td>{{ item.used_device_name || item.used_device_id || '—' }}</td>
            <td>
              <button v-if="item.status === 'unused'" class="revoke" @click="revoke(item)">{{ $t('btn_revoke') }}</button>
            </td>
          </tr>
        </tbody>
      </table>
      <BoardPagination :key="statusFilter" :total="filtered.length" :perPage="TOKEN_PAGE_SIZE" @page-changed="currentPage = $event" />
    </div>
  </section>
</template>

<script setup>
import { computed, defineEmits, defineProps, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import BoardPagination from "@/dashboard/components/misc/BoardPagination.vue";
import { TOKEN_PAGE_SIZE } from "@/dashboard/constants/enrollment";
import { mapError, revokeEnrollment } from "@/dashboard/api/enrollment";
import { parseServerTime } from "@/dashboard/utils/deviceStatus";

const props = defineProps({
  items: { type: Array, default: () => [] },
  groups: { type: Array, default: () => [] },
  loading: Boolean,
  error: { type: [Boolean, Object], default: false },
});
const emit = defineEmits(["refresh", "revoked", "forbidden"]);
const { t } = useI18n();
const statusFilter = ref("all");
const currentPage = ref(1);
const notice = ref(null);
watch(statusFilter, () => { currentPage.value = 1; });

const filtered = computed(() => {
  const rows = statusFilter.value === "all" ? props.items : props.items.filter((item) => item.status === statusFilter.value);
  return [...rows].sort((a, b) => {
    const left = parseServerTime(a.created_at);
    const right = parseServerTime(b.created_at);
    const difference = (right ? right.getTime() : 0) - (left ? left.getTime() : 0);
    return difference || String(a.enrollment_id).localeCompare(String(b.enrollment_id));
  });
});
const paged = computed(() => {
  const start = (currentPage.value - 1) * TOKEN_PAGE_SIZE;
  return filtered.value.slice(start, start + TOKEN_PAGE_SIZE);
});
function shortId(value) { return String(value || "").slice(0, 8); }
function groupLabel(groupId) {
  if (Number(groupId) === 0) return "Unknown";
  const group = props.groups.find((item) => Number(item.group_id) === Number(groupId));
  return group ? group.group_name : groupId;
}
function formatDate(value) {
  const parsed = parseServerTime(value);
  return parsed ? parsed.toLocaleString() : "—";
}
async function revoke(item) {
  if (!window.confirm(t("msg_revoke_confirm"))) return;
  notice.value = null;
  try {
    await revokeEnrollment(item.enrollment_id);
    emit("revoked", item.enrollment_id);
    emit("refresh");
  } catch (error) {
    const mapped = mapError(error);
    notice.value = mapped.messageKey ? { key: mapped.messageKey, params: mapped.params } : null;
    if (mapped.status === 403) emit("forbidden");
    if (mapped.status === 404 || mapped.status === 409) emit("refresh");
  }
}
</script>

<style scoped>
.history-board { margin-top: 24px; padding-top: 20px; border-top: 1px solid #36516b; }
.history-heading, .history-actions { display: flex; justify-content: space-between; align-items: center; gap: 12px; }
.history-heading p { color: #9eb1c5; }
.history-actions select, .ghost, .revoke { padding: 8px 11px; border: 1px solid #4f7596; border-radius: 6px; color: #fff; background: #071b30; }
.ghost, .revoke { cursor: pointer; }
.revoke { border-color: #c45b68; background: rgba(196, 58, 76, .15); }
.table-scroll { overflow-x: auto; }
.history-table { width: 100%; min-width: 900px; table-layout: fixed; border-collapse: collapse; }
.history-table th, .history-table td { padding: 10px; border-bottom: 1px solid #29445e; overflow-wrap: anywhere; }
.id { width: 10%; }.device { width: 18%; }.group { width: 12%; }.created { width: 14%; }.expires { width: 14%; }.status { width: 10%; }.used { width: 12%; }.actions { width: 10%; }
.token-state { font-weight: 700; }
.token-unused { color: #fcd34d; }.token-used { color: #6ee7b7; }.token-expired, .token-revoked { color: #a9b4c0; }
.empty { padding: 28px; text-align: center; border: 1px dashed #4d6c88; }
.notice { color: #fda4af; }
@media (max-width: 767px) { .history-heading { align-items: flex-start; flex-direction: column; } }
</style>

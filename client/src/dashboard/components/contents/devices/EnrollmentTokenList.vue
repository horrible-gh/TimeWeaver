<template>
  <div class="panel token-list">
    <div class="panel-head">
      <div class="panel-title">
        <h2>{{ $t('enroll_history_title') }}</h2>
        <p>{{ $t('enroll_history_desc') }}</p>
      </div>
      <div class="actions">
        <select v-model="statusFilter" class="select" :aria-label="$t('list_label_status')">
          <option value="all">{{ $t('select_box_all') }}</option>
          <option value="unused">{{ $t('enroll_status_unused') }}</option>
          <option value="used">{{ $t('enroll_status_used') }}</option>
          <option value="expired">{{ $t('enroll_status_expired') }}</option>
          <option value="revoked">{{ $t('enroll_status_revoked') }}</option>
        </select>
        <button class="btn" @click="$emit('refresh')">
          <i class="ph ph-arrow-clockwise"></i> {{ $t('btn_refresh') }}
        </button>
      </div>
    </div>

    <div v-if="notice" class="security-callout error" style="margin: 16px 20px">
      <i class="ph ph-warning-circle"></i>
      <span>{{ $t(notice.key, notice.params) }}</span>
    </div>

    <div v-if="loading" class="empty-state">{{ $t('msg_loading') }}</div>
    <div v-else-if="error" class="empty-state">
      <p>{{ $t('msg_list_load_failed') }}</p>
      <button class="btn" @click="$emit('refresh')">{{ $t('btn_retry') }}</button>
    </div>
    <div v-else-if="items.length === 0" class="empty-state">{{ $t('enroll_history_empty') }}</div>
    <div v-else-if="filtered.length === 0" class="empty-state">{{ $t('enroll_history_no_filtered') }}</div>

    <template v-else>
      <div class="table-scroll">
        <table>
          <thead>
            <tr>
              <th>{{ $t('enroll_label_enrollment_id') }}</th>
              <th>{{ $t('list_label_device') }}</th>
              <th>{{ $t('list_label_group') }}</th>
              <th>{{ $t('enroll_label_created_at') }}</th>
              <th>{{ $t('enroll_label_expires_at') }}</th>
              <th>{{ $t('list_label_status') }}</th>
              <th>{{ $t('enroll_label_used_device') }}</th>
              <th>{{ $t('list_label_actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in paged" :key="item.enrollment_id">
              <td class="mono" :title="String(item.enrollment_id)">{{ shortId(item.enrollment_id) }}</td>
              <td>{{ item.device_name || '—' }}</td>
              <td>{{ groupLabel(item.group_id) }}</td>
              <td>{{ formatDate(item.created_at) }}</td>
              <td>{{ formatDate(item.expires_at) }}</td>
              <td><span class="status-text" :class="item.status">{{ $t('enroll_status_' + item.status) }}</span></td>
              <td>{{ item.used_device_name || item.used_device_id || '—' }}</td>
              <td>
                <button v-if="item.status === 'unused'" class="btn danger" @click="revoke(item)">
                  {{ $t('btn_revoke') }}
                </button>
                <span v-else>—</span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div class="panel-foot">
        <span>{{ $t('list_footer_range', { total: filtered.length, start: rangeStart, end: rangeEnd }) }}</span>
        <BoardPagination
          :key="statusFilter"
          :total="filtered.length"
          :perPage="TOKEN_PAGE_SIZE"
          @page-changed="currentPage = $event"
        />
      </div>
    </template>
  </div>
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
const rangeStart = computed(() => (filtered.value.length === 0 ? 0 : (currentPage.value - 1) * TOKEN_PAGE_SIZE + 1));
const rangeEnd = computed(() => Math.min(currentPage.value * TOKEN_PAGE_SIZE, filtered.value.length));

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

<!--
  No <style scoped> block (TR0013 section 4.4 rule 7). `.panel` / `.actions` /
  `.select` / `.btn` / `.table-scroll` / `.status-text` / `.empty-state` /
  `.panel-foot` come from components.css; the `.token-list` panel modifier and
  `.security-callout` from devices.css. `.status-text.revoked` was added to
  components.css section 7 because the API has a fourth token state the deck's
  three-state table does not show.
-->

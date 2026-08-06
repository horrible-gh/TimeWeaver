<template>
  <section class="board-container device-board">
    <div class="section-heading">
      <div>
        <h2>{{ $t('devices_tab_status') }}</h2>
        <p>{{ $t('devices_page_desc') }}</p>
      </div>
      <div class="top-actions">
        <div>
          <button class="add-button" @click="openAddDeviceModal">
            <i class="ph ph-plus"></i> {{ $t('btn_add_device_manual') }}
          </button>
          <small>{{ $t('hint_add_device_manual') }}</small>
        </div>
        <div v-if="showEnrollment">
          <button class="enroll-button" @click="$emit('open-enrollment')">
            <i class="ph ph-key"></i> {{ $t('btn_enroll_agent') }}
          </button>
          <small>{{ $t('hint_enroll_agent') }}</small>
        </div>
      </div>
    </div>

    <div class="list-controls">
      <input v-model="rawSearch" type="search" :placeholder="$t('msg_search_device_name')" />
      <label>
        <span>{{ $t('label_filter_status') }}</span>
        <select v-model="statusFilter">
          <option value="all">{{ $t('select_box_all') }}</option>
          <option value="online">{{ $t('device_state_online') }}</option>
          <option value="attention">{{ $t('device_state_attention') }}</option>
          <option value="offline">{{ $t('device_state_offline') }}</option>
        </select>
      </label>
      <button class="ghost-button" @click="resetFilters">{{ $t('btn_filter_reset') }}</button>
      <button class="ghost-button" @click="$emit('refresh')">{{ $t('btn_refresh') }}</button>
    </div>

    <div v-if="loading" class="empty-state">{{ $t('msg_loading') }}</div>
    <div v-else-if="error" class="empty-state error-state">
      <p>{{ $t('msg_list_load_failed') }}</p>
      <button class="ghost-button" @click="$emit('refresh')">{{ $t('btn_retry') }}</button>
    </div>
    <div v-else-if="devices.length === 0" class="empty-state">
      <p>{{ $t('msg_no_devices') }}</p>
      <p>{{ $t('msg_no_devices_hint') }}</p>
      <button v-if="showEnrollment" class="enroll-button" @click="$emit('open-enrollment')">{{ $t('btn_enroll_agent') }}</button>
    </div>
    <div v-else-if="view.totalCount === 0" class="empty-state">
      <p>{{ $t('msg_no_filtered_devices') }}</p>
      <button class="ghost-button" @click="resetFilters">{{ $t('btn_filter_reset') }}</button>
    </div>

    <div v-else class="table-scroll">
      <table class="board-table device-table">
        <thead>
          <tr>
            <th class="title-device" @click="sort('device_name')">{{ $t('list_label_device') }} {{ sortMark('device_name') }}</th>
            <th class="title-status" @click="sort('status')">{{ $t('list_label_status') }} {{ sortMark('status') }}</th>
            <th class="title-group">{{ $t('list_label_group') }}</th>
            <th class="title-version" @click="sort('version')">{{ $t('list_label_version') }} {{ sortMark('version') }}</th>
            <th class="title-login" @click="sort('last_login_at')">{{ $t('list_label_last_login_at') }} {{ sortMark('last_login_at') }}</th>
            <th class="title-heartbeat" @click="sort('last_heartbeat_at')">{{ $t('list_label_last_heartbeat_at') }} {{ sortMark('last_heartbeat_at') }}</th>
            <th class="title-actions">{{ $t('list_label_actions') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="device in view.rows" :key="device.device_id" :class="{ highlighted: String(device.device_id) === String(highlightDeviceId) }" :style="{ '--highlight-duration': NEW_DEVICE_HIGHLIGHT_MS + 'ms' }">
            <td :data-label="$t('list_label_device')" class="device-name">{{ device.device_name }}</td>
            <td :data-label="$t('list_label_status')">
              <span class="state-badge" :class="'state-' + state(device)">{{ $t('device_state_' + state(device)) }}</span>
              <small class="state-reason">{{ reasonText(device) }}</small>
            </td>
            <td :data-label="$t('list_label_group')" class="optional-mobile">{{ groupName || userGroupId }}</td>
            <td :data-label="$t('list_label_version')" class="optional-mobile">{{ device.version || '—' }}</td>
            <td :data-label="$t('list_label_last_login_at')" class="optional-mobile" :title="device.last_login_at || ''">{{ relative(device.last_login_at) }}</td>
            <td :data-label="$t('list_label_last_heartbeat_at')" :title="device.last_heartbeat_at || ''">{{ relative(device.last_heartbeat_at) }}</td>
            <td :data-label="$t('list_label_actions')">
              <div class="button-group">
                <button v-if="showEnrollment" class="reissue-button" :title="$t('hint_reissue_for_device')" @click="$emit('open-enrollment', device)">{{ $t('btn_reissue_for_device') }}</button>
                <button class="edit-button" @click="openEditDeviceModal(device)">{{ $t('btn_edit') }}</button>
                <button class="delete-button" @click="removeDevice(device.device_id)">{{ $t('btn_remove') }}</button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
      <BoardPagination
        :key="paginationKey"
        :total="view.totalCount"
        :perPage="DEVICE_PAGE_SIZE"
        @page-changed="currentPage = $event"
      />
    </div>

    <ModalComponent
      :isOpen="isModalOpen"
      :title="isEditMode ? $t('list_label_device') + ' ' + $t('btn_edit') : $t('list_label_device') + ' ' + $t('btn_add')"
      :confirmText="$t('btn_save')"
      @close="closeModal"
      @confirm="saveDevice"
    >
      <div class="modal-form">
        <label>{{ $t('list_label_device') }}</label>
        <input v-model="deviceForm.device_name" type="text" :placeholder="$t('msg_enter_device_name')" />
        <label>{{ $t('list_label_status') }}</label>
        <select v-model="deviceForm.status">
          <option value="active">{{ $t('label_active') }}</option>
          <option value="inactive">{{ $t('label_inactive') }}</option>
        </select>
        <p v-if="actionError" class="inline-error">{{ $t(actionError) }}</p>
      </div>
    </ModalComponent>
  </section>
</template>

<script setup>
import { computed, defineEmits, defineProps, onUnmounted, reactive, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { deleteRequest, postRequest, putRequest } from "@api";
import BoardPagination from "@/dashboard/components/misc/BoardPagination.vue";
import ModalComponent from "@/dashboard/components/misc/ModalComponent.vue";
import { DEVICE_PAGE_SIZE, NEW_DEVICE_HIGHLIGHT_MS, SEARCH_DEBOUNCE_MS } from "@/dashboard/constants/enrollment";
import { computeLiveness, livenessReason, relativeTime, visibleDevices } from "@/dashboard/utils/deviceStatus";

const props = defineProps({
  devices: { type: Array, default: () => [] },
  loading: Boolean,
  error: { type: [Boolean, Object], default: false },
  groupName: { type: [String, Number], default: "" },
  highlightDeviceId: { type: [String, Number], default: null },
  now: { type: [Date, Number], default: () => new Date() },
  showEnrollment: Boolean,
});
const emit = defineEmits(["refresh", "saved", "removed", "open-enrollment"]);
const { t } = useI18n();

let user = {};
try { user = JSON.parse(localStorage.getItem("user") || "{}"); } catch (error) { user = {}; }
const userGroupId = user.group_id == null ? "" : user.group_id;
const rawSearch = ref("");
const search = ref("");
const statusFilter = ref("all");
const sortKey = ref("device_name");
const sortDir = ref("asc");
const currentPage = ref(1);
let searchTimer = null;

watch(rawSearch, (value) => {
  window.clearTimeout(searchTimer);
  searchTimer = window.setTimeout(() => { search.value = value; }, SEARCH_DEBOUNCE_MS);
});
watch([search, statusFilter], () => { currentPage.value = 1; });
onUnmounted(() => window.clearTimeout(searchTimer));

const view = computed(() => visibleDevices(props.devices, {
  search: search.value,
  status: statusFilter.value,
  sortKey: sortKey.value,
  sortDir: sortDir.value,
  page: currentPage.value,
  pageSize: DEVICE_PAGE_SIZE,
  now: props.now,
}));
const paginationKey = computed(() => `${search.value}:${statusFilter.value}`);

function sort(key) {
  if (sortKey.value === key) sortDir.value = sortDir.value === "asc" ? "desc" : "asc";
  else { sortKey.value = key; sortDir.value = "asc"; }
}
function sortMark(key) {
  if (sortKey.value !== key) return "";
  return sortDir.value === "asc" ? "▲" : "▼";
}
function state(device) { return computeLiveness(device, props.now); }
function reasonText(device) {
  const reason = livenessReason(device, props.now);
  return reason ? t(`device_reason_${reason}`) : t("device_state_online");
}
function relative(value) {
  const result = relativeTime(value, props.now);
  return t(result.key, { n: result.n });
}
function resetFilters() {
  rawSearch.value = "";
  search.value = "";
  statusFilter.value = "all";
  currentPage.value = 1;
}

const isModalOpen = ref(false);
const isEditMode = ref(false);
const actionError = ref(null);
const deviceForm = reactive({ group_id: user.group_id, device_name: "", status: "active", creator: user.user_id, modifier: user.user_id });

function openAddDeviceModal() {
  isEditMode.value = false;
  actionError.value = false;
  Object.assign(deviceForm, { group_id: user.group_id, device_name: "", status: "active", creator: user.user_id, modifier: user.user_id });
  isModalOpen.value = true;
}
function openEditDeviceModal(device) {
  isEditMode.value = true;
  actionError.value = false;
  Object.assign(deviceForm, device, { modifier: user.user_id, creator: device.creator || user.user_id });
  isModalOpen.value = true;
}
function closeModal() { isModalOpen.value = false; }
async function saveDevice() {
  actionError.value = null;
  try {
    if (isEditMode.value) await putRequest("/dashboard/devices/update_device", deviceForm, "json");
    else await postRequest("/dashboard/devices/insert_device", deviceForm, "json");
    closeModal();
    emit("saved");
  } catch (error) {
    const detail = error && error.response && error.response.data
      ? error.response.data.detail
      : null;
    actionError.value = error && error.response && error.response.status === 409
      && detail && detail.code === "device_name_conflict"
      ? "msg_device_name_conflict"
      : "msg_save_failed";
  }
}
async function removeDevice(deviceId) {
  if (!window.confirm(t("msg_delete_device_name"))) return;
  try {
    await deleteRequest(`/dashboard/devices/remove_device/${deviceId}`);
    emit("removed");
  } catch (error) {
    actionError.value = "msg_save_failed";
  }
}
</script>

<style scoped>
.device-board { margin-top: 18px; }
.section-heading, .top-actions, .list-controls, .button-group { display: flex; align-items: center; gap: 12px; }
.section-heading { justify-content: space-between; align-items: flex-start; }
.section-heading p, .top-actions small { color: #9eb1c5; }
.top-actions > div { display: flex; flex-direction: column; align-items: flex-start; max-width: 220px; }
.enroll-button, .ghost-button { border: 1px solid #3f91d4; border-radius: 6px; padding: 9px 14px; color: #fff; background: #0b5f9e; cursor: pointer; }
.ghost-button { background: transparent; }
.list-controls { margin: 20px 0 14px; flex-wrap: wrap; }
.list-controls input, .list-controls select { min-height: 38px; padding: 8px 10px; border-radius: 6px; border: 1px solid #54708b; background: #071b30; color: #fff; }
.list-controls label { display: flex; align-items: center; gap: 8px; }
.table-scroll { overflow-x: auto; }
.device-table th { cursor: pointer; }
.device-table { width: 100%; min-width: 900px; table-layout: fixed; }
.device-table .button-group { flex-wrap: wrap; row-gap: 6px; }
.title-device { width: 17%; } .title-status { width: 12%; } .title-group { width: 11%; }
.title-version { width: 9%; } .title-login { width: 13%; } .title-heartbeat { width: 14%; } .title-actions { width: 24%; }
.state-badge { display: inline-block; border-radius: 999px; padding: 3px 8px; font-weight: 700; }
.state-online { color: #6ee7b7; background: rgba(16, 185, 129, .14); }
.state-attention { color: #fcd34d; background: rgba(245, 158, 11, .14); }
.state-offline { color: #fda4af; background: rgba(244, 63, 94, .14); }
.state-reason { display: block; margin-top: 4px; color: #c2cfdd; }
.highlighted { animation: new-device var(--highlight-duration) ease-out; }
.empty-state { padding: 32px; text-align: center; border: 1px dashed #54708b; border-radius: 10px; }
.error-state, .inline-error { color: #fda4af; }
.modal-form { display: grid; gap: 10px; }
@keyframes new-device { from { background: rgba(44, 187, 255, .32); } to { background: transparent; } }
@media (max-width: 1200px) {
  .section-heading { flex-direction: column; }
}
@media (max-width: 767px) {
  .top-actions { flex-direction: column; align-items: stretch; width: 100%; }
  .table-scroll { overflow: visible; }
  .device-table thead { display: none; }
  .device-table { min-width: 0; }
  .device-table, .device-table tbody, .device-table tr, .device-table td { display: block; width: 100%; }
  .device-table tr { margin-bottom: 12px; padding: 14px; border: 1px solid #38536f; border-radius: 10px; }
  .device-table td { display: flex; justify-content: space-between; padding: 7px 0; border: 0; }
  .device-table td::before { content: attr(data-label); color: #9eb1c5; margin-right: 10px; }
  .device-table td.optional-mobile { display: none; }
}
</style>

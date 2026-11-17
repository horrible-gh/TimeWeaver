<template>
  <div class="panel">
    <div class="panel-head">
      <div class="panel-title">
        <h2>{{ $t('devices_list_title') }}</h2>
        <p>{{ $t('devices_list_desc') }}</p>
      </div>
      <div class="toolbar">
        <div class="search">
          <i class="ph ph-magnifying-glass"></i>
          <input
            v-model="rawSearch"
            type="search"
            :aria-label="$t('msg_search_device_name')"
            :placeholder="$t('msg_search_device_name')"
          />
        </div>
        <select v-model="statusFilter" class="select" :aria-label="$t('label_filter_status')">
          <option value="all">{{ $t('select_box_all') }}</option>
          <option value="online">{{ $t('device_state_online') }}</option>
          <option value="attention">{{ $t('device_state_attention') }}</option>
          <option value="offline">{{ $t('device_state_offline') }}</option>
        </select>
        <button class="btn" @click="resetFilters">{{ $t('btn_filter_reset') }}</button>
        <button class="btn" :aria-label="$t('btn_refresh')" @click="$emit('refresh')">
          <i class="ph ph-arrow-clockwise"></i> {{ $t('btn_refresh') }}
        </button>
        <button v-if="canAdd" class="btn" :disabled="busy" :title="$t('hint_add_device_manual')" @click="openAddDeviceModal">
          <i class="ph ph-plus"></i> {{ $t('btn_add_device_manual') }}
        </button>
        <button
          v-if="showEnrollment"
          class="btn primary"
          :title="$t('hint_enroll_agent')"
          @click="$emit('open-enrollment')"
        >
          <i class="ph ph-key"></i> {{ $t('btn_enroll_agent') }}
        </button>
      </div>
    </div>

    <div v-if="actionError && !isModalOpen" class="security-callout error" role="alert">{{ $t(actionError) }}</div>
    <div v-if="loading" class="empty-state">{{ $t('msg_loading') }}</div>
    <div v-else-if="error" class="empty-state">
      <p>{{ $t('msg_list_load_failed') }}</p>
      <button class="btn" @click="$emit('refresh')">{{ $t('btn_retry') }}</button>
    </div>
    <div v-else-if="devices.length === 0" class="empty-state">
      <p>{{ $t('msg_no_devices') }}</p>
      <p>{{ $t('msg_no_devices_hint') }}</p>
      <button v-if="showEnrollment" class="btn primary" @click="$emit('open-enrollment')">
        {{ $t('btn_enroll_agent') }}
      </button>
    </div>
    <div v-else-if="view.totalCount === 0" class="empty-state">
      <p>{{ $t('msg_no_filtered_devices') }}</p>
      <button class="btn" @click="resetFilters">{{ $t('btn_filter_reset') }}</button>
    </div>

    <template v-else>
      <div class="table-scroll">
        <table>
          <thead>
            <tr>
              <th @click="sort('device_name')">
                {{ $t('list_label_device') }}<span class="sort-arrow">{{ sortMark('device_name') }}</span>
              </th>
              <th @click="sort('status')">
                {{ $t('list_label_status') }}<span class="sort-arrow">{{ sortMark('status') }}</span>
              </th>
              <th>{{ $t('list_label_group') }}</th>
              <th @click="sort('version')">
                {{ $t('list_label_version') }}<span class="sort-arrow">{{ sortMark('version') }}</span>
              </th>
              <th @click="sort('last_login_at')">
                {{ $t('list_label_last_login_at') }}<span class="sort-arrow">{{ sortMark('last_login_at') }}</span>
              </th>
              <th @click="sort('last_heartbeat_at')">
                {{ $t('list_label_last_heartbeat_at') }}<span class="sort-arrow">{{ sortMark('last_heartbeat_at') }}</span>
              </th>
              <th v-if="hasActions">{{ $t('list_label_actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="device in view.rows"
              :key="device.device_id"
              :data-device="device.device_id"
              :class="{ 'row-new': String(device.device_id) === String(highlightDeviceId) }"
              :style="{ '--highlight-duration': NEW_DEVICE_HIGHLIGHT_MS + 'ms' }"
            >
              <td>
                <div class="device-name">
                  <span class="device-glyph"><i class="ph ph-hard-drives"></i></span>
                  <span>{{ device.device_name }}</span>
                </div>
              </td>
              <td>
                <span class="badge" :class="stateBadgeClass(device)">{{ $t('device_state_' + state(device)) }}</span>
                <small v-if="reasonText(device)" class="state-reason">{{ reasonText(device) }}</small>
              </td>
              <td>{{ device.group_name || device.group_id || groupName || userGroupId }}</td>
              <td>{{ device.version || '—' }}</td>
              <td :title="device.last_login_at || ''">{{ relative(device.last_login_at) }}</td>
              <td :title="device.last_heartbeat_at || ''">{{ relative(device.last_heartbeat_at) }}</td>
              <td v-if="hasActions">
                <div class="row-actions">
                  <button
                    v-if="showEnrollment && (!sessionMode || device.status === 'active')"
                    class="mini"
                    :aria-label="$t('btn_reissue_for_device')"
                    :title="$t('hint_reissue_for_device', { name: device.device_name })"
                    @click="$emit('open-enrollment', device)"
                  >
                    <i class="ph ph-key"></i>
                  </button>
                  <button v-if="canEdit(device)" :disabled="busy" class="mini" :aria-label="$t('btn_edit')" @click="openEditDeviceModal(device)">
                    <i class="ph ph-pencil-simple"></i>
                  </button>
                  <button v-if="canEdit(device)" :disabled="busy" class="mini" :aria-label="$t('btn_remove')" @click="removeDevice(device.device_id)">
                    <i class="ph ph-trash"></i>
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div class="panel-foot">
        <span>{{ $t('list_footer_range', { total: view.totalCount, start: rangeStart, end: rangeEnd }) }}</span>
        <BoardPagination
          :key="paginationKey"
          :total="view.totalCount"
          :perPage="DEVICE_PAGE_SIZE"
          @page-changed="currentPage = $event"
        />
      </div>
    </template>
  </div>

  <ModalComponent
    :isOpen="isModalOpen"
    :busy="busy"
    :title="isEditMode ? $t('list_label_device') + ' ' + $t('btn_edit') : $t('list_label_device') + ' ' + $t('btn_add')"
    :confirmText="$t('btn_save')"
    @close="closeModal"
    @confirm="saveDevice"
  >
    <div class="form-grid">
      <div v-if="sessionMode && !isEditMode" class="field full">
        <label for="device-group">{{ $t('list_label_group') }}</label>
        <select id="device-group" v-model.number="deviceForm.group_id">
          <option v-for="group in editableGroups" :key="group.group_id" :value="group.group_id">{{ group.group_name }}</option>
        </select>
      </div>
      <div class="field full">
        <label>{{ $t('list_label_device') }} <span>*</span></label>
        <input id="device-name" maxlength="255" v-model="deviceForm.device_name" type="text" :placeholder="$t('msg_enter_device_name')" />
      </div>
      <div class="field full">
        <label>{{ $t('list_label_status') }}</label>
        <select id="device-status" v-model="deviceForm.status">
          <option value="active">{{ $t('label_active') }}</option>
          <option value="inactive">{{ $t('label_inactive') }}</option>
        </select>
      </div>
    </div>
    <div v-if="actionError" class="security-callout error" role="alert">
      <i class="ph ph-warning-circle"></i>
      <span>{{ $t(actionError) }}</span>
    </div>
  </ModalComponent>
</template>

<script setup>
import { computed, defineEmits, defineProps, onUnmounted, reactive, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { canEditGroup } from "@/dashboard/utils/devicePermissions.mjs";
import { deleteRequest, postRequest, putRequest, sessionMode } from "@api";
import BoardPagination from "@/dashboard/components/misc/BoardPagination.vue";
import ModalComponent from "@/dashboard/components/misc/ModalComponent.vue";
import { DEVICE_PAGE_SIZE, NEW_DEVICE_HIGHLIGHT_MS, SEARCH_DEBOUNCE_MS } from "@/dashboard/constants/enrollment";
import { computeLiveness, livenessReason, relativeTime, visibleDevices } from "@/dashboard/utils/deviceStatus";

const props = defineProps({
  devices: { type: Array, default: () => [] },
  groups: { type: Array, default: () => [] },
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
const editableGroups = computed(() => props.groups.filter(g => g.status === "active" && canEditGroup(user, g.group_id)));
const canAdd = computed(() => !sessionMode || editableGroups.value.length > 0);
function canEdit(device) { return !sessionMode || device.can_edit === true; }
const hasActions = computed(() => props.showEnrollment || !sessionMode || props.devices.some(canEdit));
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
const rangeStart = computed(() => (view.value.totalCount === 0 ? 0 : (view.value.page - 1) * DEVICE_PAGE_SIZE + 1));
const rangeEnd = computed(() => Math.min(view.value.page * DEVICE_PAGE_SIZE, view.value.totalCount));

function sort(key) {
  if (sortKey.value === key) sortDir.value = sortDir.value === "asc" ? "desc" : "asc";
  else { sortKey.value = key; sortDir.value = "asc"; }
}
function sortMark(key) {
  if (sortKey.value !== key) return "";
  return sortDir.value === "asc" ? "▲" : "▼";
}
function state(device) { return computeLiveness(device, props.now); }
// Deck badge vocabulary: online -> green, attention -> amber, offline -> grey.
function stateBadgeClass(device) {
  const liveness = state(device);
  return {
    online: liveness === "online",
    warning: liveness === "attention",
    offline: liveness === "offline",
  };
}
// null when the device is simply healthy: the badge already says so, and the
// deck's status cell carries no second line.
function reasonText(device) {
  const reason = livenessReason(device, props.now);
  return reason ? t(`device_reason_${reason}`) : null;
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
const busy = ref(false);
const deviceForm = reactive({ group_id: user.group_id, device_name: "", status: "active", creator: user.user_id, modifier: user.user_id });

function openAddDeviceModal() {
  isEditMode.value = false;
  actionError.value = false;
  Object.assign(deviceForm, { device_id: undefined, group_id: sessionMode ? (editableGroups.value.find(g => g.group_id === user.group_id) || editableGroups.value[0])?.group_id : user.group_id, device_name: "", status: "active", creator: user.user_id, modifier: user.user_id });
  isModalOpen.value = true;
}
function openEditDeviceModal(device) {
  isEditMode.value = true;
  actionError.value = false;
  Object.assign(deviceForm, device, { modifier: user.user_id, creator: device.creator || user.user_id });
  isModalOpen.value = true;
}
function closeModal() { if (!busy.value) isModalOpen.value = false; }
async function saveDevice() {
  if (busy.value) return;
  if (!String(deviceForm.device_name || "").trim()) { actionError.value = "msg_device_name_required"; return; }
  actionError.value = null;
  busy.value = true;
  try {
    const body = sessionMode ? { device_name: deviceForm.device_name.trim(), status: deviceForm.status,
      ...(isEditMode.value ? { device_id: deviceForm.device_id } : { group_id: deviceForm.group_id }) } : deviceForm;
    if (isEditMode.value) await putRequest("/dashboard/devices/update_device", body, "json");
    else await postRequest("/dashboard/devices/insert_device", body, "json");
    isModalOpen.value = false;
    emit("saved");
  } catch (error) {
    const detail = error && error.response && error.response.data
      ? error.response.data.detail
      : null;
    actionError.value = error && error.response && error.response.status === 409
      && detail && detail.code === "device_name_conflict"
      ? "msg_device_name_conflict"
      : error?.response?.status === 403 ? "device_permission_denied" : "msg_save_failed";
  } finally { busy.value = false; }
}
async function removeDevice(deviceId) {
  if (busy.value || !window.confirm(t("msg_delete_device_name"))) return;
  busy.value = true;
  actionError.value = null;
  try {
    await deleteRequest(`/dashboard/devices/remove_device/${deviceId}`);
    emit("removed");
  } catch (error) {
    actionError.value = error?.response?.data?.detail?.code === "device_in_use" ? "device_in_use"
      : error?.response?.status === 403 ? "device_permission_denied" : "msg_save_failed";
  } finally { busy.value = false; }
}
</script>

<!--
  No <style scoped> block (TR0013 section 4.4 rule 7). `.panel` / `.toolbar` /
  `.search` / `.select` / `.btn` / `.table-scroll` / `.badge` / `.device-name` /
  `.row-actions` / `.mini` / `.panel-foot` / `.form-grid` come from
  components.css; `.state-reason` / `.row-new` / `.security-callout` from
  devices.css. The pre-renewal `.board-container` / `.board-table` /
  `.list-controls` / `.enroll-button` / `.ghost-button` / `.state-badge` /
  `.modal-form` names are gone, so this screen no longer needs list.css.
-->

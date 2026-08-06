<template>
  <div class="page-head">
    <div>
      <div class="eyebrow">{{ $t('devices_eyebrow') }}</div>
      <h1>{{ $t('devices_page_title') }}</h1>
      <p class="lead">{{ $t('devices_page_desc') }}</p>
    </div>
    <nav v-if="userValid" class="view-tabs" role="tablist" :aria-label="$t('devices_view_switch')">
      <button
        class="view-tab"
        :class="{ active: activeView === 'devices' }"
        role="tab"
        :aria-selected="activeView === 'devices'"
        @click="showDevices"
      >
        <i class="ph ph-desktop"></i>{{ $t('devices_tab_status') }}
      </button>
      <button
        v-if="canEnroll"
        class="view-tab"
        :class="{ active: activeView === 'enrollment' }"
        role="tab"
        :aria-selected="activeView === 'enrollment'"
        @click="openEnrollment"
      >
        <i class="ph ph-key"></i>{{ $t('devices_tab_enroll') }}
      </button>
    </nav>
  </div>

  <div v-if="!userValid" class="panel">
    <div class="empty-state">{{ $t('msg_relogin_required') }}</div>
  </div>

  <template v-else-if="activeView === 'devices'">
    <DeviceSummary :devices="devices" :now="now" :loading="devicesLoading" />
    <DeviceList
      :devices="devices"
      :loading="devicesLoading"
      :error="devicesError"
      :groupName="currentGroupName"
      :highlightDeviceId="highlightDeviceId"
      :now="now"
      :showEnrollment="canEnroll"
      @refresh="loadDevices"
      @saved="loadDevices"
      @removed="loadDevices"
      @open-enrollment="openEnrollment"
    />
    <div v-if="roleState === 'unknown'" class="panel">
      <div class="empty-state">{{ $t('msg_loading') }}</div>
    </div>
    <div v-else-if="roleState === 'indeterminate'" class="panel">
      <div class="empty-state">
        <p>{{ $t('enroll_role_check_failed') }}</p>
        <button class="btn" @click="probeAdmin">{{ $t('btn_retry') }}</button>
      </div>
    </div>
    <EnrollmentTokenList
      v-else-if="roleState === 'admin'"
      :items="enrollments"
      :groups="groups"
      :loading="historyLoading"
      :error="historyError"
      @refresh="loadHistory"
      @revoked="handleRevoked"
      @forbidden="handleForbidden"
    />
  </template>

  <EnrollmentPanel v-else :flow="flow" :groups="groups" :devices="devices" @close="showDevices" />
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { getRequest } from "@api";
import DeviceList from "@/dashboard/components/contents/devices/DeviceList.vue";
import DeviceSummary from "@/dashboard/components/contents/devices/DeviceSummary.vue";
import EnrollmentPanel from "@/dashboard/components/contents/devices/EnrollmentPanel.vue";
import EnrollmentTokenList from "@/dashboard/components/contents/devices/EnrollmentTokenList.vue";
import { listEnrollments, mapError } from "@/dashboard/api/enrollment";
import { useEnrollmentFlow } from "@/dashboard/composables/useEnrollmentFlow";
import { COUNTDOWN_TICK_MS, NEW_DEVICE_HIGHLIGHT_MS } from "@/dashboard/constants/enrollment";

const devices = ref([]);
const groups = ref([]);
const enrollments = ref([]);
const devicesLoading = ref(false);
const historyLoading = ref(false);
const devicesError = ref(null);
const historyError = ref(null);
const roleState = ref("unknown");
const activeView = ref("devices");
const now = ref(Date.now());
const highlightDeviceId = ref(null);
let clockTimer = null;
let highlightTimer = null;

let user = null;
try {
  const parsed = JSON.parse(localStorage.getItem("user") || "null");
  if (parsed && parsed.group_id != null && parsed.user_id != null) user = parsed;
} catch (error) {
  user = null;
}
const { t } = useI18n();
const userValid = computed(() => Boolean(user));
const userGroupId = ref(user ? user.group_id : null);
const canEnroll = computed(() => roleState.value === "admin" || roleState.value === "indeterminate");
const currentGroupName = computed(() => {
  if (Number(userGroupId.value) === 0) return t("label_all_groups");
  const group = groups.value.find((item) => Number(item.group_id) === Number(userGroupId.value));
  return group ? group.group_name : t("label_group_unavailable");
});

function unwrapList(response) {
  if (Array.isArray(response)) return response;
  if (response && Array.isArray(response.data)) return response.data;
  if (response && response.data && Array.isArray(response.data.items)) return response.data.items;
  return null;
}

async function loadDevices(targetName = null) {
  if (!user) return { found: false };
  devicesLoading.value = true;
  devicesError.value = null;
  try {
    const response = await getRequest("/dashboard/devices/get_devices", { group_id: user.group_id });
    const rows = unwrapList(response);
    if (!rows) throw new Error("Invalid device list response");
    devices.value = rows;
    const found = targetName ? rows.find((item) => item.device_name === targetName) : null;
    if (found) {
      highlightDeviceId.value = found.device_id;
      window.clearTimeout(highlightTimer);
      highlightTimer = window.setTimeout(() => { highlightDeviceId.value = null; }, NEW_DEVICE_HIGHLIGHT_MS);
    }
    return { found: Boolean(found), device: found || null };
  } catch (error) {
    devicesError.value = error;
    throw error;
  } finally {
    devicesLoading.value = false;
  }
}

async function loadGroups() {
  try {
    const response = await getRequest("/dashboard/groups/get_groups");
    const rows = unwrapList(response) || [];
    groups.value = rows.filter((group) => group.status === "active");
  } catch (error) {
    groups.value = [];
  }
}

async function loadHistory() {
  if (roleState.value !== "admin") return;
  historyLoading.value = true;
  historyError.value = null;
  try {
    enrollments.value = await listEnrollments(null);
  } catch (error) {
    const mapped = mapError(error);
    historyError.value = mapped;
    if (mapped.status === 403) handleForbidden();
  } finally {
    historyLoading.value = false;
  }
}

async function probeAdmin() {
  roleState.value = "unknown";
  historyLoading.value = true;
  historyError.value = null;
  try {
    enrollments.value = await listEnrollments(null);
    roleState.value = "admin";
    await loadGroups();
  } catch (error) {
    const mapped = mapError(error);
    if (mapped.status === 401) return;
    if (mapped.status === 403 && mapped.code === "admin_required") roleState.value = "nonAdmin";
    else {
      roleState.value = "indeterminate";
      await loadGroups();
    }
  } finally {
    historyLoading.value = false;
  }
}

function handleForbidden() {
  roleState.value = "nonAdmin";
  activeView.value = "devices";
  flow.setForbidden();
}
function handleRevoked(id) {
  flow.markRevoked(id);
}
// Called both from the plain "Enroll Agent" buttons (no argument, or a
// MouseEvent when wired straight to @click) and from a device row's key action,
// which hands over the device to reissue for. Only a real device row is treated
// as a preset, so an event object can never be mistaken for one.
function openEnrollment(device = null) {
  if (!canEnroll.value) return;
  const preset = device && device.device_name ? device : null;
  flow.openFlow(preset);
  activeView.value = "enrollment";
}
function showDevices() {
  if (flow.flowState.value !== "idle") flow.closeFlow();
  activeView.value = "devices";
}

const flow = useEnrollmentFlow({
  groups,
  devices,
  userGroupId,
  refreshDevices: loadDevices,
  reloadGroups: loadGroups,
  onForbidden: handleForbidden,
  onHistoryChange: loadHistory,
});

onMounted(() => {
  if (!user) return;
  loadDevices().catch(() => {});
  probeAdmin();
  now.value = Date.now();
  clockTimer = window.setInterval(() => { now.value = Date.now(); }, COUNTDOWN_TICK_MS);
});
onUnmounted(() => {
  window.clearInterval(clockTimer);
  window.clearTimeout(highlightTimer);
});
</script>

<!--
  No <style scoped> block (TR0013 section 4.4 rule 7). The page head comes from
  style.css section 5, `.panel` / `.empty-state` / `.btn` from components.css,
  and `.view-tabs` / `.view-tab` from devices.css section 1.
-->

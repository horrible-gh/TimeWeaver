<template>
  <div class="page-head">
    <div>
      <div class="eyebrow">{{ $t('dashboard_eyebrow') }}</div>
      <h1>{{ $t('sub_dashboard') }}</h1>
      <p class="lead">{{ $t('dashboard_lead') }}</p>
    </div>
    <button id="dashboard-refresh" class="btn" :disabled="isLoading" @click="refreshAll">
      <i class="ph ph-arrow-clockwise"></i> {{ $t('btn_refresh') }}
    </button>
  </div>
  <p v-if="hasError" data-dashboard-error role="alert">{{ $t('dashboard_load_failed') }}</p>

  <div class="metric-grid">
    <article class="metric" data-metric="devices" :aria-busy="devices.loading">
      <div class="metric-top">
        <span>{{ $t('metric_devices_label') }}</span>
        <span class="metric-icon"><i class="ph ph-hard-drives"></i></span>
      </div>
      <div class="metric-value">
        <strong>{{ deviceTotal ?? '—' }}</strong>
        <small v-if="devices.data">{{ $t('metric_devices_meta', { n: devices.data.active }) }}</small>
        <small v-else>{{ $t(devices.error ? 'dashboard_unavailable' : 'msg_loading') }}</small>
      </div>
    </article>
    <article class="metric" data-metric="schedules" :aria-busy="scheduleState.loading">
      <div class="metric-top">
        <span>{{ $t('metric_schedules_label') }}</span>
        <span class="metric-icon" style="color:var(--cyan)"><i class="ph ph-calendar"></i></span>
      </div>
      <div class="metric-value">
        <strong>{{ scheduleTotal ?? '—' }}</strong>
        <small v-if="scheduleState.data">{{ $t('metric_schedules_meta', { active: scheduleState.data.active, inactive: scheduleState.data.inactive }) }}</small>
        <small v-else>{{ $t(scheduleState.error ? 'dashboard_unavailable' : 'msg_loading') }}</small>
      </div>
    </article>
    <article class="metric" data-metric="tasks" :aria-busy="tasks.loading">
      <div class="metric-top">
        <span>{{ $t('metric_tasks_label') }}</span>
        <span class="metric-icon" style="color:var(--blue)"><i class="ph ph-wrench"></i></span>
      </div>
      <div class="metric-value">
        <strong>{{ tasks.data?.pending ?? '—' }}</strong>
        <small v-if="tasks.data">{{ $t('metric_tasks_meta', { n: tasks.data.in_progress }) }}</small>
        <small v-else>{{ $t(tasks.error ? 'dashboard_unavailable' : 'msg_loading') }}</small>
      </div>
    </article>
    <article class="metric" data-metric="errors" :aria-busy="recent.loading">
      <div class="metric-top">
        <span>{{ $t('metric_errors_label') }}</span>
        <span class="metric-icon" style="color:var(--red)"><i class="ph ph-warning"></i></span>
      </div>
      <div class="metric-value">
        <strong>{{ recentErrorCount ?? '—' }}</strong>
        <small v-if="recent.data">{{ $t('metric_errors_meta', { n: recentWarningCount }) }}</small>
        <small v-else>{{ $t(recent.error ? 'dashboard_unavailable' : 'msg_loading') }}</small>
      </div>
    </article>
  </div>

  <DashboardCharts :devices="devices" :schedules="scheduleState" :tasks="tasks" />
  <LastSchedules :schedules="recent.data || []" :loading="recent.loading || (!recent.data && !recent.error)" :failed="recent.error" :updated-at="recent.updatedAt" @refresh="fetchLastSchedules" />
</template>

<script setup>
import { reactive, computed, onMounted } from 'vue';
import { getRequest } from '@api';
import DashboardCharts from './DashboardCharts.vue';
import LastSchedules from './LastSchedules.vue';
import { dashboardCounts, latestSchedules, dashboardSection, loadDashboardSection } from '@/dashboard/utils/dashboardData.mjs';

// Metrics and charts share these responses so one refresh cannot show two
// different totals from duplicate queries to the same endpoint.
const devices = reactive(dashboardSection());
const scheduleState = reactive(dashboardSection());
const tasks = reactive(dashboardSection());
const recent = reactive(dashboardSection());
const states = [devices, scheduleState, tasks, recent];
const isLoading = computed(() => states.some(state => state.loading));
const hasError = computed(() => states.some(state => state.error));
const deviceTotal = computed(() => devices.data ? Object.values(devices.data).reduce((sum, value) => sum + value, 0) : null);
const scheduleTotal = computed(() => scheduleState.data ? Object.values(scheduleState.data).reduce((sum, value) => sum + value, 0) : null);
const recentErrorCount = computed(() => recent.data ? recent.data.filter(row => row.custom_status === 'error').length : null);
const recentWarningCount = computed(() => recent.data ? recent.data.filter(row => row.custom_status === 'warning').length : null);

const fetchCounts = (kind, state) => loadDashboardSection(state, () => getRequest(`/dashboard/charts/${kind}`), response => dashboardCounts(kind, response));
const fetchLastSchedules = () => loadDashboardSection(recent, () => getRequest('/dashboard/lastest-schedules'), latestSchedules);
const refreshAll = () => Promise.all([
  fetchCounts('devices', devices),
  fetchCounts('schedules', scheduleState),
  fetchCounts('tasks', tasks),
  fetchLastSchedules(),
]);
onMounted(refreshAll);
</script>

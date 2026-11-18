<template>
  <div class="section-label">
    <h2>{{ $t('section_service_status_label') }}</h2>
    <span>{{ $t('section_service_status_desc') }}</span>
  </div>
  <div class="tile-grid">
    <article class="tile" data-chart="devices" :aria-busy="devices.loading">
      <div class="tile-head">
        <span class="metric-icon"><i class="ph ph-hard-drives"></i></span>
        <h3>{{ $t('chart_subtitle_device') }}</h3>
      </div>
      <div class="tile-chart">
        <div v-if="devices.error" class="chart-state">{{ $t('dashboard_unavailable') }}</div>
        <div v-else-if="devices.loading || !devices.data" class="chart-state">{{ $t('msg_loading') }}</div>
        <div v-else-if="isEmpty(devices.data)" class="chart-state">{{ $t('dashboard_no_data') }}</div>
        <DevicesBarChart v-else :counts="devices.data" />
      </div>
      <div class="tile-foot">{{ $t('chart_footer_devices') }}</div>
    </article>
    <article class="tile" data-chart="schedules" :aria-busy="schedules.loading">
      <div class="tile-head">
        <span class="metric-icon" style="color:var(--cyan)"><i class="ph ph-calendar"></i></span>
        <h3>{{ $t('chart_subtitle_schedules') }}</h3>
      </div>
      <div class="tile-chart">
        <div v-if="schedules.error" class="chart-state">{{ $t('dashboard_unavailable') }}</div>
        <div v-else-if="schedules.loading || !schedules.data" class="chart-state">{{ $t('msg_loading') }}</div>
        <div v-else-if="isEmpty(schedules.data)" class="chart-state">{{ $t('dashboard_no_data') }}</div>
        <SchedulesBarChart v-else :counts="schedules.data" />
      </div>
      <div class="tile-foot">{{ $t('chart_footer_schedules') }}</div>
    </article>
    <article class="tile" data-chart="tasks" :aria-busy="tasks.loading">
      <div class="tile-head">
        <span class="metric-icon" style="color:var(--blue)"><i class="ph ph-wrench"></i></span>
        <h3>{{ $t('chart_subtitle_tasks') }}</h3>
      </div>
      <div class="tile-chart">
        <div v-if="tasks.error" class="chart-state">{{ $t('dashboard_unavailable') }}</div>
        <div v-else-if="tasks.loading || !tasks.data" class="chart-state">{{ $t('msg_loading') }}</div>
        <div v-else-if="isEmpty(tasks.data)" class="chart-state">{{ $t('dashboard_no_data') }}</div>
        <TasksDoughnutChart v-else :counts="tasks.data" />
      </div>
      <div class="tile-foot">{{ $t('chart_footer_tasks') }}</div>
    </article>
  </div>
  <div v-if="updatedAt" id="dashboard-updated" class="service-foot">{{ $t('chart_last_updated_time_title') }}: {{ updatedAt }}</div>
</template>

<script setup>
import { computed, defineProps } from 'vue';
import { useI18n } from 'vue-i18n';
import DevicesBarChart from './charts/DevicesBarChart.vue';
import SchedulesBarChart from './charts/SchedulesBarChart.vue';
import TasksDoughnutChart from './charts/TasksDoughnutChart.vue';

const props = defineProps({ devices: { type: Object, required: true }, schedules: { type: Object, required: true }, tasks: { type: Object, required: true } });
const { locale } = useI18n();
const isEmpty = data => Object.values(data).every(value => value === 0);
const updatedAt = computed(() => {
  const states = [props.devices, props.schedules, props.tasks];
  if (states.some(state => state.loading || state.error || !state.updatedAt)) return '';
  return new Date(Math.max(...states.map(state => Date.parse(state.updatedAt)))).toLocaleString(locale.value);
});
</script>

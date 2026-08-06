<template>
  <div class="page-head">
    <div>
      <div class="eyebrow">{{ $t('dashboard_eyebrow') }}</div>
      <h1>{{ $t('sub_dashboard') }}</h1>
      <p class="lead">{{ $t('dashboard_lead') }}</p>
    </div>
  </div>

  <div class="metric-grid">
    <article class="metric">
      <div class="metric-top">
        <span>{{ $t('metric_devices_label') }}</span>
        <span class="metric-icon"><i class="ph ph-hard-drives"></i></span>
      </div>
      <div class="metric-value">
        <strong>{{ deviceTotal }}</strong>
        <small>{{ $t('metric_devices_meta', { n: deviceCounts.active }) }}</small>
      </div>
    </article>
    <article class="metric">
      <div class="metric-top">
        <span>{{ $t('metric_schedules_label') }}</span>
        <span class="metric-icon" style="color:var(--cyan)"><i class="ph ph-calendar"></i></span>
      </div>
      <div class="metric-value">
        <strong>{{ scheduleTotal }}</strong>
        <small>{{ $t('metric_schedules_meta', { active: scheduleCounts.active, inactive: scheduleCounts.inactive }) }}</small>
      </div>
    </article>
    <article class="metric">
      <div class="metric-top">
        <span>{{ $t('metric_tasks_label') }}</span>
        <span class="metric-icon" style="color:var(--blue)"><i class="ph ph-wrench"></i></span>
      </div>
      <div class="metric-value">
        <strong>{{ taskCounts.pending }}</strong>
        <small>{{ $t('metric_tasks_meta', { n: taskCounts.in_progress }) }}</small>
      </div>
    </article>
    <article class="metric">
      <div class="metric-top">
        <span>{{ $t('metric_errors_label') }}</span>
        <span class="metric-icon" style="color:var(--red)"><i class="ph ph-warning"></i></span>
      </div>
      <div class="metric-value">
        <strong>{{ recentErrorCount }}</strong>
        <small>{{ $t('metric_errors_meta', { n: recentWarningCount }) }}</small>
      </div>
    </article>
  </div>

  <DashboardCharts />
  <LastSchedules :schedules="schedules" @refresh="fetchLastSchedules" />
</template>

<script>
import { defineComponent, ref, computed, onMounted } from "vue";
import { getRequest } from "@api";
import DashboardCharts from "@/dashboard/components/contents/dashboard/DashboardCharts.vue";
import LastSchedules from "@/dashboard/components/contents/dashboard/LastSchedules.vue";

export default defineComponent({
  name: 'DashboardOverview',
  components: {
    DashboardCharts,
    LastSchedules
  },
  setup() {
    const deviceCounts = ref({ active: 0, error: 0, inactive: 0 });
    const scheduleCounts = ref({ active: 0, error: 0, inactive: 0 });
    const taskCounts = ref({ in_progress: 0, pending: 0, completed: 0, error: 0 });
    const schedules = ref([]);

    const deviceTotal = computed(() => deviceCounts.value.active + deviceCounts.value.error + deviceCounts.value.inactive);
    const scheduleTotal = computed(() => scheduleCounts.value.active + scheduleCounts.value.error + scheduleCounts.value.inactive);
    const recentErrorCount = computed(() => schedules.value.filter((s) => s.custom_status === 'error').length);
    const recentWarningCount = computed(() => schedules.value.filter((s) => s.custom_status === 'warning').length);

    const fetchDeviceCounts = async () => {
      try {
        const response = await getRequest("/dashboard/charts/devices");
        deviceCounts.value = {
          active: response.active_count || 0,
          error: response.error_count || 0,
          inactive: response.inactive_count || 0,
        };
      } catch (error) {
        console.error("Failed to query device counts:", error);
      }
    };

    const fetchScheduleCounts = async () => {
      try {
        const response = await getRequest("/dashboard/charts/schedules");
        scheduleCounts.value = {
          active: response.active_count || 0,
          error: response.error_count || 0,
          inactive: response.inactive_count || 0,
        };
      } catch (error) {
        console.error("Failed to query schedule counts:", error);
      }
    };

    const fetchTaskCounts = async () => {
      try {
        const response = await getRequest("/dashboard/charts/tasks");
        taskCounts.value = {
          in_progress: response.in_progress_count || 0,
          pending: response.pending_count || 0,
          completed: response.completed_count || 0,
          error: response.error_count || 0,
        };
      } catch (error) {
        console.error("Failed to query task counts:", error);
      }
    };

    const fetchLastSchedules = async () => {
      try {
        const response = await getRequest("/dashboard/lastest-schedules");
        schedules.value = Array.isArray(response) ? response : [];
      } catch (error) {
        console.error("Failed to query latest schedules:", error);
      }
    };

    onMounted(() => {
      fetchDeviceCounts();
      fetchScheduleCounts();
      fetchTaskCounts();
      fetchLastSchedules();
    });

    return {
      deviceCounts,
      scheduleCounts,
      taskCounts,
      schedules,
      deviceTotal,
      scheduleTotal,
      recentErrorCount,
      recentWarningCount,
      fetchLastSchedules,
    };
  },
});
</script>

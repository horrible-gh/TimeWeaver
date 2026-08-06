<template>
    <div class="section-label">
        <h2>{{ $t('section_service_status_label') }}</h2>
        <span>{{ $t('section_service_status_desc') }}</span>
    </div>
    <div class="tile-grid">
        <article class="tile">
            <div class="tile-head">
                <span class="metric-icon"><i class="ph ph-hard-drives"></i></span>
                <h3>{{ $t('chart_subtitle_device') }}</h3>
            </div>
            <div class="tile-chart">
                <DevicesBarChart />
            </div>
            <div class="tile-foot">{{ $t('chart_footer_devices') }}</div>
        </article>
        <article class="tile">
            <div class="tile-head">
                <span class="metric-icon" style="color:var(--cyan)"><i class="ph ph-calendar"></i></span>
                <h3>{{ $t('chart_subtitle_schedules') }}</h3>
            </div>
            <div class="tile-chart">
                <SchedulesBarChart />
            </div>
            <div class="tile-foot">{{ $t('chart_footer_schedules') }}</div>
        </article>
        <article class="tile">
            <div class="tile-head">
                <span class="metric-icon" style="color:var(--blue)"><i class="ph ph-wrench"></i></span>
                <h3>{{ $t('chart_subtitle_tasks') }}</h3>
            </div>
            <div class="tile-chart">
                <TasksDoughnutChart />
            </div>
            <div class="tile-foot">{{ $t('chart_footer_tasks') }}</div>
        </article>
    </div>
    <div class="service-foot">{{ $t('chart_last_updated_time_title') }}: {{ currentTime }}</div>
</template>

<script>
import DevicesBarChart from "@/dashboard/components/contents/dashboard/charts/DevicesBarChart.vue";
import SchedulesBarChart from "@/dashboard/components/contents/dashboard/charts/SchedulesBarChart.vue";
import TasksDoughnutChart from "@/dashboard/components/contents/dashboard/charts/TasksDoughnutChart.vue";

export default {
    name: 'DashboardChart'
    , components: {
        DevicesBarChart, SchedulesBarChart, TasksDoughnutChart
  },
  data() {
    return {
      currentTime: new Date().toLocaleString()
    };
  }
}
</script>

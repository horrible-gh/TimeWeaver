<template>
    <section class="service-section">
        <h2>{{ $t('chart_title') }}</h2>
        <div class="tiles">
            <article class="tile">
                <h4 class="white"><i class="ph ph-hard-drives"></i> {{ $t('chart_subtitle_device') }}</h4>
                <DevicesBarChart />
            </article>
            <article class="tile">
                <h4 class="white"><i class="ph ph-calendar"></i> {{ $t('chart_subtitle_schedules') }}</h4>
                <SchedulesBarChart />
            </article>
            <article class="tile">
                <h4 class="white"><i class="ph ph-wrench"></i> {{ $t('chart_subtitle_tasks') }}</h4>
                <TasksDoughnutChart />
            </article>
        </div>
        <div class="service-section-footer">
            <p>{{ $t('chart_last_updated_time_title') }}: {{ currentTime }}</p>
        </div>
    </section>
</template>

<script>
import DevicesBarChart from "@/dashboard/components/contents/dashboard/charts/DevicesBarChart.vue";
import SchedulesBarChart from "@/dashboard/components/contents/dashboard/charts/SchedulesBarChart.vue";
import TasksDoughnutChart from "@/dashboard/components/contents/dashboard/charts/TasksDoughnutChart.vue";

export default {
    name: 'DashboardChart'
    , components: {
        DevicesBarChart, SchedulesBarChart, TasksDoughnutChart
  },  // ✅ `DevicesBarChart` 등록 확인
  data() {
    return {
      currentTime: new Date().toLocaleString()
    };
  }
}
</script>

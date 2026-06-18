<template>
  <div class="app-body-main-content">
      <DashboardCharts />
      <LastSchedules />
  </div>
  <div class="app-body-sidebar">
      <NewTask />
  </div>
</template>

<script>
import DashboardCharts from "@/dashboard/components/contents/dashboard/DashboardCharts.vue";
import LastSchedules from "@/dashboard/components/contents/dashboard/LastSchedules.vue";
import NewTask from "@/dashboard/components/sidebar/NewTask.vue";

export default {
    name: 'DashboardOverview'
    , components: {
        DashboardCharts,
        LastSchedules,
        NewTask
    }
}

</script>

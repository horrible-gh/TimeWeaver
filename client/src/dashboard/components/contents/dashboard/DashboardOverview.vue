<template>
  <div class="app-body-main-content">
      <DashboardCharts />
      <LastSchedules />
  </div>
</template>

<script>
import DashboardCharts from "@/dashboard/components/contents/dashboard/DashboardCharts.vue";
import LastSchedules from "@/dashboard/components/contents/dashboard/LastSchedules.vue";

export default {
    name: 'DashboardOverview'
    , components: {
        DashboardCharts,
        LastSchedules
    }
}

</script>

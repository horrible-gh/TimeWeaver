<template>
  <div class="app-body-main-content">
    <ScheduleHistoryList />
  </div>
  <!--
  <div class="app-body-sidebar">
  </div>
  -->
</template>

<script>
import ScheduleHistoryList from "@/dashboard/components/contents/history/ScheduleHistoryList.vue";

export default {
    name: 'ScheduleHistory'
    , components: {
      ScheduleHistoryList,
    }
}
</script>

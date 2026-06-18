<template>
  <div class="app-body-main-content">
    <ScheduleList />
  </div>
</template>

<script>
import ScheduleList from "@/dashboard/components/contents/schedules/ScheduleList.vue";

export default {
    name: 'SchedulesOverview'
    , components: {
      ScheduleList,
    }
}
</script>

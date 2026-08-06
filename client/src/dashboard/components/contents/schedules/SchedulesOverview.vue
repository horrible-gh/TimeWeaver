<template>
  <div class="page-head">
    <div>
      <div class="eyebrow">{{ $t('schedules_eyebrow') }}</div>
      <h1>{{ $t('sub_schedules') }}</h1>
      <p class="lead">{{ $t('schedules_lead') }}</p>
    </div>
  </div>
  <ScheduleList />
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

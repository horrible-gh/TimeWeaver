<template>
  <div class="page-head">
    <div>
      <div class="eyebrow">{{ $t('history_eyebrow') }}</div>
      <h1>{{ $t('sub_history') }}</h1>
      <p class="lead">{{ $t('history_lead') }}</p>
    </div>
  </div>
  <ScheduleHistoryList />
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

<template>
  <div class="page-head">
    <div>
      <div class="eyebrow">{{ $t('manual_execution_eyebrow') }}</div>
      <h1>{{ $t('sub_manual_execution') }}</h1>
      <p class="lead">{{ $t('manual_execution_lead') }}</p>
    </div>
  </div>
  <ManualExecutionHistoryList />
</template>

<script>
import ManualExecutionHistoryList from "@/dashboard/components/contents/manual_execution/ManualExecutionHistoryList.vue";

export default {
    name: 'ManualExecutionHistory'
    , components: {
      ManualExecutionHistoryList,
    }
}
</script>

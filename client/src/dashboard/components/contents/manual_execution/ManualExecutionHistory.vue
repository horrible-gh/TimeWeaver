<template>
  <div class="app-body-main-content">
    <ManualExecutionHistoryList />
  </div>
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

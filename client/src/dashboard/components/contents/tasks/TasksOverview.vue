<template>
  <div class="page-head">
    <div>
      <div class="eyebrow">{{ $t('tasks_eyebrow') }}</div>
      <h1>{{ $t('sub_tasks') }}</h1>
      <p class="lead">{{ $t('tasks_lead') }}</p>
    </div>
  </div>
  <TaskList />
</template>

<script>
import TaskList from "@/dashboard/components/contents/tasks/TaskList.vue";

export default {
    name: 'TasksOverview'
    , components: {
      TaskList,
    }
}
</script>

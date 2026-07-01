<template>
  <div class="app-body-main-content">
    <TaskList />
  </div>
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

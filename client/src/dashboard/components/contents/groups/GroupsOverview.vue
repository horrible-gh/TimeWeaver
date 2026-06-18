<template>
  <div class="app-body-main-content">
    <GroupList />
  </div>
</template>

<script>
import GroupList from "@/dashboard/components/contents/groups/GroupList.vue";

export default {
    name: 'GroupsOverview'
    , components: {
      GroupList,
    }
}
</script>

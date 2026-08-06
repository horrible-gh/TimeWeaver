<template>
  <div class="page-head">
    <div>
      <div class="eyebrow">{{ $t('groups_eyebrow') }}</div>
      <h1>{{ $t('sub_groups') }}</h1>
      <p class="lead">{{ $t('groups_lead') }}</p>
    </div>
  </div>
  <GroupList />
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

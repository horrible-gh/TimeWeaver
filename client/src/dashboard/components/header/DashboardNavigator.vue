<template>
  <div class="tabs">
    <RouterLink
      to="/"
      :class="{ active: activeTab === '/' }"
      @click="setActiveTab('/')"
    >
      Overview
    </RouterLink>

    <RouterLink
      to="/groups"
      :class="{ active: activeTab === '/groups' }"
      @click="setActiveTab('/groups')"
    >
      Groups
    </RouterLink>

    <RouterLink
      to="/devices"
      :class="{ active: activeTab === '/devices' }"
      @click="setActiveTab('/devices')"
    >
      Devices
    </RouterLink>


    <RouterLink
      to="/schedule-history"
      :class="{ active: activeTab === '/schedule-history' }"
      @click="setActiveTab('/schedule-history')"
    >
      Execution History
    </RouterLink>

  </div>
</template>

<script setup>
import { ref, watch } from 'vue';
import { useRoute } from 'vue-router';

const route = useRoute();
const activeTab = ref(route.path); // ✅ Use the current URL as the default value

const setActiveTab = (tabPath) => {
  activeTab.value = tabPath;
};

// ✅ Update activeTab automatically when the URL changes
watch(route, () => {
  activeTab.value = route.path;
});
</script>

<style scoped>
/* ✅ Tab style */
.tabs {
  display: flex;
  gap: 10px;
}

.tabs a {
  padding: 10px 15px;
  text-decoration: none;
  color: #333;
  border-bottom: 2px solid transparent;
  transition: all 0.3s;
}

/* ✅ Active tab style */
.tabs a.active {
  font-weight: bold;
  color: white;
  border-bottom: 2px solid white;
}
</style>

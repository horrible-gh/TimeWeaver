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
const activeTab = ref(route.path); // ✅ 현재 URL을 기본값으로 설정

const setActiveTab = (tabPath) => {
  activeTab.value = tabPath;
};

// ✅ URL이 변경되면 `activeTab`도 자동 업데이트
watch(route, () => {
  activeTab.value = route.path;
});
</script>

<style scoped>
/* ✅ 탭 스타일 */
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

/* ✅ 활성화된 탭 스타일 */
.tabs a.active {
  font-weight: bold;
  color: white;
  border-bottom: 2px solid white;
}
</style>

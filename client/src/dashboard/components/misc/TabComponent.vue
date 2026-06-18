<!-- TabComponent.vue -->
<template>
  <div>
    <div style="display:flex; gap:10px;">
      <button
        v-for="(tab, idx) in tabs"
        :key="idx"
        :class="{ active: activeTab === idx }"
        @click="activeTab = idx"
      >
        {{ tab.title }}
      </button>
    </div>
    <div style="margin-top:10px;">
      <slot :name="tabs[activeTab].slot"></slot>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'

const props = defineProps({
  tabs: Array // [{ title: '기본 정보', slot: 'basic' }, ...]
})

const activeTab = ref(0)
</script>

<style scoped>
button.active {
  font-weight: bold;
  border-bottom: 2px solid blue;
}
</style>

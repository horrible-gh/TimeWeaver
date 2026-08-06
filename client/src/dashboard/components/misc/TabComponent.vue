<!-- TabComponent.vue -->
<template>
  <div>
    <div class="command-tabs">
      <button
        v-for="(tab, idx) in tabs"
        :key="idx"
        class="command-tab"
        :class="{ active: activeTab === idx }"
        @click="activeTab = idx"
      >
        {{ tab.title }}
      </button>
    </div>
    <div class="tab-panel">
      <slot :name="tabs[activeTab].slot"></slot>
    </div>
  </div>
</template>

<script setup>
import { ref, defineProps } from 'vue'

defineProps({
  tabs: Array // [{ title: 'Basic information', slot: 'basic' }, ...]
})

const activeTab = ref(0)
</script>

<!--
  No <style scoped> block: `.command-tabs` / `.command-tab` are the shared tab
  rules in components.css section 11 (deck `devices.html` command tabs).
-->

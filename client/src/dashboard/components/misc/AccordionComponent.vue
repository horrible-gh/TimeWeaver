<!-- AccordionComponent.vue -->
<template>
  <div class="accordion">
    <div v-for="(item, idx) in items" :key="idx" class="accordion-item">
      <button
        class="accordion-head"
        :class="{ open: openIndex === idx }"
        :aria-expanded="openIndex === idx"
        @click="toggle(idx)"
      >
        <span>{{ item.title }}</span>
        <i class="ph ph-caret-down"></i>
      </button>
      <div v-show="openIndex === idx" class="accordion-body">
        <slot :name="item.slot"></slot>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, defineProps } from 'vue'

defineProps({
  items: Array // [{ title: 'Basic information', slot: 'basic' }, ...]
})

const openIndex = ref(null)

function toggle(index) {
  openIndex.value = openIndex.value === index ? null : index
}
</script>

<!--
  No <style scoped> block: `.accordion` / `.accordion-head` / `.accordion-body`
  are in components.css section 11. The inline grey border literal that used to
  sit on the body div is gone.
-->

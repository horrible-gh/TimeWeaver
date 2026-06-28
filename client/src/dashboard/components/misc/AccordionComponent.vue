<!-- AccordionComponent.vue -->
<template>
  <div>
    <div v-for="(item, idx) in items" :key="idx">
      <button @click="toggle(idx)" style="width:100%;text-align:left;">
        {{ item.title }}
      </button>
      <div v-show="openIndex === idx" style="padding:10px; border:1px solid #ccc;">
        <slot :name="item.slot"></slot>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'

const props = defineProps({
  items: Array // [{ title: 'Basic information', slot: 'basic' }, ...]
})

const openIndex = ref(null)

function toggle(index) {
  openIndex.value = openIndex.value === index ? null : index
}
</script>

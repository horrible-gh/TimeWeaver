<template>
  <div class="dropdown-content">
    <a v-for="(item, index) in menuItems" :key="index" @click="handleClick(item)">
      {{ item.label }}
    </a>
  </div>
</template>

<script setup>
import { defineProps, defineEmits } from "vue";

// ✅ Receive menu items from parent
defineProps({
  menuItems: Array,
});

// ✅ Forward click event to parent component
const emit = defineEmits(["select"]);

const handleClick = (item) => {
  console.log("Clicked menu:", item.label);
  emit("select", item); // ✅ Forward event to parent component
};
</script>

<style scoped>
/* ✅ Dropdown style */
.dropdown-content {
  display: block;
  position: absolute;
  background-color: #5a5a5a;
  min-width: 160px;
  box-shadow: 0px 8px 16px rgba(0, 0, 0, 0.2);
  z-index: 1;
}

.dropdown-content a {
  color: white;
  padding: 12px 16px;
  text-decoration: none;
  display: block;
}

.dropdown-content a:hover {
  background-color: #353535;
}
</style>

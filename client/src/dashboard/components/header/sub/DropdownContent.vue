<template>
  <div class="dropdown-content" role="menu">
    <button
      v-for="(item, index) in menuItems"
      :key="index"
      type="button"
      role="menuitem"
      @click="handleClick(item)"
    >
      {{ item.label }}
    </button>
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
  emit("select", item); // ✅ Forward event to parent component
};
</script>

<style scoped>
/* ✅ Item styling only. Placement (drop-down vs drop-up), surface and border
      are decided by the host component, so the same menu can be reused.
      Items are <button>, not <a href>: the host opens the menu on click, so an
      anchor without an href would leave the items unreachable by keyboard. */
.dropdown-content button {
  display: block;
  width: 100%;
  padding: 10px 14px;
  border: 0;
  background: transparent;
  color: var(--muted);
  font: inherit;
  font-size: 12px;
  text-align: left;
  text-decoration: none;
  cursor: pointer;
}

.dropdown-content button + button {
  border-top: 1px solid var(--line-soft);
}

.dropdown-content button:hover,
.dropdown-content button:focus-visible {
  color: var(--text);
  background: var(--hover-soft);
}
</style>

<template>
  <div class="dropdown-content">
    <a v-for="(item, index) in menuItems" :key="index" @click="handleClick(item)">
      {{ item.label }}
    </a>
  </div>
</template>

<script setup>
import { defineProps, defineEmits } from "vue";

// ✅ 부모로부터 메뉴 아이템을 받음
defineProps({
  menuItems: Array,
});

// ✅ 클릭 이벤트를 부모 컴포넌트에 전달
const emit = defineEmits(["select"]);

const handleClick = (item) => {
  console.log("클릭된 메뉴:", item.label);
  emit("select", item); // ✅ 부모 컴포넌트에 이벤트 전달
};
</script>

<style scoped>
/* ✅ 드롭다운 스타일 */
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

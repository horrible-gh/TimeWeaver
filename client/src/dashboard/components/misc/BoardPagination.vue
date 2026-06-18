<template>
  <nav class="pagination-container">
    <ul class="pagination">
      <!-- 처음 페이지 이동 -->
      <li class="page-item" :class="{ disabled: currentPage === 1 }">
        <button class="page-link" @click.prevent="changePage(1)">«</button>
      </li>

      <!-- 이전 페이지 이동 -->
      <li class="page-item" :class="{ disabled: currentPage === 1 }">
        <button class="page-link" @click.prevent="changePage(currentPage - 1)">{{ $t('btn_prev') }}</button>
      </li>

      <!-- 첫 번째 페이지 표시 (필요하면 항상 보이도록) -->
      <li v-if="startPage > 1" class="page-item">
        <button class="page-link" @click.prevent="changePage(1)">1</button>
      </li>

      <!-- '...' (중간 생략) -->
      <li v-if="startPage > 2" class="page-item disabled">
        <span class="page-link">...</span>
      </li>

      <!-- 페이지 번호 리스트 (최대 5개) -->
      <li v-for="page in visiblePages" :key="page" class="page-item" :class="{ active: currentPage === page }">
        <button class="page-link" @click.prevent="changePage(page)">{{ page }}</button>
      </li>

      <!-- '...' (끝 부분 생략) -->
      <li v-if="endPage < totalPages - 1" class="page-item disabled">
        <span class="page-link">...</span>
      </li>

      <!-- 마지막 페이지 표시 (필요하면 항상 보이도록) -->
      <li v-if="endPage < totalPages" class="page-item">
        <button class="page-link" @click.prevent="changePage(totalPages)">{{ totalPages }}</button>
      </li>

      <!-- 다음 페이지 이동 -->
      <li class="page-item" :class="{ disabled: currentPage === totalPages }">
        <button class="page-link" @click.prevent="changePage(currentPage + 1)">{{ $t('btn_next') }}</button>
      </li>

      <!-- 마지막 페이지 이동 -->
      <li class="page-item" :class="{ disabled: currentPage === totalPages }">
        <button class="page-link" @click.prevent="changePage(totalPages)">»</button>
      </li>
    </ul>
  </nav>
</template>

<script setup>
import { ref, computed } from "vue";
import { defineProps, defineEmits } from "vue";

const props = defineProps({ total: Number, perPage: Number });
const emit = defineEmits(["page-changed"]);

const currentPage = ref(1);
const totalPages = computed(() => Math.ceil(props.total / props.perPage));

const maxVisiblePages = 5; // ✅ 최대 표시할 페이지 개수

// ✅ 시작 페이지 & 끝 페이지 계산 (현재 페이지 중심)
const startPage = computed(() => {
  if (totalPages.value <= maxVisiblePages) return 1;
  let start = currentPage.value - Math.floor(maxVisiblePages / 2);
  if (start < 1) start = 1;
  return start;
});

const endPage = computed(() => {
  let end = startPage.value + maxVisiblePages - 1;
  if (end > totalPages.value) end = totalPages.value;
  return end;
});

// ✅ 표시할 페이지 목록
const visiblePages = computed(() => {
  const pages = [];
  for (let i = startPage.value; i <= endPage.value; i++) {
    pages.push(i);
  }
  return pages;
});

const changePage = (page) => {
  if (page >= 1 && page <= totalPages.value) {
    currentPage.value = page;
    emit("page-changed", page);
  }
};
</script>

<style scoped>
.pagination-container {
  display: flex;
  justify-content: center;
  margin-top: 20px;
}

.pagination {
  list-style: none;
  padding: 0;
  display: flex;
}

.page-item {
  margin: 0 5px;
}

.page-link {
  padding: 8px 12px;
  border: 1px solid white;
  background-color: transparent;
  color: white;
  cursor: pointer;
  border-radius: 4px;
  transition: 0.3s;
}

.page-link:hover {
  background-color: #106baf;
  color: white;
}

.page-item.disabled .page-link {
  color: #6c757d;
  cursor: not-allowed;
}

.page-item.active .page-link {
  background-color: #001b4f;
  color: white;
  font-weight: bold;
}
</style>

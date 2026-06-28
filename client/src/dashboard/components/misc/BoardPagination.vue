<template>
  <nav class="pagination-container">
    <ul class="pagination">
      <!-- Go to first page -->
      <li class="page-item" :class="{ disabled: currentPage === 1 }">
        <button class="page-link" @click.prevent="changePage(1)">«</button>
      </li>

      <!-- Go to previous page -->
      <li class="page-item" :class="{ disabled: currentPage === 1 }">
        <button class="page-link" @click.prevent="changePage(currentPage - 1)">{{ $t('btn_prev') }}</button>
      </li>

      <!-- Show first page when needed -->
      <li v-if="startPage > 1" class="page-item">
        <button class="page-link" @click.prevent="changePage(1)">1</button>
      </li>

      <!-- '...' (Middle ellipsis) -->
      <li v-if="startPage > 2" class="page-item disabled">
        <span class="page-link">...</span>
      </li>

      <!-- Page number list, up to 5 -->
      <li v-for="page in visiblePages" :key="page" class="page-item" :class="{ active: currentPage === page }">
        <button class="page-link" @click.prevent="changePage(page)">{{ page }}</button>
      </li>

      <!-- '...' (End ellipsis) -->
      <li v-if="endPage < totalPages - 1" class="page-item disabled">
        <span class="page-link">...</span>
      </li>

      <!-- Show last page when needed -->
      <li v-if="endPage < totalPages" class="page-item">
        <button class="page-link" @click.prevent="changePage(totalPages)">{{ totalPages }}</button>
      </li>

      <!-- Go to next page -->
      <li class="page-item" :class="{ disabled: currentPage === totalPages }">
        <button class="page-link" @click.prevent="changePage(currentPage + 1)">{{ $t('btn_next') }}</button>
      </li>

      <!-- Go to last page -->
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

const maxVisiblePages = 5; // ✅ Maximum number of visible pages

// ✅ Calculate start and end pages around the current page
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

// ✅ Visible page list
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

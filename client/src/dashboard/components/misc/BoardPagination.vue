<template>
  <nav>
    <ul class="pages">
      <!-- Go to first page -->
      <li class="page-item" :class="{ disabled: currentPage === 1 }">
        <button class="page" @click.prevent="changePage(1)">&laquo;</button>
      </li>

      <!-- Go to previous page -->
      <li class="page-item" :class="{ disabled: currentPage === 1 }">
        <button class="page" @click.prevent="changePage(currentPage - 1)">{{ $t('btn_prev') }}</button>
      </li>

      <!-- Show first page when needed -->
      <li v-if="startPage > 1" class="page-item">
        <button class="page" @click.prevent="changePage(1)">1</button>
      </li>

      <!-- '...' (Middle ellipsis) -->
      <li v-if="startPage > 2" class="page-item disabled">
        <span class="page">...</span>
      </li>

      <!-- Page number list, up to 5 -->
      <li v-for="page in visiblePages" :key="page" class="page-item" :class="{ active: currentPage === page }">
        <button class="page" @click.prevent="changePage(page)">{{ page }}</button>
      </li>

      <!-- '...' (End ellipsis) -->
      <li v-if="endPage < totalPages - 1" class="page-item disabled">
        <span class="page">...</span>
      </li>

      <!-- Show last page when needed -->
      <li v-if="endPage < totalPages" class="page-item">
        <button class="page" @click.prevent="changePage(totalPages)">{{ totalPages }}</button>
      </li>

      <!-- Go to next page -->
      <li class="page-item" :class="{ disabled: currentPage === totalPages }">
        <button class="page" @click.prevent="changePage(currentPage + 1)">{{ $t('btn_next') }}</button>
      </li>

      <!-- Go to last page -->
      <li class="page-item" :class="{ disabled: currentPage === totalPages }">
        <button class="page" @click.prevent="changePage(totalPages)">&raquo;</button>
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

const maxVisiblePages = 5; // Maximum number of visible pages

// Calculate start and end pages around the current page
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

// Visible page list
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

<!--
  No <style scoped> block: `.pages`, `.page`, `.page-item.active` and
  `.page-item.disabled` are the shared pager rules in components.css
  section 8 (deck vocabulary, no bridge). The three colour literals that
  used to live here are gone.
-->

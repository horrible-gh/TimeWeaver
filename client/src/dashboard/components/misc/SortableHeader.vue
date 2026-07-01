<template>
  <th
    :class="cssClass"
    :style="{ width, cursor: sortable ? 'pointer' : 'default' }"
    @click="handleClick"
  >
    {{ label }}
    <span v-if="sortable && currentSortKey === field">
      {{ sortOrder === 'asc' ? '▲' : '▼' }}
    </span>
  </th>
</template>

<script>
export default {
  name: 'SortableHeader',
  props: {
    field: String,
    label: String,
    cssClass: String,
    currentSortKey: String,
    sortOrder: {
      type: String,
      default: 'asc'
    },
    sort: Function,
    width: {
      type: String,
      default: null
    },
    sortable: {
      type: Boolean,
      default: true
    }
  },
  methods: {
    handleClick() {
      if (this.sort) {
        this.sort(this.field);
      }
    }
  }
}
</script>

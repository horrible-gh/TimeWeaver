<template>
  <div class="searchable-select">
    <input 
      type="text"
      v-model="searchText"
      :list="listId"
      @change="onSelect"
      @input="onInput"
      @blur="validateInput"
      :placeholder="placeholder"
    />
    <datalist :id="listId">
      <option 
        v-for="item in items" 
        :key="item[valueKey]" 
        :value="item[labelKey]"
      />
    </datalist>
  </div>
</template>

<script>
export default {
  name: 'SearchableSelect',
  props: {
    modelValue: [String, Number],
    items: { type: Array, required: true },
    labelKey: { type: String, default: 'name' },
    valueKey: { type: String, default: 'id' },
    placeholder: { type: String, default: 'Searching...' }
  },
  data() {
    return {
      searchText: '',
      listId: `datalist-${Math.random().toString(36).substr(2, 9)}`
    }
  },
  watch: {
    modelValue: {
      immediate: true,
      handler(val) {
        const item = this.items.find(i => i[this.valueKey] === val);
        this.searchText = item ? item[this.labelKey] : '';
      }
    }
  },
  methods: {
    onSelect() {
      const item = this.items.find(i => i[this.labelKey] === this.searchText);
      this.$emit('update:modelValue', item ? item[this.valueKey] : null);
    },
    onInput() {
      if (!this.searchText) this.$emit('update:modelValue', null);
    },
    validateInput() {
      const item = this.items.find(i => i[this.labelKey] === this.searchText);
      if (!item) {
        this.searchText = '';
        this.$emit('update:modelValue', null);
      }
    }
  }
}
</script>
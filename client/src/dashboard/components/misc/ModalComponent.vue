<template>
  <div v-if="isOpen" class="modal-overlay">
    <div class="modal-panel" @click.stop>
      <!-- Head: title + close -->
      <div class="modal-head">
        <h3 class="modal-title">{{ title }}</h3>
        <button class="modal-close" :aria-label="$t('btn_close')" @click="close">&times;</button>
      </div>

      <!-- Body: message and/or caller supplied markup -->
      <div class="modal-body">
        <p v-if="message">{{ message }}</p>
        <slot></slot>
      </div>

      <!-- Foot: cancel + optional confirm -->
      <div class="modal-foot">
        <button class="btn" @click="close">{{ $t('btn_close') }}</button>
        <button v-if="confirmText" class="btn primary" @click="confirmAction">
          {{ confirmText }}
        </button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { defineProps, defineEmits } from "vue";

defineProps({
  isOpen: Boolean, // Whether the modal is visible
  title: String, // Modal title
  message: String, // Message
  confirmText: String, // Confirm button text
});


const emit = defineEmits(["close", "confirm"]);

// Close modal
const close = () => {
  emit("close");
};

// Confirm button click
const confirmAction = () => {
  emit("confirm");
};
</script>

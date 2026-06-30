<template>
  <div v-if="isOpen" class="modal-overlay">
    <div class="modal-content" @click.stop>
      <!-- Close button -->
      <button class="modal-close" @click="close">&times;</button>

      <!-- ✅ Modal title (dynamic data) -->
      <h3 class="modal-title">{{ title }}</h3>

      <!-- ✅ Body content -->
      <div class="modal-body">
        <p v-if="message">{{ message }}</p>
        <slot></slot> <!-- ✅ Add slot so extra HTML can be inserted -->
      </div>

      <!-- ✅ Button area -->
      <div class="modal-actions">
        <button class="cancel-button" @click="close">Close</button>
        <button v-if="confirmText" class="save-button" @click="confirmAction">
          {{ confirmText }}
        </button>
      </div>
    </div>
  </div>
</template>

<script setup>
// import { defineEmits } from "vue";
import { defineProps, defineEmits } from "vue";

// ✅ Use props without assigning them to variables
defineProps({
  isOpen: Boolean, // ✅ Whether the modal is visible
  title: String, // ✅ Modal title
  message: String, // ✅ Message
  confirmText: String, // ✅ Confirm button text
});


const emit = defineEmits(["close", "confirm"]);

// ✅ Close modal
const close = () => {
  emit("close");
};

// ✅ Confirm button click
const confirmAction = () => {
  emit("confirm");
};
</script>

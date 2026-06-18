<template>
  <div v-if="isOpen" class="modal-overlay" @click="close">
    <div class="modal-content" @click.stop>
      <!-- 닫기 버튼 -->
      <button class="modal-close" @click="close">&times;</button>

      <!-- ✅ 모달 제목 (동적 데이터 적용) -->
      <h3 class="modal-title">{{ title }}</h3>

      <!-- ✅ 본문 내용 -->
      <div class="modal-body">
        <p v-if="message">{{ message }}</p>
        <slot></slot> <!-- ✅ 추가적인 HTML을 삽입할 수 있도록 slot 추가 -->
      </div>

      <!-- ✅ 버튼 영역 -->
      <div class="modal-actions">
        <button class="cancel-button" @click="close">닫기</button>
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

// ✅ props는 변수 할당 없이 사용해야 함!
defineProps({
  isOpen: Boolean, // ✅ 모달 표시 여부
  title: String, // ✅ 모달 제목
  message: String, // ✅ 메시지
  confirmText: String, // ✅ 확인 버튼 텍스트
});


const emit = defineEmits(["close", "confirm"]);

// ✅ 모달 닫기
const close = () => {
  emit("close");
};

// ✅ 확인 버튼 클릭
const confirmAction = () => {
  emit("confirm");
};
</script>

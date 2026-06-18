<template>
  <div class="app-header-actions">
    <div class="user-profile">
      <span>{{ userId || 'Guest' }}</span> <!-- ✅ ID가 없으면 'Guest' 표시 -->
      <div class="dropdown">
        <button class="dropbtn">
          <img src="@/assets/img/dashboard/avartar1.webp" class="profile-icon" />
        </button>
        <DropdownContent :menuItems="menuList" @select="handleMenuClick" />
      </div>
    </div>
  </div>
  <div class="app-header-mobile">
    <div class="dropdown">
      <button class="dropbtn">
        <i class="ph ph-list"></i>
      </button>
      <DropdownContent :menuItems="menuList" @select="handleMenuClick" />
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from "vue";
import DropdownContent from "@/dashboard/components/header/sub/DropdownContent.vue";

// ✅ 드롭다운 메뉴 리스트
const menuList = [
  { label: "홈", action: () => {window.location.href = "/dashboard";} },
  {
    label: "테스트",
    action: () => {
      localStorage.removeItem("access_token"); // ✅ 토큰 삭제
    }
  },
  {
    label: "로그아웃",
    action: () => {
      localStorage.removeItem("access_token"); // ✅ 토큰 삭제
      window.location.href = "/login"; // ✅ 로그인 페이지로 강제 이동
    }
  },
];

// ✅ 메뉴 클릭 시 실행되는 함수
const handleMenuClick = (item) => {
  console.log("선택된 메뉴:", item.label);
  item.action(); // ✅ 해당 메뉴의 액션 실행
};

// ✅ 유저 ID를 저장할 반응형 변수
const userId = ref("");

// ✅ 마운트될 때 localStorage에서 ID 가져오기
onMounted(() => {
  const user = JSON.parse(localStorage.getItem("user") || "{}"); // ✅ 문자열 → 객체 변환
  userId.value = user.name || "Guest"; // ✅ "테스트"가 표시됨
});
</script>

<style scoped>
/* ✅ 드롭다운 스타일 */
.dropbtn {
  background-color: #1f1f1f;
  color: white;
  padding: 16px;
  font-size: 16px;
  border: none;
  cursor: pointer;
}

.dropdown {
  position: relative;
  display: inline-block;
}

.dropdown-content {
  display: none;
  position: absolute;
  background-color: #5a5a5a;
  min-width: 160px;
  box-shadow: 0px 8px 16px rgba(0, 0, 0, 0.2);
  z-index: 1;
  right: 0;
}

.dropdown-content a {
  color: black;
  padding: 12px 16px;
  text-decoration: none;
  display: block;
  right: 0;
}

.dropdown-content a:hover {
  background-color: #efefef;
}

.dropdown:hover .dropdown-content {
  display: block;
}

.dropdown:hover .dropbtn {
  background-color: #1f1f1f;
}
</style>

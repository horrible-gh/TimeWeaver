<template>
  <div class="app-header-actions">
    <div class="user-profile">
      <span>{{ userId || 'Guest' }}</span> <!-- ✅ Show Guest when ID is missing -->
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

// ✅ Dropdown menu list
const menuList = [
  { label: "Home", action: () => {window.location.href = "/dashboard";} },
  {
    label: "Test",
    action: () => {
      localStorage.removeItem("access_token"); // ✅ Remove token
    }
  },
  {
    label: "Logout",
    action: () => {
      localStorage.removeItem("access_token"); // ✅ Remove token
      window.location.href = "/login"; // ✅ Force redirect to login page
    }
  },
];

// ✅ Function executed on menu click
const handleMenuClick = (item) => {
  console.log("Selected menu:", item.label);
  item.action(); // ✅ Run the selected menu action
};

// ✅ Reactive variable for the user ID
const userId = ref("");

// ✅ Read the ID from localStorage on mount
onMounted(() => {
  const user = JSON.parse(localStorage.getItem("user") || "{}"); // ✅ Convert string to object
  userId.value = user.name || "Guest"; // ✅ Show Guest when the stored user has no name
});
</script>

<style scoped>
/* ✅ Dropdown style */
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

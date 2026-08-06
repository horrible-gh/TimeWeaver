<template>
  <div ref="root" class="user dropup">
    <button
      ref="trigger"
      class="user-trigger"
      type="button"
      :aria-label="userId || 'Guest'"
      aria-haspopup="menu"
      :aria-expanded="isOpen ? 'true' : 'false'"
      @click="toggle"
    >
      <span class="avatar">
        <img src="@/assets/img/dashboard/avartar1.webp" alt="" />
      </span>
      <span class="user-copy">
        <strong>{{ userId || 'Guest' }}</strong> <!-- ✅ Show Guest when ID is missing -->
        <small v-if="userRole">{{ userRole }}</small>
      </span>
      <i class="ph ph-caret-up caret"></i>
    </button>
    <DropdownContent v-if="isOpen" :menuItems="menuList" @select="handleMenuClick" />
  </div>
</template>

<script setup>
import { onBeforeUnmount, onMounted, ref } from "vue";
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

// The menu used to open on :hover alone. The trigger sits at the very bottom of
// the sidebar and the panel floats above it across an 8px gap, so a diagonal
// reach towards an item left `.dropup` for a frame, `:hover` dropped and the
// panel disappeared mid-click. An explicit open/close state removes the pointer
// race: the menu only closes on a real decision (pick an item, click outside,
// press Escape, or click the trigger again).
const isOpen = ref(false);
const root = ref(null);
const trigger = ref(null);

function close(refocus = false) {
  if (!isOpen.value) return;
  isOpen.value = false;
  if (refocus && trigger.value) trigger.value.focus();
}
function toggle() {
  if (isOpen.value) close();
  else isOpen.value = true;
}

// The trigger and every item live inside `root`, so their own pointerdown is
// ignored here and handled by the element that owns it.
function onDocumentPointerDown(event) {
  if (!isOpen.value) return;
  if (root.value && root.value.contains(event.target)) return;
  close();
}
function onDocumentKeydown(event) {
  if (event.key === "Escape") close(true);
}

// ✅ Function executed on menu click
const handleMenuClick = (item) => {
  close(); // ✅ A picked item always dismisses the menu
  item.action(); // ✅ Run the selected menu action
};

// ✅ Reactive variables for the signed-in user
const userId = ref("");
const userRole = ref("");

// ✅ Read the ID from localStorage on mount
onMounted(() => {
  const user = JSON.parse(localStorage.getItem("user") || "{}"); // ✅ Convert string to object
  userId.value = user.name || "Guest"; // ✅ Show Guest when the stored user has no name
  userRole.value = user.role || ""; // ✅ Secondary line, hidden when the API sends no role
  document.addEventListener("pointerdown", onDocumentPointerDown);
  document.addEventListener("keydown", onDocumentKeydown);
});

onBeforeUnmount(() => {
  document.removeEventListener("pointerdown", onDocumentPointerDown);
  document.removeEventListener("keydown", onDocumentKeydown);
});
</script>

<style scoped>
/* ✅ The profile block sits at the bottom of the sidebar, so the menu opens
      upward instead of downward. Every colour comes from tokens.css. */
.dropup {
  position: relative;
}

.user-trigger {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 6px;
  border: 0;
  border-radius: var(--radius-sm);
  background: transparent;
  color: inherit;
  text-align: left;
  cursor: pointer;
}

.user-trigger:hover,
.user-trigger[aria-expanded="true"] {
  background: var(--hover-soft);
}

/* Disclosure affordance: the menu is now a click target, not a hover zone. */
.caret {
  margin-left: auto;
  color: var(--muted-2);
  font-size: 12px;
  transition: transform .15s ease;
}

.user-trigger[aria-expanded="true"] .caret {
  transform: rotate(180deg);
}

/* Rendered only while open (v-if), so no display toggle is needed here. */
.dropup :deep(.dropdown-content) {
  position: absolute;
  left: 0;
  bottom: calc(100% + 8px);
  z-index: 20;
  width: 100%;
  min-width: 170px;
  overflow: hidden;
  border: 1px solid var(--line);
  border-radius: var(--radius-sm);
  background: var(--panel-2);
  box-shadow: var(--shadow);
}
</style>

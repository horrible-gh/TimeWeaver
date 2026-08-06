<template>
  <div class="nav-groups">
    <template v-for="group in groups" :key="group.label">
      <div class="nav-label">{{ $t(group.label) }}</div>
      <nav class="nav">
        <a
          v-for="item in group.items"
          :key="item.href"
          :href="item.href"
          :class="{ active: isActive(item) }"
          @click="$emit('navigate')"
        >
          <i class="ph" :class="item.icon"></i>
          <span>{{ $t(item.label) }}</span>
        </a>
      </nav>
    </template>
  </div>
</template>

<script setup>
import { defineEmits } from "vue";
import { useRoute } from "vue-router";

defineEmits(["navigate"]);

const route = useRoute();

// ✅ `href` keeps the previous full-page navigation behaviour untouched;
//    `path` is the router path used only to decide the active highlight.
//    The router base is "/dashboard/", so the two differ by that prefix.
const groups = [
  {
    label: "shell_nav_workspace",
    items: [
      { label: "sub_dashboard", icon: "ph-speedometer", href: "/dashboard", path: "/" },
      { label: "sub_groups", icon: "ph-users", href: "/dashboard/groups", path: "/groups" },
      { label: "sub_devices", icon: "ph-hard-drives", href: "/dashboard/devices", path: "/devices" },
      { label: "sub_schedules", icon: "ph-calendar", href: "/dashboard/schedules", path: "/schedules" },
      { label: "sub_tasks", icon: "ph-alarm", href: "/dashboard/tasks", path: "/tasks" },
      { label: "sub_history", icon: "ph-clock-counter-clockwise", href: "/dashboard/schedule-history", path: "/schedule-history" },
      { label: "sub_manual_execution", icon: "ph-play-pause", href: "/dashboard/manual-execution", path: "/manual-execution" },
    ],
  },
  {
    label: "shell_nav_account",
    items: [
      { label: "sub_users", icon: "ph-user", href: "#", path: null },
      { label: "sub_logout", icon: "ph-lock-key-open", href: "/dashboard/logout", path: null },
    ],
  },
];

const isActive = (item) => item.path !== null && route.path === item.path;
</script>

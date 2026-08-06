<template>
  <div class="shell" :class="{ 'nav-open': navOpen }">
    <aside class="sidebar">
      <DashboardLogo />
      <NavMenu @navigate="closeNav" />
      <div class="sidebar-foot">
        <UserProfile />
        <NavFooter />
      </div>
    </aside>

    <div class="shell-scrim" @click="closeNav"></div>

    <main class="main">
      <header class="topbar">
        <DashboardNavigator />
        <div class="top-actions">
          <button
            class="icon-btn nav-toggle"
            type="button"
            :aria-label="$t('shell_toggle_nav')"
            :aria-expanded="navOpen ? 'true' : 'false'"
            @click="toggleNav"
          >
            <i class="ph ph-list"></i>
          </button>
        </div>
      </header>

      <div class="content">
        <router-view></router-view> <!-- ✅ Render the current route component here -->
      </div>
    </main>
  </div>
</template>

<script setup>
import { ref, watch } from "vue";
import { useRoute } from "vue-router";

import DashboardLogo from "@/dashboard/components/header/DashboardLogo.vue";
import DashboardNavigator from "@/dashboard/components/header/DashboardNavigator.vue";
import UserProfile from "@/dashboard/components/header/UserProfile.vue";
import NavMenu from "@/dashboard/components/navigator/NavMenu.vue";
import NavFooter from "@/dashboard/components/navigator/NavFooter.vue";

const route = useRoute();

// ✅ Off-canvas sidebar state. Only reachable below the 760px breakpoint,
//    where the sidebar becomes a drawer (see style.css section 8).
const navOpen = ref(false);

const toggleNav = () => {
  navOpen.value = !navOpen.value;
};

const closeNav = () => {
  navOpen.value = false;
};

// ✅ Close the drawer whenever navigation happens
watch(route, closeNav);
</script>

<script>
/*
Copyright (c) 2022 by Filip Vitas
(https://codepen.io/FilipVitas/pen/yPJybr)

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the above copyright notice.
*/

/*
MIT License

Copyright (c) 2024 Phosphor Icons

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"),
to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense,
and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.

*/
export default {
  name: "DashboardApp",
};
</script>

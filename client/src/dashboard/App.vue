<template>
<div class="app">
  <header class="app-header">
      <DashboardLogo />
      <DashboardNavigator />
      <UserProfile />
  </header>
  <div class="app-body">
      <div class="app-body-navigation">
          <NavMenu />
          <NavFooter />
      </div>
      <router-view></router-view> <!-- ✅ 현재 라우트의 컴포넌트가 여기에 렌더링됨 -->
  </div>
</div>
</template>

<script>
import DashboardLogo from "@/dashboard/components/header/DashboardLogo.vue";
import DashboardNavigator from "@/dashboard/components/header/DashboardNavigator.vue";
import UserProfile from "@/dashboard/components/header/UserProfile.vue";
import NavMenu from "@/dashboard/components/navigator/NavMenu.vue";
import NavFooter from "@/dashboard/components/navigator/NavFooter.vue";

export default {
  name: 'DashboardOverview'
  , components: {
      DashboardLogo,
      DashboardNavigator,
      UserProfile,
      NavMenu,
      NavFooter,
  }
}
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
</script>

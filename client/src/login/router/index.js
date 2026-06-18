import { createRouter, createWebHistory } from "vue-router";
import Login from "@/login/componenets/LoginForm.vue";  // 로그인 페이지

const routes = [
  { path: "/", component: Login },
];

const router = createRouter({
  history: createWebHistory(),
  routes,
});


router.beforeEach((to, from, next) => {
  const isAuthenticated = !!localStorage.getItem("access_token");

  if (!isAuthenticated && to.path !== "/login") {
    alert("로그인이 필요합니다.");
    window.location.href = "/login"; // ✅ 강제 리디렉트
  }
  else {
    next();
  }
});


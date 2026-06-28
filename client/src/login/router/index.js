import { createRouter, createWebHistory } from "vue-router";
import Login from "@/login/componenets/LoginForm.vue";  // Login page

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
    alert("Login is required.");
    window.location.href = "/login"; // ✅ Force redirect
  }
  else {
    next();
  }
});


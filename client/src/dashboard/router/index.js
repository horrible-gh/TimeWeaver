import { createRouter, createWebHistory } from "vue-router";
import DashboardForm from "@/dashboard/components/contents/dashboard/DashboardOverview.vue";  // Main page
import ScheduleHistoryForm from "@/dashboard/components/contents/history/ScheduleHistory.vue";  // Main page
import GroupsOverviewForm from "@/dashboard/components/contents/groups/GroupsOverview.vue";  // Main page
import DevicesOverviewForm from "@/dashboard/components/contents/devices/DevicesOverview.vue";  // Main page
import SchedulesOverviewForm from "@/dashboard/components/contents/schedules/SchedulesOverview.vue";  // Main page
import { postRequest } from "@api";

const routes = [
  { path: "/", component: DashboardForm },
  { path: "/schedule-history", component: ScheduleHistoryForm },
  { path: "/groups", component: GroupsOverviewForm },
  { path: "/devices", component: DevicesOverviewForm },
  { path: "/schedules", component: SchedulesOverviewForm },
];

const router = createRouter({
  history: createWebHistory("/dashboard/"),
  routes,
});

router.beforeEach((to, from, next) => {
  const isAuthenticated = !!localStorage.getItem("access_token");
  console.log('[Router] Navigation:', from.fullPath, '→', to.fullPath, 'Auth:', isAuthenticated);

  if (to.path === "/logout") {
    postRequest("/logout");
    localStorage.clear();
    sessionStorage.clear();
    window.location.href = "/login";
  } else if (!isAuthenticated && to.path !== "/login") {
    alert("Login is required.");
    window.location.href = "/login";
  } else {
    next();
  }
});



export default router;

